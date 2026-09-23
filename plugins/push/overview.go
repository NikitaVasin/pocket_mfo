package push

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type OverviewInput struct {
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
}
type OverviewDay struct {
	Date   string             `json:"date"`
	Values map[string]float64 `json:"values"`
}
type OverviewCampaign struct {
	ID     string             `json:"id"`
	Name   string             `json:"name"`
	RunID  string             `json:"runId"`
	Totals map[string]float64 `json:"totals"`
	Days   []OverviewDay      `json:"days"`
}
type AnalyticsOverview struct {
	DateFrom      string                      `json:"dateFrom"`
	DateTo        string                      `json:"dateTo"`
	Source        string                      `json:"source"`
	Currency      string                      `json:"currency"`
	Timezone      string                      `json:"timezone"`
	Campaigns     []OverviewCampaign          `json:"campaigns"`
	Sections      map[string]AnalyticsSection `json:"sections"`
	FetchedAt     time.Time                   `json:"fetchedAt"`
	NextRefreshAt time.Time                   `json:"nextRefreshAt"`
}

// AnalyticsOverview compares the latest ten campaigns that started sending by
// dateTo. Activity is measured in the requested date window, including late
// conversions of earlier sends. Test runs and future schedules are excluded.
func (p *Plugin) AnalyticsOverview(ctx context.Context, app core.App, in OverviewInput) (AnalyticsOverview, error) {
	var out AnalyticsOverview
	from, e1 := time.Parse(time.DateOnly, in.DateFrom)
	to, e2 := time.Parse(time.DateOnly, in.DateTo)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if e1 != nil || e2 != nil || from.After(to) || to.Sub(from) > 89*24*time.Hour || to.After(today) {
		return out, textError("выберите период от 1 до 90 дней, не позднее сегодня (UTC)")
	}
	cfg, err := Load(app)
	if err != nil {
		return out, err
	}
	if cfg.ApplicationID <= 0 || cfg.OAuthToken == "" {
		return out, textError("настройте AppMetrica и OAuth-токен для чтения статистики")
	}
	out = AnalyticsOverview{Source: "AppMetrica", Currency: "RUB", Timezone: "UTC", DateFrom: in.DateFrom, DateTo: in.DateTo, Campaigns: []OverviewCampaign{}, Sections: map[string]AnalyticsSection{}}
	// The query deliberately has no last-100-runs limit: dates can address history.
	records, err := app.FindRecordsByFilter(RunsCollection, "scheduledAt < {:until}", "-scheduledAt,-id", 0, 0, dbx.Params{"until": to.AddDate(0, 0, 1).Format("2006-01-02 15:04:05.000Z")})
	if err != nil {
		return out, err
	}
	indices := map[string]int{}
	groupCampaign := map[string]string{}
	groupFilters := []string{}
	for _, r := range records {
		var d runDefinition
		if err = decodeRecord(r, &d); err != nil {
			return out, err
		}
		if d.Test || d.GroupID <= 0 || d.ApplicationID != cfg.ApplicationID {
			continue
		}
		id := r.GetString("campaignId")
		if _, ok := indices[id]; !ok {
			if len(out.Campaigns) == 10 {
				continue
			}
			indices[id] = len(out.Campaigns)
			c := OverviewCampaign{ID: id, Name: r.GetString("name"), RunID: r.Id, Totals: map[string]float64{}, Days: []OverviewDay{}}
			for date := from; !date.After(to); date = date.AddDate(0, 0, 1) {
				c.Days = append(c.Days, OverviewDay{Date: date.Format(time.DateOnly), Values: map[string]float64{}})
			}
			out.Campaigns = append(out.Campaigns, c)
		}
		group := strconv.FormatInt(d.GroupID, 10)
		if _, ok := groupCampaign[group]; !ok {
			groupCampaign[group] = id
			groupFilters = append(groupFilters, "ym:pc:group=="+analyticsLiteral(group))
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	keyData, _ := json.Marshal([]any{cfg.ApplicationID, hash(cfg.OAuthToken), in, out.Campaigns, groupFilters})
	key := hash(string(keyData))
	select {
	case p.analyticsGate <- struct{}{}:
		defer func() { <-p.analyticsGate }()
	case <-ctx.Done():
		return out, textError("запрос обзора отменён")
	}
	if cached, ok := p.overviewCache[key]; ok && time.Now().Before(cached.NextRefreshAt) {
		return cached, nil
	}
	if len(out.Campaigns) > 0 {
		base := url.Values{"ids": {strconv.FormatInt(cfg.ApplicationID, 10)}, "date1": {in.DateFrom}, "date2": {in.DateTo}, "timezone": {"+00:00"}, "accuracy": {"full"}, "include_undefined": {"true"}, "limit": {"10000"}}
		eventFilters, revenueFilters := []string{}, []string{}
		for _, c := range out.Campaigns {
			eventFilters = append(eventFilters, "ym:ce2:paramValue{'clickData.push.campaignId'}=="+analyticsLiteral(c.ID))
			revenueFilters = append(revenueFilters, "ym:r2:paramValue{'clickData.push.campaignId'}=="+analyticsLiteral(c.ID))
		}
		// Daily push event counts are additive across runs. Unique device counts are
		// deliberately not summed, as one device can receive several campaign sends.
		requests := []struct {
			section, metrics, dimensions, filter string
			size                                 int
			daily                                bool
		}{
			{"push", "ym:pc:sentEvents,ym:pc:receivedEvents,ym:pc:openedEvents", "ym:pc:group,ym:pc:date", "(" + strings.Join(groupFilters, " OR ") + ")", 3, true},
			{"events", "ym:ce2:uniqParamValues{'clickData.clickId'}", "ym:ce2:paramValue{'clickData.push.campaignId'},ym:ce2:paramValue{'conversion.status'}", "(" + strings.Join(eventFilters, " OR ") + ")", 1, false},
			{"eventDays", "ym:ce2:uniqParamValues{'clickData.clickId'}", "ym:ce2:paramValue{'clickData.push.campaignId'},ym:ce2:date,ym:ce2:paramValue{'conversion.status'}", "(" + strings.Join(eventFilters, " OR ") + ")", 1, true},
			{"revenue", "ym:r2:inappRevenueRUB", "ym:r2:paramValue{'clickData.push.campaignId'},ym:r2:date,ym:r2:paramValue{'conversion.status'}", "(" + strings.Join(revenueFilters, " OR ") + ") AND ym:r2:inappRevenueType=='one_time_purchase' AND (ym:r2:paramValue{'conversion.status'}=='approved' OR ym:r2:paramValue{'conversion.status'}=='hold')", 1, true},
		}
		for _, request := range requests {
			if len(request.filter) > 4000 {
				out.Sections[request.section] = unavailableAnalytics("Слишком много отправок для обзора. Используйте отчёт отдельной кампании.")
				continue
			}
			data, e := p.queryAnalytics(ctx, cfg, base, request.metrics, request.dimensions, request.filter, request.size)
			section := analyticsSection(data, e)
			if e == nil {
				// Validate all dimensions before exposing any values from this section.
				for _, row := range data.Data {
					id := overviewDimension(row, 0)
					if request.section == "push" {
						id = groupCampaign[id]
					}
					if _, ok := indices[id]; !ok {
						section = unavailableAnalytics("AppMetrica вернула неизвестную кампанию.")
						break
					}
					if request.daily {
						date, err := time.Parse(time.DateOnly, overviewDimension(row, 1))
						if err != nil || date.Before(from) || date.After(to) {
							section = unavailableAnalytics("AppMetrica вернула дату вне периода.")
							break
						}
					}
					if request.section != "push" {
						status := overviewStatus(row, len(row.Dimensions)-1)
						allowed := status == "click" || status == "lead" || status == "approved" || status == "hold" || status == "rejected"
						if request.section == "revenue" {
							allowed = status == "approved" || status == "hold"
						}
						if !allowed {
							section = unavailableAnalytics("AppMetrica вернула неизвестный этап.")
							break
						}
					}
				}
			}
			out.Sections[request.section] = section
			if section.Status != "ready" {
				continue
			}
			keys := []string{"click", "lead", "approved", "hold", "rejected"}
			if request.section == "push" {
				keys = []string{"sent", "received", "opened"}
			}
			if request.section == "revenue" {
				keys = []string{"revenue", "holdRevenue"}
			}
			for i := range out.Campaigns {
				c := &out.Campaigns[i]
				if request.section != "eventDays" {
					for _, key := range keys {
						c.Totals[key] = 0
					}
				}
				if request.daily {
					for j := range c.Days {
						for _, key := range keys {
							c.Days[j].Values[key] = 0
						}
					}
				}
			}
			for _, row := range data.Data {
				id := overviewDimension(row, 0)
				if request.section == "push" {
					id = groupCampaign[id]
				}
				c := &out.Campaigns[indices[id]]
				var day map[string]float64
				if request.daily {
					date, _ := time.Parse(time.DateOnly, overviewDimension(row, 1))
					day = c.Days[int(date.Sub(from)/(24*time.Hour))].Values
				}
				if request.section == "push" {
					for i, key := range keys {
						c.Totals[key] += *row.Metrics[i]
						day[key] += *row.Metrics[i]
					}
				} else {
					key := overviewStatus(row, len(row.Dimensions)-1)
					if request.section == "revenue" {
						if key == "approved" {
							key = "revenue"
						} else {
							key = "holdRevenue"
						}
					}
					if request.section != "eventDays" {
						c.Totals[key] += *row.Metrics[0]
					}
					if request.daily {
						day[key] += *row.Metrics[0]
					}
				}
			}
		}
	}
	out.FetchedAt = time.Now().UTC()
	ttl := 5 * time.Minute
	for _, section := range out.Sections {
		if section.Status != "ready" {
			ttl = 30 * time.Second
		}
	}
	out.NextRefreshAt = out.FetchedAt.Add(ttl)
	if ctx.Err() == nil {
		if len(p.overviewCache) >= 16 {
			clear(p.overviewCache)
		}
		p.overviewCache[key] = out
	}
	return out, nil
}
func overviewDimension(row analyticsRow, index int) string {
	d := row.Dimensions[index]
	if d.ID != nil {
		return *d.ID
	}
	if d.Name != nil {
		return *d.Name
	}
	return ""
}
func overviewStatus(row analyticsRow, index int) string {
	if row.Dimensions[index].Name == nil {
		return "click"
	}
	return *row.Dimensions[index].Name
}
