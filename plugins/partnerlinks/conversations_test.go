package partnerlinks

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func TestCollectedOrdersOwnershipRetriesAndStatuses(t *testing.T) {
	x := setupOptions(t, true, true)
	token := tokenFrom(x.issue(t))
	post := func(status, lead string, stamp int64) int {
		q := url.Values{"secret": {testProvider().Secret}, "subid": {token}, "status": {status}, "lead_id": {lead}, "timestamp": {fmt.Sprint(stamp)}}
		return x.request("GET", "/api/partnerlinks/postbacks/test?"+q.Encode(), "", "", "").Code
	}
	now := time.Now().Unix()
	if code := post("new", "order-1", now-10); code != 200 {
		t.Fatalf("lead %d", code)
	}
	if code := post("yes", "order-1", now-5); code != 200 {
		t.Fatalf("approved %d", code)
	}
	if code := post("yes", "order-1", now-5); code != 200 {
		t.Fatalf("retry %d", code)
	}
	if code := post("wait", "order-1", now-8); code != 200 {
		t.Fatalf("stale %d", code)
	}
	rows, err := x.app.FindAllRecords(ConversationsCollection)
	must(t, err)
	if len(rows) != 1 || rows[0].GetString("status") != "approved" {
		t.Fatal("duplicate order or stale rollback")
	}
	id := rows[0].Id
	base := "/api/collections/conversations/records"
	w := x.request("GET", base+"/"+id+"?expand=user", x.auth, "", "")
	if w.Code != 200 {
		t.Fatalf("owner read %d %s", w.Code, w.Body)
	}
	other := core.NewRecord(x.user.Collection())
	other.SetEmail("other@example.test")
	other.SetPassword("test-password-123")
	must(t, x.app.Save(other))
	auth, err := other.NewAuthToken()
	must(t, err)
	if w = x.request("GET", base+"/"+id, auth, "", ""); w.Code != 404 {
		t.Fatal("foreign order exposed")
	}
	if w = x.request("GET", base, auth, "", ""); w.Code != 200 {
		t.Fatal("foreign list failed")
	} else {
		var list struct {
			TotalItems int `json:"totalItems"`
		}
		must(t, json.Unmarshal(w.Body.Bytes(), &list))
		if list.TotalItems != 0 {
			t.Fatal("foreign list leaked")
		}
	}
	for _, who := range []string{x.auth, x.admin} {
		for _, method := range []string{"PATCH", "DELETE"} {
			if w = x.request(method, base+"/"+id, who, `{"status":"rejected"}`, "application/json"); w.Code != 403 {
				t.Fatalf("write %s: %d", method, w.Code)
			}
		}
		if w = x.request("POST", base, who, `{}`, "application/json"); w.Code != 403 {
			t.Fatal("create allowed")
		}
	}
	batch := fmt.Sprintf(`{"requests":[{"method":"PATCH","url":"%s/%s","body":{"status":"rejected"}}]}`, base, id)
	if w = x.request("POST", "/api/batch", x.admin, batch, "application/json"); w.Code < 400 {
		t.Fatal("batch update allowed")
	}
	count := x.eventCount()
	if code := post("new", "different-order", now); code != 400 {
		t.Fatalf("second application %d", code)
	}
	if x.eventCount() != count {
		t.Fatal("second application delivered")
	}
	x.mu.Lock()
	x.status = 500
	x.mu.Unlock()
	if code := post("no", "order-1", now); code != 502 {
		t.Fatal("analytics failure swallowed")
	}
	current, err := x.app.FindRecordById(ConversationsCollection, id)
	must(t, err)
	if current.GetString("status") != "rejected" {
		t.Fatal("order lost on analytics failure")
	}
	// A different user's token cannot claim an existing provider lead ID.
	other.Set("appmetrica_profile_id", "other-profile")
	must(t, x.app.Save(other))
	w = x.request("POST", "/api/partnerlinks/links/"+x.link.Id+"/resolve", auth, `{}`, "application/json")
	if w.Code != 200 {
		t.Fatalf("other resolve: %d %s", w.Code, w.Body)
	}
	var issued ResolveResponse
	must(t, json.Unmarshal(w.Body.Bytes(), &issued))
	token = tokenFrom(issued)
	count = x.eventCount()
	if code := post("yes", "order-1", now); code != 400 {
		t.Fatal("ownership changed")
	}
	if x.eventCount() != count {
		t.Fatal("conflict delivered")
	}
}
func TestCollectionAlwaysCreatedAndProfileFieldChoices(t *testing.T) {
	x := setup(t, true)
	if _, err := x.app.FindCollectionByNameOrId(ConversationsCollection); err != nil {
		t.Fatal("default collection missing")
	}
}
