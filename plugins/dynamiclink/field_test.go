package dynamiclink

import (
	"encoding/json"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

func TestDecodeAndDefaults(t *testing.T) {
	value, err := Decode([]byte(`{"url":"https://example.com/path?a=1"}`))
	if err != nil || value.Mode != "appView" || !value.SaveCooke || !value.ShowLoader || value.ChangeClient || value.OpenURLsInBrowser {
		t.Fatalf("defaults: %+v %v", value, err)
	}
	for _, raw := range []string{`[]`, `{}`, `"https://example.com"`, `{"url":"javascript:alert(1)"}`, `{"url":"https://u:p@example.com"}`, `{"url":"https://example.com","mode":"other"}`, `{"url":"https://example.com","saveCooke":"false"}`, `{"url":"https://example.com","showLoader":null}`, `{"url":"https://example.com","unknown":true}`, `{"url":"https://example.com","warningDialog":{"title":"Only title"}}`} {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Errorf("accepted invalid %s", raw)
		}
	}
	value, err = Decode([]byte(`{"url":"http://localhost:8090","mode":"browser","saveCooke":false,"showLoader":false,"warningDialog":{"title":"Title","content":"Body"}}`))
	if err != nil || value.SaveCooke || value.ShowLoader || value.WarningDialog.Content != "Body" {
		t.Fatalf("explicit flags: %+v %v", value, err)
	}
}

func TestStandaloneFieldRoundTripAndValidation(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	Register(app)
	Register(app)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ClearBootstrap()
	c := core.NewBaseCollection("banners")
	c.Fields.Add(&Field{JSONField: core.JSONField{Name: "destination", Required: true}}, &Field{JSONField: core.JSONField{Name: "optional"}})
	if err := app.Save(c); err != nil {
		t.Fatal(err)
	}
	// Field type survives schema export/import and a database reload.
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	decoded := &core.Collection{}
	if err = json.Unmarshal(data, decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded.Fields.GetByName("destination").(*Field); !ok {
		t.Fatal("schema lost custom field type")
	}
	saved, err := app.FindCollectionByNameOrId(c.Id)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := saved.Fields.GetByName("destination").(*Field); !ok {
		t.Fatal("database lost field type")
	}
	r := core.NewRecord(saved)
	if err = app.Save(r); err == nil {
		t.Fatal("required field accepted null")
	}
	r.Set("destination", map[string]any{"url": "https://example.com"})
	if err = app.Save(r); err != nil {
		t.Fatal(err)
	}
	loaded, err := app.FindRecordById(c, r.Id)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err = loaded.UnmarshalJSONField("destination", &stored); err != nil {
		t.Fatal(err)
	}
	if stored["saveCooke"] != true || stored["mode"] != "appView" {
		t.Fatal("defaults were not stored")
	}
	r.Set("destination", map[string]any{"url": "https://example.com", "mode": "invalid"})
	if err = app.Save(r); err == nil {
		t.Fatal("invalid value saved")
	}
	loaded, err = app.FindRecordById(c, r.Id)
	if err != nil {
		t.Fatal(err)
	}
	value, err := Decode([]byte(loaded.GetString("destination")))
	if err != nil || value.Mode != "appView" {
		t.Fatal("failed update changed data")
	}
}
