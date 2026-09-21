package migrations

import (
	"github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase"
	"testing"
)

func TestDemoSingleton(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	polymorphicrelation.Register(app)
	variants.Register(app)
	singleton.Register(app)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(app.Bootstrap())
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	check(app.RunAppMigrations())
	cfg, err := singleton.Load(app, demoSingletonID)
	check(err)
	if !cfg.Enabled {
		t.Fatal("demo singleton disabled")
	}
	rows, err := app.FindAllRecords(demoSingletonID)
	check(err)
	if len(rows) != 6 {
		t.Fatalf("want 6 independent records, got %d", len(rows))
	}
	rows[0].Set("title", "User edit")
	check(app.Save(rows[0]))
	check(ensureDemoSingleton(app))
	fresh, err := app.FindRecordById(demoSingletonID, rows[0].Id)
	check(err)
	if fresh.GetString("title") != "User edit" {
		t.Fatal("seed overwrote user edits")
	}
}
