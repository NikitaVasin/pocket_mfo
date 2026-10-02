package partnerlinks

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

func TestGeneratedSecretsPersistAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	options := Options{Managed: &ManagedConfig{Providers: []Provider{RafinadNew("first", ""), RafinadNew("second", ""), RafinadNew("explicit", "explicit-provider-secret")}}}
	start := func() core.App {
		app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dir})
		Register(app, options)
		must(t, app.Bootstrap())
		return app
	}
	app := start()
	before, err := Load(app)
	must(t, err)
	first, _ := provider(before, "first")
	second, _ := provider(before, "second")
	decoded, err := base64.RawURLEncoding.DecodeString(first.Secret)
	must(t, err)
	if len(decoded) != 32 || first.Secret == second.Secret {
		t.Fatal("secrets must be independent 32-byte random values")
	}
	raw, err := loadStored(app)
	must(t, err)
	if raw.ProviderCredentials["first"] != first.Secret || len(raw.Providers) != 0 || raw.ProviderCredentials["explicit"] != "" {
		t.Fatal("persist only generated credentials, not code definitions or explicit secrets")
	}
	must(t, app.ClearBootstrap())
	app = start()
	defer app.ClearBootstrap()
	after, err := Load(app)
	must(t, err)
	for _, p := range before.Providers {
		current, err := provider(after, p.ID)
		must(t, err)
		if current.Secret != p.Secret {
			t.Fatal("restart rotated secret")
		}
	}
	if before.Version != after.Version {
		t.Fatal("restart rewrote unchanged configuration")
	}
	// An omitted secret during a later save must retain the existing value.
	for i := range after.Providers {
		after.Providers[i].Secret = ""
	}
	_, err = Configure(app, *after)
	must(t, err)
	after, err = Load(app)
	must(t, err)
	current, _ := provider(after, "first")
	if current.Secret != first.Secret {
		t.Fatal("save rotated secret")
	}
}

func TestGeneratedSecretsRollbackAndAdminVisibility(t *testing.T) {
	x := setup(t, true)
	before, err := Load(x.app)
	must(t, err)
	err = x.app.RunInTransaction(func(tx core.App) error {
		c, err := Load(tx)
		if err != nil {
			return err
		}
		c.Providers = append(c.Providers, RafinadNew("generated", ""))
		if _, err := Configure(tx, *c); err != nil {
			return err
		}
		return errors.New("abort")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	c, err := Load(x.app)
	must(t, err)
	if c.Version != before.Version || len(c.Providers) != len(before.Providers) {
		t.Fatal("rolled-back credential persisted")
	}
	c.Providers = append(c.Providers, RafinadNew("generated", ""))
	c, err = Configure(x.app, *c)
	must(t, err)
	p, _ := provider(c, "generated")
	for _, auth := range []string{"", x.auth} {
		w := x.request("GET", "/api/partnerlinks/admin/config", auth, "", "")
		if w.Code < 400 || strings.Contains(w.Body.String(), p.Secret) {
			t.Fatal("non-superuser received secret")
		}
	}
	w := x.request("GET", "/api/partnerlinks/admin/config", x.admin, "", "")
	var view adminConfig
	must(t, json.Unmarshal(w.Body.Bytes(), &view))
	visible, err := provider(&view.Config, "generated")
	must(t, err)
	if w.Code != 200 || visible.Secret != p.Secret || !visible.HasSecret || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("admin cannot read persistent secret")
	}
	for _, auth := range []string{x.auth, x.admin} {
		w := x.request("GET", "/api/collections/pl_config/records", auth, "", "")
		if w.Code < 400 || strings.Contains(w.Body.String(), p.Secret) {
			t.Fatal("Records API exposed credentials")
		}
	}
}
