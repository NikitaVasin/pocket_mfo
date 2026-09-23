package push

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const analyticsHost = "https://api.appmetrica.yandex.com/stat/v1/data"

type AnalyticsInput struct {
	RunID string `json:"runId"`
	Scope string `json:"scope,omitempty"`
}

type AnalyticsSection struct {
	Status         string             `json:"status"`
	Error          string             `json:"error,omitempty"`
	Values         map[string]float64 `json:"values,omitempty"`
	Sampled        bool               `json:"sampled"`
	DataLagSeconds int64              `json:"dataLagSeconds"`
}

type AnalyticsReport struct {
	Source        string           `json:"source"`
	Scope         string           `json:"scope"`
	RunID         string           `json:"runId"`
	CampaignID    string           `json:"campaignId"`
	Test          bool             `json:"test"`
	DateFrom      string           `json:"dateFrom"`
	DateTo        string           `json:"dateTo"`
	Timezone      string           `json:"timezone"`
	Currency      string           `json:"currency"`
	FetchedAt     time.Time        `json:"fetchedAt"`
	NextRefreshAt time.Time        `json:"nextRefreshAt"`
	Push          AnalyticsSection `json:"push"`
	Events        AnalyticsSection `json:"events"`
	Revenue       AnalyticsSection `json:"revenue"`
}

type analyticsCacheEntry struct{ report AnalyticsReport }

type analyticsRow struct {
	Dimensions []struct {
		Name *string `json:"name"`
		ID   *string `json:"id"`
	} `json:"dimensions"`
	Metrics []*float64 `json:"metrics"`
}

type analyticsResponse struct {
	Totals    []*float64     `json:"totals"`
	Data      []analyticsRow `json:"data"`
	TotalRows *int           `json:"total_rows"`
	Sampled   *bool          `json:"sampled"`
	DataLag   int64          `json:"data_lag"`
}

// Analytics reads aggregates from AppMetrica. Local records only identify the
// application, period and attribution keys; they never supply metric values.
func (p *Plugin) Analytics(ctx context.Context, app core.App, in AnalyticsInput) (AnalyticsReport, error) {
	var out AnalyticsReport
	if in.Scope == "" {
		in.Scope = "run"
	}
	if in.Scope != "run" && in.Scope != "campaign" {
		return out, textError("неизвестный охват аналитики")
	}
	run, err := app.FindRecordById(RunsCollection, in.RunID)
	if err != nil {
		return out, textError("запуск не найден")
	}
	var definition runDefinition
	if err = decodeRecord(run, &definition); err != nil {
		return out, err
	}
	cfg, err := Load(app)
	if err != nil {
		return out, err
	}
	if cfg.ApplicationID != definition.ApplicationID || cfg.OAuthToken == "" {
		return out, textError("проверьте Application ID и OAuth-токен AppMetrica")
	}
	runs := []*core.Record{run}
	if in.Scope == "campaign" {
		runs, err = app.FindRecordsByFilter(RunsCollection, "campaignId={:id}", "created,id", 0, 0, dbx.Params{"id": run.GetString("campaignId")})
		if err != nil {
			return out, err
		}
	}
	now := time.Now().UTC()
	first := now
	groups := []string{}
	hasRuns := false
	for _, item := range runs {
		var d runDefinition
		if err = decodeRecord(item, &d); err != nil {
			return out, err
		}
		if in.Scope == "campaign" && d.Test {
			continue
		}
		if d.ApplicationID != cfg.ApplicationID {
			return out, textError("запуски кампании относятся к разным приложениям AppMetrica")
		}
		hasRuns = true
		if created := item.GetDateTime("created").Time(); created.Before(first) {
			first = created
		}
		if d.GroupID > 0 {
			groups = append(groups, "ym:pc:group=='"+strconv.FormatInt(d.GroupID, 10)+"'")
		}
	}
	out = AnalyticsReport{Source: "AppMetrica", Scope: in.Scope, RunID: run.Id, CampaignID: run.GetString("campaignId"), Test: in.Scope == "run" && definition.Test,
		DateFrom: first.Format(time.DateOnly), DateTo: now.Format(time.DateOnly), Timezone: "UTC", Currency: "RUB"}
	// Including the credential fingerprint prevents serving results after token rotation.
	keyBytes, _ := json.Marshal([]any{out, groups, hash(cfg.OAuthToken)})
	key := hash(string(keyBytes))
	select {
	case p.analyticsGate <- struct{}{}:
		defer func() { <-p.analyticsGate }()
	case <-ctx.Done():
		return out, textError("запрос аналитики отменён")
	}
	if cached, ok := p.analyticsCache[key]; ok && time.Now().Before(cached.report.NextRefreshAt) {
		return cached.report, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	base := url.Values{"ids": {strconv.FormatInt(cfg.ApplicationID, 10)}, "date1": {out.DateFrom}, "date2": {out.DateTo}, "timezone": {"+00:00"}, "accuracy": {"full"}, "include_undefined": {"true"}, "limit": {"100"}}
	if len(groups) == 0 {
		out.Push = unavailableAnalytics("Группа отправки ещё не создана в AppMetrica.")
	} else if filter := strings.Join(groups, " OR "); len(filter) > 4000 {
		out.Push = unavailableAnalytics("Слишком много запусков для общего отчёта доставки. Откройте результаты отдельного запуска.")
	} else {
		data, e := p.queryAnalytics(ctx, cfg, base, "ym:pc:sentDevices,ym:pc:receivedDevices,ym:pc:shownDevices,ym:pc:openedDevices", "", "("+filter+")", 4)
		out.Push = analyticsSection(data, e)
		if e == nil {
			out.Push.Values = map[string]float64{"sent": *data.Totals[0], "received": *data.Totals[1], "shown": *data.Totals[2], "opened": *data.Totals[3]}
		}
	}
	if out.Test || !hasRuns {
		out.Events = AnalyticsSection{Status: "not_applicable", Error: "Тестовые запуски не участвуют в бизнес-атрибуции."}
		out.Revenue = out.Events
	} else {
		attrKey, attrValue := "runId", run.Id
		if in.Scope == "campaign" {
			attrKey, attrValue = "campaignId", out.CampaignID
		}
		// Both are server-created PocketBase IDs, nevertheless escape filter literals.
		eventFilter := "ym:ce2:paramValue{'clickData.push." + attrKey + "'}==" + analyticsLiteral(attrValue)
		data, e := p.queryAnalytics(ctx, cfg, base, "ym:ce2:uniqParamValues{'clickData.clickId'}", "ym:ce2:paramValue{'conversion.status'}", eventFilter, 1)
		out.Events = analyticsSection(data, e)
		if e == nil {
			values := map[string]float64{"click": 0, "lead": 0, "approved": 0, "hold": 0, "rejected": 0}
			for _, row := range data.Data {
				status := "click" // Only the redirect event has no conversion payload.
				if row.Dimensions[0].Name != nil {
					status = *row.Dimensions[0].Name
				}
				if _, ok := values[status]; !ok {
					out.Events = unavailableAnalytics("AppMetrica вернула неизвестный этап конверсии.")
					break
				}
				values[status] += *row.Metrics[0]
			}
			if out.Events.Status == "ready" {
				out.Events.Values = values
			}
		}
		revenueFilter := "ym:r2:paramValue{'clickData.push." + attrKey + "'}==" + analyticsLiteral(attrValue) + " AND ym:r2:inappRevenueType=='one_time_purchase' AND (ym:r2:paramValue{'conversion.status'}=='approved' OR ym:r2:paramValue{'conversion.status'}=='hold')"
		data, e = p.queryAnalytics(ctx, cfg, base, "ym:r2:inappRevenueRUB,ym:r2:purchases", "ym:r2:paramValue{'conversion.status'}", revenueFilter, 2)
		out.Revenue = analyticsSection(data, e)
		if e == nil {
			values := map[string]float64{"approved": 0, "hold": 0, "approvedEvents": 0, "holdEvents": 0}
			for _, row := range data.Data {
				name := row.Dimensions[0].Name
				if name == nil || (*name != "approved" && *name != "hold") {
					out.Revenue = unavailableAnalytics("AppMetrica вернула неизвестный статус revenue.")
					break
				}
				values[*name] += *row.Metrics[0]
				values[*name+"Events"] += *row.Metrics[1]
			}
			if out.Revenue.Status == "ready" {
				out.Revenue.Values = values
			}
		}
	}
	out.FetchedAt = time.Now().UTC()
	ttl := 5 * time.Minute
	for _, section := range []AnalyticsSection{out.Push, out.Events, out.Revenue} {
		if section.Status == "unavailable" {
			ttl = 30 * time.Second
		}
	}
	out.NextRefreshAt = out.FetchedAt.Add(ttl)
	if ctx.Err() == nil {
		if len(p.analyticsCache) >= 32 {
			clear(p.analyticsCache)
		}
		p.analyticsCache[key] = analyticsCacheEntry{out}
	}
	return out, nil
}

func analyticsLiteral(value string) string {
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(value) + "'"
}

func unavailableAnalytics(message string) AnalyticsSection {
	return AnalyticsSection{Status: "unavailable", Error: message}
}
func analyticsSection(data analyticsResponse, err error) AnalyticsSection {
	if err != nil {
		return unavailableAnalytics(err.Error())
	}
	return AnalyticsSection{Status: "ready", Sampled: *data.Sampled, DataLagSeconds: data.DataLag}
}

func (p *Plugin) queryAnalytics(ctx context.Context, cfg Config, base url.Values, metrics, dimensions, filter string, size int) (analyticsResponse, error) {
	var out analyticsResponse
	params := url.Values{}
	for k, v := range base {
		params[k] = v
	}
	params.Set("metrics", metrics)
	params.Set("filters", filter)
	if dimensions != "" {
		params.Set("dimensions", dimensions)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, analyticsHost+"?"+params.Encode(), nil)
	if err != nil {
		return out, textError("не удалось подготовить запрос AppMetrica")
	}
	req.Header.Set("Authorization", "OAuth "+cfg.OAuthToken)
	response, err := p.client.Do(req)
	if err != nil {
		return out, textError("AppMetrica недоступна или истекло время ожидания")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		hint := "повторите позже"
		switch response.StatusCode {
		case 401, 403:
			hint = "проверьте права OAuth-токена на чтение статистики приложения"
		case 429:
			hint = "исчерпана квота API, повторите позже"
		}
		// Never forward raw provider responses or URLs: they may contain credentials or payloads.
		return out, fmt.Errorf("AppMetrica HTTP %d: %s", response.StatusCode, hint)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 || json.Unmarshal(body, &out) != nil {
		return out, textError("некорректный ответ аналитики AppMetrica")
	}
	valid := func(values []*float64) bool {
		if len(values) != size {
			return false
		}
		for _, value := range values {
			if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
				return false
			}
		}
		return true
	}
	if !valid(out.Totals) || out.Sampled == nil || out.TotalRows == nil || out.DataLag < 0 {
		return out, textError("AppMetrica вернула неполные метрики")
	}
	if dimensions != "" && *out.TotalRows != len(out.Data) {
		return out, textError("AppMetrica вернула не все строки; итог не рассчитан")
	}
	for _, row := range out.Data {
		if !valid(row.Metrics) || (dimensions != "" && len(row.Dimensions) != len(strings.Split(dimensions, ","))) {
			return out, textError("AppMetrica вернула неполные строки")
		}
	}
	return out, nil
}
