package polymorphicrelation

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestReferenceKeysAreCaseSensitive(t *testing.T) {
	x := setup(t, Restrict, false)
	for _, value := range []any{
		map[string]any{"CollectionId": x.articles.Id, "RecordId": x.article.Id},
		map[string]any{"collectionId": x.articles.Id, "RECORDID": x.article.Id},
	} {
		r := core.NewRecord(x.comments)
		r.Set("subject", value)
		if err := x.app.Save(r); err == nil {
			t.Fatalf("accepted noncanonical reference: %#v", value)
		}
	}
}

func TestCompanionIDCollisionPreservesExistingField(t *testing.T) {
	x := setup(t, Restrict, false)
	f := &Field{JSONField: core.JSONField{Id: "another_subject", Name: "other"}, CollectionIDs: []string{x.articles.Id}}
	name := ServiceFieldName(f.Id, x.articles.Id)
	x.comments.Fields.Add(&core.RelationField{Id: name, Name: "manualParent", CollectionId: x.articles.Id, MaxSelect: 1})
	mustSave(t, x.app, x.comments)
	r := x.comment(t, nil)
	r.Set("manualParent", x.article.Id)
	mustSave(t, x.app, r)
	x.comments.Fields.Add(f)
	if err := x.app.Save(x.comments); err == nil {
		t.Fatal("silently replaced an existing field with a generated companion")
	}
	if got := read(t, x.app, x.comments.Id, r.Id).GetString("manualParent"); got != x.article.Id {
		t.Fatal("existing relation was lost")
	}
}

func TestAuthResponseExpand(t *testing.T) {
	x := setup(t, Restrict, false)
	users := core.NewAuthCollection("members")
	users.AuthRule = types.Pointer("")
	users.Fields.Add(&Field{JSONField: core.JSONField{Name: "subject"}, CollectionIDs: []string{x.articles.Id}})
	mustSave(t, x.app, users)
	u := core.NewRecord(users)
	u.SetEmail("member@example.test")
	u.SetPassword("test-password-123")
	u.Set("subject", Reference{x.articles.Id, x.article.Id})
	mustSave(t, x.app, u)
	out := request(t, handler(t, x.app), "POST", "/api/collections/members/auth-with-password?expand=subject", map[string]any{
		"identity": "member@example.test", "password": "test-password-123",
	}, 200)
	record := out["record"].(map[string]any)
	expand, _ := record["expand"].(map[string]any)
	parent, _ := expand["subject"].(map[string]any)
	if parent["id"] != x.article.Id || parent["secret"] != nil {
		t.Fatalf("auth response lost expand or leaked a hidden field: %#v", record)
	}
}

func TestRealtimeEnrichmentPreservesRuleContext(t *testing.T) {
	x := setup(t, Restrict, false)
	users := core.NewAuthCollection("members")
	users.ManageRule = types.Pointer("@request.context = 'default'")
	users.Fields.Add(&Field{JSONField: core.JSONField{Name: "subject"}, CollectionIDs: []string{x.articles.Id}})
	mustSave(t, x.app, users)
	u := core.NewRecord(users)
	u.SetEmail("private@example.test")
	u.SetPassword("test-password-123")
	u.Set("subject", Reference{x.articles.Id, x.article.Id})
	mustSave(t, x.app, u)
	info := &core.RequestInfo{Context: core.RequestInfoContextRealtime, Method: "GET", Query: map[string]string{"expand": "subject"}}
	if err := x.app.OnRecordEnrich().Trigger(&core.RecordEnrichEvent{App: x.app, Record: u, RequestInfo: info}); err != nil {
		t.Fatal(err)
	}
	if u.PublicExport()["email"] != nil {
		t.Fatal("realtime applied the HTTP context ManageRule and disclosed the email")
	}
	if info.Query["expand"] != "subject" {
		t.Fatal("modified shared subscription options")
	}
}

func TestSaveNoValidatePreservesRelationInvariant(t *testing.T) {
	x := setup(t, Restrict, true)
	r := x.comment(t, x.article)
	r.Set(ServiceFieldName(x.field.Id, x.videos.Id), x.video.Id)
	if err := x.app.SaveNoValidate(r); err != nil {
		t.Fatal(err)
	}
	stored := read(t, x.app, x.comments.Id, r.Id)
	if stored.GetString(ServiceFieldName(x.field.Id, x.articles.Id)) != x.article.Id || stored.GetString(ServiceFieldName(x.field.Id, x.videos.Id)) != "" {
		t.Fatal("direct model write broke companion consistency")
	}
	for _, invalid := range []any{nil, Reference{x.articles.Id, "missing00000000"}} {
		stored.Set("subject", invalid)
		if err := x.app.SaveNoValidate(stored); err == nil {
			t.Fatal("SaveNoValidate bypassed relation validation")
		}
	}
	ref, err := x.field.reference(read(t, x.app, x.comments.Id, r.Id))
	if err != nil || ref == nil || ref.RecordID != x.article.Id {
		t.Fatal("failed write changed the stored reference")
	}
}
