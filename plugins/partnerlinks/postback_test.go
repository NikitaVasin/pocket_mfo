package partnerlinks

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestPostbackScalarLimitBeforeSideEffects(t *testing.T) {
	x := setup(t, true)
	for _, tc := range []struct {
		name  string
		value any
		want  int
	}{
		{"long_string", strings.Repeat("1", 4097), 400},
		{"long_number", json.Number(strings.Repeat("1", 4097)), 400},
		{"boundary_string", strings.Repeat("1", 4096), 200},
		{"boundary_number", json.Number(strings.Repeat("1", 4096)), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := tokenFrom(x.issue(t))
			before, _, err := loadConversation(x.app, token)
			must(t, err)
			beforeJSON, err := json.Marshal(before)
			must(t, err)
			events := x.eventCount()
			body, err := json.Marshal(map[string]any{"subid": token, "status": "yes", "offer_id": tc.value})
			must(t, err)
			w := x.request("POST", "/api/partnerlinks/postbacks/test?secret="+url.QueryEscape(testProvider().Secret), "", string(body), "application/json")
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body)
			}
			if tc.want == 400 {
				after, _, err := loadConversation(x.app, token)
				must(t, err)
				afterJSON, err := json.Marshal(after)
				must(t, err)
				if !bytes.Equal(beforeJSON, afterJSON) || x.eventCount() != events {
					t.Fatal("invalid scalar changed the record or emitted analytics")
				}
			} else if x.eventCount() != events+1 {
				t.Fatal("valid scalar did not emit analytics")
			}
		})
	}
}

func TestPostbackOmittedFieldsPreserveStorageAndPayloadContract(t *testing.T) {
	x := setup(t, true)
	token := tokenFrom(x.issue(t))
	q := url.Values{"secret": {testProvider().Secret}, "subid": {token}, "status": {"yes"}, "lead_id": {"order-1"}, "event_id": {"event-1"}, "amount": {"0"}, "currency": {"RUB"}, "offer_id": {"offer-1"}}
	post := func() {
		t.Helper()
		w := x.request("GET", "/api/partnerlinks/postbacks/test?"+q.Encode(), "", "", "")
		if w.Code != 200 {
			t.Fatalf("postback %d: %s", w.Code, w.Body)
		}
	}
	post()
	q.Set("status", "wait")
	for _, key := range []string{"lead_id", "event_id", "amount", "currency", "offer_id"} {
		q.Del(key)
	}
	post()
	r, _, err := loadConversation(x.app, token)
	must(t, err)
	for key, want := range map[string]string{"status": "hold", "leadId": "order-1", "eventId": "event-1", "amount": "0", "currency": "RUB"} {
		if r.GetString(key) != want {
			t.Errorf("%s = %q, want %q", key, r.GetString(key), want)
		}
	}
	var extra map[string]string
	must(t, r.UnmarshalJSONField("extra", &extra))
	if extra["offer"] != "offer-1" {
		t.Fatal("omitted extra erased persisted attributes")
	}
	x.mu.Lock()
	payload := x.events[len(x.events)-1].Get("event_json")
	x.mu.Unlock()
	var event struct {
		Conversion map[string]any `json:"conversion"`
	}
	must(t, json.Unmarshal([]byte(payload), &event))
	if len(event.Conversion) != 2 || event.Conversion["status"] != "hold" || event.Conversion["rawStatus"] != "wait" {
		t.Fatalf("omitted fields leaked into outgoing event: %v", event.Conversion)
	}
}
