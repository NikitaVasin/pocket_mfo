package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NikitaVasin/pocket_mfo/plugins/schemalock"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type testFixture struct {
	app         *pocketbase.PocketBase
	s           *Server
	handler     http.Handler
	admin, user string
	owner       *core.Record
}

func setup(t *testing.T) *testFixture {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	s := Register(app, Options{ContentCollections: []string{"articles"}})
	schemalock.Register(app)
	must(t, app.Bootstrap())
	t.Cleanup(func() { app.Cron().Stop(); _ = app.ClearBootstrap() })
	c := core.NewBaseCollection("articles")
	c.Fields.Add(&core.TextField{Name: "title", Required: true}, &core.TextField{Name: "secret", Hidden: true})
	must(t, app.Save(c))
	u := core.NewAuthCollection("members")
	must(t, app.Save(u))
	user := core.NewRecord(u)
	user.SetEmail("u@example.test")
	user.SetPassword("password-12345")
	must(t, app.Save(user))
	token, err := user.NewAuthToken()
	must(t, err)
	a, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	must(t, err)
	owner := core.NewRecord(a)
	owner.SetEmail("a@example.test")
	owner.SetPassword("password-12345")
	must(t, app.Save(owner))
	admin, err := owner.NewAuthToken()
	must(t, err)
	router, err := apis.NewRouter(app)
	must(t, err)
	must(t, app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}, func(*core.ServeEvent) error { return nil }))
	handler, err := router.BuildMux()
	must(t, err)
	return &testFixture{app, s, handler, admin, token, owner}
}
func (f *testFixture) request(method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Authorization", token)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func (f *testFixture) rpc(token, method string, params any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	return f.request("POST", "/api/mcp", "Bearer "+token, string(body))
}
func toolResult(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	var v map[string]any
	must(t, json.Unmarshal(w.Body.Bytes(), &v))
	r, ok := v["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %s", w.Body)
	}
	return r
}

func TestScopedContentAndRevocation(t *testing.T) {
	f := setup(t)
	key, token, err := f.s.CreateKey(Key{Name: "Editor", Owner: f.owner.Id, Tools: []string{"content_create", "content_list", "content_schema"}, Collections: []string{"articles"}})
	must(t, err)
	if strings.Contains(token, " ") {
		t.Fatal("bad token")
	}
	list := toolResult(t, f.rpc(token, "tools/list", map[string]any{}))
	if len(list["tools"].([]any)) != 3 {
		t.Fatal(list)
	}
	call := func(name string, args any) map[string]any {
		return toolResult(t, f.rpc(token, "tools/call", map[string]any{"name": name, "arguments": args}))
	}
	for _, args := range []any{map[string]any{"collection": "mcp_keys", "data": map[string]any{"name": "evil"}}, map[string]any{"collection": "articles", "data": map[string]any{"title": "ok", "secret": "hidden"}}} {
		if call("content_create", args)["isError"] != true {
			t.Fatal("scope bypass")
		}
	}
	// A request hook must run, not only a model/form validator.
	f.app.OnRecordCreateRequest("articles").BindFunc(func(e *core.RecordRequestEvent) error {
		if e.Record.GetString("title") == "blocked" {
			return e.ForbiddenError("blocked", nil)
		}
		return e.Next()
	})
	if call("content_create", map[string]any{"collection": "articles", "data": map[string]any{"title": "blocked"}})["isError"] != true {
		t.Fatal("request hook bypass")
	}
	if call("content_create", map[string]any{"collection": "articles", "data": map[string]any{"title": "Hello"}})["isError"] == true {
		t.Fatal("create failed")
	}
	rr, err := f.app.FindAllRecords("articles")
	must(t, err)
	if len(rr) != 1 || rr[0].GetString("title") != "Hello" {
		t.Fatal("failed writes had side effects")
	}
	raw := f.rpc(token, "tools/call", map[string]any{"name": "content_delete", "arguments": map[string]any{"collection": "articles", "id": rr[0].Id}})
	if !strings.Contains(raw.Body.String(), "error") {
		t.Fatal("ungranted tool")
	}
	keys := f.request("GET", "/api/mcp/admin/keys", f.admin, "")
	if keys.Code != 200 || strings.Contains(keys.Body.String(), token) || strings.Contains(keys.Body.String(), digest(token)) {
		t.Fatal("key disclosure", keys.Body)
	}
	for _, auth := range []string{"", f.user} {
		if w := f.request("POST", "/api/mcp/admin/keys", auth, `{"name":"bad","tools":["content_create"],"collections":["articles"]}`); w.Code < 400 {
			t.Fatal("non-admin key creation")
		}
	}
	for _, path := range []string{"/api/collections/mcp_keys/records/" + key.ID, "/api/collections/mcp_keys/records"} {
		if w := f.request("GET", path, f.admin, ""); w.Code < 400 && strings.Contains(w.Body.String(), digest(token)) {
			t.Fatal("records leak")
		}
	}
	must(t, Revoke(f.app, key.ID))
	if w := f.rpc(token, "tools/list", map[string]any{}); w.Code != 401 {
		t.Fatalf("revoked key: %d", w.Code)
	}
}

func TestTransportAndExpiry(t *testing.T) {
	f := setup(t)
	key, token, err := f.s.CreateKey(Key{Name: "Read", Owner: f.owner.Id, Tools: []string{"content_list"}, Collections: []string{"articles"}, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)})
	must(t, err)
	r := httptest.NewRequest("POST", "/api/mcp", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://untrusted.example")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("origin accepted")
	}
	if w = f.request("GET", "/api/mcp", "Bearer "+token, ""); w.Code != 405 {
		t.Fatal("GET should not stream")
	}
	// Official MCP client proves wire compatibility without an external listener.
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	httpClient := &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, req)
		return w.Result(), nil
	})}
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: "http://localhost/api/mcp", HTTPClient: httpClient}, nil)
	must(t, err)
	defer session.Close()
	tools, err := session.ListTools(t.Context(), nil)
	must(t, err)
	if len(tools.Tools) != 1 {
		t.Fatal("unexpected tools")
	}
	record, err := f.app.FindRecordById(keysCollection, key.ID)
	must(t, err)
	record.Set("expiresAt", time.Now().Add(-time.Hour))
	must(t, save(f.app, record))
	if _, err = authenticate(f.app, token); err == nil {
		t.Fatal("expired key accepted")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDecodeRejectsTrailingAndUnknown(t *testing.T) {
	for _, body := range []string{`{"bad":1}`, `{} {}`, `[]`} {
		if err := Decode(bytes.NewBufferString(body), &struct{}{}); err == nil {
			t.Fatal(body)
		}
	}
}
