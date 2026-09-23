package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/mcp"
	"github.com/NikitaVasin/pocket_mfo/plugins/partnerlinks"
	"github.com/NikitaVasin/pocket_mfo/plugins/schemalock"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	app                 *pocketbase.PocketBase
	p                   *Plugin
	handler             http.Handler
	user, other         *core.Record
	auth, admin         string
	device, otherDevice DeviceInput
	sent                []map[string]any
	ambiguous           bool
	mu                  sync.Mutex
}

func setup(t *testing.T, locked bool) *fixture {
	t.Helper()
	x := &fixture{}
	x.app = pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	variants.Register(x.app)
	dynamiclink.Register(x.app)
	partnerlinks.Register(x.app, partnerlinks.Options{AuthCollections: []string{"members"}, Attribution: Attribution})
	bridge := mcp.Register(x.app, mcp.Options{})
	x.p = register(x.app, Options{AuthCollections: []string{"members"}, MCP: bridge}, &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "OAuth server-only-token" {
			t.Errorf("bad authorization")
		}
		if r.URL.Host != "push.api.appmetrica.yandex.net" {
			t.Errorf("bad destination")
		}
		body := `{}`
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/push/v1/management/groups"):
			body = `{"groups":[]}`
		case r.Method == "POST" && r.URL.Path == "/push/v1/management/groups":
			body = `{"group":{"id":701}}`
		case r.Method == "POST" && r.URL.Path == "/push/v1/send-batch":
			var v map[string]any
			if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
				t.Error(err)
			}
			x.mu.Lock()
			x.sent = append(x.sent, v)
			ambiguous := x.ambiguous
			x.mu.Unlock()
			if ambiguous {
				return nil, fmt.Errorf("connection reset")
			}
			body = `{"push_response":{"transfer_id":81}}`
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/push/v1/status/"):
			body = `{"transfer":{"id":81,"status":"sent"}}`
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	if locked {
		schemalock.Register(x.app)
	}
	must(t, x.app.Bootstrap())
	t.Cleanup(func() { x.app.Cron().Stop(); _ = x.app.ClearBootstrap() })
	c := core.NewAuthCollection("members")
	c.Fields.Add(&core.TextField{Name: "tier"})
	must(t, x.app.Save(c))
	for i, dest := range []**core.Record{&x.user, &x.other} {
		r := core.NewRecord(c)
		r.SetEmail(fmt.Sprintf("u%d@example.test", i))
		r.SetPassword("password-12345")
		r.Set("tier", []string{"premium", "free"}[i])
		must(t, x.app.Save(r))
		*dest = r
	}
	var err error
	x.auth, err = x.user.NewAuthToken()
	must(t, err)
	ac, err := x.app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	must(t, err)
	a := core.NewRecord(ac)
	a.SetEmail("a@example.test")
	a.SetPassword("password-12345")
	must(t, x.app.Save(a))
	x.admin, err = a.NewAuthToken()
	must(t, err)
	router, err := apis.NewRouter(x.app)
	must(t, err)
	must(t, x.app.OnServe().Trigger(&core.ServeEvent{App: x.app, Router: router}, func(*core.ServeEvent) error { return nil }))
	x.handler, err = router.BuildMux()
	must(t, err)
	_, err = Configure(x.app, Config{ApplicationID: 123, OAuthToken: "server-only-token", SendRate: 1000})
	must(t, err)
	x.device = DeviceInput{ID: "device000000001", DeviceID: "12345678901234567890", Secret: secret(), Platform: "android", Language: "ru", AppVersion: "1.0", Enabled: true, NotificationPermission: "authorized"}
	_, err = x.p.RegisterDevice(x.app, x.user, x.device)
	must(t, err)
	x.otherDevice = DeviceInput{ID: "device000000002", DeviceID: "999", Secret: secret(), Platform: "ios", Language: "en", Enabled: true, NotificationPermission: "authorized"}
	_, err = x.p.RegisterDevice(x.app, x.other, x.otherDevice)
	must(t, err)
	return x
}
func (x *fixture) request(method, path, auth string, value any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(value)
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	x.handler.ServeHTTP(w, r)
	return w
}
func (x *fixture) campaign(t *testing.T) Campaign {
	a, err := x.p.SaveAudience(x.app, Audience{Name: "Premium Android", AuthCollection: "members", Condition: &Condition{Kind: "all", Children: []Condition{{Kind: "field", Source: "user", Field: "tier", Op: "eq", Value: "premium"}, {Kind: "field", Source: "device", Field: "platform", Op: "eq", Value: "android"}}}})
	must(t, err)
	c, err := x.p.SaveCampaign(x.app, Campaign{Name: "Test", AudienceIDs: []string{a.ID}, Message: Message{Title: "Title", Text: "Hello", Action: "route", Target: "/orders"}})
	must(t, err)
	return c
}
func (x *fixture) due(t *testing.T) {
	rr, err := x.app.FindAllRecords(jobsCollection)
	must(t, err)
	for _, r := range rr {
		r.Set("nextAttempt", "")
		must(t, save(x.app, r))
	}
}

func TestCampaignDeliveryAttributionAndPermissions(t *testing.T) {
	for _, locked := range []bool{false, true} {
		t.Run(fmt.Sprint(locked), func(t *testing.T) {
			x := setup(t, locked)
			c := x.campaign(t)
			preview, err := x.p.Preview(x.app, c.ID)
			must(t, err)
			if preview.Devices != 1 || preview.Users != 1 {
				t.Fatal(preview)
			}
			input := Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "launch-0001"}
			run, err := x.p.Launch(x.app, input)
			must(t, err)
			same, err := x.p.Launch(x.app, input)
			must(t, err)
			if same["id"] != run["id"] {
				t.Fatal("duplicate launch")
			}
			wrong := input
			wrong.Version++
			if _, err = x.p.Launch(x.app, wrong); err == nil {
				t.Fatal("idempotency conflict accepted")
			}
			if len(x.sent) != 0 {
				t.Fatal("launch performed network I/O")
			}
			must(t, x.p.Process(t.Context()))
			if len(x.sent) != 1 {
				t.Fatal("missing send")
			}
			batch := x.sent[0]["push_batch_request"].(map[string]any)["batch"].([]any)[0].(map[string]any)
			ids := batch["devices"].([]any)[0].(map[string]any)["id_values"].([]any)
			if len(ids) != 1 || ids[0] != x.device.DeviceID {
				t.Fatal("wrong recipients", ids)
			}
			for _, auth := range []string{"", x.auth} {
				w := x.request("POST", "/api/push/admin/launch", auth, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "forbidden-0001"})
				if w.Code < 400 {
					t.Fatal("unauthorized launch")
				}
			}
			for _, collection := range serviceCollections {
				w := x.request("POST", "/api/collections/"+collection+"/records", x.admin, map[string]any{"name": "bad"})
				if w.Code < 400 {
					t.Fatal("service write", collection)
				}
			}
			state := x.request("GET", "/api/push/admin/state", x.admin, nil)
			if state.Code != 200 || strings.Contains(state.Body.String(), "server-only-token") || strings.Contains(state.Body.String(), x.device.Secret) {
				t.Fatal("config leaked or state failed", state.Body)
			}
			id := run["id"].(string)
			record, err := x.app.FindRecordById(RunsCollection, id)
			must(t, err)
			var def runDefinition
			must(t, decodeRecord(record, &def))
			bad := OpenInput{DeviceID: x.otherDevice.ID, Secret: x.otherDevice.Secret, RunID: id, Token: def.OpenToken}
			if err = x.p.TrackOpen(x.app, x.other, bad); err == nil {
				t.Fatal("nonrecipient open accepted")
			}
			good := OpenInput{DeviceID: x.device.ID, Secret: x.device.Secret, RunID: id, Token: def.OpenToken}
			must(t, x.p.TrackOpen(x.app, x.user, good))
			must(t, x.p.TrackOpen(x.app, x.user, good))
			attr, err := Attribution(x.app, x.user, "anylink00000001")
			must(t, err)
			if attr["runId"] != id {
				t.Fatal("attribution missing")
			}
			x.due(t)
			must(t, x.p.Process(t.Context()))
			report, err := x.p.Report(x.app, id)
			must(t, err)
			if report["status"] != "sent" || report["opens"] != 1 || len(x.sent) != 1 {
				t.Fatal(report)
			}
			// Raw records and realtime enrichment cannot disclose launch credentials.
			view := x.request("GET", "/api/collections/push_runs/records/"+id, x.admin, nil)
			if strings.Contains(view.Body.String(), def.OpenToken) {
				t.Fatal("open token leaked")
			}
			// Partner Links snapshots attribution in its existing conversion record.
			config := partnerlinks.DefaultConfig()
			config.BaseURL = "https://links.example"
			config.ApplicationID = 123
			config.PostAPIKey = "post-key"
			config.Providers = []partnerlinks.Provider{{ID: "test", Name: "Test", URLTemplate: "{url}?subid={clickData}", Secret: "long-provider-secret", SecretLocation: "query", SecretName: "secret", RevenueStatus: "approved", Fields: partnerlinks.PostbackFields{Token: "subid", Status: "status", LeadID: "lead"}, Statuses: map[string]string{"ok": "approved"}}}
			_, err = partnerlinks.Configure(x.app, config)
			must(t, err)
			links, err := x.app.FindCollectionByNameOrId(partnerlinks.LinksCollection)
			must(t, err)
			link := core.NewRecord(links)
			link.Set("name", "Offer")
			link.Set("provider", "test")
			link.Set("active", true)
			link.Set("link", map[string]any{"url": "https://partner.example"})
			must(t, x.app.Save(link))
			w := x.request("POST", "/api/partnerlinks/links/"+link.Id+"/resolve", x.auth, map[string]any{})
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			conversions, err := x.app.FindAllRecords("conversations")
			must(t, err)
			if len(conversions) != 1 || !strings.Contains(conversions[0].GetString("clickData"), `"runId":"`+id+`"`) {
				t.Fatal("source not preserved")
			}
		})
	}
}

func TestLostResponseDoesNotResendAndOwnershipChanges(t *testing.T) {
	x := setup(t, true)
	c := x.campaign(t)
	x.ambiguous = true
	run, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "ambiguous-1"})
	must(t, err)
	must(t, x.p.Process(t.Context()))
	report, err := x.p.Report(x.app, run["id"].(string))
	must(t, err)
	if report["status"] != "unknown" {
		t.Fatal(report)
	}
	x.due(t)
	must(t, x.p.Process(t.Context()))
	if len(x.sent) != 1 {
		t.Fatal("blind resend")
	}
	// Stolen ID without the installation secret cannot rebind a device.
	stolen := x.device
	stolen.Secret = secret()
	if _, err = x.p.RegisterDevice(x.app, x.other, stolen); err == nil {
		t.Fatal("stolen device")
	}
	_, err = x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "rebound-0001"})
	must(t, err)
	_, err = x.p.RegisterDevice(x.app, x.other, x.device)
	must(t, err)
	must(t, x.p.Process(t.Context()))
	if len(x.sent) != 1 {
		t.Fatal("sent to previous account")
	}
}

func TestAudienceTreeAndAtomicFailures(t *testing.T) {
	x := setup(t, false)
	for _, node := range []Condition{{Kind: "field", Source: "user", Field: "tokenKey", Op: "eq", Value: "bad"}, {Kind: "field", Source: "device", Field: "secretHash", Op: "eq", Value: "bad"}, {Kind: "not", Children: []Condition{}}, {Kind: "conversion", Children: []Condition{{Kind: "field", Source: "conversion", Field: "tokenHash", Op: "eq", Value: "bad"}}}} {
		if _, err := x.p.SaveAudience(x.app, Audience{Name: "bad", AuthCollection: "members", Condition: &node}); err == nil {
			t.Fatal("invalid condition accepted")
		}
	}
	a, err := x.p.SaveAudience(x.app, Audience{Name: "Not iOS", AuthCollection: "members", Condition: &Condition{Kind: "not", Children: []Condition{{Kind: "field", Source: "device", Field: "platform", Op: "eq", Value: "ios"}}}})
	must(t, err)
	count, err := x.p.AudiencePreview(x.app, a)
	must(t, err)
	if count.Devices != 1 {
		t.Fatal(count)
	}
	stale := a
	a.Name = "Changed"
	_, err = x.p.SaveAudience(x.app, a)
	must(t, err)
	stale.Name = "Stale"
	if _, err = x.p.SaveAudience(x.app, stale); err == nil {
		t.Fatal("stale accepted")
	}
	c := x.campaign(t)
	before, err := x.app.FindAllRecords(RunsCollection)
	must(t, err)
	x.app.OnRecordCreate(jobsCollection).BindFunc(func(e *core.RecordEvent) error { return fmt.Errorf("forced failure") })
	if _, err = x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "rollback-0001"}); err == nil {
		t.Fatal("expected rollback")
	}
	after, err := x.app.FindAllRecords(RunsCollection)
	must(t, err)
	if len(before) != len(after) {
		t.Fatal("run survived rollback")
	}
	d, err := x.app.FindRecordById(DevicesCollection, x.device.ID)
	must(t, err)
	if !d.GetDateTime("lastSent").IsZero() {
		t.Fatal("cooldown survived rollback")
	}
}

func TestScheduledCampaignFreezesDefinitionAndCanCancel(t *testing.T) {
	x := setup(t, true)
	c := x.campaign(t)
	in := Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "schedule-001", ScheduledAt: time.Now().Add(time.Hour).Format(time.RFC3339)}
	run, err := x.p.Launch(x.app, in)
	must(t, err)
	if run["status"] != "scheduled" {
		t.Fatal(run)
	}
	must(t, x.p.Process(context.Background()))
	if len(x.sent) != 0 {
		t.Fatal("sent early")
	}
	must(t, x.p.Cancel(x.app, run["id"].(string)))
	must(t, x.p.Process(t.Context()))
	if len(x.sent) != 0 {
		t.Fatal("sent cancelled")
	}
	in.IdempotencyKey = "schedule-002"
	run, err = x.p.Launch(x.app, in)
	must(t, err)
	c.Message.Text = "Changed"
	_, err = x.p.SaveCampaign(x.app, c)
	must(t, err)
	r, err := x.app.FindRecordById(RunsCollection, run["id"].(string))
	must(t, err)
	r.Set("scheduledAt", types.NowDateTime())
	must(t, save(x.app, r))
	must(t, x.p.Process(t.Context()))
	body, _ := json.Marshal(x.sent)
	if strings.Contains(string(body), "Changed") || !strings.Contains(string(body), "Hello") {
		t.Fatal("definition changed")
	}
}

func TestDeviceWithoutCompletedRegistrationCanBeDisabled(t *testing.T) {
	x := setup(t, true)
	must(t, x.p.DisableDevice(x.app, x.user, "missing00000001", secret()))
	if err := x.p.DisableDevice(x.app, x.other, x.device.ID, x.device.Secret); err == nil {
		t.Fatal("another user disabled a registered installation")
	}
	a := Audience{Name: "Recent", AuthCollection: "members", Condition: &Condition{Kind: "field", Source: "device", Field: "lastSeen", Op: "withinHours", Value: 24}}
	result, err := x.p.AudiencePreview(x.app, a)
	must(t, err)
	if result.Devices != 2 {
		t.Fatalf("Go integer duration: %+v", result)
	}
}

func TestTerminationCancelsAndDrainsDispatcher(t *testing.T) {
	x := setup(t, true)
	c := x.campaign(t)
	_, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "shutdown-test"})
	must(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := x.p.Process(ctx); err != context.Canceled {
		t.Fatalf("cancelled worker: %v", err)
	}
	if len(x.sent) != 0 {
		t.Fatal("cancelled worker sent messages")
	}
	started, finished := make(chan struct{}), make(chan struct{})
	x.p.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	for _, job := range x.app.Cron().Jobs() {
		if job.Id() == "push_dispatch" {
			go func() { job.Run(); close(finished) }()
		}
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatcher did not start")
	}
	must(t, x.app.OnTerminate().Trigger(&core.TerminateEvent{App: x.app}, func(*core.TerminateEvent) error { return nil }))
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatcher did not stop")
	}
}

func TestConcurrentLaunchReservesOnce(t *testing.T) {
	x := setup(t, true)
	c := x.campaign(t)
	input := Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "parallel-start"}
	type outcome struct {
		run map[string]any
		err error
	}
	results := make(chan outcome, 4)
	for range 4 {
		go func() { r, err := x.p.Launch(x.app, input); results <- outcome{r, err} }()
	}
	id := ""
	for range 4 {
		r := <-results
		must(t, r.err)
		if id != "" && id != r.run["id"] {
			t.Fatal("duplicate launch")
		}
		id = r.run["id"].(string)
	}
	runs, err := x.app.FindAllRecords(RunsCollection)
	must(t, err)
	jobs, err := x.app.FindAllRecords(jobsCollection)
	must(t, err)
	if len(runs) != 1 || len(jobs) != 1 {
		t.Fatal("duplicate persisted work")
	}
}

func TestDeletedUserIsRemovedBeforeDispatch(t *testing.T) {
	x := setup(t, true)
	c := x.campaign(t)
	_, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "delete-before-send"})
	must(t, err)
	must(t, x.app.Delete(x.user))
	must(t, x.p.Process(t.Context()))
	if len(x.sent) != 0 {
		t.Fatal("sent to a deleted account")
	}
}

func TestConversionAudienceUsesOneRowAndCorrectOwner(t *testing.T) {
	x := setup(t, true)
	c, err := x.app.FindCollectionByNameOrId("conversations")
	must(t, err)
	for i, values := range [][3]string{{x.user.Id, "approved", "USD"}, {x.user.Id, "rejected", "RUB"}, {x.other.Id, "approved", "RUB"}} {
		r := core.NewRecord(c)
		r.Set("user", map[string]string{"collectionId": x.user.Collection().Id, "recordId": values[0]})
		r.Set("userId", values[0])
		r.Set("authCollection", x.user.Collection().Id)
		r.Set("provider", "test")
		r.Set("clickId", fmt.Sprint(i))
		r.Set("status", values[1])
		r.Set("currency", values[2])
		must(t, x.app.Save(r))
	}
	a := Audience{Name: "Approved RUB", AuthCollection: "members", Condition: &Condition{Kind: "conversion", Children: []Condition{{Kind: "all", Children: []Condition{{Kind: "field", Source: "conversion", Field: "status", Op: "eq", Value: "approved"}, {Kind: "field", Source: "conversion", Field: "currency", Op: "eq", Value: "RUB"}}}}}}
	p, err := x.p.AudiencePreview(x.app, a)
	must(t, err)
	if p.Users != 1 || p.Devices != 1 {
		t.Fatalf("same-row selection: %+v", p)
	}
	a.UserIDs = []string{x.user.Id}
	p, err = x.p.AudiencePreview(x.app, a)
	must(t, err)
	if p.Users != 0 {
		t.Fatal("mixed rows or another user's conversion")
	}
}

func TestAudienceCoverageCountsDistinctRecipientsAgainstAllUsers(t *testing.T) {
	x := setup(t, true)
	c := x.user.Collection()
	noDevice := core.NewRecord(c)
	noDevice.SetEmail("no-device@example.test")
	noDevice.SetPassword("password-12345")
	must(t, x.app.Save(noDevice))
	extra := x.device
	extra.ID = "device000000003"
	extra.DeviceID = "555"
	_, err := x.p.RegisterDevice(x.app, x.user, extra)
	must(t, err)
	audience := Audience{AuthCollection: "members", Condition: &Condition{Kind: "field", Source: "user", Field: "tier", Op: "eq", Value: "premium"}}
	result, err := x.p.AudiencePreview(x.app, audience)
	must(t, err)
	if result.Users != 1 || result.Devices != 2 || result.TotalUsers == nil || *result.TotalUsers != 3 {
		t.Fatalf("coverage: %+v", result)
	}
	for _, user := range []*core.Record{x.user, x.other, noDevice} {
		must(t, x.app.Delete(user))
	}
	result, err = x.p.AudiencePreview(x.app, audience)
	must(t, err)
	if result.Users != 0 || result.Devices != 0 || result.TotalUsers == nil || *result.TotalUsers != 0 {
		t.Fatalf("empty coverage: %+v", result)
	}
}
