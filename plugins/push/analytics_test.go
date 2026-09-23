package push

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func analyticsRun(t *testing.T, x *fixture) string {
	t.Helper()
	c := x.campaign(t)
	run, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "analytics-run"})
	must(t, err)
	must(t, x.p.Process(t.Context()))
	return run["id"].(string)
}

func statResponse(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

func TestAnalyticsUsesOnlyAttributedAppMetricaAggregates(t *testing.T) {
	x := setup(t, true)
	id := analyticsRun(t, x)
	record, err := x.app.FindRecordById(RunsCollection, id)
	must(t, err)
	var d runDefinition
	must(t, decodeRecord(record, &d))
	// Neither a second real group nor a test group may leak into run scope.
	for i, group := range []int64{702, 999} {
		c, err := x.app.FindCollectionByNameOrId(RunsCollection)
		must(t, err)
		r := core.NewRecord(c)
		next := d
		next.GroupID = group
		next.Test = i == 1
		r.Set("campaignId", record.GetString("campaignId"))
		r.Set("definition", next)
		r.Set("requestKey", secret())
		r.Set("status", "sent")
		r.Set("name", "other run")
		must(t, save(x.app, r))
	}
	calls := 0
	scope := "run"
	x.p.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		filter := q.Get("filters")
		if r.Method != "GET" || r.URL.Host != "api.appmetrica.yandex.com" || r.URL.Path != "/stat/v1/data" || r.Header.Get("Authorization") != "OAuth server-only-token" {
			t.Fatal("wrong analytics request")
		}
		if q.Get("ids") != "123" || q.Get("accuracy") != "full" || q.Get("timezone") != "+00:00" {
			t.Fatal(q)
		}
		if strings.Contains(filter, "999") {
			t.Fatal("test group included")
		}
		if strings.HasPrefix(q.Get("metrics"), "ym:pc:") {
			if !strings.Contains(filter, "701") || strings.Contains(filter, "702") != (scope == "campaign") {
				t.Fatal(filter)
			}
			return statResponse(`{"totals":[10,8,7,6],"data":[],"total_rows":0,"sampled":false,"data_lag":17}`, 200), nil
		}
		key, value := "runId", id
		if scope == "campaign" {
			key, value = "campaignId", record.GetString("campaignId")
		}
		if !strings.Contains(filter, "clickData.push."+key+"'}=='"+value+"'") {
			t.Fatal(filter)
		}
		if strings.HasPrefix(q.Get("metrics"), "ym:ce2:") {
			if q.Get("metrics") != "ym:ce2:uniqParamValues{'clickData.clickId'}" || q.Get("include_undefined") != "true" {
				t.Fatal(q)
			}
			return statResponse(`{"totals":[5],"data":[{"dimensions":[{"name":null}],"metrics":[5]},{"dimensions":[{"name":"lead"}],"metrics":[4]},{"dimensions":[{"name":"approved"}],"metrics":[2]}],"total_rows":3,"sampled":true,"data_lag":0}`, 200), nil
		}
		if !strings.Contains(filter, "one_time_purchase") || !strings.Contains(filter, "conversion.status'}=='approved'") || !strings.Contains(filter, "conversion.status'}=='hold'") || q.Get("metrics") != "ym:r2:inappRevenueRUB,ym:r2:purchases" {
			t.Fatal(q)
		}
		return statResponse(`{"totals":[1250.75,4],"data":[{"dimensions":[{"name":"approved"}],"metrics":[1000.25,3]},{"dimensions":[{"name":"hold"}],"metrics":[250.5,1]}],"total_rows":2,"sampled":false,"data_lag":0}`, 200), nil
	})
	for _, auth := range []string{"", x.auth} {
		w := x.request("POST", "/api/push/admin/analytics", auth, map[string]string{"runId": id})
		if w.Code < 400 || calls != 0 {
			t.Fatal("unprivileged analytics request")
		}
	}
	w := x.request("POST", "/api/push/admin/analytics", x.admin, map[string]string{"runId": id})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out AnalyticsReport
	must(t, json.Unmarshal(w.Body.Bytes(), &out))
	if out.Source != "AppMetrica" || out.Currency != "RUB" || out.Push.Values["opened"] != 6 || out.Events.Values["lead"] != 4 || !out.Events.Sampled || out.Revenue.Values["approved"] != 1000.25 || out.Revenue.Values["hold"] != 250.5 || out.Revenue.Values["approvedEvents"] != 3 {
		t.Fatalf("unexpected metrics: %+v", out)
	}
	if out.NextRefreshAt.Sub(out.FetchedAt) != 5*time.Minute || out.Push.DataLagSeconds != 17 {
		t.Fatal("missing freshness")
	}
	if strings.Contains(w.Body.String(), "server-only-token") {
		t.Fatal("token exposed")
	}
	cached, err := x.p.Analytics(t.Context(), x.app, AnalyticsInput{RunID: id})
	must(t, err)
	if calls != 3 || !cached.FetchedAt.Equal(out.FetchedAt) {
		t.Fatal("cache missed")
	}
	scope = "campaign"
	all, err := x.p.Analytics(t.Context(), x.app, AnalyticsInput{RunID: id, Scope: scope})
	must(t, err)
	if calls != 6 || all.Scope != "campaign" || all.Revenue.Values["approved"] != 1000.25 {
		t.Fatal("campaign report failed")
	}
}

func TestAnalyticsFailuresNeverBecomeZeros(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"forbidden", `{"message":"server-only-token"}`, 403},
		{"quota", `{}`, 429},
		{"invalid", `not json`, 200},
		{"null metric", `{"totals":[null],"data":[],"total_rows":0,"sampled":false}`, 200},
		{"truncated", `{"totals":[5],"data":[],"total_rows":101,"sampled":false}`, 200},
		{"missing metadata", `{"totals":[0],"data":[]}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := setup(t, true)
			id := analyticsRun(t, x)
			x.p.client.Transport = transport(func(r *http.Request) (*http.Response, error) { return statResponse(tc.body, tc.status), nil })
			out, err := x.p.Analytics(t.Context(), x.app, AnalyticsInput{RunID: id})
			must(t, err)
			for _, section := range []AnalyticsSection{out.Push, out.Events, out.Revenue} {
				if section.Status != "unavailable" || section.Values != nil || section.Error == "" || strings.Contains(section.Error, "server-only-token") {
					t.Fatalf("unsafe partial metrics: %+v", section)
				}
			}
			if out.NextRefreshAt.Sub(out.FetchedAt) != 30*time.Second {
				t.Fatal("wrong retry cache")
			}
			after, err := x.app.FindRecordById(RunsCollection, id)
			must(t, err)
			if after.GetString("status") == "failed" {
				t.Fatal("analytics changed send status")
			}
		})
	}
}

func TestAnalyticsTestRunSkipsBusinessAndTokenChangeInvalidatesCache(t *testing.T) {
	x := setup(t, true)
	id := analyticsRun(t, x)
	run, err := x.app.FindRecordById(RunsCollection, id)
	must(t, err)
	var d runDefinition
	must(t, decodeRecord(run, &d))
	d.Test = true
	run.Set("definition", d)
	must(t, save(x.app, run))
	calls := 0
	x.p.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if !strings.HasPrefix(r.URL.Query().Get("metrics"), "ym:pc:") {
			t.Fatal("test has business metrics")
		}
		return statResponse(`{"totals":[1,1,1,1],"data":[],"total_rows":0,"sampled":false,"data_lag":0}`, 200), nil
	})
	out, err := x.p.Analytics(t.Context(), x.app, AnalyticsInput{RunID: id})
	must(t, err)
	if !out.Test || out.Events.Status != "not_applicable" || out.Revenue.Values != nil || calls != 1 {
		t.Fatal(out)
	}
	cfg, err := Load(x.app)
	must(t, err)
	cfg.OAuthToken = "rotated-token"
	_, err = Configure(x.app, cfg)
	must(t, err)
	_, err = x.p.Analytics(t.Context(), x.app, AnalyticsInput{RunID: id})
	must(t, err)
	if calls != 2 {
		t.Fatal("old token cache reused")
	}
	_, err = x.p.Analytics(t.Context(), x.app, AnalyticsInput{RunID: id, Scope: "invalid"})
	if err == nil || calls != 2 {
		t.Fatal("invalid scope accepted")
	}
}
