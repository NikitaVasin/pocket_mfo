package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

func TestDemoAdmin(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })

	if err := ensureDemoAdmin(app); err != nil {
		t.Fatal(err)
	}
	admin, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "admin@admin.com")
	if err != nil {
		t.Fatal(err)
	}
	if !admin.ValidatePassword("123456") {
		t.Fatal("demo password must authenticate")
	}
	admin.SetPassword("changed-password-123")
	if err := app.Save(admin); err != nil {
		t.Fatal(err)
	}
	if err := ensureDemoAdmin(app); err != nil {
		t.Fatal(err)
	}
	preserved, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "admin@admin.com")
	if err != nil {
		t.Fatal(err)
	}
	if preserved.Id != admin.Id || !preserved.ValidatePassword("changed-password-123") {
		t.Fatal("existing account and password must be preserved")
	}
}
