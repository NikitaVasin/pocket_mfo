package singleton

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/subscriptions"
	"github.com/pocketbase/pocketbase/tools/types"
	"pocket_mfo/plugins/variants"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T, withVariants, reverse bool) (*pocketbase.PocketBase, *core.Collection) {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if reverse && withVariants {
		variants.Register(app)
	}
	Register(app)
	Register(app)
	if !reverse && withVariants {
		variants.Register(app)
	}
	must(t, app.Bootstrap())
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	c := core.NewBaseCollection("settings")
	c.Fields.Add(&core.TextField{Name: "title", Required: true})
	c.ListRule = types.Pointer("")
	c.ViewRule = types.Pointer("")
	must(t, app.Save(c))
	return app, c
}
func record(c *core.Collection, set string) *core.Record {
	r := core.NewRecord(c)
	r.Set("title", "example")
	if set != "" {
		r.Set(setField, set)
	}
	return r
}
func configure(t *testing.T, app core.App, c *core.Collection, enabled bool) *core.Collection {
	t.Helper()
	_, err := Configure(app, Config{Collection: c.Id, Enabled: enabled})
	must(t, err)
	c, err = app.FindCollectionByNameOrId(c.Id)
	must(t, err)
	return c
}
func publish(t *testing.T, app core.App, c *core.Collection) *variants.Config {
	t.Helper()
	u := core.NewAuthCollection("members")
	u.Fields.Add(&core.BoolField{Name: "premium"})
	must(t, app.Save(u))
	cfg, err := variants.Publish(app, variants.Config{Collection: c.Id, AuthCollection: u.Id, Variables: true, Experiments: true,
		Default: variants.Variant{Key: "default", Name: "Default"}, Variants: []variants.Variant{{Key: "premium", Name: "Premium", Condition: &variants.Condition{Kind: "field", Field: "premium", Op: "eq", Value: true},
			Experiments: []variants.Experiment{{Key: "test", Name: "Test", Active: true, Groups: []variants.Group{{Key: "a", Name: "A", From: 1, To: 5000}, {Key: "b", Name: "B", From: 5001, To: 10000}}}}}}})
	must(t, err)
	return cfg
}

func TestSingletonLifecycleAndConcurrentCreates(t *testing.T) {
	app, c := fixture(t, false, false)
	c = configure(t, app, c, true)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if app.SaveNoValidate(record(c, "")) == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful creates: %d", successes.Load())
	}
	rows, err := app.FindAllRecords(c)
	must(t, err)
	rows[0].Set("title", "edited")
	must(t, app.Save(rows[0]))
	must(t, app.SaveNoValidate(rows[0]))
	if app.Save(record(c, "")) == nil {
		t.Fatal("second record accepted")
	}
	must(t, app.Delete(rows[0]))
	must(t, app.Save(record(c, "")))
	c = configure(t, app, c, false)
	must(t, app.Save(record(c, "")))
	_, err = Configure(app, Config{Collection: c.Id, Enabled: true})
	var conflict *ConflictError
	if !errors.As(err, &conflict) || len(conflict.Conflicts) != 1 || conflict.Conflicts[0].Count != 2 {
		t.Fatalf("conflicts: %#v, %v", conflict, err)
	}
	cfg, err := Load(app, c.Id)
	must(t, err)
	if cfg.Enabled {
		t.Fatal("failed enable persisted")
	}
	c, err = app.FindCollectionByNameOrId(c.Id)
	must(t, err)
	if c.GetIndex(indexName(c.Id)) != "" {
		t.Fatal("failed enable installed index")
	}
}

func TestVariantsIntegration(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, first := range []bool{false, true} {
			name := "variants-first"
			if first {
				name = "singleton-first"
			}
			if reverse {
				name += "-reverse-hooks"
			}
			t.Run(name, func(t *testing.T) {
				app, c := fixture(t, true, reverse)
				must(t, app.Save(record(c, "")))
				if first {
					c = configure(t, app, c, true)
				}
				cfg := publish(t, app, c)
				c = configure(t, app, c, true)
				sets, err := app.FindAllRecords("pv_sets", dbx.HashExp{"collection": c.Id})
				must(t, err)
				for _, s := range sets {
					if s.GetString("variant") != "default" {
						must(t, app.Save(record(c, s.Id)))
					}
					if app.SaveNoValidate(record(c, s.Id)) == nil {
						t.Fatal("duplicate set accepted")
					}
				}
				rows, err := app.FindAllRecords(c)
				must(t, err)
				if len(rows) != 4 {
					t.Fatalf("records %d", len(rows))
				}
				rows[0].Set(setField, rows[1].GetString(setField))
				if app.Save(rows[0]) == nil {
					t.Fatal("move to occupied set accepted")
				}
				if app.Save(record(c, "")) == nil {
					t.Fatal("implicit default duplicate accepted")
				}
				cfg.Variants = nil
				_, err = variants.Publish(app, *cfg)
				must(t, err)
				for _, s := range sets {
					if app.Save(record(c, s.Id)) == nil {
						t.Fatal("archived set duplicated")
					}
				}
				c = configure(t, app, c, false)
				must(t, app.Save(record(c, sets[0].Id)))
				_, err = Configure(app, Config{Collection: c.Id, Enabled: true})
				var conflict *ConflictError
				if !errors.As(err, &conflict) || conflict.Conflicts[0].Set != sets[0].Id || conflict.Conflicts[0].Name == "" {
					t.Fatalf("missing set conflict: %v", err)
				}
			})
		}
	}
}

func TestSchemaProtectionAndCleanup(t *testing.T) {
	app, c := fixture(t, false, false)
	c = configure(t, app, c, true)
	for _, change := range []string{"remove", "replace", "partial"} {
		c, err := app.FindCollectionByNameOrId(c.Id)
		must(t, err)
		switch change {
		case "remove":
			c.RemoveIndex(indexName(c.Id))
		case "replace":
			c.AddIndex(indexName(c.Id), false, "title", "")
		case "partial":
			c.AddIndex(indexName(c.Id), true, "(1)", "title != ''")
		}
		if app.SaveNoValidate(c) == nil {
			t.Fatal("index change accepted: " + change)
		}
	}
	c, err := app.FindCollectionByNameOrId(c.Id)
	must(t, err)
	c.Name = "renamed_settings"
	c.Fields.Add(&core.BoolField{Name: "published"})
	must(t, app.Save(c))
	must(t, app.Save(record(c, "")))
	if app.Save(record(c, "")) == nil {
		t.Fatal("rename lost constraint")
	}
	sc, err := app.FindCollectionByNameOrId(configs)
	must(t, err)
	r, err := app.FindRecordById(sc, key(c.Id))
	must(t, err)
	r.Set("enabled", false)
	if app.SaveNoValidate(r) == nil || app.Delete(r) == nil || app.Delete(sc) == nil {
		t.Fatal("service protection failed")
	}
	must(t, app.Delete(c))
	rows, err := app.FindAllRecords(sc)
	must(t, err)
	if len(rows) != 0 {
		t.Fatal("orphan config")
	}
}

func TestSingletonHTTPAndRules(t *testing.T) {
	app, c := fixture(t, true, false)
	cfg := publish(t, app, c)
	c = configure(t, app, c, true)
	sc, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	must(t, err)
	admin := core.NewRecord(sc)
	admin.SetEmail("singleton@example.test")
	admin.SetPassword("test-password-123")
	must(t, app.Save(admin))
	token, err := admin.NewAuthToken()
	must(t, err)
	router, err := apis.NewRouter(app)
	must(t, err)
	must(t, app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}, func(e *core.ServeEvent) error { return nil }))
	handler, err := router.BuildMux()
	must(t, err)
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	path := "/api/singleton/admin/collections/" + c.Id
	if request("GET", path, "", "").Code == http.StatusOK {
		t.Fatal("anonymous config access")
	}
	if request("PUT", path, token, `{}`).Code != 400 {
		t.Fatal("missing enabled accepted")
	}
	if w := request("GET", path, token, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatal(w.Body.String())
	}
	base := "/api/collections/" + c.Id + "/records"
	if w := request("POST", base, "", `{"title":"denied"}`); w.Code == 200 {
		t.Fatal("anonymous create")
	}
	if w := request("POST", base, token, `{"title":"Default"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request("POST", base, token, `{"title":"Duplicate"}`); w.Code != 400 {
		t.Fatalf("duplicate HTTP %d %s", w.Code, w.Body.String())
	}
	sets, err := app.FindAllRecords("pv_sets", dbx.HashExp{"collection": c.Id, "variant": "premium"})
	must(t, err)
	for _, s := range sets {
		must(t, app.Save(record(c, s.Id)))
	}
	u, err := app.FindCollectionByNameOrId(cfg.AuthCollection)
	must(t, err)
	user := core.NewRecord(u)
	user.SetEmail("member@example.test")
	user.SetPassword("test-password-123")
	user.Set("premium", true)
	must(t, app.Save(user))
	userToken, err := user.NewAuthToken()
	must(t, err)
	for _, auth := range []string{"", userToken} {
		w := request("GET", base, auth, "")
		var result struct {
			Items []map[string]any `json:"items"`
			Total int              `json:"totalItems"`
		}
		must(t, json.Unmarshal(w.Body.Bytes(), &result))
		if w.Code != 200 || result.Total != 1 {
			t.Fatalf("visibility: %s", w.Body.String())
		}
		if auth != "" {
			d, err := variants.Resolve(app, cfg, user)
			must(t, err)
			if result.Items[0][setField] != d.Set {
				t.Fatal("wrong audience")
			}
		}
	}
	if w := request("PUT", path, token, `{"enabled":false}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}

func TestSingletonRealtimeIsolation(t *testing.T) {
	app, c := fixture(t, true, false)
	cfg := publish(t, app, c)
	c = configure(t, app, c, true)
	sets, err := app.FindAllRecords("pv_sets", dbx.HashExp{"collection": c.Id})
	must(t, err)
	for _, set := range sets {
		must(t, app.Save(record(c, set.Id)))
	}
	u, err := app.FindCollectionByNameOrId(cfg.AuthCollection)
	must(t, err)
	user := core.NewRecord(u)
	user.SetEmail("realtime@example.test")
	user.SetPassword("test-password-123")
	user.Set("premium", true)
	must(t, app.Save(user))
	decision, err := variants.Resolve(app, cfg, user)
	must(t, err)
	// Install native realtime hooks, then subscribe to the entire collection.
	_, err = apis.NewRouter(app)
	must(t, err)
	client := subscriptions.NewDefaultClient()
	client.Set(apis.RealtimeClientAuthKey, user)
	client.Subscribe(c.Name + "/*")
	app.SubscriptionsBroker().Register(client)
	t.Cleanup(func() { app.SubscriptionsBroker().Unregister(client.Id()); client.Discard() })
	messages := make(chan subscriptions.Message, 10)
	go func() {
		for msg := range client.Channel() {
			messages <- msg
		}
	}()
	rows, err := app.FindAllRecords(c)
	must(t, err)
	for _, row := range rows {
		row.Set("title", "Updated")
		must(t, app.Save(row))
	}
	select {
	case msg := <-messages:
		var event struct {
			Record map[string]any `json:"record"`
		}
		must(t, json.Unmarshal(msg.Data, &event))
		if event.Record[setField] != decision.Set {
			t.Fatal("foreign singleton set in realtime")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing singleton realtime event")
	}
	select {
	case <-messages:
		t.Fatal("unexpected foreign event")
	case <-time.After(100 * time.Millisecond):
	}
}
