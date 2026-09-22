package partnerlinks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/schemalock"
	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/types"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	app           *pocketbase.PocketBase
	handler       http.Handler
	opts          Options
	config        *Config
	user, link    *core.Record
	auth, admin   string
	mu            sync.Mutex
	events        []url.Values
	loggedURLs    []string
	status        int
	revenueStatus int
	delay         time.Duration
}

func testProvider() Provider {
	return Provider{ID: "test", Name: "Test", URLTemplate: "{url}?subid={clickData}", Secret: "test-provider-secret-123", SecretLocation: "query", SecretName: "secret", Fields: PostbackFields{Token: "subid", Status: "status", LeadID: "lead_id", EventID: "event_id", Timestamp: "timestamp", Amount: "amount", Currency: "currency"}, Statuses: map[string]string{"new": "lead", "yes": "approved", "wait": "hold", "no": "rejected"}, ExtraFields: map[string]string{"offer": "offer_id"}}
}
func setup(t *testing.T, locked bool) *fixture { return setupOptions(t, locked, false) }
func setupOptions(t *testing.T, locked, collect bool, configuration ...Options) *fixture {
	t.Helper()
	x := &fixture{status: 200}
	x.opts = Options{AuthCollections: []string{"members"}, VariantCollections: []string{"offers"}}
	if len(configuration) > 0 {
		x.opts.Managed = configuration[0].Managed
		x.opts.LockAdminConfig = configuration[0].LockAdminConfig
	}
	x.app = pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		x.events = append(x.events, r.URL.Query())
		status, delay := x.status, x.delay
		if r.URL.Path == "/logs/v1/import/revenue" && x.revenueStatus != 0 {
			status = x.revenueStatus
		}
		x.mu.Unlock()
		if delay > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(delay):
			}
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.appmetrica.yandex.ru" || (r.URL.Path != "/logs/v1/import/events" && r.URL.Path != "/logs/v1/import/revenue") || r.Method != "POST" {
			return nil, fmt.Errorf("unexpected request")
		}
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = target.Scheme
		u.Host = target.Host
		copy.URL = &u
		return http.DefaultTransport.RoundTrip(copy)
	}), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	variants.Register(x.app)
	singleton.Register(x.app)
	dynamiclink.Register(x.app)
	register(x.app, x.opts, client)
	register(x.app, x.opts, client)
	if locked {
		schemalock.Register(x.app)
	}
	must(t, x.app.Bootstrap())
	t.Cleanup(func() { _ = x.app.ClearBootstrap() })
	x.app.Settings().Batch.Enabled = true
	must(t, x.app.Save(x.app.Settings()))
	c := core.NewAuthCollection("members")
	c.Fields.Add(&core.TextField{Name: "appmetrica_profile_id"})
	must(t, x.app.Save(c))
	x.user = core.NewRecord(c)
	x.user.Set("appmetrica_profile_id", "profile-42")
	x.user.SetEmail("user@example.test")
	x.user.SetPassword("test-password-123")
	must(t, x.app.Save(x.user))
	var err error
	x.auth, err = x.user.NewAuthToken()
	must(t, err)
	offers := core.NewBaseCollection("offers")
	offers.ViewRule = types.Pointer("")
	must(t, x.app.Save(offers))
	_, err = variants.Publish(x.app, variants.Config{Collection: offers.Id, AuthCollection: c.Id, Variables: true, Experiments: true, Default: variants.Variant{Key: "default", Experiments: []variants.Experiment{{Key: "layout", Active: true, Groups: []variants.Group{{Key: "a", From: 1, To: 5000}, {Key: "b", From: 5001, To: 10000}}}}}})
	must(t, err)
	a, err := x.app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	must(t, err)
	admin := core.NewRecord(a)
	admin.SetEmail("admin@example.test")
	admin.SetPassword("admin-password-123")
	must(t, x.app.Save(admin))
	x.admin, err = admin.NewAuthToken()
	must(t, err)
	cfg := DefaultConfig()
	cfg.BaseURL = "https://links.example"
	cfg.ApplicationID = 1234
	cfg.PostAPIKey = "fake-post-api-key"
	cfg.Providers = []Provider{testProvider()}
	overlayManaged(&cfg, managed(x.app))
	x.config, err = Configure(x.app, cfg)
	must(t, err)
	links, err := x.app.FindCollectionByNameOrId(LinksCollection)
	must(t, err)
	x.link = core.NewRecord(links)
	x.link.Set("name", "Offer")
	x.link.Set("provider", "test")
	x.link.Set("link", map[string]any{"url": "https://partner.example/apply?existing=a%26b"})
	x.link.Set("active", true)
	must(t, x.app.Save(x.link))
	router, err := apis.NewRouter(x.app)
	must(t, err)
	router.Bind(&hook.Handler[*core.RequestEvent]{Priority: apis.DefaultActivityLoggerMiddlewarePriority - 1, Func: func(r *core.RequestEvent) error {
		err := r.Next()
		x.mu.Lock()
		x.loggedURLs = append(x.loggedURLs, r.Request.URL.RequestURI())
		x.mu.Unlock()
		return err
	}})
	must(t, x.app.OnServe().Trigger(&core.ServeEvent{App: x.app, Router: router}, func(*core.ServeEvent) error { return nil }))
	x.handler, err = router.BuildMux()
	must(t, err)
	return x
}
func (x *fixture) request(method, path, auth, body, contentType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	x.handler.ServeHTTP(w, r)
	return w
}
func (x *fixture) issue(t *testing.T) ResolveResponse {
	t.Helper()
	w := x.request("POST", "/api/partnerlinks/links/"+x.link.Id+"/resolve", x.auth, `{}`, "application/json")
	if w.Code != 200 {
		t.Fatalf("issue %d: %s", w.Code, w.Body)
	}
	var result ResolveResponse
	must(t, json.Unmarshal(w.Body.Bytes(), &result))
	return result
}
func tokenFrom(r ResolveResponse) string { return r.Link.URL[strings.LastIndex(r.Link.URL, "/")+1:] }
func (x *fixture) eventCount() int       { x.mu.Lock(); defer x.mu.Unlock(); return len(x.events) }

func TestFlowAndFormats(t *testing.T) {
	x := setup(t, true)
	issued := x.issue(t)
	token := tokenFrom(issued)
	if x.eventCount() != 0 || issued.Link.Mode != "appView" || !issued.Link.SaveCooke || !issued.Link.ShowLoader {
		t.Fatal("issuance must not track or change opening defaults")
	}
	data, err := readClick(x.app, token)
	must(t, err)
	if data.UserID != x.user.Id || data.ApplicationID != 1234 {
		t.Fatalf("wrong click data: %+v", data)
	}
	path := "/api/partnerlinks/r/" + token
	for i := 0; i < 2; i++ {
		w := x.request("GET", path, "", "", "")
		if w.Code != 302 {
			t.Fatalf("redirect %d %s", w.Code, w.Body)
		}
		u, err := url.Parse(w.Header().Get("Location"))
		must(t, err)
		if u.Query().Get("subid") != token || u.Query().Get("existing") != "a&b" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("incorrect redirect")
		}
	}
	if x.eventCount() != 2 {
		t.Fatal("no deduplication is intended")
	}
	before := x.eventCount()
	x.request("HEAD", path, "", "", "")
	if x.eventCount() != before {
		t.Fatal("HEAD must not track")
	}
	for _, tc := range []struct{ method, format, status, event string }{{"GET", "", "new", "offer_lead"}, {"POST", "application/x-www-form-urlencoded", "yes", "offer_approved"}, {"POST", "application/json", "wait", "offer_hold"}, {"GET", "", "no", "offer_rejected"}} {
		values := url.Values{"subid": {token}, "status": {tc.status}, "lead_id": {"lead-1"}, "event_id": {"evt-1"}, "timestamp": {fmt.Sprint(time.Now().Unix() - 10)}, "amount": {"12.50"}, "currency": {"RUB"}, "offer_id": {"external-offer"}}
		path := "/api/partnerlinks/postbacks/test?secret=" + url.QueryEscape(testProvider().Secret)
		body := ""
		if tc.method == "GET" {
			path += "&" + values.Encode()
		} else if tc.format == "application/json" {
			m := map[string]string{}
			for k, v := range values {
				m[k] = v[0]
			}
			b, _ := json.Marshal(m)
			body = string(b)
		} else {
			body = values.Encode()
		}
		w := x.request(tc.method, path, "", body, tc.format)
		if w.Code != 200 {
			t.Fatalf("postback %d %s", w.Code, w.Body)
		}
		x.mu.Lock()
		event := x.events[len(x.events)-1]
		x.mu.Unlock()
		if event.Get("event_name") != tc.event || event.Get("post_api_key") != "fake-post-api-key" || event.Get("profile_id") != x.user.Id || event.Get("session_type") != "foreground" || event.Get("os_name") != "" {
			t.Fatalf("wrong event %v", event)
		}
		var payload map[string]any
		must(t, json.Unmarshal([]byte(event.Get("event_json")), &payload))
		if payload["conversion"].(map[string]any)["leadId"] != "lead-1" || strings.Contains(event.Get("event_json"), testProvider().Secret) {
			t.Fatal("wrong conversion payload")
		}
	}
	if x.eventCount() != 6 {
		t.Fatal("statuses must not synthesize lead")
	}
}

func readClick(app core.App, token string) (clickData, error) {
	_, data, err := loadConversation(app, token)
	return data, err
}
func TestExpiryTamperingAndLimit(t *testing.T) {
	x := setup(t, false)
	token := tokenFrom(x.issue(t))
	r, data, err := loadConversation(x.app, token)
	must(t, err)
	if len(token) != 43 {
		t.Fatalf("token length %d", len(token))
	}
	for _, bad := range []string{token + "x", token[:42], strings.Repeat("a", 43), "v1.k1.old-encrypted-token", "garbage"} {
		if w := x.request("GET", "/api/partnerlinks/r/"+bad, "", "", ""); w.Code != 400 {
			t.Fatalf("bad token %d", w.Code)
		}
	}
	if x.eventCount() != 0 {
		t.Fatal("bad tokens delivered")
	}
	data.IssuedAt = time.Now().Unix() - 2*86400
	data.OpenUntil = time.Now().Unix() - 1
	r.Set("clickData", data)
	r.Set("clickTimestamp", data.IssuedAt)
	must(t, save(x.app, r))
	if w := x.request("GET", "/api/partnerlinks/r/"+token, "", "", ""); w.Code != 410 {
		t.Fatalf("open expiry %d", w.Code)
	}
	path := "/api/partnerlinks/postbacks/test?secret=" + testProvider().Secret + "&status=yes&subid=" + token
	if w := x.request("GET", path, "", "", ""); w.Code != 200 {
		t.Fatalf("late conversion %d %s", w.Code, w.Body)
	}
	cfg, err := Load(x.app)
	must(t, err)
	cfg.Providers[0].MaxTokenLength = 10
	_, err = Configure(x.app, *cfg)
	must(t, err)
	if w := x.request("POST", "/api/partnerlinks/links/"+x.link.Id+"/resolve", x.auth, `{}`, "application/json"); w.Code != 400 {
		t.Fatal("provider limit ignored")
	}
	rows, err := x.app.FindAllRecords(ConversationsCollection)
	must(t, err)
	if len(rows) != 1 {
		t.Fatal("failed resolve created an orphan conversion")
	}
}

func TestVariantSnapshotAndLinkVisibility(t *testing.T) {
	x := setup(t, true)
	issued := x.issue(t)
	data, err := readClick(x.app, tokenFrom(issued))
	must(t, err)
	if len(data.Experiments) != 1 || data.Experiments[0].Group == "" {
		t.Fatalf("missing snapshot: %+v", data.Experiments)
	}
	cfg, err := variants.Load(x.app, "offers")
	must(t, err)
	cfg.Default.Experiments[0].Groups = []variants.Group{{Key: "c", From: 1, To: 5000}, {Key: "d", From: 5001, To: 10000}}
	_, err = variants.Publish(x.app, *cfg)
	must(t, err)
	newData, err := readClick(x.app, tokenFrom(x.issue(t)))
	must(t, err)
	if newData.Experiments[0].Group == data.Experiments[0].Group {
		t.Fatal("new issuance ignored current experiment")
	}
	w := x.request("GET", "/api/partnerlinks/postbacks/test?secret="+testProvider().Secret+"&status=yes&subid="+tokenFrom(issued), "", "", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	x.mu.Lock()
	payload := x.events[len(x.events)-1].Get("event_json")
	x.mu.Unlock()
	var sent struct {
		ClickData clickData `json:"clickData"`
	}
	must(t, json.Unmarshal([]byte(payload), &sent))
	if sent.ClickData.Experiments[0].Group != data.Experiments[0].Group {
		t.Fatal("conversion recomputed experiments")
	}
	linkCfg, err := variants.Publish(x.app, variants.Config{Collection: LinksCollection, AuthCollection: x.user.Collection().Id, Variables: true, Experiments: true, Default: variants.Variant{Key: "default", Experiments: []variants.Experiment{{Key: "offer", Active: true, Groups: []variants.Group{{Key: "visible", From: 1, To: 5000}, {Key: "other", From: 5001, To: 10000}}}}}})
	must(t, err)
	decision, err := variants.Resolve(x.app, linkCfg, x.user)
	must(t, err)
	issue := "/api/partnerlinks/links/" + x.link.Id + "/resolve"
	if w := x.request("POST", issue, x.auth, `{}`, "application/json"); w.Code != 404 {
		t.Fatalf("hidden variant exposed: %d", w.Code)
	}
	link, err := x.app.FindRecordById(LinksCollection, x.link.Id)
	must(t, err)
	link.Set(variants.SetField, decision.Set)
	must(t, x.app.Save(link))
	x.issue(t)
}

func TestTemplatesAndProviderValidation(t *testing.T) {
	p := testProvider()
	for _, template := range []string{"{url}?subid={clickData}", "{url}&subid={clickData}", "https://tracking.example/go?destination={url}&subid={clickData}"} {
		p.URLTemplate = template
		target, err := renderURL(&p, "https://partner.example/a?x=one%26two#details", "opaque_token")
		must(t, err)
		u, err := url.Parse(target)
		must(t, err)
		if u.Query().Get("subid") != "opaque_token" {
			t.Fatal("incorrect token substitution")
		}
		if strings.HasPrefix(template, "{url}") && u.Query().Get("x") != "one&two" {
			t.Fatal("existing query corrupted")
		}
		if !strings.HasPrefix(template, "{url}") && u.Query().Get("destination") != "https://partner.example/a?x=one%26two#details" {
			t.Fatal("nested URL double encoded")
		}
	}
	for _, template := range []string{"javascript:{clickData}", "https://{clickData}.example/a", "{url}.evil?x={clickData}", "https://example.test/#{clickData}", "{url}?x={unknown}&s={clickData}", "{url}?s=no-token"} {
		p.URLTemplate = template
		if _, err := renderURL(&p, "https://partner.example/a", "opaque_token"); err == nil {
			t.Fatalf("accepted %s", template)
		}
	}
}

func TestFailuresAndAuthorization(t *testing.T) {
	x := setup(t, true)
	token := tokenFrom(x.issue(t))
	base := "/api/partnerlinks/postbacks/test?secret=" + testProvider().Secret + "&subid=" + token
	for _, suffix := range []string{"&status=unknown", "&status=yes&timestamp=1", "&status=yes&timestamp=9999999999", "&status=yes&amount=NaN", "&status=yes&status=no"} {
		w := x.request("GET", base+suffix, "", "", "")
		if w.Code != 400 {
			t.Fatalf("expected 400: %d %s", w.Code, w.Body)
		}
	}
	if w := x.request("GET", strings.Replace(base, testProvider().Secret, "wrong", 1)+"&status=yes", "", "", ""); w.Code != 403 {
		t.Fatal("secret bypass")
	}
	if x.eventCount() != 0 {
		t.Fatal("invalid requests delivered events")
	}
	issue := "/api/partnerlinks/links/" + x.link.Id + "/resolve"
	for _, auth := range []string{"", x.admin} {
		if w := x.request("POST", issue, auth, `{}`, "application/json"); w.Code == 200 {
			t.Fatal("issue auth bypass")
		}
	}
	if w := x.request("POST", issue, x.auth, `{"profileId":"p","userId":"forged"}`, "application/json"); w.Code != 400 {
		t.Fatal("client identity accepted")
	}
	for _, rule := range []*string{nil, types.Pointer("id = 'not-visible'")} {
		c, err := x.app.FindCollectionByNameOrId(LinksCollection)
		must(t, err)
		c.ViewRule = rule
		must(t, x.app.Save(c))
		if w := x.request("POST", issue, x.auth, `{}`, "application/json"); w.Code != 404 {
			t.Fatalf("ViewRule bypass: %d", w.Code)
		}
	}
	x.mu.Lock()
	x.status = 500
	x.mu.Unlock()
	if w := x.request("GET", base+"&status=yes", "", "", ""); w.Code != 502 {
		t.Fatal("must let partner retry failed delivery")
	}
	if w := x.request("GET", "/api/partnerlinks/r/"+token, "", "", ""); w.Code != 302 {
		t.Fatal("delivery failure must not block redirect")
	}
	x.mu.Lock()
	x.delay = 3 * time.Second
	x.mu.Unlock()
	started := time.Now()
	if w := x.request("GET", "/api/partnerlinks/r/"+token, "", "", ""); w.Code != 302 {
		t.Fatal("timeout blocked redirect")
	}
	if time.Since(started) > 2800*time.Millisecond {
		t.Fatal("redirect waited too long")
	}
}

func TestConfigProtectionAndRollback(t *testing.T) {
	x := setup(t, true)
	for _, auth := range []string{"", x.auth} {
		w := x.request("GET", "/api/partnerlinks/admin/config", auth, "", "")
		if w.Code == 200 {
			t.Fatal("settings exposed")
		}
	}
	w := x.request("GET", "/api/partnerlinks/admin/config", x.admin, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), testProvider().Secret) || strings.Contains(w.Body.String(), "fake-post-api-key") {
		t.Fatal("admin secret visibility or Post API key redaction")
	}
	var red Config
	must(t, json.Unmarshal(w.Body.Bytes(), &red))
	red.BaseURL = "https://new.example"
	red.Providers[0].Secret = "" // Blank still preserves an existing secret.
	b, _ := json.Marshal(red)
	w = x.request("PUT", "/api/partnerlinks/admin/config", x.admin, string(b), "application/json")
	if w.Code != 200 {
		t.Fatalf("locked configure %d %s", w.Code, w.Body)
	}
	cfg, err := Load(x.app)
	must(t, err)
	if cfg.PostAPIKey != "fake-post-api-key" || cfg.Providers[0].Secret != testProvider().Secret {
		t.Fatal("omitted secrets lost")
	}
	if w = x.request("PUT", "/api/partnerlinks/admin/config", x.admin, string(b), "application/json"); w.Code != 400 {
		t.Fatal("stale write accepted")
	}
	before := *cfg
	cfg.Providers[0].Secret = "short"
	if _, err = Configure(x.app, *cfg); err == nil {
		t.Fatal("invalid config saved")
	}
	fresh, err := Load(x.app)
	must(t, err)
	if fresh.Version != before.Version || fresh.Providers[0].Secret != testProvider().Secret {
		t.Fatal("partial write")
	}
	col, err := x.app.FindCollectionByNameOrId(configsCollection)
	must(t, err)
	for _, name := range []string{configsCollection, col.Id} {
		for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
			path := "/api/collections/" + name + "/records"
			if method == "PATCH" || method == "DELETE" {
				path += "/" + configID
			}
			w = x.request(method, path, x.admin, `{"definition":{}}`, "application/json")
			if w.Code < 400 {
				t.Fatalf("service access %s %s: %d", method, name, w.Code)
			}
		}
	}
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		if w = x.request(method, "/api/partnerlinks/admin/config", x.admin, "{}", "application/json"); w.Code != 403 {
			t.Fatalf("schemalock permitted adjacent method %s", method)
		}
	}
	b, _ = json.Marshal(map[string]any{"requests": []any{map[string]any{"method": "PATCH", "url": "/api/collections/partner_links/records/" + x.link.Id, "body": map[string]any{"name": "must rollback"}}, map[string]any{"method": "PATCH", "url": "/api/collections/" + col.Id + "/records/" + configID, "body": map[string]any{"definition": map[string]any{}}}}})
	w = x.request("POST", "/api/batch", x.admin, string(b), "application/json")
	if w.Code < 400 {
		t.Fatal("batch bypass")
	}
	link, err := x.app.FindRecordById(LinksCollection, x.link.Id)
	must(t, err)
	if link.GetString("name") != "Offer" {
		t.Fatal("batch did not rollback")
	}
}

func TestNestedJSONAndHeaderSecret(t *testing.T) {
	x := setup(t, false)
	token := tokenFrom(x.issue(t))
	cfg, err := Load(x.app)
	must(t, err)
	cfg.Providers[0].SecretLocation = "header"
	cfg.Providers[0].SecretName = "X-Partner-Secret"
	cfg.Providers[0].Fields.Token = "data.token"
	cfg.Providers[0].Fields.Status = "data.status"
	_, err = Configure(x.app, *cfg)
	must(t, err)
	body, _ := json.Marshal(map[string]any{"data": map[string]any{"token": token, "status": "yes"}})
	r := httptest.NewRequest("POST", "/api/partnerlinks/postbacks/test", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Partner-Secret", testProvider().Secret)
	w := httptest.NewRecorder()
	x.handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("nested JSON %d %s", w.Code, w.Body)
	}
}

func TestRequestLoggingRedactsTokensAndSecrets(t *testing.T) {
	x := setup(t, true)
	token := tokenFrom(x.issue(t))
	for _, path := range []string{
		"/api/partnerlinks/r/" + token,
		"/api/partnerlinks/r/invalid?profile=private",
		"/api/partnerlinks/postbacks/test?secret=" + testProvider().Secret + "&subid=" + token + "&status=yes",
		"/api/partnerlinks/postbacks/test?secret=wrong&subid=" + token + "&status=yes",
	} {
		r := httptest.NewRequest("GET", path, nil)
		x.handler.ServeHTTP(httptest.NewRecorder(), r)
		x.mu.Lock()
		logged := x.loggedURLs[len(x.loggedURLs)-1]
		x.mu.Unlock()
		if strings.Contains(logged, "?") || strings.Contains(logged, token) || strings.Contains(logged, "secret=") {
			t.Fatal("sensitive data remains for activity logger")
		}
	}
}

func TestDisabledLinksAndProviderBinding(t *testing.T) {
	x := setup(t, false)
	token := tokenFrom(x.issue(t))
	x.link.Set("active", false)
	must(t, x.app.Save(x.link))
	if w := x.request("GET", "/api/partnerlinks/r/"+token, "", "", ""); w.Code != 404 {
		t.Fatal("disabled link opened")
	}
	if w := x.request("POST", "/api/partnerlinks/links/"+x.link.Id+"/resolve", x.auth, `{}`, "application/json"); w.Code != 404 {
		t.Fatal("disabled link issued")
	}
	// Disabling new/opening links must not discard late conversions.
	if w := x.request("GET", "/api/partnerlinks/postbacks/test?secret="+testProvider().Secret+"&status=yes&subid="+token, "", "", ""); w.Code != 200 {
		t.Fatal("disabled link lost conversion")
	}
	cfg, err := Load(x.app)
	must(t, err)
	p := testProvider()
	p.ID = "another"
	cfg.Providers = append(cfg.Providers, p)
	_, err = Configure(x.app, *cfg)
	must(t, err)
	before := x.eventCount()
	if w := x.request("GET", "/api/partnerlinks/postbacks/another?secret="+p.Secret+"&status=yes&subid="+token, "", "", ""); w.Code != 400 {
		t.Fatal("cross-provider token accepted")
	}
	if x.eventCount() != before {
		t.Fatal("cross-provider event delivered")
	}
}

func TestProfileIDFromAuthenticatedRecord(t *testing.T) {
	x := setup(t, true)
	path := "/api/partnerlinks/links/" + x.link.Id + "/resolve"
	issued := x.issue(t)
	data, err := readClick(x.app, tokenFrom(issued))
	must(t, err)
	if data.UserID != x.user.Id {
		t.Fatalf("profile: %q", data.UserID)
	}
	if w := x.request("POST", path, x.auth, `{"profileId":"forged"}`, "application/json"); w.Code != 400 {
		t.Fatalf("client profile allowed: %d", w.Code)
	}
	x.user.Set("appmetrica_profile_id", "updated-profile")
	must(t, x.app.Save(x.user))
	fresh, err := readClick(x.app, tokenFrom(x.issue(t)))
	must(t, err)
	if fresh.UserID != x.user.Id {
		t.Fatal("profile did not refresh from database")
	}
	w := x.request("GET", "/api/partnerlinks/postbacks/test?secret="+testProvider().Secret+"&subid="+tokenFrom(issued)+"&status=new", "", "", "")
	if w.Code != 200 {
		t.Fatalf("old snapshot postback: %d %s", w.Code, w.Body)
	}
	x.mu.Lock()
	profile := x.events[0].Get("profile_id")
	x.mu.Unlock()
	if profile != x.user.Id {
		t.Fatal("postback changed click profile")
	}
	if x.eventCount() != 1 {
		t.Fatal("rejected issuance delivered events")
	}
}

func TestBodyPostbackSecret(t *testing.T) {
	x := setup(t, true)
	token := tokenFrom(x.issue(t))
	cfg, err := Load(x.app)
	must(t, err)
	cfg.Providers[0].SecretLocation = "body"
	cfg.Providers[0].SecretName = "auth.secret"
	_, err = Configure(x.app, *cfg)
	must(t, err)
	path := "/api/partnerlinks/postbacks/test"
	jsonBody := fmt.Sprintf(`{"auth":{"secret":%q},"subid":%q,"status":"yes"}`, testProvider().Secret, token)
	form := url.Values{"auth.secret": {testProvider().Secret}, "subid": {token}, "status": {"yes"}}.Encode()
	for _, tc := range []struct {
		name, method, path, body, media string
		code                            int
	}{
		{"json", "POST", path, jsonBody, "application/json", 200},
		{"form", "POST", path, form, "application/x-www-form-urlencoded", 200},
		{"wrong", "POST", path, strings.Replace(jsonBody, testProvider().Secret, "incorrect", 1), "application/json", 403},
		{"missing", "POST", path, fmt.Sprintf(`{"subid":%q,"status":"yes"}`, token), "application/json", 403},
		{"query not body", "POST", path + "?auth.secret=" + testProvider().Secret, `{}`, "application/json", 403},
		{"get", "GET", path + "?auth.secret=" + testProvider().Secret + "&subid=" + token + "&status=yes", "", "", 403},
		{"malformed", "POST", path, `{"auth":`, "application/json", 400},
		{"object", "POST", path, `{"auth":{"secret":{}}}`, "application/json", 403},
		{"too large", "POST", path, strings.Repeat("x", maxBody+1), "application/json", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := x.eventCount()
			w := x.request(tc.method, tc.path, "", tc.body, tc.media)
			if w.Code != tc.code {
				t.Fatalf("%d: %s", w.Code, w.Body)
			}
			if tc.code != 200 && x.eventCount() != count {
				t.Fatal("rejected postback delivered event")
			}
		})
	}
	x.mu.Lock()
	for _, event := range x.events {
		if strings.Contains(event.Get("event_json"), testProvider().Secret) {
			t.Error("secret forwarded")
		}
		if event.Get("event_name") != "offer_approved" {
			t.Error("wrong conversion")
		}
	}
	x.mu.Unlock()
	cfg, err = Load(x.app)
	must(t, err)
	cfg.Providers[0].ExtraFields["leaked"] = "auth.secret"
	if _, err = Configure(x.app, *cfg); err == nil {
		t.Fatal("secret mapping accepted")
	}
	unchanged, err := Load(x.app)
	must(t, err)
	if _, ok := unchanged.Providers[0].ExtraFields["leaked"]; ok {
		t.Fatal("invalid config persisted")
	}
}

func TestDynamicLinkRecordsAPIAndResolve(t *testing.T) {
	x := setup(t, true)
	path := "/api/collections/partner_links/records"
	body := `{"name":"Typed link","provider":"test","active":true,"link":{"url":"https://partner.example/typed","mode":"view","saveCooke":false,"showLoader":false,"changeClient":true,"openUrlsInBrowser":true,"title":"Title","warningDialog":{"title":"Continue?","content":"Partner"}}}`
	count, err := x.app.CountRecords(LinksCollection)
	must(t, err)
	if w := x.request("POST", path, x.auth, body, "application/json"); w.Code != 403 {
		t.Fatalf("ordinary user created link: %d", w.Code)
	}
	after, err := x.app.CountRecords(LinksCollection)
	must(t, err)
	if after != count {
		t.Fatal("denied create wrote record")
	}
	w := x.request("POST", path, x.admin, body, "application/json")
	if w.Code != 200 {
		t.Fatalf("create %d: %s", w.Code, w.Body)
	}
	var created struct {
		ID string `json:"id"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &created))
	x.link, err = x.app.FindRecordById(LinksCollection, created.ID)
	must(t, err)
	result := x.issue(t)
	if result.Link.Mode != "view" || result.Link.SaveCooke || result.Link.ShowLoader || !result.Link.ChangeClient || !result.Link.OpenURLsInBrowser || result.Link.Title != "Title" || result.Link.WarningDialog.Content != "Partner" {
		t.Fatalf("lost typed options: %+v", result.Link)
	}
	if !strings.HasPrefix(result.Link.URL, x.config.BaseURL+"/api/partnerlinks/r/") {
		t.Fatal("resolve exposed source URL")
	}
	for _, bad := range []string{`{"link":"https://partner.example"}`, `{"link":{"url":"javascript:bad"}}`, `{"link":{"url":"https://partner.example","showLoader":"yes"}}`, `{"link":null}`} {
		w = x.request("PATCH", path+"/"+created.ID, x.admin, bad, "application/json")
		if w.Code != 400 {
			t.Fatalf("invalid update accepted: %d %s", w.Code, w.Body)
		}
	}
	current, err := x.app.FindRecordById(LinksCollection, created.ID)
	must(t, err)
	link, err := opening(current)
	must(t, err)
	if link.URL != "https://partner.example/typed" || link.Mode != "view" {
		t.Fatal("failed updates changed link")
	}
	if x.eventCount() != 0 {
		t.Fatal("record editing or resolve sent click")
	}
}

func TestResolveEmptyBodyFromPocketBaseDart(t *testing.T) {
	x := setup(t, true)
	w := x.request("POST", "/api/partnerlinks/links/"+x.link.Id+"/resolve", x.auth, "", "application/json")
	if w.Code != 200 {
		t.Fatalf("empty body %d %s", w.Code, w.Body)
	}
}

func TestResolveAppliesGlobalPolicyWithoutChangingSource(t *testing.T) {
	x := setup(t, true)
	must(t, dynamiclink.Configure(x.app, "members"))
	rows, err := x.app.FindAllRecords(dynamiclink.SettingsCollection)
	must(t, err)
	rows[0].Set("mode", "browser")
	rows[0].Set("warningPolicy", "replace")
	rows[0].Set("warningTitle", "Global")
	rows[0].Set("warningContent", "Read terms")
	must(t, x.app.Save(rows[0]))
	result := x.issue(t)
	if result.Link.Mode != "browser" || result.Link.WarningDialog == nil || result.Link.WarningDialog.Title != "Global" || result.Link.SkipWarningDialog {
		t.Fatal("resolve lost global policy")
	}
	record, err := x.app.FindRecordById(LinksCollection, x.link.Id)
	must(t, err)
	source, err := opening(record)
	must(t, err)
	if source.Mode != "appView" || source.WarningDialog != nil {
		t.Fatal("global policy changed source")
	}
}
