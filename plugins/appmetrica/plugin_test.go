package appmetrica

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NikitaVasin/pocket_mfo/plugins/schemalock"
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
func newApp(t *testing.T, options Options) *pocketbase.PocketBase {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	Register(app, options)
	must(t, app.Bootstrap())
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	return app
}
func TestMigrationConflictAndRollback(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	must(t, app.Bootstrap())
	defer app.ClearBootstrap()
	Register(app, Options{})
	RegisterLegacySource(app, "partner", func(core.App) (Config, error) { return Config{ApplicationID: 123, PostAPIKey: "post-secret"}, nil })
	RegisterLegacySource(app, "push", func(core.App) (Config, error) { return Config{ApplicationID: 456, OAuthToken: "oauth-secret"}, nil })
	if initialize(app) == nil {
		t.Fatal("conflict accepted")
	}
	if _, err := app.FindCollectionByNameOrId(Collection); err == nil {
		t.Fatal("partial migration")
	}
	RegisterLegacySource(app, "push", func(core.App) (Config, error) { return Config{ApplicationID: 123, OAuthToken: "oauth-secret"}, nil })
	must(t, initialize(app))
	c, err := Load(app)
	must(t, err)
	if c.ApplicationID != 123 || c.PostAPIKey != "post-secret" || c.OAuthToken != "oauth-secret" {
		t.Fatal("migration lost settings")
	}
	c.OAuthToken = "rotated"
	_, err = Configure(app, c)
	must(t, err)
	must(t, initialize(app))
	c, err = Load(app)
	must(t, err)
	if c.OAuthToken != "rotated" {
		t.Fatal("legacy reimported")
	}
}
func TestManagedConfigAndIdentityGuard(t *testing.T) {
	id := int64(123)
	token := "code-secret"
	app := newApp(t, Options{Managed: &ManagedConfig{ApplicationID: &id, OAuthToken: &token}})
	token = "mutated"
	c, err := Load(app)
	must(t, err)
	if c.OAuthToken != "code-secret" {
		t.Fatal("managed aliases")
	}
	raw, err := rawConfig(app)
	must(t, err)
	if raw.OAuthToken != "" {
		t.Fatal("managed secret persisted")
	}
	c.PostAPIKey = "editable"
	_, err = Configure(app, c)
	must(t, err)
	if _, err = Configure(app, c); err == nil {
		t.Fatal("stale version accepted")
	}
	p := app.Store().Get(storeKey).(*Plugin)
	p.options.Managed = nil
	GuardApplicationChange(app, "consumer", func(core.App, int64, int64) error { return fmt.Errorf("bound devices") })
	c, err = Load(app)
	must(t, err)
	old := c
	c.ApplicationID = 456
	if _, err = Configure(app, c); err == nil {
		t.Fatal("guard bypassed")
	}
	c, err = Load(app)
	must(t, err)
	if c.Version != old.Version || c.ApplicationID != 123 {
		t.Fatal("partial write")
	}
	p.options.Managed = &ManagedConfig{ApplicationID: types.Pointer(int64(456))}
	if initialize(app) == nil {
		t.Fatal("code change bypassed guard")
	}
}
func TestAdminProtectionAndRedaction(t *testing.T) {
	for _, locked := range []bool{false, true} {
		t.Run(fmt.Sprint(locked), func(t *testing.T) {
			app := newApp(t, Options{})
			if locked {
				schemalock.Register(app)
			}
			cfg, err := Load(app)
			must(t, err)
			cfg.ApplicationID = 123
			cfg.PostAPIKey = "post-secret"
			cfg.OAuthToken = "oauth-secret"
			_, err = Configure(app, cfg)
			must(t, err)
			col, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
			must(t, err)
			admin := core.NewRecord(col)
			admin.SetEmail("admin@example.test")
			admin.SetPassword("password-12345")
			must(t, app.Save(admin))
			token, err := admin.NewAuthToken()
			must(t, err)
			users := core.NewAuthCollection("members")
			must(t, app.Save(users))
			user := core.NewRecord(users)
			user.SetEmail("member@example.test")
			user.SetPassword("password-12345")
			must(t, app.Save(user))
			userToken, err := user.NewAuthToken()
			must(t, err)
			router, err := apis.NewRouter(app)
			must(t, err)
			must(t, app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}, func(*core.ServeEvent) error { return nil }))
			handler, err := router.BuildMux()
			must(t, err)
			request := func(method, path, auth string, body any) *httptest.ResponseRecorder {
				b, _ := json.Marshal(body)
				r := httptest.NewRequest(method, path, bytes.NewReader(b))
				r.Header.Set("Authorization", auth)
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			for _, auth := range []string{"", userToken} {
				if w := request("POST", "/api/appmetrica/admin/connect", auth, map[string]any{}); w.Code < 400 {
					t.Fatal("non-admin connect")
				}
				for _, method := range []string{"GET", "PUT", "POST"} {
					path := "/api/appmetrica/admin/config"
					if method == "POST" {
						path = "/api/appmetrica/admin/check"
					}
					if w := request(method, path, auth, cfg); w.Code < 400 {
						t.Fatal("non-admin access")
					}
				}
			}
			w := request("GET", "/api/appmetrica/admin/config", token, nil)
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "post-secret") || strings.Contains(w.Body.String(), "oauth-secret") {
				t.Fatal("redaction")
			}
			cfg, err = Load(app)
			must(t, err)
			cfg.OAuthToken = ""
			cfg.PostAPIKey = ""
			cfg.SDKAPIKey = "public-sdk-key"
			w = request("PUT", "/api/appmetrica/admin/config", token, cfg)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			after, err := Load(app)
			must(t, err)
			if after.OAuthToken != "oauth-secret" || after.PostAPIKey != "post-secret" {
				t.Fatal("blank secret lost")
			}
			for _, path := range []string{"/api/collections/" + Collection + "/records", "/api/collections/" + Collection + "/records/" + configID} {
				w = request("GET", path, token, nil)
				if w.Code < 400 {
					t.Fatal("records leak")
				}
			}
			w = request("DELETE", "/api/collections/"+Collection+"/truncate", token, nil)
			if w.Code < 400 {
				t.Fatal("truncate allowed")
			}
			for _, method := range []string{"PATCH", "DELETE", "POST"} {
				if w = request(method, "/api/appmetrica/admin/config", token, cfg); w.Code < 400 {
					t.Fatal("unexpected method")
				}
			}
			r, err := app.FindRecordById(Collection, configID)
			must(t, err)
			r.Set("definition", map[string]any{})
			if app.Save(r) == nil {
				t.Fatal("direct write allowed")
			}
			cfg = after
			cfg.EventNames["click"] = "changed"
			w = request("PUT", "/api/appmetrica/admin/config", token, cfg)
			if w.Code != 400 {
				t.Fatal("events HTTP edit")
			}
			after, err = Load(app)
			must(t, err)
			if after.Version != cfg.Version {
				t.Fatal("failed request changed config")
			}
		})
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCheckNeverWritesAndDoesNotVerifyAbsentRemotePostKey(t *testing.T) {
	calls := 0
	p := &Plugin{client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" {
			t.Fatal("check wrote to API")
		}
		if r.Header.Get("Authorization") != "OAuth secret" {
			t.Fatal("wrong token")
		}
		body := `{"application":{"id":123}}`
		if strings.Contains(r.URL.Path, "groups") {
			body = `{"groups":[]}`
		} else if strings.Contains(r.URL.Path, "stat/") {
			body = `{"totals":[0]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}}
	c := Config{ApplicationID: 123, PostAPIKey: "post", OAuthToken: "secret"}
	status := p.Check(context.Background(), c)
	if calls != 3 || !status.Push.Verified || !status.Reports.Verified || status.Events.Verified || !status.Events.Configured {
		t.Fatal("incorrect readiness")
	}
	p.client.Transport = transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"message":"secret"}`))}, nil
	})
	status = p.Check(context.Background(), c)
	b, _ := json.Marshal(status)
	if status.Push.Verified || strings.Contains(string(b), "secret") {
		t.Fatal("error disclosed credential or confirmed failed check")
	}
}

func TestCheckComparesPostKeyWithoutEvents(t *testing.T) {
	p := &Plugin{client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("check wrote")
		}
		body := `{"application":{"id":123,"import_token":"post-secret"}}`
		if strings.Contains(r.URL.Path, "groups") {
			body = `{"groups":[]}`
		} else if strings.Contains(r.URL.Path, "stat/") {
			body = `{"totals":[0]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	for _, key := range []string{"post-secret", "wrong-key"} {
		result := p.Check(context.Background(), Config{ApplicationID: 123, OAuthToken: "oauth-secret", PostAPIKey: key})
		if result.Events.Verified != (key == "post-secret") {
			t.Fatal("incorrect comparison")
		}
		b, _ := json.Marshal(result)
		if strings.Contains(string(b), "post-secret") || strings.Contains(string(b), "oauth-secret") {
			t.Fatal("check leaked credentials")
		}
	}
}
