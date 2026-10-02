package migrations

import (
	"errors"
	"testing"

	"github.com/NikitaVasin/pocket_mfo/plugins/partnerlinks"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

func TestDemoPartnerOptInAndCleanup(t *testing.T) {
	t.Setenv("DEMO_PARTNER_ENABLED", "")
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	partnerlinks.Register(app, partnerlinks.Options{Managed: &partnerlinks.ManagedConfig{Providers: []partnerlinks.Provider{partnerlinks.RafinadNew("rafinad-new", "")}}})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ClearBootstrap()
	if err := ensureDemoPartnerLinks(app); err != nil {
		t.Fatal(err)
	}
	cfg, err := partnerlinks.Load(app)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) != 1 || cfg.Providers[0].ID != "rafinad-new" {
		t.Fatal("default example contains non-Rafinad provider")
	}
	secret := cfg.Providers[0].Secret
	t.Setenv("DEMO_PARTNER_ENABLED", "1")
	if err := ensureDemoPartnerLinks(app); err != nil {
		t.Fatal(err)
	}
	if err := removeLegacyDemoPartner(app); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FindRecordById(partnerlinks.LinksCollection, "demopartner0001"); err != nil {
		t.Fatal("opted-in fixture missing", err)
	}
	t.Setenv("DEMO_PARTNER_ENABLED", "")
	err = app.RunInTransaction(func(tx core.App) error {
		if err := removeLegacyDemoPartner(tx); err != nil {
			return err
		}
		return errors.New("abort")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	if _, err := app.FindRecordById(partnerlinks.LinksCollection, "demopartner0001"); err != nil {
		t.Fatal("cleanup didn't roll back", err)
	}
	for range 2 {
		if err := removeLegacyDemoPartner(app); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err = partnerlinks.Load(app)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) != 1 || cfg.Providers[0].ID != "rafinad-new" || cfg.Providers[0].Secret != secret {
		t.Fatal("cleanup changed preset or secret")
	}
	if _, err := app.FindRecordById(partnerlinks.LinksCollection, "demopartner0001"); err == nil {
		t.Fatal("demo offer remains")
	}
	// User-created references prevent removal of a partner in active use.
	t.Setenv("DEMO_PARTNER_ENABLED", "1")
	if err := ensureDemoPartnerLinks(app); err != nil {
		t.Fatal(err)
	}
	link, err := app.FindRecordById(partnerlinks.LinksCollection, "demopartner0001")
	if err != nil {
		t.Fatal(err)
	}
	link.Set("name", "Edited offer")
	if err := app.Save(link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEMO_PARTNER_ENABLED", "")
	if err := removeLegacyDemoPartner(app); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FindRecordById(partnerlinks.LinksCollection, "demopartner0001"); err != nil {
		t.Fatal("user content removed", err)
	}
	cfg, err = partnerlinks.Load(app)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) != 2 {
		t.Fatal("referenced provider removed")
	}
}

func TestDemoCleanupPreservesEditedConfiguration(t *testing.T) {
	for _, scenario := range []string{"status-mapping", "secret-location", "opening", "inactive"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("DEMO_PARTNER_ENABLED", "1")
			app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
			partnerlinks.Register(app, partnerlinks.Options{})
			if err := app.Bootstrap(); err != nil {
				t.Fatal(err)
			}
			defer app.ClearBootstrap()
			if err := ensureDemoPartnerLinks(app); err != nil {
				t.Fatal(err)
			}
			cfg, err := partnerlinks.Load(app)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "status-mapping":
				cfg.Providers[0].Statuses["lead"] = "hold"
			case "secret-location":
				cfg.Providers[0].SecretLocation = "header"
			}
			if _, err := partnerlinks.Configure(app, *cfg); err != nil {
				t.Fatal(err)
			}
			link, err := app.FindRecordById(partnerlinks.LinksCollection, "demopartner0001")
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "opening" {
				link.Set("link", map[string]any{"url": "https://example.com/offer", "mode": "appView"})
			}
			if scenario == "inactive" {
				link.Set("active", false)
			}
			if err := app.Save(link); err != nil {
				t.Fatal(err)
			}
			before, err := partnerlinks.Load(app)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("DEMO_PARTNER_ENABLED", "")
			if err := removeLegacyDemoPartner(app); err != nil {
				t.Fatal(err)
			}
			if _, err := app.FindRecordById(partnerlinks.LinksCollection, "demopartner0001"); err != nil {
				t.Fatal("edited fixture was deleted", err)
			}
			after, err := partnerlinks.Load(app)
			if err != nil {
				t.Fatal(err)
			}
			if after.Version != before.Version || len(after.Providers) != 1 {
				t.Fatal("edited provider changed")
			}
		})
	}
}
