package variants

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/search"
	"github.com/pocketbase/pocketbase/tools/subscriptions"
	"github.com/pocketbase/pocketbase/tools/types"
)

type fixture struct {
	app           *pocketbase.PocketBase
	users, offers *core.Collection
	user          *core.Record
	cfg           *Config
	h             http.Handler
	token         string
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func newFixture(t *testing.T) *fixture {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	polymorphicrelation.Register(app)
	Register(app)
	Register(app)
	must(t, app.Bootstrap())
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	u := core.NewAuthCollection("members")
	u.Fields.Add(&core.BoolField{Name: "premium"})
	must(t, app.Save(u))
	r := core.NewRecord(u)
	r.SetEmail("member@example.test")
	r.SetPassword("test-password-123")
	r.Set("premium", true)
	must(t, app.Save(r))
	c := core.NewBaseCollection("offers")
	c.Fields.Add(&core.TextField{Name: "title"})
	c.ListRule = types.Pointer("")
	c.ViewRule = types.Pointer("")
	must(t, app.Save(c))
	base := core.NewRecord(c)
	base.Set("title", "original")
	must(t, app.Save(base))
	cfg, err := Publish(app, Config{Collection: c.Id, AuthCollection: u.Id, Variables: true, Experiments: true, Default: Variant{Key: "default"}, Variants: []Variant{{Key: "premium", Name: "Premium", Condition: &Condition{Kind: "field", Field: "premium", Op: "eq", Value: true}, Experiments: []Experiment{{Key: "trial", Name: "Trial", Active: true, Groups: []Group{{Key: "a", Name: "A", From: 1, To: 5000}, {Key: "b", Name: "B", From: 5001, To: 10000}}}}}}})
	must(t, err)
	u, err = app.FindCollectionByNameOrId(u.Id)
	must(t, err)
	c, err = app.FindCollectionByNameOrId(c.Id)
	must(t, err)
	r, err = app.FindRecordById(u.Id, r.Id)
	must(t, err)
	for _, key := range []string{"a", "b"} {
		rec := core.NewRecord(c)
		rec.Set("title", key)
		rec.Set(SetField, setID(c.Id, "premium", "trial", key))
		must(t, app.Save(rec))
	}
	router, err := apis.NewRouter(app)
	must(t, err)
	must(t, app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}, func(e *core.ServeEvent) error { return nil }))
	h, err := router.BuildMux()
	must(t, err)
	token, err := r.NewAuthToken()
	must(t, err)
	return &fixture{app: app, users: u, offers: c, user: r, cfg: cfg, h: h, token: token}
}

func TestContentSetIndexProtection(t *testing.T) {
	x := newFixture(t)
	name := "idx_pv_" + digest(x.offers.Id)[:16]
	for _, change := range []string{"remove", "replace", "partial"} {
		t.Run(change, func(t *testing.T) {
			c, err := x.app.FindCollectionByNameOrId(x.offers.Id)
			must(t, err)
			switch change {
			case "remove":
				c.RemoveIndex(name)
			case "replace":
				c.AddIndex(name, false, "title", "")
			case "partial":
				c.AddIndex(name, false, SetField, "title != ''")
			}
			if err := x.app.Save(c); err == nil {
				t.Fatal("managed content_set index change was accepted")
			}
		})
	}
	c, err := x.app.FindCollectionByNameOrId(x.offers.Id)
	must(t, err)
	c.Name = "renamed_offers"
	c.AddIndex(name, false, "`content_set` ASC", "")
	c.AddIndex("idx_offers_title", false, "title", "")
	must(t, x.app.Save(c))
	if c.GetIndex("idx_offers_title") == "" {
		t.Fatal("custom index was lost")
	}
}

func call(t *testing.T, x *fixture, method, path, token string, body any, status int) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", token)
	}
	w := httptest.NewRecorder()
	x.h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	out := map[string]any{}
	must(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}
func items(m map[string]any) []any { return m["items"].([]any) }

func TestNativeSelectionAndParticipation(t *testing.T) {
	x := newFixture(t)
	guest := call(t, x, "GET", "/api/collections/offers/records", "", nil, 200)
	if guest["totalItems"] != float64(1) || items(guest)[0].(map[string]any)["title"] != "original" {
		t.Fatal(guest)
	}
	user := call(t, x, "GET", "/api/collections/offers/records?perPage=1", x.token, nil, 200)
	if user["totalItems"] != float64(1) {
		t.Fatal(user)
	}
	d := call(t, x, "GET", "/api/variants/me", x.token, nil, 200)
	decision := items(d)[0].(map[string]any)
	if decision["set"] != items(user)[0].(map[string]any)[SetField] || decision["experiment"] != "trial" {
		t.Fatal(d, user)
	}
	wrong := "a"
	if decision["group"] == wrong {
		wrong = "b"
	}
	rr, err := x.app.FindAllRecords(x.offers, dbx.HashExp{"title": wrong})
	must(t, err)
	call(t, x, "GET", "/api/collections/offers/records/"+rr[0].Id, x.token, nil, 404)
	call(t, x, "GET", "/api/collections/offers/records?filter=title%3D%27"+wrong+"%27", x.token, nil, 200)
	h := call(t, x, "GET", "/api/variants/me/history", x.token, nil, 200)
	if h["totalItems"] != float64(1) {
		t.Fatal(h)
	}
	call(t, x, "GET", "/api/variants/me", "", nil, 401)
	u, err := x.app.FindRecordById(x.users, x.user.Id)
	must(t, err)
	if u.GetInt(BucketField) != Bucket(x.users.Id, u.Id) {
		t.Fatal("bucket not initialized")
	}
	u.Set("premium", false)
	must(t, x.app.Save(u))
	changed := call(t, x, "GET", "/api/collections/offers/records", x.token, nil, 200)
	if items(changed)[0].(map[string]any)["title"] != "original" {
		t.Fatal(changed)
	}
	h = call(t, x, "GET", "/api/variants/me/history?perPage=1", x.token, nil, 200)
	if h["totalItems"] != float64(2) || len(items(h)) != 1 {
		t.Fatal(h)
	}
}

func TestBucketBoundariesAndRepublish(t *testing.T) {
	x := newFixture(t)
	for _, tc := range []struct {
		bucket int
		group  string
	}{{1, "a"}, {5000, "a"}, {5001, "b"}, {10000, "b"}} {
		_, err := x.app.DB().Update(x.users.Name, dbx.Params{BucketField: tc.bucket}, dbx.HashExp{"id": x.user.Id}).Execute()
		must(t, err)
		out := call(t, x, "GET", "/api/variants/me", x.token, nil, 200)
		if items(out)[0].(map[string]any)["group"] != tc.group {
			t.Fatal(tc, out)
		}
	}
	ex := &x.cfg.Variants[0].Experiments[0]
	ex.Groups[0].To = 9999
	ex.Groups[1].From = 10000
	cfg, err := Publish(x.app, *x.cfg)
	must(t, err)
	if cfg.Version != 2 {
		t.Fatal(cfg)
	}
	if _, err := Publish(x.app, *x.cfg); err == nil {
		t.Fatal("accepted stale configuration")
	}
	cfg.Experiments = false
	cfg, err = Publish(x.app, *cfg)
	must(t, err)
	// Base premium is deliberately empty: no fallback to default or experiment content.
	out := call(t, x, "GET", "/api/collections/offers/records", x.token, nil, 200)
	if len(items(out)) != 0 {
		t.Fatal(out)
	}
	cfg.Variables = false
	_, err = Publish(x.app, *cfg)
	must(t, err)
	out = call(t, x, "GET", "/api/collections/offers/records", x.token, nil, 200)
	if len(items(out)) != 1 || items(out)[0].(map[string]any)["title"] != "original" {
		t.Fatal(out)
	}
}

func TestRelatedSameRowAndPriority(t *testing.T) {
	x := newFixture(t)
	sub := core.NewBaseCollection("subscriptions")
	sub.Fields.Add(&core.RelationField{Name: "user", CollectionId: x.users.Id, MaxSelect: 1}, &core.TextField{Name: "status"}, &core.NumberField{Name: "amount"})
	must(t, x.app.Save(sub))
	for _, values := range []struct {
		s string
		n int
	}{{"active", 0}, {"expired", 100}} {
		r := core.NewRecord(sub)
		r.Set("user", x.user.Id)
		r.Set("status", values.s)
		r.Set("amount", values.n)
		must(t, x.app.Save(r))
	}
	x.cfg.Experiments = false
	x.cfg.Variants = append([]Variant{{Key: "subscriber", Name: "Subscriber", Condition: &Condition{Kind: "exists", Relation: "subscriptions_via_user", Children: []Condition{{Kind: "all", Children: []Condition{{Kind: "field", Field: "status", Op: "eq", Value: "active"}, {Kind: "field", Field: "amount", Op: "gt", Value: float64(10)}}}}}}}, x.cfg.Variants...)
	cfg, err := Publish(x.app, *x.cfg)
	must(t, err)
	d, err := Resolve(x.app, cfg, x.user)
	must(t, err)
	if d.Variant != "premium" {
		t.Fatal("different related rows matched together", d)
	}
	r := core.NewRecord(sub)
	r.Set("user", x.user.Id)
	r.Set("status", "active")
	r.Set("amount", 20)
	must(t, x.app.Save(r))
	d, err = Resolve(x.app, cfg, x.user)
	must(t, err)
	if d.Variant != "subscriber" {
		t.Fatal(d)
	}
	// Changes are picked up by a fresh SELECT without changing users or config.
	r.Set("status", "expired")
	must(t, x.app.Save(r))
	d, err = Resolve(x.app, cfg, x.user)
	must(t, err)
	if d.Variant != "premium" {
		t.Fatal(d)
	}
	cfg.Variants[0].Condition.Kind = "notExists"
	cfg, err = Publish(x.app, *cfg)
	must(t, err)
	d, err = Resolve(x.app, cfg, x.user)
	must(t, err)
	if d.Variant != "subscriber" {
		t.Fatal(d)
	}
	// Renaming/removing a dependency cannot silently invalidate a published audience.
	sub.Fields.RemoveByName("status")
	if err = x.app.Save(sub); err == nil {
		t.Fatal("accepted broken dependency")
	}
}

func TestNativeExpandAndAccessRules(t *testing.T) {
	x := newFixture(t)
	must(t, ensureUserBucket(x.app, x.user))
	d, err := Resolve(x.app, x.cfg, x.user)
	must(t, err)
	recs, err := x.app.FindAllRecords(x.offers)
	must(t, err)
	links := core.NewBaseCollection("links")
	links.ListRule = types.Pointer("")
	links.ViewRule = types.Pointer("")
	links.Fields.Add(&core.RelationField{Name: "offer", CollectionId: x.offers.Id, MaxSelect: 1}, &polymorphicrelation.Field{JSONField: core.JSONField{Name: "subject"}, CollectionIDs: []string{x.offers.Id}})
	must(t, x.app.Save(links))
	for _, offer := range recs {
		r := core.NewRecord(links)
		r.Set("offer", offer.Id)
		r.Set("subject", polymorphicrelation.Reference{CollectionID: x.offers.Id, RecordID: offer.Id})
		must(t, x.app.Save(r))
	}
	out := call(t, x, "GET", "/api/collections/links/records?expand=offer,subject", x.token, nil, 200)
	count := 0
	for _, raw := range items(out) {
		r := raw.(map[string]any)
		expand, _ := r["expand"].(map[string]any)
		for _, name := range []string{"offer", "subject"} {
			if related, ok := expand[name].(map[string]any); ok {
				count++
				if related[SetField] != d.Set {
					t.Fatal("leaked set", related)
				}
			}
		}
	}
	if count != 2 {
		t.Fatal(out)
	}
	x.cfg.ListRule = nil
	x.cfg.ViewRule = nil
	_, err = Publish(x.app, *x.cfg)
	must(t, err)
	call(t, x, "GET", "/api/collections/offers/records", x.token, nil, 403)
}

func TestProtectionRollbackAndConcurrency(t *testing.T) {
	x := newFixture(t)
	original := x.cfg.Version
	x.cfg.Variants[0].Experiments[0].Groups[1].From = 5000
	if _, err := Publish(x.app, *x.cfg); err == nil {
		t.Fatal("accepted overlap")
	}
	cfg, err := Load(x.app, x.offers.Id)
	must(t, err)
	if cfg.Version != original {
		t.Fatal("publish was not atomic")
	}
	u, err := x.app.FindRecordById(x.users, x.user.Id)
	must(t, err)
	u.Set(BucketField, 9999)
	must(t, x.app.Save(u))
	if u.GetInt(BucketField) != Bucket(x.users.Id, u.Id) {
		t.Fatal("accepted bucket override")
	}
	x.users.UpdateRule = types.Pointer(`id = @request.auth.id`)
	must(t, x.app.Save(x.users))
	call(t, x, "PATCH", "/api/collections/members/records/"+u.Id, x.token, map[string]any{"content_bucket+": 1}, 200)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- observeCollection(x.app, u, x.offers.Id) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	rr, err := x.app.FindAllRecords(history)
	must(t, err)
	if len(rr) != 1 {
		t.Fatal("duplicate history", len(rr))
	}
	_, err = x.app.FindCollectionByNameOrId(viewName(x.offers.Id))
	must(t, err)
	// The view is virtual and starts with a primary-key user lookup.
	var explain []struct {
		Detail string `db:"detail"`
	}
	must(t, x.app.DB().NewQuery("EXPLAIN QUERY PLAN SELECT content_set FROM "+ident(viewName(x.offers.Id))+" WHERE id={:id}").Bind(dbx.Params{"id": u.Id}).All(&explain))
	t.Log(explain)
	if len(explain) == 0 || !strings.Contains(explain[0].Detail, "SEARCH") {
		t.Fatal("user lookup must use an index", explain)
	}
}

func TestMultipleUsersDoNotMultiplyGuestPagination(t *testing.T) {
	x := newFixture(t)
	for i := 0; i < 10; i++ {
		u := core.NewRecord(x.users)
		u.SetEmail(fmt.Sprintf("u%d@example.test", i))
		u.SetPassword("test-password-123")
		must(t, x.app.Save(u))
	}
	guest := call(t, x, "GET", "/api/collections/offers/records?perPage=1", "", nil, 200)
	if guest["totalItems"] != float64(1) || len(items(guest)) != 1 {
		t.Fatal("guest join multiplied rows", guest)
	}
	member := call(t, x, "GET", "/api/collections/offers/records", x.token, nil, 200)
	if member["totalItems"] != float64(1) {
		t.Fatal(member)
	}
	// Zero users must still leave the guest baseline accessible.
	all, err := x.app.FindAllRecords(x.users)
	must(t, err)
	for _, u := range all {
		must(t, x.app.Delete(u))
	}
	guest = call(t, x, "GET", "/api/collections/offers/records", "", nil, 200)
	if guest["totalItems"] != float64(1) {
		t.Fatal("empty users view blocked guest", guest)
	}
}

func TestNativeRealtimeAndDeliveryHistory(t *testing.T) {
	x := newFixture(t)
	must(t, ensureUserBucket(x.app, x.user))
	d, err := Resolve(x.app, x.cfg, x.user)
	must(t, err)
	client := subscriptions.NewDefaultClient()
	client.Set(apis.RealtimeClientAuthKey, x.user)
	client.Subscribe(`offers/*?options={"query":{"fields":"id,title"}}`)
	x.app.SubscriptionsBroker().Register(client)
	t.Cleanup(func() { x.app.SubscriptionsBroker().Unregister(client.Id()); client.Discard() })
	messages := make(chan subscriptions.Message, 10)
	go func() {
		for m := range client.Channel() {
			messages <- m
		}
	}()
	rr, err := x.app.FindAllRecords(x.offers)
	must(t, err)
	for _, r := range rr {
		r.Set("title", r.GetString("title")+" updated")
		must(t, x.app.Save(r))
	}
	var msg subscriptions.Message
	select {
	case msg = <-messages:
	case <-time.After(5 * time.Second):
		t.Fatal("no realtime event")
	}
	var data struct {
		Record map[string]any `json:"record"`
	}
	must(t, json.Unmarshal(msg.Data, &data))
	r, err := x.app.FindRecordById(x.offers, data.Record["id"].(string))
	must(t, err)
	if r.GetString(SetField) != d.Set {
		t.Fatal("realtime leaked another set")
	}
	select {
	case m := <-messages:
		t.Fatal("unexpected foreign event", m)
	case <-time.After(100 * time.Millisecond):
	}
	h, err := x.app.FindAllRecords(history)
	must(t, err)
	if len(h) != 0 {
		t.Fatal("candidate broadcast recorded delivery")
	}
	e := &core.RealtimeMessageEvent{RequestEvent: &core.RequestEvent{App: x.app}, Client: client, Message: &msg}
	must(t, x.app.OnRealtimeMessageSend().Trigger(e, func(e *core.RealtimeMessageEvent) error { return nil }))
	h, err = x.app.FindAllRecords(history)
	must(t, err)
	if len(h) != 1 {
		t.Fatal("delivery missing history", len(h))
	}
	// Current native rules immediately apply to the existing subscription.
	x.cfg.Experiments = false
	x.cfg.Variables = false
	_, err = Publish(x.app, *x.cfg)
	must(t, err)
	rr, err = x.app.FindAllRecords(x.offers)
	must(t, err)
	for _, r := range rr {
		r.Set("title", r.GetString("title")+" again")
		must(t, x.app.Save(r))
	}
	select {
	case msg = <-messages:
	case <-time.After(5 * time.Second):
		t.Fatal("no baseline event after publish")
	}
	must(t, json.Unmarshal(msg.Data, &data))
	r, err = x.app.FindRecordById(x.offers, data.Record["id"].(string))
	must(t, err)
	if r.GetString(SetField) != setID(x.offers.Id, "default", "", "") {
		t.Fatal("stale realtime rules")
	}
	select {
	case m := <-messages:
		t.Fatal("unexpected stale event", m)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPreviewAndRestart(t *testing.T) {
	x := newFixture(t)
	d, err := Resolve(x.app, x.cfg, x.user)
	must(t, err)
	if d.Experiment != "trial" || d.Bucket != Bucket(x.users.Id, x.user.Id) {
		t.Fatal(d)
	}
	u, err := x.app.FindRecordById(x.users, x.user.Id)
	must(t, err)
	if u.GetInt(BucketField) != 0 {
		t.Fatal("preview mutated user")
	}
	must(t, x.app.ClearBootstrap())
	must(t, x.app.Bootstrap())
	d2, err := Resolve(x.app, x.cfg, x.user)
	must(t, err)
	if d2 != d {
		t.Fatal("unstable restart", d, d2)
	}
	var stored int
	must(t, x.app.DB().NewQuery("SELECT count(*) FROM pv_history").Row(&stored))
	if stored != 0 {
		t.Fatal("preview recorded history")
	}
}

func TestOtherAuthAndHistoryIsolation(t *testing.T) {
	x := newFixture(t)
	other := core.NewAuthCollection("other_members")
	must(t, x.app.Save(other))
	u := core.NewRecord(other)
	u.SetEmail("other@example.test")
	u.SetPassword("test-password-123")
	must(t, x.app.Save(u))
	token, err := u.NewAuthToken()
	must(t, err)
	out := call(t, x, "GET", "/api/collections/offers/records", token, nil, 200)
	if len(items(out)) != 0 {
		t.Fatal("foreign auth collection should be excluded", out)
	}
	call(t, x, "GET", "/api/variants/me", x.token, nil, 200)
	h := call(t, x, "GET", "/api/variants/me/history", token, nil, 200)
	if h["totalItems"] != float64(0) {
		t.Fatal("history crossed auth collections", h)
	}
	foreign := call(t, x, "GET", "/api/variants/me", token, nil, 200)
	if len(items(foreign)) != 0 {
		t.Fatal(foreign)
	}
	call(t, x, "GET", "/api/variants/admin/users/"+x.users.Id+"/"+x.user.Id, token, nil, 403)
	call(t, x, "GET", "/api/collections/pv_configs/records", x.token, nil, 403)
	// Use a realistic population so SQLite does not prefer a one-row table scan.
	_, err = x.app.DB().NewQuery(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10000) INSERT INTO members (id,email,password,tokenKey) SELECT printf('%015d',i), printf('query%d@example.test',i), password, printf('%050d',i) FROM n,members WHERE members.id={:id}`).Bind(dbx.Params{"id": x.user.Id}).Execute()
	must(t, err)
	_, err = x.app.DB().NewQuery(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10000) INSERT INTO offers (id,title,content_set) SELECT printf('%015d',i), 'synthetic', {:set} FROM n`).Bind(dbx.Params{"set": setID(x.offers.Id, "default", "", "")}).Execute()
	must(t, err)
	_, err = x.app.DB().NewQuery("ANALYZE").Execute()
	must(t, err)
	// The native API query joins users and the decision view using primary keys.
	auth, err := x.app.FindRecordById(x.users, x.user.Id)
	must(t, err)
	col, err := x.app.FindCollectionByNameOrId(x.offers.Id)
	must(t, err)
	resolver := core.NewRecordFieldResolver(x.app, col, &core.RequestInfo{Auth: auth}, true)
	expr, err := search.FilterData(*col.ListRule).BuildExpr(resolver)
	must(t, err)
	q := x.app.RecordQuery(col).AndWhere(expr)
	must(t, resolver.UpdateQuery(q))
	built := q.Build()
	var plan []struct {
		Detail string `db:"detail"`
	}
	must(t, x.app.DB().NewQuery("EXPLAIN QUERY PLAN "+built.SQL()).Bind(built.Params()).All(&plan))
	for _, step := range plan {
		t.Log(step.Detail)
		if strings.Contains(step.Detail, "SCAN u") {
			t.Fatal("native access scanned the user population", plan)
		}
	}
}
