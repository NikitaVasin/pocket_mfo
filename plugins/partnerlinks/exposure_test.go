package partnerlinks

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"

	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase/core"
)

func TestShownExperimentsSurviveReallocationThroughConversion(t *testing.T) {
	x := setup(t, true)
	c, err := x.app.FindCollectionByNameOrId("offers")
	must(t, err)
	cfg, err := variants.Load(x.app, c.Id)
	must(t, err)
	d, err := variants.Resolve(x.app, cfg, x.user)
	must(t, err)
	row := core.NewRecord(c)
	row.Set(variants.SetField, d.Set)
	must(t, x.app.Save(row))
	w := x.request("GET", "/api/collections/offers/records/"+row.Id, x.auth, "", "")
	if w.Code != 200 {
		t.Fatalf("content: %d %s", w.Code, w.Body)
	}
	var content struct {
		Context variants.Exposure `json:"variantContext"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &content))
	if content.Context.Token == "" {
		t.Fatal("missing signed context")
	}
	cfg.Experiments = false
	_, err = variants.Publish(x.app, *cfg)
	must(t, err)
	body, err := json.Marshal(map[string]any{"exposureTokens": []string{content.Context.Token}})
	must(t, err)
	w = x.request("POST", "/api/partnerlinks/links/"+x.link.Id+"/resolve", x.auth, string(body), "application/json")
	if w.Code != 200 {
		t.Fatalf("resolve: %d %s", w.Code, w.Body)
	}
	var result ResolveResponse
	must(t, json.Unmarshal(w.Body.Bytes(), &result))
	token := tokenFrom(result)
	data, err := readClick(x.app, token)
	must(t, err)
	if data.AttributionBasis != "content_exposure" || !reflect.DeepEqual(data.AnalyticsExperiments, content.Context.Experiments) || data.Exposures[0].Token != "" {
		t.Fatalf("attribution changed: %+v", data)
	}
	if w := x.request("GET", "/api/partnerlinks/r/"+token, "", "", ""); w.Code != 302 {
		t.Fatal(w.Body)
	}
	q := url.Values{"secret": {testProvider().Secret}, "subid": {token}, "status": {"yes"}, "lead_id": {"lead-exposure"}}
	for i := 0; i < 2; i++ {
		w := x.request("GET", "/api/partnerlinks/postbacks/test?"+q.Encode(), "", "", "")
		if w.Code != 200 {
			t.Fatalf("postback: %d %s", w.Code, w.Body)
		}
	}
	if x.eventCount() != 2 {
		t.Fatal("duplicate conversion event")
	}
	for _, event := range x.events {
		var attrs struct {
			Experiments map[string]string `json:"experiments"`
			Basis       string            `json:"attributionBasis"`
			ClickID     string            `json:"clickId"`
		}
		must(t, json.Unmarshal([]byte(event.Get("event_json")), &attrs))
		if event.Get("profile_id") != x.user.Id || attrs.ClickID != result.ClickID || attrs.Basis != "content_exposure" || !reflect.DeepEqual(attrs.Experiments, content.Context.Experiments) {
			t.Fatal("funnel dimensions diverged")
		}
	}
	before, err := x.app.CountRecords(ConversationsCollection)
	must(t, err)
	w = x.request("POST", "/api/partnerlinks/links/"+x.link.Id+"/resolve", x.auth, `{"exposureTokens":["forged"]}`, "application/json")
	if w.Code != 400 {
		t.Fatal("forged context accepted")
	}
	after, err := x.app.CountRecords(ConversationsCollection)
	must(t, err)
	if before != after || x.eventCount() != 2 {
		t.Fatal("invalid context has side effects")
	}
}

func TestResolveAutomaticallyIncludesNewExperimentCollections(t *testing.T) {
	x := setup(t, false)
	c := core.NewBaseCollection("new_design")
	must(t, x.app.Save(c))
	_, err := variants.Publish(x.app, variants.Config{Collection: c.Id, AuthCollection: x.user.Collection().Id, Variables: true, Default: variants.Variant{Key: "default"}})
	must(t, err)
	issued := x.issue(t)
	data, err := readClick(x.app, tokenFrom(issued))
	must(t, err)
	if data.AnalyticsExperiments[c.Name] != "default" || data.AnalyticsExperiments["offers"] == "" {
		t.Fatal("new collection requires manual options")
	}
	if data.AttributionBasis != "current_assignment" {
		t.Fatal("fallback mislabeled as impression")
	}
}

func TestChangedOpeningPolicyRejectsStaleContextWithoutCreatingClick(t *testing.T) {
	x := setup(t, true)
	must(t, dynamiclink.Configure(x.app, "members"))
	rows, err := x.app.FindAllRecords(dynamiclink.SettingsCollection)
	must(t, err)
	row := rows[0]
	w := x.request("GET", "/api/collections/"+dynamiclink.SettingsCollection+"/records/"+row.Id, x.auth, "", "")
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	var content struct {
		Context variants.Exposure `json:"variantContext"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &content))
	body, err := json.Marshal(map[string]any{"exposureTokens": []string{content.Context.Token}})
	must(t, err)
	resolve := func() int {
		return x.request("POST", "/api/partnerlinks/links/"+x.link.Id+"/resolve", x.auth, string(body), "application/json").Code
	}
	if code := resolve(); code != 200 {
		t.Fatalf("fresh opening context: %d", code)
	}
	row.Set("mode", "appView")
	must(t, x.app.Save(row))
	before, err := x.app.CountRecords(ConversationsCollection)
	must(t, err)
	if code := resolve(); code != 409 {
		t.Fatalf("stale opening context: %d", code)
	}
	after, err := x.app.CountRecords(ConversationsCollection)
	must(t, err)
	if after != before || x.eventCount() != 0 {
		t.Fatal("stale context has side effects")
	}
}
