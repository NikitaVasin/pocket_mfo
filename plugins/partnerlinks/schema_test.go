package partnerlinks

import (
	"encoding/json"
	"testing"

	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/schemalock"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestLegacyLinksMigrateAtomically(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	must(t, app.Bootstrap())
	defer app.ClearBootstrap()
	c := core.NewBaseCollection(LinksCollection)
	c.ViewRule = types.Pointer("@request.auth.id != ''")
	c.Fields.Add(&core.TextField{Name: "name"}, &core.TextField{Name: "provider"}, &core.URLField{Name: "url"}, &core.BoolField{Name: "active"}, &core.JSONField{Name: "opening"}, &core.TextField{Name: "custom"})
	must(t, app.Save(c))
	records := []*core.Record{}
	for i, raw := range []string{`{"mode":"browser","saveCooke":false,"showLoader":false,"changeClient":true,"openUrlsInBrowser":true,"skipWarningDialog":true,"warningDialog":{"title":"Title","content":"Body"},"title":"Offer","trackName":"test"}`, `{"mode":"invalid"}`, `null`} {
		r := core.NewRecord(c)
		r.Set("name", i)
		r.Set("provider", "test")
		r.Set("url", "https://partner.example/offer?x=1")
		r.Set("opening", raw)
		r.Set("active", true)
		r.Set("custom", "preserve")
		must(t, app.Save(r))
		records = append(records, r)
	}
	cfgCol := core.NewBaseCollection(configsCollection)
	cfgCol.System = true
	cfgCol.Fields.Add(&core.JSONField{Name: "definition", Hidden: true, MaxSize: 262144})
	must(t, app.Save(cfgCol))
	cfg := DefaultConfig()
	cfg.Providers = []Provider{testProvider()}
	cfgRecord := core.NewRecord(cfgCol)
	cfgRecord.Id = configID
	cfgRecord.Set("definition", cfg)
	must(t, app.Save(cfgRecord))
	Register(app, Options{})
	schemalock.Register(app)
	if err := install(app); err == nil {
		t.Fatal("invalid legacy options migrated")
	}
	unchanged, err := app.FindCollectionByNameOrId(c.Id)
	must(t, err)
	if unchanged.Fields.GetByName("link") != nil || unchanged.Fields.GetByName("url") == nil || unchanged.Fields.GetByName("opening") == nil {
		t.Fatal("failed migration changed schema")
	}
	before, err := app.FindRecordById(c, records[0].Id)
	must(t, err)
	if before.GetString("opening") != records[0].GetString("opening") {
		t.Fatal("failed migration changed existing data")
	}
	// Repair historical fixture directly; normal writes now use the new contract.
	_, err = app.DB().NewQuery("UPDATE partner_links SET opening = '{}' WHERE id = {:id}").Bind(map[string]any{"id": records[1].Id}).Execute()
	must(t, err)
	must(t, install(app))
	migrated, err := app.FindCollectionByNameOrId(c.Id)
	must(t, err)
	if _, ok := migrated.Fields.GetByName("link").(*dynamiclink.Field); !ok {
		t.Fatal("missing dynamicLink field")
	}
	if migrated.Fields.GetByName("url") != nil || migrated.Fields.GetByName("opening") != nil || *migrated.ViewRule != *c.ViewRule {
		t.Fatal("legacy fields or rule changed unexpectedly")
	}
	for i, r := range records {
		current, err := app.FindRecordById(migrated, r.Id)
		must(t, err)
		value, err := opening(current)
		must(t, err)
		if value.URL != r.GetString("url") || current.GetString("custom") != "preserve" || current.GetString("provider") != "test" {
			t.Fatal("lost record data")
		}
		if i == 0 {
			if value.Mode != "browser" || value.SaveCooke || value.ShowLoader || !value.ChangeClient || !value.OpenURLsInBrowser || !value.SkipWarningDialog || value.WarningDialog.Content != "Body" || value.TrackName != "test" || value.Title != "Offer" {
				t.Fatalf("lost options: %+v", value)
			}
		} else if !value.SaveCooke || !value.ShowLoader || value.Mode != "appView" {
			t.Fatal("lost defaults")
		}
	}
	schema, _ := json.Marshal(migrated)
	must(t, install(app))
	again, err := app.FindCollectionByNameOrId(c.Id)
	must(t, err)
	actual, _ := json.Marshal(again)
	if string(schema) != string(actual) {
		t.Fatal("migration not idempotent")
	}
}
