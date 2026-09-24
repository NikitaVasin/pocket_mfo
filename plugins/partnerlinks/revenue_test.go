package partnerlinks

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func TestRevenueSelectedStatusAndValidationBeforeSideEffects(t *testing.T) {
	for _, selected := range []string{"", "approved", "hold"} {
		name := selected
		if name == "" {
			name = "legacy_default"
		}
		t.Run(name, func(t *testing.T) {
			rawRevenueStatus := "yes"
			if selected == "hold" {
				rawRevenueStatus = "wait"
			}
			x := setupOptions(t, true, true)
			x.config.Providers[0].SendRevenue = true
			x.config.Providers[0].RevenueStatus = selected
			var err error
			x.config, err = Configure(x.app, *x.config)
			must(t, err)
			token := tokenFrom(x.issue(t))
			click, err := readClick(x.app, token)
			must(t, err)
			q := url.Values{"secret": {testProvider().Secret}, "subid": {token}, "status": {rawRevenueStatus}, "lead_id": {"order-1"}, "amount": {"123.45000001"}, "currency": {"RUB"}}
			post := func() int { return x.request("GET", "/api/partnerlinks/postbacks/test?"+q.Encode(), "", "", "").Code }
			for _, invalid := range []struct{ key, value string }{{"amount", "1e3"}, {"amount", "10000000000"}, {"amount", "1.000000001"}, {"currency", "rub"}, {"lead_id", ""}} {
				old := q.Get(invalid.key)
				q.Set(invalid.key, invalid.value)
				if code := post(); code != 400 {
					t.Fatalf("invalid %v: %d", invalid, code)
				}
				q.Set(invalid.key, old)
			}
			if x.eventCount() != 0 {
				t.Fatal("invalid revenue emitted events")
			}
			rows, err := x.app.FindAllRecords(ConversationsCollection)
			must(t, err)
			if len(rows) != 1 || rows[0].GetString("status") != "pending" {
				t.Fatal("invalid revenue saved order")
			}
			for _, status := range []string{"new", "wait", "no", "yes"} {
				before := x.eventCount()
				q.Set("status", status)
				q.Set("amount", "123.45000001")
				q.Set("currency", "RUB")
				if status != rawRevenueStatus {
					q.Del("amount")
					q.Del("currency")
				}
				if code := post(); code != 200 {
					t.Fatalf("post %s: %d", status, code)
				}
				want := 1
				if status == rawRevenueStatus {
					want = 2
				}
				if x.eventCount()-before != want {
					t.Fatal("wrong number of events")
				}
			}
			x.mu.Lock()
			var event url.Values
			for _, candidate := range x.events {
				if candidate.Get("revenue_event_type") != "" {
					event = candidate
				}
			}
			x.mu.Unlock()
			if event.Get("revenue_event_type") != "one_time_purchase" || event.Get("price") != "123.45000001" || event.Get("currency") != "RUB" || event.Get("product_id") != x.link.Id || event.Get("profile_id") != x.user.Id || event.Get("session_type") != "foreground" || event.Get("quantity") != "1" || event.Get("event_name") != "" {
				t.Fatalf("wrong revenue: %v", event)
			}
			var payload map[string]any
			must(t, json.Unmarshal([]byte(event.Get("payload")), &payload))
			if payload["conversion"].(map[string]any)["leadId"] != "order-1" {
				t.Fatal("missing order attribution")
			}
			var gotExperiments map[string]string
			encoded, err := json.Marshal(payload["experiments"])
			must(t, err)
			must(t, json.Unmarshal(encoded, &gotExperiments))
			if !reflect.DeepEqual(gotExperiments, click.AnalyticsExperiments) {
				t.Fatal("Revenue lost experiment snapshot")
			}
			// A receipt prevents a duplicate request even if the provider is down.
			before := x.eventCount()
			x.mu.Lock()
			x.revenueStatus = 503
			x.mu.Unlock()
			q.Set("status", rawRevenueStatus)
			q.Set("amount", "123.45000001")
			q.Set("currency", "RUB")
			if code := post(); code != 200 || x.eventCount() != before {
				t.Fatalf("duplicate was sent again: %d", code)
			}

		})
	}
}

func TestRevenueStatusLegacyConfigAndInvalidUpdateRollback(t *testing.T) {
	x := setup(t, false)
	record, err := x.app.FindRecordById(configsCollection, configID)
	must(t, err)
	var legacy map[string]any
	must(t, json.Unmarshal([]byte(record.GetString("definition")), &legacy))
	for _, p := range legacy["providers"].([]any) {
		delete(p.(map[string]any), "revenueStatus")
	}
	record.Set("definition", legacy)
	must(t, save(x.app, record))
	record, err = x.app.FindRecordById(configsCollection, configID)
	must(t, err)
	before := record.GetString("definition")
	w := x.request("GET", "/api/partnerlinks/admin/config", x.admin, "", "")
	if w.Code != 200 {
		t.Fatalf("get config %d", w.Code)
	}
	var current Config
	must(t, json.Unmarshal(w.Body.Bytes(), &current))
	if current.Providers[0].RevenueStatus != "approved" {
		t.Fatal("old config lost approved default")
	}
	for _, invalid := range []string{"lead", "rejected", "click", "offer_hold", "unknown"} {
		current.Providers[0].RevenueStatus = invalid
		body, err := json.Marshal(current)
		must(t, err)
		w = x.request("PUT", "/api/partnerlinks/admin/config", x.admin, string(body), "application/json")
		if w.Code != 400 {
			t.Fatalf("accepted revenueStatus %q: %d", invalid, w.Code)
		}
		stored, err := x.app.FindRecordById(configsCollection, configID)
		must(t, err)
		if stored.GetString("definition") != before {
			t.Fatal("invalid setting partially saved")
		}
	}
}
