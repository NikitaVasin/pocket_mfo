package push

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProviderFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name               string
		code               int
		body, want, status string
	}{
		{"http validation", 400, `{"errors":[{"error_type":"invalid_parameter","message":"Missing android content"}]}`, "invalid_parameter: Missing android content", "failed"},
		{"http forbidden", 403, `{"message":"Access denied"}`, "HTTP 403: Access denied", "failed"},
		{"rate limit", 429, `{"errors":["Too many requests"]}`, "Too many requests", "sending"},
		{"unavailable", 503, `<html>secret response</html>`, "HTTP 503", "unknown"},
		{"async validation", 200, `{"transfer":{"id":81,"status":"failed","errors":["Android sender missing","server-only-token"]}}`, "Android sender missing; [скрыто]", "failed"},
		{"async no reason", 200, `{"transfer":{"id":81,"status":"failed"}}`, "не вернула причину", "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := setup(t, true)
			original := x.p.client.Transport
			sends := 0
			x.p.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/push/v1/send-batch" {
					sends++
				}
				if (tc.code != 200 && r.URL.Path == "/push/v1/send-batch") || (tc.code == 200 && strings.HasPrefix(r.URL.Path, "/push/v1/status/")) {
					return &http.Response{StatusCode: tc.code, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
				}
				return original.RoundTrip(r)
			})
			c := x.campaign(t)
			run, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "diagnostic-test"})
			must(t, err)
			must(t, x.p.Process(t.Context()))
			if tc.code == 200 {
				x.due(t)
				must(t, x.p.Process(t.Context()))
			}
			report, err := x.p.Report(x.app, run["id"].(string))
			must(t, err)
			if report["status"] != tc.status || !strings.Contains(report["error"].(string), tc.want) {
				t.Fatalf("unexpected report: %v", report)
			}
			jobs := report["jobs"].([]map[string]any)
			if len(jobs) != 1 || jobs[0]["recipients"] != 1 || jobs[0]["clientTransferId"] == "" || jobs[0]["error"] != report["error"] {
				t.Fatal(jobs)
			}
			if tc.status == "failed" && jobs[0]["nextAttempt"] != "" {
				t.Fatal("terminal job shows retry")
			}
			serialized, _ := json.Marshal(report)
			for _, secret := range []string{"server-only-token", x.device.Secret, x.device.DeviceID, "secret response", `"definition"`, `"openToken"`} {
				if strings.Contains(string(serialized), secret) {
					t.Fatal("report exposes private data")
				}
			}
			if tc.status == "failed" {
				x.due(t)
				must(t, x.p.Process(t.Context()))
				if sends != 1 {
					t.Fatal("failed send retried")
				}
			}
		})
	}
}

func TestRefreshLegacyFailureOnlyReadsProviderStatus(t *testing.T) {
	x := setup(t, true)
	c := x.campaign(t)
	run, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "legacy-failure"})
	must(t, err)
	must(t, x.p.Process(t.Context()))
	jobs, err := x.app.FindAllRecords(jobsCollection)
	must(t, err)
	jobs[0].Set("status", "failed")
	jobs[0].Set("error", "")
	must(t, save(x.app, jobs[0]))
	must(t, x.p.updateRun(run["id"].(string)))
	calls := 0
	x.p.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/push/v1/status/") {
			t.Fatal("refresh attempted to send")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"transfer":{"id":81,"status":"failed","errors":["Sender not configured"]}}`))}, nil
	})
	for _, auth := range []string{"", x.auth} {
		w := x.request("POST", "/api/push/admin/refresh_report", auth, map[string]any{"runId": run["id"]})
		if w.Code < 400 || calls != 0 {
			t.Fatal("unprivileged refresh")
		}
	}
	w := x.request("POST", "/api/push/admin/refresh_report", x.admin, map[string]any{"runId": run["id"]})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Sender not configured") || calls != 1 {
		t.Fatal(w.Code, w.Body.String(), calls)
	}
	report, err := x.p.Report(x.app, run["id"].(string))
	must(t, err)
	if !strings.Contains(report["error"].(string), "Sender not configured") {
		t.Fatal("diagnostic not persisted")
	}
}

func TestDiagnosticBoundsAndRedaction(t *testing.T) {
	value := diagnostic("token\n"+strings.Repeat("я", 4000), "token")
	if strings.Contains(value, "token") || strings.Contains(value, "\n") || len([]rune(value)) > 2001 {
		t.Fatal("unsafe diagnostic")
	}
}

func TestSendOnlyIncludesRecipientPlatforms(t *testing.T) {
	for _, platform := range []string{"android", "ios", "mixed"} {
		t.Run(platform, func(t *testing.T) {
			x := setup(t, true)
			ids := []string{x.device.ID}
			if platform == "ios" {
				ids = []string{x.otherDevice.ID}
			}
			if platform == "mixed" {
				ids = append(ids, x.otherDevice.ID)
			}
			c := x.campaign(t)
			_, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "platform-test", TestDeviceIDs: ids})
			must(t, err)
			must(t, x.p.Process(t.Context()))
			if len(x.sent) != 1 {
				t.Fatal("expected one send")
			}
			batch := x.sent[0]["push_batch_request"].(map[string]any)["batch"].([]any)
			want := 1
			if platform == "mixed" {
				want = 2
			}
			if len(batch) != want {
				t.Fatal(batch)
			}
			seen := map[string]string{}
			for _, item := range batch {
				b := item.(map[string]any)
				messages := b["messages"].(map[string]any)
				if len(messages) != 1 {
					t.Fatal("unrelated platforms included")
				}
				recipients := b["devices"].([]any)[0].(map[string]any)["id_values"].([]any)
				if len(recipients) != 1 {
					t.Fatal("platform recipients mixed")
				}
				for key := range messages {
					seen[key] = recipients[0].(string)
				}
			}
			if platform != "ios" && seen["android"] != x.device.DeviceID {
				t.Fatal(seen)
			}
			if platform != "android" && seen["iOS"] != x.otherDevice.DeviceID {
				t.Fatal(seen)
			}
		})
	}
}
