package polymorphicrelation

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func TestExplicitSystemHostRegistration(t *testing.T) {
	app := newApp(t)
	target := core.NewBaseCollection("owners")
	mustSave(t, app, target)
	owner := core.NewRecord(target)
	mustSave(t, app, owner)
	makeHost := func(name string) *core.Collection {
		c := core.NewBaseCollection(name)
		c.System = true
		c.Fields.Add(&Field{JSONField: core.JSONField{Name: "owner", Required: true}, CollectionIDs: []string{target.Id}, OnDelete: Cascade})
		return c
	}
	host := makeHost("server_orders")
	if err := app.Save(host); err == nil {
		t.Fatal("system host accepted without server opt-in")
	}
	Register(app, Options{SystemCollections: []string{"server_orders", "system_auth"}})
	Register(app) // another plugin registration must not remove the server option
	host = makeHost("server_orders")
	mustSave(t, app, host)
	record := core.NewRecord(host)
	record.Set("owner", Reference{CollectionID: target.Id, RecordID: owner.Id})
	mustSave(t, app, record)
	if err := app.Save(makeHost("another_system_collection")); err == nil {
		t.Fatal("opt-in leaked to another collection")
	}
	systemAuth := core.NewAuthCollection("system_auth")
	systemAuth.System = true
	systemAuth.Fields.Add(&Field{JSONField: core.JSONField{Name: "owner"}, CollectionIDs: []string{target.Id}})
	if err := app.Save(systemAuth); err == nil {
		t.Fatal("system auth host allowed")
	}
	superusers, err := app.FindCollectionByNameOrId("_superusers")
	if err != nil {
		t.Fatal(err)
	}
	badTarget := makeHost("server_orders")
	badTarget.Fields.GetByName("owner").(*Field).CollectionIDs = []string{superusers.Id}
	if err := badTarget.Fields.GetByName("owner").(*Field).ValidateSettings(nil, app, badTarget); err == nil {
		t.Fatal("system target allowed")
	}
	if err := app.Delete(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FindRecordById(host.Id, record.Id); err == nil {
		t.Fatal("system-owned record did not cascade with owner")
	}
	otherApp := newApp(t)
	otherTarget := core.NewBaseCollection("owners")
	mustSave(t, otherApp, otherTarget)
	foreign := makeHost("server_orders")
	foreign.Fields.GetByName("owner").(*Field).CollectionIDs = []string{otherTarget.Id}
	if err := otherApp.Save(foreign); err == nil {
		t.Fatal("opt-in leaked to another application")
	}
}
