package partnerlinks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const appMetricaURL = "https://api.appmetrica.yandex.ru/logs/v1/import/events"

func (p *plugin) send(parent context.Context, c *Config, data clickData, event string, timestamp int64, conversion *conversionData) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	attributes := map[string]any{"clickData": data, "experiments": data.AnalyticsExperiments}
	if conversion != nil {
		attributes["conversion"] = conversion
	}
	b, err := json.Marshal(attributes)
	if err != nil {
		return err
	}
	q := url.Values{"post_api_key": {c.PostAPIKey}, "application_id": {strconv.FormatInt(data.ApplicationID, 10)}, "profile_id": {data.UserID}, "session_type": {"foreground"}, "event_name": {c.EventNames[event]}, "event_timestamp": {strconv.FormatInt(timestamp, 10)}, "event_json": {string(b)}}
	return p.deliver(ctx, appMetricaURL, q)
}

func (p *plugin) deliver(ctx context.Context, endpoint string, q url.Values) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("appmetrica: invalid request")
	}
	response, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("appmetrica: transport failure")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("appmetrica: HTTP %d", response.StatusCode)
	}
	return nil
}

func (p *plugin) sendRevenue(parent context.Context, c *Config, data clickData, timestamp int64, conversion conversionData) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	payload, err := json.Marshal(map[string]any{"clickData": data, "conversion": conversion, "experiments": data.AnalyticsExperiments})
	if err != nil {
		return err
	}
	// Prevent AppMetrica's documented payload truncation.
	if len(payload) > 30*1024 {
		return fmt.Errorf("appmetrica: revenue payload too large")
	}
	q := url.Values{"post_api_key": {c.PostAPIKey}, "application_id": {strconv.FormatInt(data.ApplicationID, 10)}, "profile_id": {data.UserID}, "session_type": {"foreground"}, "event_timestamp": {strconv.FormatInt(timestamp, 10)}, "revenue_event_type": {"one_time_purchase"}, "price": {conversion.Amount}, "currency": {conversion.Currency}, "product_id": {data.LinkID}, "quantity": {"1"}, "payload": {string(payload)}}
	return p.deliver(ctx, "https://api.appmetrica.yandex.ru/logs/v1/import/revenue", q)
}
