package dynamiclink

import (
	"errors"
	"testing"

	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/types"
)

func categoryTestApp(t *testing.T) (*pocketbase.PocketBase, []*core.Record) {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	variants.Register(app)
	singleton.Register(app)
	Register(app)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(app.Bootstrap())
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	auth := core.NewAuthCollection("members")
	auth.Fields.Add(&core.BoolField{Name: "premium"})
	check(app.Save(auth))
	check(Configure(app, auth.Id))
	cfg, err := variants.Load(app, SettingsCollection)
	check(err)
	cfg.Variants = []variants.Variant{{Key: "premium", Condition: &variants.Condition{Kind: "field", Field: "premium", Op: "eq", Value: true}}}
	cfg, err = variants.Publish(app, *cfg)
	check(err)
	user := core.NewRecord(auth)
	user.SetEmail("category@example.test")
	user.SetPassword("test-password-123")
	user.Set("premium", true)
	check(app.Save(user))
	decision, err := variants.Resolve(app, cfg, user)
	check(err)
	c, err := app.FindCollectionByNameOrId(SettingsCollection)
	check(err)
	r := core.NewRecord(c)
	r.Set(variants.SetField, decision.Set)
	r.Set("mode", "view")
	check(app.Save(r))
	rows, err := app.FindAllRecords(c)
	check(err)
	return app, rows
}

func TestDefaultCategoriesUpgradeAndPreserveOptions(t *testing.T) {
	app, rows := categoryTestApp(t)
	for _, row := range rows {
		cats, err := decodeCategories(row)
		if err != nil || len(cats) != 0 {
			t.Fatal("independent Dynamic Link must have no default category", err)
		}
	}
	custom := []Category{{Key: "offers", Label: "Мои офферы", Options: OpeningOptions{Mode: "appView", SaveCooke: types.Pointer(false)}}, {Key: "banners", Label: "Баннеры"}}
	rows[0].Set("categories", custom)
	if err := app.Save(rows[0]); err != nil {
		t.Fatal(err)
	}
	before := rows[0].GetString("categories")
	RegisterDefaultCategory(app, "offers", "Офферы")
	RegisterDefaultCategory(app, "offers", "Офферы")
	serve := func() error {
		router, err := apis.NewRouter(app)
		if err != nil {
			return err
		}
		return app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router})
	}
	for range 2 {
		if err := serve(); err != nil {
			t.Fatal(err)
		}
		for i, old := range rows {
			row, err := app.FindRecordById(SettingsCollection, old.Id)
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				if row.GetString("categories") != before {
					t.Fatal("existing category overwritten")
				}
			} else {
				cats, err := decodeCategories(row)
				if err != nil || len(cats) != 1 || cats[0].Key != "offers" || cats[0].Label != "Офферы" || cats[0].Options != (OpeningOptions{}) {
					t.Fatal("missing inherited category", cats, err)
				}
			}
			if row.GetString("mode") != old.GetString("mode") {
				t.Fatal("general policy changed")
			}
		}
	}
	// Startup also upgrades persisted policies before serving HTTP.
	rows[1].Set("categories", []Category{})
	if err := app.Save(rows[1]); err != nil {
		t.Fatal(err)
	}
	if err := app.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	row, err := app.FindRecordById(SettingsCollection, rows[1].Id)
	if err != nil {
		t.Fatal(err)
	}
	cats, err := decodeCategories(row)
	if err != nil || len(cats) != 1 || cats[0].Key != "offers" {
		t.Fatal("startup did not add category", err)
	}
}

func TestDefaultCategoriesUpgradeRollsBack(t *testing.T) {
	app, rows := categoryTestApp(t)
	RegisterDefaultCategory(app, "offers", "Офферы")
	saves := 0
	app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{Id: "test-fail-second-policy", Func: func(e *core.RecordEvent) error {
		if e.Record.Collection().Name == SettingsCollection {
			saves++
			if saves == 2 {
				return errors.New("test save failure")
			}
		}
		return e.Next()
	}})
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}); err == nil {
		t.Fatal("expected failed upgrade")
	}
	if saves != 2 {
		t.Fatalf("expected two attempted saves, got %d", saves)
	}
	for _, old := range rows {
		row, err := app.FindRecordById(SettingsCollection, old.Id)
		if err != nil {
			t.Fatal(err)
		}
		if row.GetString("categories") != old.GetString("categories") {
			t.Fatal("partial upgrade persisted")
		}
	}
}
