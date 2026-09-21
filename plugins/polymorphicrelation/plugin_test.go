package polymorphicrelation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/subscriptions"
	"github.com/pocketbase/pocketbase/tools/types"
)

type fixture struct {
	app                        *pocketbase.PocketBase
	articles, videos, comments *core.Collection
	article, video             *core.Record
	field                      *Field
}

func newApp(t *testing.T) *pocketbase.PocketBase {
	t.Helper()
	a := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	Register(a)
	if err := a.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.ClearBootstrap() })
	return a
}

func setup(t *testing.T, policy DeletePolicy, required bool) *fixture {
	t.Helper()
	x := &fixture{app: newApp(t)}
	x.articles = core.NewBaseCollection("articles")
	x.videos = core.NewBaseCollection("videos")
	for _, c := range []*core.Collection{x.articles, x.videos} {
		c.Fields.Add(&core.TextField{Name: "title"}, &core.TextField{Name: "secret", Hidden: true})
		c.ListRule = types.Pointer("")
		c.ViewRule = types.Pointer("")
		mustSave(t, x.app, c)
	}
	x.article = core.NewRecord(x.articles)
	x.video = core.NewRecord(x.videos)
	for _, r := range []*core.Record{x.article, x.video} {
		r.Id = "sameparent00001"
		r.Set("title", r.Collection().Name)
		r.Set("secret", "private")
		mustSave(t, x.app, r)
	}
	x.comments = core.NewBaseCollection("comments")
	x.field = &Field{JSONField: core.JSONField{Name: "subject", Required: required}, CollectionIDs: []string{x.articles.Id, x.videos.Id}, OnDelete: policy}
	x.comments.Fields.Add(x.field, &core.TextField{Name: "text"})
	x.comments.ListRule = types.Pointer("")
	x.comments.ViewRule = types.Pointer("")
	x.comments.CreateRule = types.Pointer("")
	x.comments.UpdateRule = types.Pointer("")
	mustSave(t, x.app, x.comments)
	return x
}

func mustSave(t *testing.T, app core.App, m core.Model) {
	t.Helper()
	if err := app.Save(m); err != nil {
		t.Fatal(err)
	}
}
func (x *fixture) comment(t *testing.T, parent *core.Record) *core.Record {
	t.Helper()
	r := core.NewRecord(x.comments)
	if parent != nil {
		r.Set("subject", Reference{parent.Collection().Id, parent.Id})
	}
	mustSave(t, x.app, r)
	return r
}
func read(t *testing.T, app core.App, c, id string) *core.Record {
	t.Helper()
	r, err := app.FindRecordById(c, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestStorageAndValidation(t *testing.T) {
	x := setup(t, Restrict, false)
	a := ServiceFieldName(x.field.Id, x.articles.Id)
	v := ServiceFieldName(x.field.Id, x.videos.Id)
	r := x.comment(t, x.article)
	loaded := read(t, x.app, x.comments.Id, r.Id)
	if loaded.GetString(a) != x.article.Id || loaded.GetString(v) != "" {
		t.Fatal("wrong companion columns")
	}
	loaded.Set("subject", Reference{x.videos.Id, x.video.Id})
	mustSave(t, x.app, loaded)
	loaded = read(t, x.app, x.comments.Id, r.Id)
	if loaded.GetString(a) != "" || loaded.GetString(v) != x.video.Id {
		t.Fatal("parent switch did not clear previous type")
	}
	loaded.Set("text", "unrelated patch")
	mustSave(t, x.app, loaded)
	ref, _ := x.field.reference(read(t, x.app, x.comments.Id, r.Id))
	if ref == nil || ref.CollectionID != x.videos.Id {
		t.Fatal("unrelated update lost relation")
	}
	for _, value := range []any{[]string{x.article.Id}, map[string]any{}, Reference{"unknown", x.article.Id}, Reference{x.articles.Id, "missing00000000"}, "not an object"} {
		bad := core.NewRecord(x.comments)
		bad.Set("subject", value)
		if err := x.app.Save(bad); err == nil {
			t.Fatalf("accepted invalid reference: %#v", value)
		}
	}
	loaded.Set("subject", nil)
	mustSave(t, x.app, loaded)
	if got, _ := x.field.reference(read(t, x.app, x.comments.Id, r.Id)); got != nil {
		t.Fatal("expected null")
	}
	if err := x.app.RunInTransaction(func(tx core.App) error {
		loaded.Set("subject", Reference{x.articles.Id, x.article.Id})
		if err := tx.Save(loaded); err != nil {
			return err
		}
		return errors.New("rollback")
	}); err == nil {
		t.Fatal("expected rollback")
	}
	if got, _ := x.field.reference(read(t, x.app, x.comments.Id, r.Id)); got != nil {
		t.Fatal("transaction did not roll back")
	}
}

func TestRequiredAndMultipleFields(t *testing.T) {
	x := setup(t, Restrict, true)
	r := core.NewRecord(x.comments)
	if err := x.app.Save(r); err == nil {
		t.Fatal("required field accepted null")
	}
	second := &Field{JSONField: core.JSONField{Name: "other"}, CollectionIDs: x.field.CollectionIDs}
	x.comments.Fields.Add(second)
	mustSave(t, x.app, x.comments)
	r = core.NewRecord(x.comments)
	r.Set("subject", Reference{x.articles.Id, x.article.Id})
	r.Set("other", Reference{x.videos.Id, x.video.Id})
	mustSave(t, x.app, r)
	if r.GetString(ServiceFieldName(second.Id, x.videos.Id)) != x.video.Id {
		t.Fatal("second field missing")
	}
	x.field.OnDelete = SetNull
	if err := x.app.Save(x.comments); err == nil {
		t.Fatal("required setNull accepted")
	}
}

func TestDeletePolicies(t *testing.T) {
	for _, policy := range []DeletePolicy{Restrict, SetNull, Cascade} {
		t.Run(string(policy), func(t *testing.T) {
			x := setup(t, policy, false)
			r := x.comment(t, x.article)
			other := x.comment(t, x.video)
			err := x.app.Delete(x.article)
			if policy == Restrict {
				if err == nil {
					t.Fatal("restrict allowed deletion")
				}
				read(t, x.app, x.articles.Id, x.article.Id)
				read(t, x.app, x.comments.Id, r.Id)
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if policy == Cascade {
					if _, err := x.app.FindRecordById(x.comments.Id, r.Id); err == nil {
						t.Fatal("child survived cascade")
					}
				} else {
					child := read(t, x.app, x.comments.Id, r.Id)
					ref, err := x.field.reference(child)
					if err != nil || ref != nil || child.GetString(ServiceFieldName(x.field.Id, x.articles.Id)) != "" {
						t.Fatal("setNull did not synchronize", ref, err)
					}
				}
			}
			read(t, x.app, x.comments.Id, other.Id)
		})
	}
}

func TestSchemaLifecycle(t *testing.T) {
	x := setup(t, Restrict, false)
	r := x.comment(t, x.article)
	name := ServiceFieldName(x.field.Id, x.articles.Id)
	x.field.CollectionIDs = []string{x.videos.Id}
	if err := x.app.Save(x.comments); err == nil {
		t.Fatal("removed a referenced type")
	}
	x.comments, _ = x.app.FindCollectionByNameOrId(x.comments.Id)
	x.field = x.comments.Fields.GetByName("subject").(*Field)
	x.field.Name = "parent"
	mustSave(t, x.app, x.comments)
	if read(t, x.app, x.comments.Id, r.Id).GetString(name) != x.article.Id {
		t.Fatal("rename lost relation")
	}
	x.articles.Name = "posts"
	mustSave(t, x.app, x.articles)
	if read(t, x.app, x.comments.Id, r.Id).GetString(name) != x.article.Id {
		t.Fatal("collection rename lost relation")
	}
	third := core.NewBaseCollection("photos")
	mustSave(t, x.app, third)
	x.field.CollectionIDs = append(x.field.CollectionIDs, third.Id)
	mustSave(t, x.app, x.comments)
	if x.comments.Fields.GetByName(ServiceFieldName(x.field.Id, third.Id)) == nil {
		t.Fatal("new target missing")
	}
	x.field.CollectionIDs = []string{x.articles.Id, x.videos.Id}
	mustSave(t, x.app, x.comments)
	if x.comments.Fields.GetByName(ServiceFieldName(x.field.Id, third.Id)) != nil {
		t.Fatal("unused target survived")
	}
	// JSON roundtrip exercises custom type registration used by schema import/export.
	data, err := json.Marshal(x.comments)
	if err != nil {
		t.Fatal(err)
	}
	var copy core.Collection
	if err = json.Unmarshal(data, &copy); err != nil {
		t.Fatal(err)
	}
	if _, ok := copy.Fields.GetByName("parent").(*Field); !ok {
		t.Fatal("lost custom field type")
	}
	x.comments.Fields.RemoveById(x.field.Id)
	mustSave(t, x.app, x.comments)
	if x.comments.Fields.GetByName(name) != nil {
		t.Fatal("orphaned companion")
	}
	for _, idx := range x.comments.Indexes {
		if strings.Contains(idx, "pmr_") {
			t.Fatal("orphaned index")
		}
	}
}

func handler(t *testing.T, app core.App) http.Handler {
	t.Helper()
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return mux
}

func request(t *testing.T, h http.Handler, method, path string, body any, status int) map[string]any {
	t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: want %d, got %d: %s", method, path, status, w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRecordsAPI(t *testing.T) {
	x := setup(t, SetNull, false)
	h := handler(t, x.app)
	base := "/api/collections/comments/records"
	created := request(t, h, "POST", base+"?expand=subject", map[string]any{"subject": Reference{x.articles.Id, x.article.Id}}, 200)
	id := created["id"].(string)
	assertPublic := func(out map[string]any, expanded bool) {
		t.Helper()
		for name := range managed(x.comments) {
			if _, ok := out[name]; ok {
				t.Fatal("service column leaked", name)
			}
		}
		if out["subject"].(map[string]any)["collectionId"] != x.articles.Id {
			t.Fatal("incorrect public reference")
		}
		if expanded {
			parent := out["expand"].(map[string]any)["subject"].(map[string]any)
			if parent["title"] != "articles" || parent["secret"] != nil {
				t.Fatal("wrong expand or leaked secret", parent)
			}
		}
	}
	assertPublic(created, true)
	assertPublic(request(t, h, "GET", base+"/"+id+"?expand=subject", nil, 200), true)
	list := request(t, h, "GET", base+"?expand=subject", nil, 200)
	assertPublic(list["items"].([]any)[0].(map[string]any), true)
	assertPublic(request(t, h, "PATCH", base+"/"+id+"?expand=subject", map[string]any{"text": "updated"}, 200), true)
	name := ServiceFieldName(x.field.Id, x.articles.Id)
	for _, key := range []string{name, name + "+", "+" + name, name + "-"} {
		request(t, h, "PATCH", base+"/"+id, map[string]any{key: x.article.Id}, 400)
	}
	filter := url.QueryEscape(fmt.Sprintf("%s = '%s'", name, x.article.Id))
	filtered := request(t, h, "GET", base+"?filter="+filter, nil, 200)
	if filtered["totalItems"] != float64(1) {
		t.Fatal("native filter failed")
	}
	x.articles.ViewRule = nil
	mustSave(t, x.app, x.articles)
	private := request(t, h, "GET", base+"/"+id+"?expand=subject", nil, 200)
	if exp, ok := private["expand"].(map[string]any); ok && exp["subject"] != nil {
		t.Fatal("expand bypassed ViewRule")
	}
	cleared := request(t, h, "PATCH", base+"/"+id, map[string]any{"subject": nil}, 200)
	if cleared["subject"] != nil {
		t.Fatal("expected JSON null")
	}
}

func TestCreateRuleUsesDerivedCompanions(t *testing.T) {
	x := setup(t, Restrict, false)
	name := ServiceFieldName(x.field.Id, x.articles.Id)
	x.comments.CreateRule = types.Pointer(name + ".title = 'articles'")
	x.comments.ListRule = types.Pointer(name + " != ''")
	mustSave(t, x.app, x.comments)
	h := handler(t, x.app)
	request(t, h, "POST", "/api/collections/comments/records", map[string]any{"subject": Reference{x.articles.Id, x.article.Id}}, 200)
	request(t, h, "POST", "/api/collections/comments/records", map[string]any{"subject": Reference{x.videos.Id, x.video.Id}}, 400)
	request(t, h, "POST", "/api/collections/comments/records", map[string]any{"subject": Reference{x.videos.Id, x.video.Id}, name: x.article.Id}, 400)
	out := request(t, h, "GET", "/api/collections/comments/records", nil, 200)
	if out["totalItems"] != float64(1) {
		t.Fatal("list rule did not use native relation")
	}
}

func TestNativeAuthTargetAndSchemaRestrictions(t *testing.T) {
	x := setup(t, Restrict, false)
	users := core.NewAuthCollection("members")
	users.ViewRule = types.Pointer("")
	mustSave(t, x.app, users)
	user := core.NewRecord(users)
	user.SetEmail("private@example.test")
	user.SetPassword("test-password-123")
	mustSave(t, x.app, user)
	x.field.CollectionIDs = append(x.field.CollectionIDs, users.Id)
	mustSave(t, x.app, x.comments)
	comment := x.comment(t, user)
	out := request(t, handler(t, x.app), "GET", "/api/collections/comments/records/"+comment.Id+"?expand=subject", nil, 200)
	parent := out["expand"].(map[string]any)["subject"].(map[string]any)
	for _, key := range []string{"email", "password", "tokenKey"} {
		if parent[key] != nil {
			t.Fatalf("leaked auth field %s", key)
		}
	}
	if err := x.app.Delete(users); err == nil {
		t.Fatal("deleted target collection still used by schema")
	}
	superusers, _ := x.app.FindCollectionByNameOrId("_superusers")
	x.field.CollectionIDs = []string{superusers.Id}
	if err := x.app.Save(x.comments); err == nil {
		t.Fatal("accepted system target")
	}
}

func TestSchemaImportWithForwardReferences(t *testing.T) {
	x := setup(t, Restrict, false)
	data, err := json.Marshal([]*core.Collection{x.comments, x.videos, x.articles})
	if err != nil {
		t.Fatal(err)
	}
	var exported []map[string]any
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	for _, c := range exported {
		delete(c, "created")
		delete(c, "updated")
	}
	destination := newApp(t)
	if err := destination.ImportCollections(exported, false); err != nil {
		t.Fatal(err)
	}
	imported, err := destination.FindCollectionByNameOrId("comments")
	if err != nil {
		t.Fatal(err)
	}
	f, ok := imported.Fields.GetByName("subject").(*Field)
	if !ok || len(f.Relations) != 2 {
		t.Fatal("custom schema was not restored")
	}
	for _, id := range f.CollectionIDs {
		if imported.Fields.GetByName(ServiceFieldName(f.Id, id)) == nil {
			t.Fatal("missing imported companion")
		}
	}
}

func TestRealtimePublicValueAndExpand(t *testing.T) {
	x := setup(t, SetNull, false)
	h := handler(t, x.app) // registers the native realtime hooks
	client := subscriptions.NewDefaultClient()
	client.Subscribe(`comments/*?options={"query":{"expand":"subject"}}`)
	x.app.SubscriptionsBroker().Register(client)
	t.Cleanup(func() { x.app.SubscriptionsBroker().Unregister(client.Id()); client.Discard() })
	messages := make(chan subscriptions.Message, 10)
	go func() {
		for m := range client.Channel() {
			messages <- m
		}
	}()
	created := request(t, h, "POST", "/api/collections/comments/records", map[string]any{"subject": Reference{x.articles.Id, x.article.Id}}, 200)
	for _, action := range []string{"create", "update"} {
		if action == "update" {
			request(t, h, "PATCH", "/api/collections/comments/records/"+created["id"].(string), map[string]any{"text": "changed"}, 200)
		}
		select {
		case message := <-messages:
			var data struct {
				Action string         `json:"action"`
				Record map[string]any `json:"record"`
			}
			if err := json.Unmarshal(message.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.Action != action {
				t.Fatal("wrong event", data.Action)
			}
			if data.Record["subject"].(map[string]any)["recordId"] != x.article.Id {
				t.Fatal("wrong realtime reference")
			}
			parent := data.Record["expand"].(map[string]any)["subject"].(map[string]any)
			if parent["title"] != "articles" || parent["secret"] != nil {
				t.Fatal("invalid realtime expand", parent)
			}
			for name := range managed(x.comments) {
				if data.Record[name] != nil {
					t.Fatal("realtime leaked companion")
				}
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no realtime event")
		}
	}
	for _, opts := range client.Subscriptions() {
		if opts.Query["expand"] != "subject" {
			t.Fatal("mutated shared subscription options")
		}
	}
}

func TestDeleteClearsMultipleFieldsAndRollsBack(t *testing.T) {
	x := setup(t, SetNull, false)
	second := &Field{JSONField: core.JSONField{Name: "other"}, CollectionIDs: x.field.CollectionIDs, OnDelete: SetNull}
	x.comments.Fields.Add(second)
	mustSave(t, x.app, x.comments)
	child := core.NewRecord(x.comments)
	child.Set("subject", Reference{x.articles.Id, x.article.Id})
	child.Set("other", Reference{x.articles.Id, x.article.Id})
	mustSave(t, x.app, child)
	// A native required relation fails after the plugin's setNull work. Everything must roll back.
	blockers := core.NewBaseCollection("blockers")
	blockers.Fields.Add(&core.RelationField{Name: "parent", CollectionId: x.articles.Id, Required: true, MaxSelect: 1})
	mustSave(t, x.app, blockers)
	blocker := core.NewRecord(blockers)
	blocker.Set("parent", x.article.Id)
	mustSave(t, x.app, blocker)
	if err := x.app.Delete(x.article); err == nil {
		t.Fatal("required native relation should block deletion")
	}
	loaded := read(t, x.app, x.comments.Id, child.Id)
	for _, f := range []*Field{x.field, second} {
		if ref, _ := f.reference(loaded); ref == nil {
			t.Fatal("setNull survived a rollback")
		}
	}
	if err := x.app.Delete(blocker); err != nil {
		t.Fatal(err)
	}
	if err := x.app.Delete(x.article); err != nil {
		t.Fatal(err)
	}
	loaded = read(t, x.app, x.comments.Id, child.Id)
	for _, f := range []*Field{x.field, second} {
		if ref, _ := f.reference(loaded); ref != nil {
			t.Fatal("field was not cleared")
		}
	}
}

func TestCascadeCycle(t *testing.T) {
	x := setup(t, Cascade, false)
	x.field.CollectionIDs = append(x.field.CollectionIDs, x.comments.Id)
	mustSave(t, x.app, x.comments)
	a := x.comment(t, nil)
	b := x.comment(t, a)
	a = read(t, x.app, x.comments.Id, a.Id)
	a.Set("subject", Reference{x.comments.Id, b.Id})
	mustSave(t, x.app, a)
	if err := x.app.Delete(a); err != nil {
		t.Fatal(err)
	}
	for _, r := range []*core.Record{a, b} {
		if _, err := x.app.FindRecordById(x.comments.Id, r.Id); err == nil {
			t.Fatal("cycle not deleted")
		}
	}
}
