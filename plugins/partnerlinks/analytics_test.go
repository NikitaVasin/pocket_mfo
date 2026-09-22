package partnerlinks

import (
	"encoding/json"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"reflect"
	"testing"
)

func TestAnalyticsSnapshotSharedWithClientAndFrozenForPostbacks(t *testing.T) {
	x := setup(t, true)
	w := x.request("GET", "/api/variants/me", x.auth, "", "")
	if w.Code != 200 {
		t.Fatalf("me: %d %s", w.Code, w.Body)
	}
	var me struct {
		Experiments map[string]string `json:"experiments"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &me))
	issued := x.issue(t)
	data, err := readClick(x.app, tokenFrom(issued))
	must(t, err)
	if !reflect.DeepEqual(me.Experiments, data.AnalyticsExperiments) || len(me.Experiments) != 1 {
		t.Fatal("client and click snapshots differ")
	}
	cfg, err := variants.Load(x.app, "offers")
	must(t, err)
	cfg.Variables = false
	cfg.Experiments = false
	_, err = variants.Publish(x.app, *cfg)
	must(t, err)
	w = x.request("GET", "/api/variants/me", x.auth, "", "")
	me.Experiments = nil
	must(t, json.Unmarshal(w.Body.Bytes(), &me))
	if len(me.Experiments) != 0 {
		t.Fatal("disabled variables included")
	}
	w = x.request("GET", "/api/partnerlinks/r/"+tokenFrom(issued), "", "", "")
	if w.Code != 302 {
		t.Fatal(w.Body.String())
	}
	w = x.request("GET", "/api/partnerlinks/postbacks/test?secret="+testProvider().Secret+"&subid="+tokenFrom(issued)+"&status=new", "", "", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, event := range x.events {
		if event.Get("profile_id") != x.user.Id {
			t.Fatal("wrong profile")
		}
		var body struct {
			Experiments map[string]string `json:"experiments"`
			ClickData   map[string]any    `json:"clickData"`
		}
		must(t, json.Unmarshal([]byte(event.Get("event_json")), &body))
		if !reflect.DeepEqual(body.Experiments, data.AnalyticsExperiments) {
			t.Fatal("snapshot changed")
		}
		if _, ok := body.ClickData["profileId"]; ok {
			t.Fatal("duplicate identity retained")
		}
	}
}
