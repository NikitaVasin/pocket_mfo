package dynamiclink

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestVariantPoliciesAndResponseIsolation(t *testing.T) {
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	variants.Register(app)
	singleton.Register(app)
	Register(app)
	must(app.Bootstrap())
	defer app.ClearBootstrap()
	auth := core.NewAuthCollection("members")
	auth.Fields.Add(&core.BoolField{Name: "premium"})
	must(app.Save(auth))
	user := core.NewRecord(auth)
	user.SetEmail("user@example.test")
	user.SetPassword("test-password-123")
	must(app.Save(user))
	premium := core.NewRecord(auth)
	premium.SetEmail("premium@example.test")
	premium.SetPassword("test-password-123")
	premium.Set("premium", true)
	must(app.Save(premium))
	must(Configure(app, auth.Id))
	must(Configure(app, auth.Id))
	cfg, err := variants.Load(app, SettingsCollection)
	must(err)
	cfg.Variants = []variants.Variant{{Key: "premium", Condition: &variants.Condition{Kind: "field", Field: "premium", Op: "eq", Value: true}, Experiments: []variants.Experiment{{Key: "warning", Active: true, Groups: []variants.Group{{Key: "a", From: 1, To: 5000}, {Key: "b", From: 5001, To: 10000}}}}}}
	cfg, err = variants.Publish(app, *cfg)
	must(err)
	settings, err := app.FindCollectionByNameOrId(SettingsCollection)
	must(err)
	rows, err := app.FindAllRecords(settings)
	must(err)
	rows[0].Set("mode", "browser")
	rows[0].Set("warningPolicy", "disabled")
	must(app.Save(rows[0]))
	original := Value{URL: "https://example.com", Mode: "appView", SaveCooke: true, ShowLoader: true}
	original.SkipWarningDialog = true
	got, err := Apply(app, user, original)
	must(err)
	if got.Mode != "browser" || !got.SkipWarningDialog || got.WarningDialog != nil {
		t.Fatal("default override missing")
	}
	got, err = Apply(app, premium, original)
	must(err)
	if got.Mode != "appView" {
		t.Fatal("empty experiment inherited default")
	}
	decision, err := variants.Resolve(app, cfg, premium)
	must(err)
	r := core.NewRecord(settings)
	r.Set(variants.SetField, decision.Set)
	r.Set("mode", "view")
	r.Set("warningPolicy", "replace")
	r.Set("warningTitle", "Terms")
	r.Set("warningContent", "Read before opening")
	must(app.Save(r))
	duplicate := core.NewRecord(settings)
	duplicate.Set(variants.SetField, decision.Set)
	if app.Save(duplicate) == nil {
		t.Fatal("singleton policy not enforced")
	}
	r.Set("warningContent", "")
	if app.Save(r) == nil {
		t.Fatal("invalid replacement accepted")
	}
	got, err = Apply(app, premium, original)
	must(err)
	if got.Mode != "view" || got.SkipWarningDialog || got.WarningDialog.Title != "Terms" {
		t.Fatal("variant warning did not override link")
	}
	banners := core.NewBaseCollection("banners")
	banners.ListRule = types.Pointer("")
	banners.ViewRule = types.Pointer("")
	banners.Fields.Add(&Field{JSONField: core.JSONField{Name: "link"}})
	must(app.Save(banners))
	banner := core.NewRecord(banners)
	banner.Set("link", original)
	must(app.Save(banner))
	parent := core.NewBaseCollection("pages")
	parent.ListRule = types.Pointer("")
	parent.ViewRule = types.Pointer("")
	parent.Fields.Add(&core.RelationField{Name: "banner", CollectionId: banners.Id, MaxSelect: 1})
	must(app.Save(parent))
	page := core.NewRecord(parent)
	page.Set("banner", banner.Id)
	must(app.Save(page))
	admins, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	must(err)
	admin := core.NewRecord(admins)
	admin.SetEmail("admin@example.test")
	admin.SetPassword("test-password-123")
	must(app.Save(admin))
	router, err := apis.NewRouter(app)
	must(err)
	handler, err := router.BuildMux()
	must(err)
	for _, tc := range []struct {
		user *core.Record
		mode string
	}{{user, "browser"}, {premium, "view"}, {admin, "appView"}, {user, "browser"}} {
		token, err := tc.user.NewAuthToken()
		must(err)
		req := httptest.NewRequest("GET", "/api/collections/pages/records/"+page.Id+"?expand=banner", nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("read %d %s", w.Code, w.Body)
		}
		var data struct {
			Expand struct {
				Banner struct {
					Link Value `json:"link"`
				} `json:"banner"`
			} `json:"expand"`
		}
		must(json.Unmarshal(w.Body.Bytes(), &data))
		if data.Expand.Banner.Link.Mode != tc.mode {
			t.Fatalf("wrong response override: %s", w.Body)
		}
	}
	stored, err := app.FindRecordById(banners, banner.Id)
	must(err)
	value, err := Decode([]byte(stored.GetString("link")))
	must(err)
	if value.Mode != "appView" {
		t.Fatal("response override persisted to database")
	}
}
