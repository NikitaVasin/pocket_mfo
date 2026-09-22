package partnerlinks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

func postConversion(x *fixture, token, status, lead string) int {
	q := url.Values{"secret": {testProvider().Secret}, "subid": {token}, "status": {status}, "lead_id": {lead}}
	return x.request("GET", "/api/partnerlinks/postbacks/test?"+q.Encode(), "", "", "").Code
}
func agePending(t *testing.T, x *fixture, token string, issued int64) *core.Record {
	t.Helper()
	r, d, err := loadConversation(x.app, token)
	must(t, err)
	d.IssuedAt = issued
	d.OpenUntil = issued + 86400
	r.Set("clickTimestamp", issued)
	r.Set("clickData", d)
	r.Set("statusChangedAt", issued)
	must(t, save(x.app, r))
	return r
}
func TestResolveStoresSnapshotAndOpaqueToken(t *testing.T) {
	x := setup(t, true)
	first, second := x.issue(t), x.issue(t)
	if first.ClickID == second.ClickID || first.Link.URL == second.Link.URL {
		t.Fatal("resolve reused a conversion")
	}
	token := tokenFrom(first)
	r, d, err := loadConversation(x.app, token)
	must(t, err)
	if len(token) != 43 || r.GetString("tokenHash") == token || r.GetString("tokenHash") == "" {
		t.Fatal("not an opaque hashed token")
	}
	if r.GetString("status") != "pending" || r.GetString("leadId") != "" || d.ProfileID != "profile-42" || len(d.Experiments) == 0 || d.ClickID != first.ClickID {
		t.Fatalf("missing snapshot: %+v", d)
	}
	if x.eventCount() != 0 {
		t.Fatal("resolve sent a click")
	}
	for _, auth := range []string{x.auth, x.admin} {
		w := x.request("GET", "/api/collections/conversations/records/"+r.Id, auth, "", "")
		if w.Code != 200 {
			t.Fatalf("owner read: %d", w.Code)
		}
		if strings.Contains(w.Body.String(), "tokenHash") || strings.Contains(w.Body.String(), token) {
			t.Fatal("token lookup credential leaked")
		}
	}
	if code := postConversion(x, token, "wait", "order"); code != 200 {
		t.Fatalf("direct hold: %d", code)
	}
	r, _, err = loadConversation(x.app, token)
	must(t, err)
	if r.GetString("status") != "hold" || x.eventCount() != 1 {
		t.Fatal("hold must not synthesize a lead")
	}
	if code := postConversion(x, token, "yes", "other"); code != 400 {
		t.Fatalf("second application accepted: %d", code)
	}
	if code := postConversion(x, tokenFrom(second), "yes", "order"); code != 400 {
		t.Fatalf("application claimed by another issued link: %d", code)
	}
	if x.eventCount() != 1 {
		t.Fatal("conflicts delivered")
	}
}

func TestDefaultRetentionBoundaryAndNoResurrection(t *testing.T) {
	x := setup(t, true)
	cfg, err := Load(x.app)
	must(t, err)
	if cfg.PendingRetentionDays != 14 || cfg.ConversionRetentionDays != 0 {
		t.Fatal("wrong defaults")
	}
	now := time.Now().Unix()
	expired := tokenFrom(x.issue(t))
	old := agePending(t, x, expired, now-14*86400)
	recent := tokenFrom(x.issue(t))
	agePending(t, x, recent, now-13*86400)
	converted := tokenFrom(x.issue(t))
	if code := postConversion(x, converted, "yes", "permanent"); code != 200 {
		t.Fatalf("conversion %d", code)
	}
	r, _, err := loadConversation(x.app, converted)
	must(t, err)
	r.Set("statusChangedAt", now-400*86400)
	must(t, save(x.app, r))
	count := x.eventCount()
	if code := postConversion(x, expired, "new", "late"); code != 410 {
		t.Fatalf("expired pending accepted before cleanup: %d", code)
	}
	removed, err := cleanupAt(x.app, now)
	must(t, err)
	if removed != 1 {
		t.Fatalf("removed %d", removed)
	}
	if _, err = x.app.FindRecordById(ConversationsCollection, old.Id); err == nil {
		t.Fatal("expired row remains")
	}
	if code := postConversion(x, expired, "new", "late"); code != 400 {
		t.Fatalf("deleted token resurrected: %d", code)
	}
	if w := x.request("GET", "/api/partnerlinks/r/"+expired, "", "", ""); w.Code != 400 {
		t.Fatal("deleted redirect accepted")
	}
	if x.eventCount() != count {
		t.Fatal("expired token sent analytics")
	}
	if code := postConversion(x, converted, "no", "permanent"); code != 200 {
		t.Fatalf("unlimited converted record expired: %d", code)
	}
}

func TestConfiguredRetentionAndStatusChangeClock(t *testing.T) {
	x := setup(t, true)
	cfg, err := Load(x.app)
	must(t, err)
	cfg.PendingRetentionDays = 0
	cfg.ConversionRetentionDays = 30
	_, err = Configure(x.app, *cfg)
	must(t, err)
	now := time.Now().Unix()
	pending := tokenFrom(x.issue(t))
	agePending(t, x, pending, now-400*86400)
	if n, err := cleanupAt(x.app, now); err != nil || n != 0 {
		t.Fatalf("unlimited pending deleted: %d %v", n, err)
	}
	if code := postConversion(x, pending, "wait", "order"); code != 200 {
		t.Fatalf("late first conversion: %d", code)
	}
	r, _, err := loadConversation(x.app, pending)
	must(t, err)
	if r.GetInt64("statusChangedAt") < now {
		t.Fatal("retention not reset on first status")
	}
	r.Set("statusChangedAt", now-29*86400)
	must(t, save(x.app, r))
	if code := postConversion(x, pending, "wait", "order"); code != 200 {
		t.Fatal("repeat failed")
	}
	r, _, err = loadConversation(x.app, pending)
	must(t, err)
	if r.GetInt64("statusChangedAt") != now-29*86400 {
		t.Fatal("duplicate extended retention")
	}
	if code := postConversion(x, pending, "yes", "order"); code != 200 {
		t.Fatal("status change failed")
	}
	r, _, err = loadConversation(x.app, pending)
	must(t, err)
	if r.GetInt64("statusChangedAt") < now {
		t.Fatal("status change did not renew retention")
	}
	r.Set("statusChangedAt", now-30*86400)
	must(t, save(x.app, r))
	count := x.eventCount()
	if code := postConversion(x, pending, "no", "order"); code != 410 {
		t.Fatalf("expired converted accepted: %d", code)
	}
	if x.eventCount() != count {
		t.Fatal("expired event delivered")
	}
	n, err := cleanupAt(x.app, now)
	must(t, err)
	if n != 1 {
		t.Fatalf("expired converted not deleted: %d", n)
	}
}

func TestRetentionDefaultsForOldConfigAndAtomicValidation(t *testing.T) {
	x := setup(t, true)
	r, err := x.app.FindRecordById(configsCollection, configID)
	must(t, err)
	var raw map[string]any
	must(t, json.Unmarshal([]byte(r.GetString("definition")), &raw))
	delete(raw, "pendingRetentionDays")
	delete(raw, "conversionRetentionDays")
	raw["postbackTtlSeconds"] = 15552000
	r.Set("definition", raw)
	must(t, save(x.app, r))
	cfg, err := Load(x.app)
	must(t, err)
	if cfg.PendingRetentionDays != 14 || cfg.ConversionRetentionDays != 0 {
		t.Fatal("legacy config did not receive defaults")
	}
	for _, days := range []int{-1, 36501} {
		bad := *cfg
		bad.PendingRetentionDays = days
		if _, err = Configure(x.app, bad); err == nil {
			t.Fatal("invalid retention accepted")
		}
		current, err := Load(x.app)
		must(t, err)
		if current.Version != cfg.Version || current.PendingRetentionDays != 14 {
			t.Fatal("invalid settings partially saved")
		}
	}
	cfg.PendingRetentionDays = 0
	_, err = Configure(x.app, *cfg)
	must(t, err)
	current, err := Load(x.app)
	must(t, err)
	if current.PendingRetentionDays != 0 {
		t.Fatal("explicit unlimited replaced by default")
	}
}

func TestCleanupTransactionRollback(t *testing.T) {
	x := setup(t, true)
	now := time.Now().Unix()
	for i := 0; i < 2; i++ {
		agePending(t, x, tokenFrom(x.issue(t)), now-15*86400)
	}
	deleted := 0
	x.app.OnRecordDelete().Bind(&hook.Handler[*core.RecordEvent]{Id: "test-cleanup-failure", Func: func(e *core.RecordEvent) error {
		if e.Record.Collection().Name == ConversationsCollection {
			deleted++
			if deleted == 2 {
				return fmt.Errorf("test failure")
			}
		}
		return e.Next()
	}})
	if n, err := cleanupAt(x.app, now); err == nil || n != 0 {
		t.Fatal("cleanup failure not reported")
	}
	rows, err := x.app.FindAllRecords(ConversationsCollection)
	must(t, err)
	if len(rows) != 2 {
		t.Fatal("partial cleanup committed")
	}
	x.app.OnRecordDelete().Unbind("test-cleanup-failure")
	if n, err := cleanupAt(x.app, now); err != nil || n != 2 {
		t.Fatalf("retry %d %v", n, err)
	}
}

func TestConcurrentApplicationsCannotClaimSamePartnerID(t *testing.T) {
	x := setup(t, true)
	tokens := []string{tokenFrom(x.issue(t)), tokenFrom(x.issue(t))}
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range tokens {
		wg.Add(1)
		go func(i int) { defer wg.Done(); codes[i] = postConversion(x, tokens[i], "new", "same-order") }(i)
	}
	wg.Wait()
	if !((codes[0] == 200 && codes[1] == 400) || (codes[0] == 400 && codes[1] == 200)) {
		t.Fatalf("concurrent claim: %v", codes)
	}
	if x.eventCount() != 1 {
		t.Fatal("conflicting claim delivered")
	}
}

func TestLegacyConversationMigrationPreservesOrdersAndRollsBack(t *testing.T) {
	x := setup(t, true)
	token := tokenFrom(x.issue(t))
	if code := postConversion(x, token, "yes", "legacy-order"); code != 200 {
		t.Fatalf("postback %d", code)
	}
	r, _, err := loadConversation(x.app, token)
	must(t, err)
	id := r.Id
	// Recreate the previous release's service schema on this isolated test DB.
	c, err := x.app.FindCollectionByNameOrId(ConversationsCollection)
	must(t, err)
	for _, name := range []string{"tokenHash", "clickData", "statusChangedAt"} {
		c.Fields.RemoveByName(name)
	}
	c.Fields.GetByName("leadId").(*core.TextField).Required = true
	c.Fields.GetByName("status").(*core.SelectField).Values = []string{"lead", "approved", "hold", "rejected"}
	c.Indexes = []string{legacyConversationIndex}
	c.System = false
	must(t, x.app.SaveNoValidateWithContext(context.WithValue(context.Background(), internalKey{}, true), c))
	// Inject a failure after schema migration but before commit.
	x.app.OnCollectionUpdate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "test-migration-failure", Func: func(e *core.CollectionEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		if e.Collection.Name == ConversationsCollection {
			return fmt.Errorf("test failure")
		}
		return nil
	}})
	if err = installConversations(x.app, []string{"members"}); err == nil {
		t.Fatal("migration failure swallowed")
	}
	unchanged, err := x.app.FindCollectionByNameOrId(c.Id)
	must(t, err)
	if unchanged.System || unchanged.Fields.GetByName("tokenHash") != nil {
		t.Fatal("partial schema migration committed")
	}
	x.app.OnCollectionUpdate().Unbind("test-migration-failure")
	must(t, installConversations(x.app, []string{"members"}))
	migrated, err := x.app.FindRecordById(ConversationsCollection, id)
	must(t, err)
	if !migrated.Collection().System {
		t.Fatal("legacy collection not promoted to system")
	}
	if migrated.GetString("leadId") != "legacy-order" || migrated.GetString("status") != "approved" || migrated.GetString("user") != r.GetString("user") || migrated.GetInt64("statusChangedAt") != r.GetInt64("eventTimestamp") {
		t.Fatal("legacy order lost")
	}
	before, _ := json.Marshal(migrated.PublicExport())
	must(t, installConversations(x.app, []string{"members"}))
	again, err := x.app.FindRecordById(ConversationsCollection, id)
	must(t, err)
	after, _ := json.Marshal(again.PublicExport())
	if string(before) != string(after) {
		t.Fatal("migration not idempotent")
	}
	x.issue(t) // relaxed leadId constraint admits a new pending conversion
}

func TestDelayedLeadIDBindsWithoutRewindingStatus(t *testing.T) {
	x := setup(t, true)
	token := tokenFrom(x.issue(t))
	if code := postConversion(x, token, "wait", ""); code != 200 {
		t.Fatalf("anonymous hold %d", code)
	}
	r, _, err := loadConversation(x.app, token)
	must(t, err)
	originalChanged := r.GetInt64("statusChangedAt")
	q := url.Values{"secret": {testProvider().Secret}, "subid": {token}, "status": {"new"}, "lead_id": {"late-id"}, "timestamp": {fmt.Sprint(time.Now().Unix() - 100)}}
	w := x.request("GET", "/api/partnerlinks/postbacks/test?"+q.Encode(), "", "", "")
	if w.Code != 200 {
		t.Fatalf("delayed lead %d", w.Code)
	}
	r, _, err = loadConversation(x.app, token)
	must(t, err)
	if r.GetString("status") != "hold" || r.GetString("leadId") != "late-id" || r.GetInt64("statusChangedAt") != originalChanged {
		t.Fatal("delayed application ID not bound correctly")
	}
	if code := postConversion(x, token, "yes", "different-id"); code != 400 {
		t.Fatalf("ID replaced %d", code)
	}
}
