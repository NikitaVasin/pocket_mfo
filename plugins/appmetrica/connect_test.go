package appmetrica

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestInvalidApplicationResponseNeitherConnectsNorVerifies(t *testing.T) {
	for name, body := range map[string]string{
		"wrong-app": `{"application":{"id":456,"api_key128":"sdk","import_token":"post-secret"}}`,
		"malformed": `{"application":"post-secret"}`,
		"oversized": strings.Repeat("x", (4<<20)+1),
	} {
		t.Run(name, func(t *testing.T) {
			app := newApp(t, Options{})
			before, err := Load(app)
			must(t, err)
			p := app.Store().Get(storeKey).(*Plugin)
			calls := 0
			p.client = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/application/123") {
					t.Fatal("unexpected request after invalid application response")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			_, err = p.Connect(context.Background(), app, ConnectInput{Version: before.Version, ApplicationID: 123, OAuthToken: "oauth-secret"})
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("invalid response accepted or disclosed")
			}
			after, err := Load(app)
			must(t, err)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed connection changed configuration")
			}
			status := p.Check(context.Background(), Config{ApplicationID: 123, OAuthToken: "oauth-secret", PostAPIKey: "post-secret"})
			encoded, err := json.Marshal(status)
			must(t, err)
			if calls != 2 || status.Events.Verified || status.Push.Verified || status.Reports.Verified || strings.Contains(string(encoded), "secret") {
				t.Fatal("invalid response confirmed readiness or disclosed credentials")
			}
		})
	}
}

func TestOAuthConnectionImportsCredentialsAndRequiresSelection(t *testing.T) {
	app := newApp(t, Options{})
	p := app.Store().Get(storeKey).(*Plugin)
	single := false
	forbidKeys := false
	p.client = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "OAuth supplied-token" {
			t.Fatal("unexpected request")
		}
		body := `{"application":{"id":123,"api_key128":"sdk-public","import_token":"import-secret"}}`
		if strings.HasSuffix(r.URL.Path, "/applications") {
			body = `{"applications":[{"id":123,"name":"First","import_token":"not-for-browser"},{"id":456,"name":"Second"}]}`
			if single {
				body = `{"applications":[{"id":123,"name":"First"}]}`
			}
		}
		if forbidKeys {
			body = `{"application":{"id":123}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	before, err := Load(app)
	must(t, err)
	result, err := p.Connect(context.Background(), app, ConnectInput{Version: before.Version, OAuthToken: "supplied-token"})
	must(t, err)
	if len(result.Applications) != 2 || result.Settings != nil {
		t.Fatal("ambiguous application chosen")
	}
	b, _ := json.Marshal(result)
	if strings.Contains(string(b), "not-for-browser") {
		t.Fatal("discovery leaked keys")
	}
	unchanged, err := Load(app)
	must(t, err)
	if unchanged.Version != before.Version || unchanged.OAuthToken != "" {
		t.Fatal("discovery saved credentials")
	}
	forbidKeys = true
	if _, err = p.Connect(context.Background(), app, ConnectInput{Version: before.Version, OAuthToken: "supplied-token", ApplicationID: 123}); err == nil {
		t.Fatal("missing keys accepted")
	}
	unchanged, err = Load(app)
	must(t, err)
	if unchanged.Version != before.Version {
		t.Fatal("failed connection changed settings")
	}
	forbidKeys = false
	single = true
	result, err = p.Connect(context.Background(), app, ConnectInput{Version: before.Version, OAuthToken: "supplied-token"})
	must(t, err)
	b, _ = json.Marshal(result)
	if strings.Contains(string(b), "supplied-token") || strings.Contains(string(b), "import-secret") {
		t.Fatal("connection leaked credentials")
	}
	saved, err := Load(app)
	must(t, err)
	if saved.ApplicationID != 123 || saved.OAuthToken != "supplied-token" || saved.PostAPIKey != "import-secret" || saved.SDKAPIKey != "sdk-public" {
		t.Fatal("incomplete import")
	}
	if _, err = p.Connect(context.Background(), app, ConnectInput{Version: before.Version, OAuthToken: "supplied-token"}); err == nil {
		t.Fatal("stale import accepted")
	}
}
func TestConnectDoesNotBypassIdentityOrVersionGuard(t *testing.T) {
	app := newApp(t, Options{})
	cfg, err := Load(app)
	must(t, err)
	cfg.ApplicationID = 999
	cfg.OAuthToken = "old"
	cfg, err = Configure(app, cfg)
	must(t, err)
	p := app.Store().Get(storeKey).(*Plugin)
	p.client = &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"application":{"id":123,"api_key128":"sdk","import_token":"post"}}`))}, nil
	})}
	GuardApplicationChange(app, "device", func(core.App, int64, int64) error { return io.ErrClosedPipe })
	if _, err = p.Connect(context.Background(), app, ConnectInput{Version: cfg.Version, OAuthToken: "new", ApplicationID: 123}); err == nil {
		t.Fatal("connection bypassed identity guard")
	}
	after, err := Load(app)
	must(t, err)
	if after.Version != cfg.Version || after.ApplicationID != 999 || after.OAuthToken != "old" {
		t.Fatal("connection partially saved")
	}
}

func TestChangingApplicationClearsCachedKeys(t *testing.T) {
	app := newApp(t, Options{})
	c, err := Load(app)
	must(t, err)
	c.ApplicationID = 123
	c.PostAPIKey = "old-post"
	c.SDKAPIKey = "old-sdk"
	c, err = Configure(app, c)
	must(t, err)
	c.ApplicationID = 456
	c, err = Configure(app, c)
	must(t, err)
	if c.PostAPIKey != "" || c.SDKAPIKey != "" {
		t.Fatal("old application keys reused")
	}
}

func TestCodeConfiguredConnectionRefreshesAtServeAndFailsClosedOnNewID(t *testing.T) {
	app := newApp(t, Options{Managed: &ManagedConfig{ApplicationID: types.Pointer(int64(123)), OAuthToken: types.Pointer("code-token")}})
	p := app.Store().Get(storeKey).(*Plugin)
	p.client = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("startup writes remotely")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"application":{"id":123,"api_key128":"public-key","import_token":"post-key"}}`))}, nil
	})}
	serve := func() {
		router, err := apis.NewRouter(app)
		must(t, err)
		must(t, app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}, func(*core.ServeEvent) error { return nil }))
	}
	serve()
	c, err := Load(app)
	must(t, err)
	if c.PostAPIKey != "post-key" || c.SDKAPIKey != "public-key" {
		t.Fatal("code config did not import keys")
	}
	raw, err := rawConfig(app)
	must(t, err)
	if raw.OAuthToken != "" {
		t.Fatal("code token persisted")
	}
	version := c.Version
	serve()
	c, err = Load(app)
	must(t, err)
	if c.Version != version {
		t.Fatal("unchanged startup invalidated config")
	}
	p.options.Managed.ApplicationID = types.Pointer(int64(456))
	serve()
	c, err = Load(app)
	must(t, err)
	if c.ApplicationID != 456 || c.PostAPIKey != "" || c.SDKAPIKey != "" {
		t.Fatal("failed connection reused previous app keys")
	}
}
