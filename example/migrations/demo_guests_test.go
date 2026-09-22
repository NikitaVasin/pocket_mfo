package migrations

import (
	"bytes"
	"encoding/json"
	"github.com/NikitaVasin/pocket_mfo/plugins/partnerlinks"
	"github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
	"github.com/NikitaVasin/pocket_mfo/plugins/schemalock"
	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStandardGuestAuthAndUsersRename(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	polymorphicrelation.Register(app)
	variants.Register(app)
	singleton.Register(app)
	partnerlinks.Register(app, partnerlinks.Options{AuthCollections: []string{"users"}})
	schemalock.Register(app)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(app.Bootstrap())
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	check(app.RunAppMigrations())
	c, err := app.FindCollectionByNameOrId("users")
	check(err)
	if c.Id != demoMembersID {
		t.Fatal("auth collection identity changed")
	}
	// An older database keeps records and relation targets through rename.
	c.Name = "demo_members"
	check(app.Save(c))
	check(ensureDemoGuests(app))
	c, err = app.FindCollectionByNameOrId("users")
	check(err)
	if _, err := app.FindCollectionByNameOrId("demo_members"); err == nil {
		t.Fatal("old collection remains")
	}
	if _, err := app.FindAuthRecordByEmail(c, demoMembers[0].email); err != nil {
		t.Fatal("lost user")
	}
	router, err := apis.NewRouter(app)
	check(err)
	check(app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}, func(*core.ServeEvent) error { return nil }))
	handler, err := router.BuildMux()
	check(err)
	request := func(path, token string, body map[string]any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	input := map[string]any{"content_bucket": (variants.Bucket(c.Id, "guesttest000001") + 1) % 10000, "id": "guesttest000001", "email": "", "verified": false, "password": "long-guest-secret-123456789", "passwordConfirm": "long-guest-secret-123456789"}
	path := "/api/collections/users/records"
	w := request(path, "", input)
	if w.Code != 200 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	r, err := app.FindRecordById(c, "guesttest000001")
	check(err)
	if r.GetString("password") == input["password"] || !r.ValidatePassword(input["password"].(string)) {
		t.Fatal("password not hashed")
	}
	if r.GetInt("content_bucket") != variants.Bucket(c.Id, r.Id) {
		t.Fatal("client overrode server bucket")
	}
	auth := map[string]any{"identity": r.Id, "identityField": "id", "password": input["password"]}
	for i := 0; i < 2; i++ {
		w = request("/api/collections/users/auth-with-password", "", auth)
		if w.Code != 200 {
			t.Fatalf("auth %d %s", w.Code, w.Body)
		}
	}
	var response struct {
		Token string `json:"token"`
	}
	check(json.Unmarshal(w.Body.Bytes(), &response))
	auth["password"] = "wrong"
	if request("/api/collections/users/auth-with-password", "", auth).Code != 400 {
		t.Fatal("wrong password accepted")
	}
	if request(path, "", input).Code != 400 {
		t.Fatal("duplicate id accepted")
	}
	before, err := app.CountRecords(c.Id)
	check(err)
	for key, value := range map[string]any{"tier": "premium", "name": "Injected", "email": "ordinary@example.test", "verified": true} {
		bad := map[string]any{}
		for k, v := range input {
			bad[k] = v
		}
		bad["id"] = "guesttest000002"
		bad[key] = value
		if request(path, "", bad).Code == 200 {
			t.Fatalf("allowed injected %s", key)
		}
	}
	// Authenticated clients cannot create further users under the guest rule.
	input["id"] = "guesttest000003"
	if request(path, response.Token, input).Code == 200 {
		t.Fatal("authenticated registration accepted")
	}
	after, err := app.CountRecords(c.Id)
	check(err)
	if before != after {
		t.Fatal("failed requests changed records")
	}
}
