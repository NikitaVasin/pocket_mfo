package variants

import (
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestInstallDoesNotTakeOverApplicationCollection(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	must(t, app.Bootstrap())
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	c := core.NewBaseCollection(configs)
	c.Fields.Add(&core.JSONField{Name: "definition"})
	must(t, app.Save(c))
	if err := install(app); err == nil {
		t.Fatal("adopted a non-system application collection")
	}
	stored, err := app.FindCollectionByNameOrId(c.Id)
	must(t, err)
	if stored.System {
		t.Fatal("failed installation modified application schema")
	}
}

func TestUnconfiguredAuthBucketBelongsToApplication(t *testing.T) {
	x := newFixture(t)
	u := core.NewAuthCollection("unrelated_members")
	u.Fields.Add(&core.NumberField{Name: BucketField})
	u.UpdateRule = types.Pointer("id = @request.auth.id")
	must(t, x.app.Save(u))
	r := core.NewRecord(u)
	r.SetEmail("unrelated@example.test")
	r.SetPassword("test-password-123")
	r.Set(BucketField, 123)
	must(t, x.app.Save(r))
	if r.GetInt(BucketField) != 123 {
		t.Fatal("registration overwrote an application-owned field")
	}
	r.Set(BucketField, 0)
	must(t, x.app.Save(r))
	must(t, ensureUserBucket(x.app, r))
	if r.GetInt(BucketField) != 0 {
		t.Fatal("unconfigured auth collection acquired a Variants bucket")
	}
	token, err := r.NewAuthToken()
	must(t, err)
	call(t, x, "PATCH", "/api/collections/unrelated_members/records/"+r.Id, token, map[string]any{BucketField: 456}, 200)
	stored, err := x.app.FindRecordById(u, r.Id)
	must(t, err)
	if stored.GetInt(BucketField) != 456 {
		t.Fatal("HTTP request overwrote the application-owned bucket")
	}
}
