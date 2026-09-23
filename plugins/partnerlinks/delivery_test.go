package partnerlinks

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func revenueRequest(t *testing.T, x *fixture) (string, func() int) {
	t.Helper()
	x.config.Providers[0].SendRevenue = true
	var err error
	x.config, err = Configure(x.app, *x.config)
	must(t, err)
	token := tokenFrom(x.issue(t))
	q := url.Values{"secret": {testProvider().Secret}, "subid": {token}, "status": {"yes"}, "lead_id": {"order"}, "amount": {"12.50"}, "currency": {"RUB"}}
	return token, func() int { return x.request("GET", "/api/partnerlinks/postbacks/test?"+q.Encode(), "", "", "").Code }
}

func TestDeliveryRetriesOnlyUnconfirmedRevenue(t *testing.T) {
	x := setup(t, true)
	token, post := revenueRequest(t, x)
	x.mu.Lock()
	x.revenueStatus = 503
	x.mu.Unlock()
	if post() != 502 || x.eventCount() != 2 {
		t.Fatal("partial failure wasn't retained")
	}
	x.mu.Lock()
	x.revenueStatus = 200
	x.mu.Unlock()
	if post() != 200 || x.eventCount() != 3 {
		t.Fatal("confirmed event was repeated or revenue wasn't retried")
	}
	if post() != 200 || x.eventCount() != 3 {
		t.Fatal("confirmed revenue repeated")
	}
	r, data, err := loadConversation(x.app, token)
	must(t, err)
	state, err := readDelivery(r)
	must(t, err)
	if state.Items["event_approved"].State != "sent" || state.Items["revenue"].State != "sent" {
		t.Fatal("receipts missing")
	}
	x.mu.Lock()
	for _, e := range x.events {
		if e.Get("revenue_event_type") != "" && (e.Get("transaction_id") != data.ClickID || e.Get("order_id") != "order") {
			t.Error("unstable transaction identity")
		}
	}
	x.mu.Unlock()
	for _, auth := range []string{x.auth, x.admin} {
		w := x.request("GET", "/api/collections/conversations/records/"+r.Id, auth, "", "")
		if w.Code != 200 || strings.Contains(w.Body.String(), deliveryField) {
			t.Fatal("delivery state exposed")
		}
	}
}

func TestDeliveryClaimIsAtomicAcrossConcurrentRequests(t *testing.T) {
	x := setup(t, true)
	_, post := revenueRequest(t, x)
	var wg sync.WaitGroup
	codes := make(chan int, 10)
	for range 10 {
		wg.Go(func() { codes <- post() })
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 && code != 502 {
			t.Fatalf("unexpected response %d", code)
		}
	}
	if post() != 200 || x.eventCount() != 2 {
		t.Fatalf("duplicate concurrent sends: %d", x.eventCount())
	}
}

func TestUnknownDeliveryRequiresReconciliationAndSurvivesRestart(t *testing.T) {
	x := setup(t, true)
	token, post := revenueRequest(t, x)
	r, data, err := loadConversation(x.app, token)
	must(t, err)
	conversion := conversionData{Status: "approved", LeadID: "order", Amount: "12.50", Currency: "RUB"}
	must(t, collect(x.app, token, data.IssuedAt, conversion, true))
	calls := 0
	p := &plugin{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("lost response") })}}
	if p.deliverOnce(t.Context(), x.app, x.config, data, token, "event_approved") == nil {
		t.Fatal("ambiguous delivery succeeded")
	}
	if calls != 1 {
		t.Fatal("no external attempt")
	}
	// A new plugin instance uses the persisted claim, not a process mutex.
	p = &plugin{client: p.client}
	if p.deliverOnce(t.Context(), x.app, x.config, data, token, "event_approved") == nil || calls != 1 {
		t.Fatal("ambiguous request repeated")
	}
	if post() != 502 || x.eventCount() != 0 {
		t.Fatal("HTTP retried unknown send")
	}
	must(t, ReconcileDelivery(x.app, r.Id, "event_approved", true))
	if post() != 200 || x.eventCount() != 1 {
		t.Fatal("reconciliation didn't resume remaining revenue")
	}
	if err := ReconcileDelivery(x.app, r.Id, "revenue", false); err == nil {
		t.Fatal("confirmed receipt could be reset")
	}
}

func TestLegacyReceiptIsNotAssumedAndInvalidInputDoesNotChangeIt(t *testing.T) {
	x := setup(t, true)
	token, post := revenueRequest(t, x)
	r, _, err := loadConversation(x.app, token)
	must(t, err)
	r.Set("status", "approved")
	r.Set(deliveryField, nil)
	must(t, save(x.app, r))
	if post() != 502 || x.eventCount() != 0 {
		t.Fatal("legacy record assumed undelivered")
	}
	r, _, err = loadConversation(x.app, token)
	must(t, err)
	before, _ := json.Marshal(r)
	w := x.request("POST", "/api/collections/conversations/records/"+r.Id, x.admin, `{"deliveryState":{}}`, "application/json")
	if w.Code < 400 {
		t.Fatal("untrusted receipt update")
	}
	after, err := x.app.FindRecordById(ConversationsCollection, r.Id)
	must(t, err)
	b, _ := json.Marshal(after)
	if string(before) != string(b) {
		t.Fatal("denied request changed receipt")
	}
	must(t, ReconcileDelivery(x.app, r.Id, "event_approved", true))
	must(t, ReconcileDelivery(x.app, r.Id, "revenue", true))
	if post() != 200 || x.eventCount() != 0 {
		t.Fatal("reconciled legacy sent again")
	}
}
