package typedconfig

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NikitaVasin/pocket_mfo/plugins/schemalock"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/types"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	app                 *pocketbase.PocketBase
	source, host        *core.Collection
	offer, screen, user *core.Record
	handler             http.Handler
	admin, auth         string
}

func schema(source string) Schema {
	return Schema{Version: 1, Definitions: map[string]Node{"style": {Kind: "object", Fields: []Property{{Name: "compact", Label: "Компактно", Required: true, Node: Node{Kind: "boolean"}}}}}, Types: []Block{
		{Key: "heading", Label: "Заголовок", Fields: []Property{{Name: "text", Label: "Текст", Required: true, Node: Node{Kind: "string", MaxLength: 80}}, {Name: "style", Label: "Оформление", Node: Node{Ref: "style"}}}},
		{Key: "offer", Label: "Оффер", Fields: []Property{{Name: "offer", Label: "Предложение", Required: true, Node: Node{Kind: "reference", Source: &Source{Collection: source, LabelField: "title", Mapping: map[string]string{"title": "title", "rating": "rating"}}, Fields: []Property{{Name: "title", Label: "Название", Required: true, Node: Node{Kind: "string"}}, {Name: "rating", Label: "Рейтинг", Required: true, Node: Node{Kind: "number"}}}}}}},
		{Key: "group", Label: "Группа", Fields: []Property{{Name: "offers", Label: "Предложения", Required: true, Node: Node{Kind: "array", Items: &Node{Kind: "reference", Source: &Source{Collection: source}}}}}},
	}}
}
func setup(t *testing.T, locked bool) *fixture {
	t.Helper()
	x := &fixture{app: pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})}
	variants.Register(x.app)
	Register(x.app)
	Register(x.app)
	if locked {
		schemalock.Register(x.app)
	}
	must(t, x.app.Bootstrap())
	t.Cleanup(func() { _ = x.app.ClearBootstrap() })
	x.source = core.NewBaseCollection("offers")
	x.source.ListRule = types.Pointer("")
	x.source.ViewRule = types.Pointer("")
	x.source.Fields.Add(&core.TextField{Name: "title"}, &core.NumberField{Name: "rating"}, &core.TextField{Name: "secret", Hidden: true})
	must(t, x.app.Save(x.source))
	x.offer = core.NewRecord(x.source)
	x.offer.Set("title", "Тестовый оффер")
	x.offer.Set("rating", 4.5)
	must(t, x.app.Save(x.offer))
	x.host = core.NewBaseCollection("screens")
	x.host.ListRule = types.Pointer("")
	x.host.ViewRule = types.Pointer("")
	x.host.CreateRule = types.Pointer("")
	x.host.UpdateRule = types.Pointer("")
	x.host.Fields.Add(&Field{JSONField: core.JSONField{Name: "items"}, Schema: schema(x.source.Id)})
	must(t, x.app.Save(x.host))
	x.screen = core.NewRecord(x.host)
	x.screen.Set("items", []Item{{ID: "heading", Type: "heading", Data: map[string]any{"text": "Витрина"}}, {ID: "offer", Type: "offer", Data: map[string]any{"offer": map[string]any{"id": x.offer.Id}}}, {ID: "group", Type: "group", Data: map[string]any{"offers": []any{map[string]any{"id": x.offer.Id}}}}})
	must(t, x.app.Save(x.screen))
	users := core.NewAuthCollection("members")
	must(t, x.app.Save(users))
	x.user = core.NewRecord(users)
	x.user.SetEmail("user@example.test")
	x.user.SetPassword("password-12345")
	must(t, x.app.Save(x.user))
	var err error
	x.auth, err = x.user.NewAuthToken()
	must(t, err)
	su, err := x.app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	must(t, err)
	admin := core.NewRecord(su)
	admin.SetEmail("admin@example.test")
	admin.SetPassword("password-12345")
	must(t, x.app.Save(admin))
	x.admin, err = admin.NewAuthToken()
	must(t, err)
	router, err := apis.NewRouter(x.app)
	must(t, err)
	must(t, x.app.OnServe().Trigger(&core.ServeEvent{App: x.app, Router: router}, func(*core.ServeEvent) error { return nil }))
	x.handler, err = router.BuildMux()
	must(t, err)
	return x
}
func (x *fixture) request(method, path, auth, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", auth)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	x.handler.ServeHTTP(w, r)
	return w
}
func (x *fixture) items(t *testing.T) []Item {
	r, err := x.app.FindRecordById(x.host, x.screen.Id)
	must(t, err)
	items, err := Decode([]byte(r.GetString("items")))
	must(t, err)
	return items
}
func TestProjectionPermissionsAndNativeAPI(t *testing.T) {
	for _, locked := range []bool{false, true} {
		t.Run(map[bool]string{false: "unlocked", true: "locked"}[locked], func(t *testing.T) {
			x := setup(t, locked)
			path := "/api/collections/screens/records/" + x.screen.Id
			for _, auth := range []string{"", x.auth, x.admin} {
				w := x.request("GET", path, auth, "")
				if w.Code != 200 {
					t.Fatalf("%d %s", w.Code, w.Body)
				}
				var body map[string]any
				must(t, json.Unmarshal(w.Body.Bytes(), &body))
				for name := range companions(x.host) {
					if _, ok := body[name]; ok {
						t.Fatal("service relation exposed")
					}
				}
				data := body["items"].([]any)[1].(map[string]any)["data"].(map[string]any)["offer"].(map[string]any)
				if auth == x.admin {
					if data["id"] != x.offer.Id {
						t.Fatal("admin lost editable reference")
					}
				} else if data["title"] != "Тестовый оффер" || data["rating"] != 4.5 {
					t.Fatal(data)
				}
			}
			x.source.ViewRule = nil
			must(t, x.app.Save(x.source))
			w := x.request("GET", path, x.auth, "")
			var body struct {
				Items []Item `json:"items"`
			}
			must(t, json.Unmarshal(w.Body.Bytes(), &body))
			if len(body.Items) != 1 {
				t.Fatal("restricted source leaked", w.Body)
			}
			for _, auth := range []string{"", x.auth} {
				w := x.request("PATCH", path, auth, `{"items":[]}`)
				if w.Code != 403 {
					t.Fatal("non-admin wrote configuration", w.Code)
				}
			}
			for name := range companions(x.host) {
				w := x.request("PATCH", path, x.admin, `{"`+name+`":[]}`)
				if w.Code != 400 {
					t.Fatal("managed companion writable", w.Code)
				}
			}
			if len(x.items(t)) != 3 {
				t.Fatal("rejected request changed content")
			}
		})
	}
}
func TestNestedDependenciesCascadeAndRollback(t *testing.T) {
	x := setup(t, false)
	id := "deny-deletion"
	x.app.OnRecordDeleteExecute(x.source.Name).Bind(&hook.Handler[*core.RecordEvent]{Id: id, Priority: 200, Func: func(e *core.RecordEvent) error { return errors.New("rollback") }})
	if err := x.app.Delete(x.offer); err == nil {
		t.Fatal("expected rollback")
	}
	if len(x.items(t)) != 3 {
		t.Fatal("failed deletion removed items")
	}
	x.app.OnRecordDeleteExecute(x.source.Name).Unbind(id)
	must(t, x.app.Delete(x.offer))
	items := x.items(t)
	if len(items) != 1 || items[0].ID != "heading" {
		t.Fatalf("dependent blocks survived: %+v", items)
	}
	r, err := x.app.FindRecordById(x.host, x.screen.Id)
	must(t, err)
	for name := range companions(x.host) {
		if len(r.GetStringSlice(name)) != 0 {
			t.Fatal("stale relation")
		}
	}
}
func TestValidationAndSchemaRollback(t *testing.T) {
	x := setup(t, false)
	original := x.screen.GetString("items")
	for _, bad := range []string{`[{"id":"x","type":"heading","data":{"text":12}}]`, `[{"id":"x","type":"heading","data":{"text":"ok","oops":1}}]`, `[{"id":"x","type":"missing","data":{}}]`, `[{"id":"x","type":"heading","data":{}},{"id":"x","type":"heading","data":{}}]`, `[{"id":"x","type":"offer","data":{"offer":{"id":"notexisting0001"}}}]`} {
		x.screen.Set("items", json.RawMessage(bad))
		if err := x.app.SaveNoValidate(x.screen); err == nil {
			t.Fatal("invalid unvalidated save accepted", bad)
		}
		r, err := x.app.FindRecordById(x.host, x.screen.Id)
		must(t, err)
		if r.GetString("items") != original {
			t.Fatal("failed update changed content")
		}
	}
	f := x.host.Fields.GetByName("items").(*Field)
	f.Schema.Version++
	f.Schema.Types[0].Fields[0].Node.Kind = "number"
	if err := x.app.Save(x.host); err == nil {
		t.Fatal("incompatible existing content accepted")
	}
	c, err := x.app.FindCollectionByNameOrId(x.host.Id)
	must(t, err)
	if c.Fields.GetByName("items").(*Field).Schema.Version != 1 {
		t.Fatal("schema not rolled back")
	}
	x.source.Fields.GetByName("title").SetHidden(true)
	if err := x.app.Save(x.source); err == nil {
		t.Fatal("mapped field may become secret")
	}
}
func TestVariantsContextCoversProjectedContent(t *testing.T) {
	x := setup(t, true)
	_, err := variants.Publish(x.app, variants.Config{Collection: x.host.Id, AuthCollection: x.user.Collection().Id, Variables: true, Default: variants.Variant{Key: "default"}})
	must(t, err)
	path := "/api/collections/screens/records/" + x.screen.Id
	get := func() variants.Exposure {
		w := x.request("GET", path, x.auth, "")
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
		var body struct {
			Context variants.Exposure `json:"variantContext"`
		}
		must(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body.Context
	}
	before := get()
	x.offer.Set("title", "Новая редакция")
	must(t, x.app.Save(x.offer))
	after := get()
	if before.Revision == after.Revision || after.Token == "" {
		t.Fatal("projection missing from experiment revision")
	}
	contexts, err := variants.VerifyExposures(x.user, []string{before.Token})
	must(t, err)
	if contexts[0].Revision != before.Revision {
		t.Fatal("historic attribution changed")
	}
}

func TestSourceEditAndRenamePreserveProjectionContract(t *testing.T) {
	x := setup(t, false)
	x.offer.Set("title", "")
	if err := x.app.SaveNoValidate(x.offer); err == nil {
		t.Fatal("source edit invalidated required projected value")
	}
	saved, err := x.app.FindRecordById(x.source, x.offer.Id)
	must(t, err)
	if saved.GetString("title") != "Тестовый оффер" {
		t.Fatal("source edit wasn't rolled back")
	}
	title := x.source.Fields.GetByName("title")
	title.SetName("renamed_title")
	must(t, x.app.Save(x.source))
	w := x.request("GET", "/api/collections/screens/records/"+x.screen.Id, x.auth, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Тестовый оффер") {
		t.Fatal("mapping broke on source field rename", w.Body)
	}
	if err := x.app.Delete(x.source); err == nil {
		t.Fatal("source collection deletion broke declared schema")
	}
}

func TestDeletingOneItemRetainsSharedSource(t *testing.T) {
	x := setup(t, true)
	w := x.request("PATCH", "/api/collections/screens/records/"+x.screen.Id, x.admin, `{"items":[]}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	if _, err := x.app.FindRecordById(x.source, x.offer.Id); err != nil {
		t.Fatal("removing item deleted shared source")
	}
	w = x.request("DELETE", "/api/collections/screens/records/"+x.screen.Id, x.auth, "")
	if w.Code == 204 {
		t.Fatal("ordinary user deleted config")
	}
}
