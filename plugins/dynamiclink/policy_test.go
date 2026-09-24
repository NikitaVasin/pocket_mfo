package dynamiclink

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
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
	if got.Mode != "browser" {
		t.Fatal("empty experiment must use browser defaults")
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

func TestCategoryOverridesAndValidation(t *testing.T) {
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
	must(app.Save(auth))
	user := core.NewRecord(auth)
	user.SetEmail("category@example.test")
	user.SetPassword("test-password-123")
	must(app.Save(user))
	must(Configure(app, auth.Id))
	rows, err := app.FindAllRecords(SettingsCollection)
	must(err)
	policy := rows[0]
	policy.Set("mode", "view")
	policy.Set("openingOptions", map[string]any{"saveCooke": true, "changeClient": true, "showLoader": false})
	policy.Set("warningPolicy", "replace")
	policy.Set("warningTitle", "Global")
	policy.Set("warningContent", "Terms")
	policy.Set("categories", []Category{{Key: "offers", Label: "Офферы", Options: OpeningOptions{Mode: "appView", SaveCooke: types.Pointer(false), WarningPolicy: "disabled"}}})
	must(app.Save(policy))
	must(Configure(app, auth.Id)) // An upgrade/repeated call must preserve settings.
	source := Value{URL: "https://example.com", Category: "offers", Mode: "browser", Title: "Offer"}
	got, err := Apply(app, user, source)
	must(err)
	if got.Mode != "appView" || got.SaveCooke || got.ShowLoader || !got.ChangeClient || !got.SkipWarningDialog || got.WarningDialog != nil || got.Title != "Offer" || got.Category != "offers" {
		t.Fatalf("category/global precedence: %+v", got)
	}
	source.Category = ""
	got, err = Apply(app, user, source)
	must(err)
	if got.Mode != "view" || !got.SaveCooke || got.WarningDialog == nil || got.WarningDialog.Title != "Global" {
		t.Fatalf("global fallback: %+v", got)
	}
	for _, raw := range []string{
		`[{"key":"offers","label":"One","options":{}},{"key":"offers","label":"Two","options":{}}]`,
		`[{"key":"bad key","label":"One","options":{}}]`,
		`[{"key":"offers","label":"","options":{}}]`,
		`[{"key":"offers","label":"One","options":{"mode":"invalid"}}]`,
		`[{"key":"offers","label":"One","options":{"saveCooke":"false"}}]`,
		`[{"key":"offers","label":"One","options":{"warningPolicy":"replace"}}]`,
		`[{"key":"offers","label":"One","options":{"unknown":true}}]`,
	} {
		policy.Set("categories", json.RawMessage(raw))
		if app.Save(policy) == nil {
			t.Fatalf("accepted invalid categories: %s", raw)
		}
		stored, err := app.FindRecordById(SettingsCollection, policy.Id)
		must(err)
		categories, err := decodeCategories(stored)
		must(err)
		if len(categories) != 1 || categories[0].Key != "offers" {
			t.Fatal("rejected update changed categories")
		}
	}
	banners := core.NewBaseCollection("banners")
	banners.ListRule, banners.ViewRule = types.Pointer(""), types.Pointer("")
	banners.Fields.Add(&Field{JSONField: core.JSONField{Name: "link"}})
	must(app.Save(banners))
	banner := core.NewRecord(banners)
	source.Category = "offers"
	banner.Set("link", source)
	must(app.Save(banner))
	source.Category = "missing"
	banner.Set("link", source)
	if app.Save(banner) == nil {
		t.Fatal("unknown category accepted")
	}
	admins, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	must(err)
	admin := core.NewRecord(admins)
	admin.SetEmail("admin@example.test")
	admin.SetPassword("test-password-123")
	must(app.Save(admin))
	app.Settings().Batch.Enabled = true
	must(app.Save(app.Settings()))
	router, err := apis.NewRouter(app)
	must(err)
	must(app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}, func(*core.ServeEvent) error { return nil }))
	handler, err := router.BuildMux()
	must(err)
	for _, tc := range []struct {
		actor *core.Record
		mode  string
	}{{user, "appView"}, {admin, "browser"}} {
		token, err := tc.actor.NewAuthToken()
		must(err)
		req := httptest.NewRequest("GET", "/api/collections/banners/records/"+banner.Id, nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("read: %d %s", w.Code, w.Body)
		}
		var data struct {
			Link Value `json:"link"`
		}
		must(json.Unmarshal(w.Body.Bytes(), &data))
		if data.Link.Mode != tc.mode {
			t.Fatalf("incorrect response: %s", w.Body)
		}
		req = httptest.NewRequest("PATCH", "/api/collections/"+SettingsCollection+"/records/"+policy.Id, strings.NewReader(`{"categories":[{"key":"offers","label":"Offer","options":{"mode":"invalid"}}]}`))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code < 400 {
			t.Fatalf("invalid/unauthorized policy write: %d %s", w.Code, w.Body)
		}
	}
	// Deletes are forbidden even when Schema Lock is not installed. Batch must
	// roll back an otherwise valid update before the forbidden delete.
	for _, actor := range []*core.Record{nil, user, admin} {
		token := ""
		if actor != nil {
			token, err = actor.NewAuthToken()
			must(err)
		}
		for _, collection := range []string{SettingsCollection, policy.Collection().Id} {
			for _, path := range []string{"/api/collections/" + collection + "/records/" + policy.Id, "/api/collections/" + collection + "/truncate", "/api/collections/" + collection} {
				req := httptest.NewRequest("DELETE", path, nil)
				req.Header.Set("Authorization", token)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				if w.Code < 400 {
					t.Fatalf("policy delete allowed: %s %d", path, w.Code)
				}
			}
		}
	}
	token, err := admin.NewAuthToken()
	must(err)
	batch, err := json.Marshal(map[string]any{"requests": []any{
		map[string]any{"method": "PATCH", "url": "/api/collections/" + SettingsCollection + "/records/" + policy.Id, "body": map[string]any{"mode": "browser"}},
		map[string]any{"method": "DELETE", "url": "/api/collections/" + SettingsCollection + "/records/" + policy.Id},
	}})
	must(err)
	req := httptest.NewRequest("POST", "/api/batch", strings.NewReader(string(batch)))
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("batch delete allowed: %d %s", w.Code, w.Body)
	}
	policy, err = app.FindRecordById(SettingsCollection, policy.Id)
	must(err)
	if policy.GetString("mode") != "view" {
		t.Fatal("failed delete changed policy")
	}
	policy.Set("categories", []Category{})
	must(app.Save(policy))
	source.Category = "offers"
	got, err = Apply(app, user, source)
	must(err)
	if got.Mode != "view" {
		t.Fatal("deleted category must use global policy")
	}
	banner, err = app.FindRecordById(banners, banner.Id)
	must(err)
	must(app.Save(banner)) // Unrelated edits remain possible after deleting a category.
	err = app.RunInTransaction(func(tx core.App) error {
		row, err := tx.FindRecordById(SettingsCollection, policy.Id)
		if err != nil {
			return err
		}
		row.Set("mode", "appView")
		if err := tx.Save(row); err != nil {
			return err
		}
		return fmt.Errorf("rollback")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	got, err = Apply(app, user, source)
	must(err)
	if got.Mode != "view" {
		t.Fatal("transaction escaped rollback")
	}
}
