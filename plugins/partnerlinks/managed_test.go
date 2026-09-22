package partnerlinks

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestManagedSettings(t *testing.T) {
	m := &ManagedConfig{
		BaseURL: types.Pointer("https://code.example/"), ApplicationID: types.Pointer(int64(987)),
		PostAPIKey: types.Pointer("code-post-api-key"), OpenTTLSeconds: types.Pointer(int64(3600)),
		PendingRetentionDays: types.Pointer(0), ConversionRetentionDays: types.Pointer(0),
		EventNames: map[string]string{"lead": "code_lead"},
		Providers:  []Provider{RafinadNew("rafinad", "code-partner-secret-123")},
	}
	x := setupOptions(t, true, false, Options{Managed: m})
	*m.ApplicationID = 0
	m.EventNames["lead"] = "changed"
	m.Providers[0].Statuses["1"] = "rejected"
	c, err := Load(x.app)
	must(t, err)
	if c.ApplicationID != 987 || c.BaseURL != "https://code.example" || c.PendingRetentionDays != 0 || c.EventNames["lead"] != "code_lead" {
		t.Fatal("managed snapshot changed")
	}
	p, err := provider(c, "rafinad")
	must(t, err)
	if p.Statuses["1"] != "lead" {
		t.Fatal("caller owns managed map")
	}
	p.Statuses["1"] = "rejected"
	c, err = Load(x.app)
	must(t, err)
	p, _ = provider(c, "rafinad")
	if p.Statuses["1"] != "lead" {
		t.Fatal("Load aliases managed maps")
	}

	w := x.request("GET", "/api/partnerlinks/admin/config", x.admin, "", "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "code-post-api-key") || !strings.Contains(w.Body.String(), "code-partner-secret-123") {
		t.Fatal("admin visibility/cache")
	}
	var view adminConfig
	must(t, json.Unmarshal(w.Body.Bytes(), &view))
	if len(view.Locks.Fields) != 6 || !reflect.DeepEqual(view.Locks.Providers, []string{"rafinad"}) || len(view.Presets) != 1 {
		t.Fatal("missing metadata")
	}
	for name, mutate := range map[string]func(*adminConfig){
		"application": func(c *adminConfig) { c.ApplicationID++ },
		"key":         func(c *adminConfig) { c.PostAPIKey = "another-key" },
		"url":         func(c *adminConfig) { c.BaseURL = "https://other.example" },
		"ttl":         func(c *adminConfig) { c.OpenTTLSeconds++ },
		"pending":     func(c *adminConfig) { c.PendingRetentionDays++ },
		"retention":   func(c *adminConfig) { c.ConversionRetentionDays++ },
		"event":       func(c *adminConfig) { c.EventNames["lead"] = "another_event" },
		"provider":    func(c *adminConfig) { c.Providers[1].Secret = "replacement-secret-123" },
		"delete":      func(c *adminConfig) { c.Providers = c.Providers[:1] },
	} {
		t.Run(name, func(t *testing.T) {
			before, err := Load(x.app)
			must(t, err)
			candidate := adminSettings(x.app, *before)
			candidate.Locks = configLocks{} // Forged metadata cannot remove server locks.
			candidate.PendingRetentionDays = before.PendingRetentionDays
			candidate.EventNames["click"] = "must_rollback"
			mutate(&candidate)
			b, _ := json.Marshal(candidate)
			w := x.request("PUT", "/api/partnerlinks/admin/config", x.admin, string(b), "application/json")
			if w.Code != 400 {
				t.Fatalf("managed edit accepted: %d %s", w.Code, w.Body)
			}
			after, err := Load(x.app)
			must(t, err)
			if after.Version != before.Version || after.EventNames["click"] == "must_rollback" {
				t.Fatal("partial write")
			}
		})
	}
	c, err = Load(x.app)
	must(t, err)
	c.EventNames["click"] = "editable_click"
	c.Providers[0].Name = "Editable partner"
	view = adminSettings(x.app, *c)
	b, _ := json.Marshal(view)
	w = x.request("PUT", "/api/partnerlinks/admin/config", x.admin, string(b), "application/json")
	if w.Code != 200 {
		t.Fatalf("editable fields rejected: %s", w.Body)
	}
	raw, err := loadStored(x.app)
	must(t, err)
	b, _ = json.Marshal(raw)
	if strings.Contains(string(b), "code-") || raw.ApplicationID == 987 || raw.EventNames["click"] != "editable_click" {
		t.Fatal("managed values persisted or editable changes lost")
	}
	c, err = Load(x.app)
	must(t, err)
	err = x.app.RunInTransaction(func(tx core.App) error {
		c.EventNames["click"] = "rollback"
		if _, err := Configure(tx, *c); err != nil {
			return err
		}
		return errors.New("abort")
	})
	if err == nil {
		t.Fatal("transaction should abort")
	}
	after, err := Load(x.app)
	must(t, err)
	if after.EventNames["click"] != "editable_click" {
		t.Fatal("transaction did not roll back")
	}
}

func TestRemovingManagedRestoresStoredSettings(t *testing.T) {
	x := setup(t, true)
	p := RafinadNew("test", "overlay-provider-secret-123")
	setManaged(x.app, &ManagedConfig{PostAPIKey: types.Pointer("overlay-key"), Providers: []Provider{p}})
	c, err := Load(x.app)
	must(t, err)
	c.OpenTTLSeconds = 7200
	_, err = Configure(x.app, *c)
	must(t, err)
	setManaged(x.app, nil)
	c, err = Load(x.app)
	must(t, err)
	if c.PostAPIKey != "fake-post-api-key" || c.Providers[0].Secret != testProvider().Secret || c.OpenTTLSeconds != 7200 {
		t.Fatal("stored values not restored")
	}
}

func TestAdminConfigLock(t *testing.T) {
	for _, schemaLock := range []bool{false, true} {
		t.Run(fmt.Sprint(schemaLock), func(t *testing.T) {
			x := setupOptions(t, schemaLock, false, Options{LockAdminConfig: true})
			w := x.request("GET", "/api/partnerlinks/admin/config", x.admin, "", "")
			var c adminConfig
			must(t, json.Unmarshal(w.Body.Bytes(), &c))
			if w.Code != 200 || !c.Locks.All {
				t.Fatal("read-only config unavailable")
			}
			c.Locks.All = false
			c.BaseURL = "https://cannot-save.example"
			b, _ := json.Marshal(c)
			for _, auth := range []string{"", x.auth, x.admin} {
				w = x.request("PUT", "/api/partnerlinks/admin/config", auth, string(b), "application/json")
				if w.Code != 403 && w.Code != 401 {
					t.Fatalf("HTTP lock bypassed: %d", w.Code)
				}
			}
			before, err := Load(x.app)
			must(t, err)
			if before.Version != x.config.Version || before.BaseURL != x.config.BaseURL {
				t.Fatal("blocked write changed settings")
			}
			before.BaseURL = "https://trusted-code.example"
			_, err = Configure(x.app, *before)
			must(t, err)
			w = x.request("PATCH", "/api/collections/partner_links/records/"+x.link.Id, x.admin, `{"name":"Still editable"}`, "application/json")
			if w.Code != 200 {
				t.Fatalf("lock must not affect link records: %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestInvalidManagedBootstrap(t *testing.T) {
	for name, m := range map[string]*ManagedConfig{
		"duplicate": {Providers: []Provider{RafinadNew("r", "long-enough-secret"), RafinadNew("r", "long-enough-secret")}},
		"secret":    {Providers: []Provider{RafinadNew("r", "short")}},
		"ttl":       {OpenTTLSeconds: types.Pointer(int64(0))},
	} {
		t.Run(name, func(t *testing.T) {
			app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
			Register(app, Options{Managed: m})
			defer app.ClearBootstrap()
			if err := app.Bootstrap(); err == nil {
				t.Fatal("invalid managed config accepted")
			}
			if _, err := app.FindCollectionByNameOrId(configsCollection); err == nil {
				t.Fatal("failed bootstrap left plugin schema")
			}
		})
	}
}

func TestRafinadNewFlow(t *testing.T) {
	p := RafinadNew("test", "rafinad-secret-for-tests")
	p.SendRevenue = true
	x := setupOptions(t, true, false, Options{Managed: &ManagedConfig{Providers: []Provider{p}}})
	token := tokenFrom(x.issue(t))
	w := x.request("GET", "/api/partnerlinks/r/"+token, "", "", "")
	u, err := url.Parse(w.Header().Get("Location"))
	must(t, err)
	if w.Code != 302 || u.Query().Get("p_click_id") != token || u.Query().Get("existing") != "a&b" {
		t.Fatal("Rafinad click parameter")
	}
	for _, tc := range []struct{ status, event string }{{"1", "offer_lead"}, {"2", "offer_hold"}, {"3", "offer_rejected"}, {"4", "offer_approved"}} {
		q := url.Values{"p_click_id": {token}, "order_id": {"rafinad-order-1"}, "status": {tc.status}, "publisher_commission": {"123.45"}, "order_total": {"99999"}, "currency": {"RUB"}, "secret": {p.Secret}}
		before := x.eventCount()
		q.Set("secret", "wrong")
		w = x.request("GET", "/api/partnerlinks/postbacks/test?"+q.Encode(), "", "", "")
		if w.Code != 403 || x.eventCount() != before {
			t.Fatal("invalid secret caused side effects")
		}
		q.Set("secret", p.Secret)
		w = x.request("GET", "/api/partnerlinks/postbacks/test?"+q.Encode(), "", "", "")
		if w.Code != 200 {
			t.Fatalf("Rafinad status %s: %d %s", tc.status, w.Code, w.Body)
		}
		x.mu.Lock()
		event := x.events[before]
		last := x.events[len(x.events)-1]
		x.mu.Unlock()
		if event.Get("event_name") != tc.event {
			t.Fatalf("wrong event %v", event)
		}
		if tc.status == "4" && last.Get("price") != "123.45" {
			t.Fatalf("commission not used for revenue: %v", last)
		}
	}
	presets := ProviderPresets()
	presets[0].Provider.Statuses["1"] = "rejected"
	if RafinadNew("r", p.Secret).Statuses["1"] != "lead" || ProviderPresets()[0].Provider.Statuses["1"] != "lead" {
		t.Fatal("shared preset maps")
	}
}

func TestOnlyManagedProvidersCanBeRemovedFromCode(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	Register(app, Options{Managed: &ManagedConfig{Providers: []Provider{RafinadNew("r", "only-managed-secret")}}})
	must(t, app.Bootstrap())
	defer app.ClearBootstrap()
	c, err := Load(app)
	must(t, err)
	_, err = Configure(app, *c)
	must(t, err)
	setManaged(app, nil)
	c, err = Load(app)
	must(t, err)
	if c.Providers == nil || len(c.Providers) != 0 {
		t.Fatal("admin API must expose an empty array after removing code providers")
	}
}
