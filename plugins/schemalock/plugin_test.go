package schemalock

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

type fixture struct {
	app                    *pocketbase.PocketBase
	h                      http.Handler
	token                  string
	content, users, system *core.Collection
	record, systemRecord   *core.Record
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T, lockFirst bool) *fixture {
	t.Helper()
	a := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if lockFirst {
		Register(a)
	}
	polymorphicrelation.Register(a)
	variants.Register(a)
	singleton.Register(a)
	Register(a)
	Register(a)
	must(t, a.Bootstrap())
	t.Cleanup(func() { _ = a.ClearBootstrap() })
	a.Settings().Batch.Enabled = true
	a.Settings().Batch.MaxRequests = 20
	a.Settings().RateLimits.Enabled = false
	must(t, a.Save(a.Settings()))
	u := core.NewAuthCollection("members")
	u.Fields.Add(&core.BoolField{Name: "premium"})
	must(t, a.Save(u))
	c := core.NewBaseCollection("content")
	c.ListRule, c.ViewRule = types.Pointer(""), types.Pointer("")
	c.Fields.Add(&core.TextField{Name: "title"}, &core.FileField{Name: "attachment", MaxSelect: 1})
	must(t, a.Save(c))
	r := core.NewRecord(c)
	r.Set("title", "original")
	must(t, a.Save(r))
	s := core.NewBaseCollection("system_content")
	s.System = true
	s.Fields.Add(&core.TextField{Name: "title"})
	must(t, a.Save(s))
	sr := core.NewRecord(s)
	sr.Set("title", "system original")
	must(t, a.Save(sr))
	su, err := a.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	must(t, err)
	admin := core.NewRecord(su)
	admin.SetEmail("test@example.test")
	admin.SetPassword("test-password-123")
	must(t, a.Save(admin))
	token, err := admin.NewAuthToken()
	must(t, err)
	router, err := apis.NewRouter(a)
	must(t, err)
	// Even future custom endpoints are denied unless explicitly allowed in code.
	router.POST("/api/custom-schema", func(e *core.RequestEvent) error { t.Error("forbidden handler executed"); return e.NoContent(204) })
	se := &core.ServeEvent{App: a, Router: router}
	must(t, a.OnServe().Trigger(se, func(e *core.ServeEvent) error { return nil }))
	if len(se.UIExtensions) != 4 || se.UIExtensions[3].Name != "schemalock" {
		t.Fatalf("extension order: %+v", se.UIExtensions)
	}
	h, err := router.BuildMux()
	must(t, err)
	return &fixture{a, h, token, c, u, s, r, sr}
}

func (x *fixture) request(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		var err error
		b, err = json.Marshal(body)
		must(t, err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Authorization", x.token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	x.h.ServeHTTP(w, req)
	return w
}

func expect(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body.String())
	}
}

func (x *fixture) snapshot(t *testing.T) string {
	t.Helper()
	cols, err := x.app.FindAllCollections()
	must(t, err)
	var schema []struct {
		Name string `db:"name"`
		SQL  string `db:"sql"`
	}
	must(t, x.app.DB().NewQuery("SELECT name, COALESCE(sql, '') AS sql FROM sqlite_master ORDER BY name").All(&schema))
	r, err := x.app.FindRecordById(x.content, x.record.Id)
	must(t, err)
	s, err := x.app.FindRecordById(x.system, x.systemRecord.Id)
	must(t, err)
	b, err := json.Marshal([]any{cols, schema, r, s, x.app.Settings()})
	must(t, err)
	return string(b)
}

func TestForbiddenAPIsDoNotMutate(t *testing.T) {
	x := setup(t, true)
	before := x.snapshot(t)
	cases := []struct{ method, path string }{
		{"POST", "/api/collections"}, {"PUT", "/api/collections/import"},
		{"POST", "/api/collections/meta/dry-run-view"}, {"POST", "/api/sql"},
		{"POST", "/api/backups/test.zip/restore"},
		{"PUT", "/api/singleton/admin/collections/content"}, {"POST", "/api/custom-schema"},
		{"GET", "/api/unknown"}, {"GET", "/api"},
	}
	for _, id := range []string{x.content.Id, x.content.Name} {
		for _, method := range []string{"PATCH", "DELETE"} {
			cases = append(cases, struct{ method, path string }{method, "/api/collections/" + id})
		}
		cases = append(cases, struct{ method, path string }{"DELETE", "/api/collections/" + id + "/truncate"})
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			expect(t, x.request(t, c.method, c.path, map[string]any{"name": "hacked", "query": "DROP TABLE content", "deleteMissing": true, "collections": []any{}, "enabled": true}), 403)
			if after := x.snapshot(t); after != before {
				t.Fatal("forbidden request changed persistent state")
			}
		})
	}
	// The server policy cannot be bypassed by dropping authentication.
	x.token = ""
	expect(t, x.request(t, "POST", "/api/sql", nil), 403)
}

func TestAdministrationRemainsAvailable(t *testing.T) {
	x := setup(t, false)
	for _, path := range []string{"/api/crons", "/api/logs", "/api/logs/stats", "/api/backups"} {
		expect(t, x.request(t, "GET", path, nil), 200)
	}
	expect(t, x.request(t, "PATCH", "/api/settings", map[string]any{"meta": map[string]any{"appName": "Schema Lock settings test"}}), 200)
	if x.app.Settings().Meta.AppName != "Schema Lock settings test" {
		t.Fatal("settings update was not saved")
	}
	ran := make(chan struct{}, 1)
	must(t, x.app.Cron().Add("schemalock-test", "0 0 1 1 *", func() { ran <- struct{}{} }))
	expect(t, x.request(t, "POST", "/api/crons/schemalock-test", nil), 204)
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("cron did not run")
	}
	// Export uses the read-only collections endpoint.
	expect(t, x.request(t, "GET", "/api/collections?perPage=500", nil), 200)
	// Administrative access doesn't grant collection or restore permissions.
	expect(t, x.request(t, "PATCH", "/api/collections/content", map[string]any{"name": "renamed"}), 403)
	expect(t, x.request(t, "POST", "/api/backups/test.zip/restore", nil), 403)
	x.token = ""
	for _, path := range []string{"/api/settings", "/api/crons", "/api/logs", "/api/backups"} {
		w := x.request(t, "GET", path, nil)
		if w.Code != 401 && w.Code != 403 {
			t.Fatalf("unauthenticated access to %s: %d", path, w.Code)
		}
	}
}

func TestRecordCRUDAndSystemProtection(t *testing.T) {
	x := setup(t, false)
	for _, path := range []string{"/api/settings", "/api/health", "/api/collections", "/api/collections/meta/scaffolds", "/api/collections/meta/oauth2-providers", "/api/collections/content", "/api/singleton/admin/collections/content"} {
		expect(t, x.request(t, "GET", path, nil), 200)
	}
	expect(t, x.request(t, "POST", "/api/collections/_superusers/auth-with-password", map[string]any{"identity": "test@example.test", "password": "test-password-123"}), 200)
	expect(t, x.request(t, "POST", "/api/collections/_superusers/auth-refresh", nil), 200)
	for _, collection := range []string{"content", "_superusers"} {
		body := map[string]any{"title": "created"}
		if collection == "_superusers" {
			body = map[string]any{"email": "second@example.test", "password": "test-password-123", "passwordConfirm": "test-password-123"}
		}
		w := x.request(t, "POST", "/api/collections/"+collection+"/records", body)
		expect(t, w, 200)
		var r struct {
			ID string `json:"id"`
		}
		must(t, json.Unmarshal(w.Body.Bytes(), &r))
		path := "/api/collections/" + collection + "/records/" + r.ID
		expect(t, x.request(t, "GET", path, nil), 200)
		update := map[string]any{"title": "updated"}
		if collection == "_superusers" {
			update = map[string]any{"email": "updated@example.test"}
		}
		expect(t, x.request(t, "PATCH", path, update), 200)
		if collection == "_superusers" {
			admin, err := x.app.FindRecordById(collection, r.ID)
			must(t, err)
			token, err := admin.NewAuthToken()
			must(t, err)
			old := x.token
			x.token = token
			expect(t, x.request(t, "POST", "/api/sql", nil), 403)
			x.token = old
		}
		expect(t, x.request(t, "DELETE", path, nil), 204)
	}
	for _, id := range []string{x.system.Name, x.system.Id} {
		base := "/api/collections/" + id + "/records"
		expect(t, x.request(t, "GET", base, nil), 200)
		expect(t, x.request(t, "POST", base, map[string]any{"title": "hacked"}), 403)
		expect(t, x.request(t, "PATCH", base+"/"+x.systemRecord.Id, map[string]any{"title": "hacked"}), 403)
		expect(t, x.request(t, "DELETE", base+"/"+x.systemRecord.Id, nil), 403)
	}
}

func TestBatchCannotBypassProtection(t *testing.T) {
	x := setup(t, false)
	for _, action := range []map[string]any{
		{"method": "POST", "url": "/api/collections", "body": map[string]any{"name": "hacked", "type": "base"}},
		{"method": "POST", "url": "/api/sql", "body": map[string]any{"query": "DROP TABLE content"}},
		{"method": "POST", "url": "/api/collections/system_content/records", "body": map[string]any{"title": "hacked"}},
		{"method": "PATCH", "url": "/api/collections/system_content/records/" + x.systemRecord.Id, "body": map[string]any{"title": "hacked"}},
		{"method": "PUT", "url": "/api/collections/system_content/records", "body": map[string]any{"id": x.systemRecord.Id, "title": "hacked"}},
		{"method": "DELETE", "url": "/api/collections/system_content/records/" + x.systemRecord.Id},
	} {
		before := x.snapshot(t)
		w := x.request(t, "POST", "/api/batch", map[string]any{"requests": []any{
			map[string]any{"method": "PATCH", "url": "/api/collections/content/records/" + x.record.Id, "body": map[string]any{"title": "must rollback"}}, action,
		}})
		expect(t, w, 400) // PocketBase wraps the internal 403 as a failed transaction.
		if x.snapshot(t) != before {
			t.Fatal("failed batch was not atomic")
		}
	}
	w := x.request(t, "POST", "/api/batch", map[string]any{"requests": []any{map[string]any{"method": "PATCH", "url": "/api/collections/content/records/" + x.record.Id, "body": map[string]any{"title": "batch update"}}}})
	expect(t, w, 200)
	r, err := x.app.FindRecordById(x.content, x.record.Id)
	must(t, err)
	if r.GetString("title") != "batch update" {
		t.Fatal("batch update missing")
	}
}

func TestVariantsRulesAreLockedButCodeIsTrusted(t *testing.T) {
	x := setup(t, true)
	path := "/api/variants/admin/collections/" + x.content.Id
	cfg := variants.Config{Collection: x.content.Id, AuthCollection: x.users.Id, Variables: true, ListRule: types.Pointer(""), ViewRule: types.Pointer(""), Default: variants.Variant{Key: "default"}}
	before := x.snapshot(t)
	bad := cfg
	bad.ListRule = nil
	expect(t, x.request(t, "PUT", path, bad), 403)
	if x.snapshot(t) != before {
		t.Fatal("initial rule override mutated schema")
	}
	w := x.request(t, "PUT", path, cfg)
	expect(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &cfg))
	for _, field := range []string{"list", "view"} {
		before = x.snapshot(t)
		bad = cfg
		if field == "list" {
			bad.ListRule = types.Pointer("id != ''")
		} else {
			bad.ViewRule = nil
		}
		expect(t, x.request(t, "PUT", path, bad), 403)
		if x.snapshot(t) != before {
			t.Fatal("rule override mutated schema")
		}
	}
	cfg.Variants = []variants.Variant{{Key: "premium", Name: "Premium", Condition: &variants.Condition{Kind: "field", Field: "premium", Op: "eq", Value: true}}}
	w = x.request(t, "PUT", path, cfg)
	expect(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &cfg))
	expect(t, x.request(t, "GET", path, nil), 200)
	sets, err := x.app.FindAllRecords("pv_sets")
	must(t, err)
	for _, s := range sets {
		expect(t, x.request(t, "PATCH", "/api/collections/pv_sets/records/"+s.Id, map[string]any{"name": "direct"}), 403)
	}
	cfg.ListRule = types.Pointer("id != ''")
	saved, err := variants.Publish(x.app, cfg)
	must(t, err)
	if !reflect.DeepEqual(saved.ListRule, cfg.ListRule) {
		t.Fatal("Go Publish must retain rule editing")
	}
	// Model operations and Singleton configuration still work from Go.
	x.systemRecord.Set("title", "from code")
	must(t, x.app.Save(x.systemRecord))
	c := core.NewBaseCollection("from_code")
	must(t, x.app.Save(c))
	c.Fields.Add(&core.TextField{Name: "new_field"})
	must(t, x.app.Save(c))
	_, err = singleton.Configure(x.app, singleton.Config{Collection: c.Id, Enabled: true})
	must(t, err)
	_, err = singleton.Configure(x.app, singleton.Config{Collection: c.Id, Enabled: false})
	must(t, err)
	must(t, x.app.Delete(c))
}

func TestFilesAndRealtime(t *testing.T) {
	x := setup(t, false)
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	file, err := mw.CreateFormFile("attachment", "test.txt")
	must(t, err)
	_, err = io.WriteString(file, "file contents")
	must(t, err)
	must(t, mw.Close())
	req := httptest.NewRequest("PATCH", "/api/collections/content/records/"+x.record.Id, &b)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", x.token)
	w := httptest.NewRecorder()
	x.h.ServeHTTP(w, req)
	expect(t, w, 200)
	r, err := x.app.FindRecordById(x.content, x.record.Id)
	must(t, err)
	w = x.request(t, "GET", "/api/files/content/"+r.Id+"/"+r.GetString("attachment"), nil)
	expect(t, w, 200)
	if w.Body.String() != "file contents" {
		t.Fatal("file data mismatch")
	}
	expect(t, x.request(t, "POST", "/api/files/token", nil), 200)
	server := httptest.NewServer(x.h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err = http.NewRequestWithContext(ctx, "GET", server.URL+"/api/realtime", nil)
	must(t, err)
	res, err := http.DefaultClient.Do(req)
	must(t, err)
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var data struct {
			ClientID string `json:"clientId"`
		}
		must(t, json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &data))
		if data.ClientID == "" {
			t.Fatal("missing realtime client")
		}
		payload, err := json.Marshal(map[string]any{"clientId": data.ClientID, "subscriptions": []string{"content/*"}})
		must(t, err)
		// Subscription must originate from the same IP as the SSE connection.
		sub, err := http.NewRequestWithContext(ctx, "POST", server.URL+"/api/realtime", bytes.NewReader(payload))
		must(t, err)
		sub.Header.Set("Content-Type", "application/json")
		sub.Header.Set("Authorization", x.token)
		response, err := http.DefaultClient.Do(sub)
		must(t, err)
		defer response.Body.Close()
		if response.StatusCode != 204 {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("realtime subscription: %d %s", response.StatusCode, body)
		}
		return
	}
	t.Fatalf("realtime connect missing: %v", scanner.Err())
}
