package push

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func overviewRun(t *testing.T, x *fixture, id string, group int64, day string, test bool) {
	t.Helper()
	coll, err := x.app.FindCollectionByNameOrId(RunsCollection)
	must(t, err)
	r := core.NewRecord(coll)
	r.Set("campaignId", id)
	r.Set("name", id)
	r.Set("requestKey", secret())
	r.Set("scheduledAt", day+" 10:00:00.000Z")
	r.Set("definition", runDefinition{ApplicationID: 123, GroupID: group, Test: test})
	must(t, save(x.app, r))
}
func TestOverviewLastTenAndPeriodDistinctCounts(t *testing.T) {
	x := setup(t, true)
	for i := range 12 {
		overviewRun(t, x, fmt.Sprintf("campaign%02d", i), int64(200+i), fmt.Sprintf("2025-01-%02d", i+1), false)
	}
	overviewRun(t, x, "campaign11", 212, "2025-01-13", false)
	overviewRun(t, x, "test-campaign", 998, "2025-01-19", true)
	overviewRun(t, x, "future-campaign", 999, "2025-02-01", false)
	calls := 0
	historical := false
	x.p.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		filter := q.Get("filters")
		dimensions := q.Get("dimensions")
		if r.Method != "GET" || r.Header.Get("Authorization") != "OAuth server-only-token" || q.Get("limit") != "10000" {
			t.Fatal("invalid overview request")
		}
		if strings.Contains(filter, "998") || strings.Contains(filter, "999") || strings.Contains(filter, "test-campaign") || strings.Contains(filter, "future-campaign") {
			t.Fatal("test or future send included")
		}
		size := 1
		if strings.HasPrefix(dimensions, "ym:pc:") {
			size = 3
		}
		totals := make([]float64, size)
		var rows []any
		row := func(dims []any, values ...float64) any { return map[string]any{"dimensions": dims, "metrics": values} }
		name := func(value string) any { return map[string]any{"name": value} }
		if !historical {
			if q.Get("date1") != "2025-01-01" || q.Get("date2") != "2025-01-20" {
				t.Fatal(q)
			}
			if strings.Contains(filter, "campaign00") || strings.Contains(filter, "campaign01") {
				t.Fatal("oldest campaigns included")
			}
			switch {
			case strings.HasPrefix(dimensions, "ym:pc:"):
				if strings.Contains(filter, "'200'") || strings.Contains(filter, "'201'") || !strings.Contains(filter, "'212'") {
					t.Fatal(filter)
				}
				rows = []any{row([]any{map[string]any{"id": "211", "name": "Remote group name"}, name("2025-01-15")}, 2, 2, 1), row([]any{map[string]any{"id": "212", "name": "Another group"}, name("2025-01-16")}, 1, 1, 1)}
			case strings.HasPrefix(dimensions, "ym:r2:"):
				rows = []any{row([]any{name("campaign11"), name("2025-01-15"), name("approved")}, 10.25), row([]any{name("campaign11"), name("2025-01-16"), name("hold")}, 20)}
			case strings.Contains(dimensions, "ym:ce2:date"):
				rows = []any{row([]any{name("campaign11"), name("2025-01-15"), name("lead")}, 1), row([]any{name("campaign11"), name("2025-01-16"), name("lead")}, 1)}
			default:
				rows = []any{row([]any{name("campaign11"), name("lead")}, 1), row([]any{name("campaign11"), map[string]any{"name": nil}}, 2)}
			}
		}
		body, _ := json.Marshal(map[string]any{"totals": totals, "data": rows, "total_rows": len(rows), "sampled": false, "data_lag": 12})
		return statResponse(string(body), 200), nil
	})
	input := OverviewInput{DateFrom: "2025-01-01", DateTo: "2025-01-20"}
	for _, auth := range []string{"", x.auth} {
		w := x.request("POST", "/api/push/admin/analytics_overview", auth, input)
		if w.Code < 400 || calls != 0 {
			t.Fatal("unprivileged overview")
		}
	}
	w := x.request("POST", "/api/push/admin/analytics_overview", x.admin, input)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out AnalyticsOverview
	must(t, json.Unmarshal(w.Body.Bytes(), &out))
	if len(out.Campaigns) != 10 || out.Campaigns[0].ID != "campaign11" || calls != 4 {
		t.Fatal("incorrect campaign selection", len(out.Campaigns), calls)
	}
	c := out.Campaigns[0]
	if c.Totals["sent"] != 3 || c.Totals["opened"] != 2 || c.Totals["lead"] != 1 || c.Totals["revenue"] != 10.25 || c.Totals["holdRevenue"] != 20 {
		t.Fatal(c.Totals)
	}
	if c.Days[14].Values["lead"] != 1 || c.Days[15].Values["lead"] != 1 || c.Days[0].Values["lead"] != 0 {
		t.Fatal("daily values lost")
	}
	_, err := x.p.AnalyticsOverview(t.Context(), x.app, input)
	must(t, err)
	if calls != 4 {
		t.Fatal("overview cache missed")
	}
	historical = true
	old, err := x.p.AnalyticsOverview(t.Context(), x.app, OverviewInput{DateFrom: "2025-01-01", DateTo: "2025-01-02"})
	must(t, err)
	if len(old.Campaigns) != 2 || old.Campaigns[0].ID != "campaign01" {
		t.Fatal("historical selection ignored cutoff")
	}
}
func TestOverviewValidationAndPartialFailure(t *testing.T) {
	x := setup(t, true)
	overviewRun(t, x, "campaign-one", 701, "2025-01-01", false)
	calls := 0
	x.p.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		size := 1
		if strings.HasPrefix(r.URL.Query().Get("metrics"), "ym:pc:") {
			size = 3
		}
		if strings.Contains(r.URL.Query().Get("dimensions"), "ym:r2:date") {
			return statResponse(`{"totals":[100],"data":[],"total_rows":10001,"sampled":false}`, 200), nil
		}
		data, _ := json.Marshal(map[string]any{"totals": make([]float64, size), "data": []any{}, "total_rows": 0, "sampled": false})
		return statResponse(string(data), 200), nil
	})
	for _, in := range []OverviewInput{{"invalid", "2025-01-01"}, {"2025-01-02", "2025-01-01"}, {"2025-01-01", "2025-12-01"}, {"2025-01-01", time.Now().AddDate(1, 0, 0).Format(time.DateOnly)}} {
		if _, err := x.p.AnalyticsOverview(t.Context(), x.app, in); err == nil || calls != 0 {
			t.Fatal("invalid period accepted")
		}
	}
	out, err := x.p.AnalyticsOverview(t.Context(), x.app, OverviewInput{"2025-01-01", "2025-01-02"})
	must(t, err)
	if out.Sections["revenue"].Status != "unavailable" || out.Sections["push"].Status != "ready" {
		t.Fatal(out.Sections)
	}
	if _, ok := out.Campaigns[0].Totals["revenue"]; ok {
		t.Fatal("incomplete revenue shown as zero")
	}
	if _, ok := out.Campaigns[0].Days[0].Values["revenue"]; ok {
		t.Fatal("incomplete series shown as zero")
	}
	if out.NextRefreshAt.Sub(out.FetchedAt) != 30*time.Second {
		t.Fatal("bad failed cache ttl")
	}
}
