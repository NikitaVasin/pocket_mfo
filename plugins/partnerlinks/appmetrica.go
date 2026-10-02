package partnerlinks

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/NikitaVasin/pocket_mfo/plugins/appmetrica"
	"net/url"
	"strconv"
	"time"
)

const appMetricaURL = appmetrica.EventsURL

type deliveryHTTPError = appmetrica.DeliveryHTTPError

func (p *plugin) send(parent context.Context, c *Config, data clickData, event string, timestamp int64, conversion *conversionData) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	attributes := analyticsAttributes(data)
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
	return appmetrica.Deliver(ctx, p.client, endpoint, q)
}

func (p *plugin) sendRevenue(parent context.Context, c *Config, data clickData, timestamp int64, conversion conversionData) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	attributes := analyticsAttributes(data)
	attributes["conversion"] = conversion
	payload, err := json.Marshal(attributes)
	if err != nil {
		return err
	}
	// Prevent AppMetrica's documented payload truncation.
	if len(payload) > 30*1024 {
		return fmt.Errorf("appmetrica: revenue payload too large")
	}
	q := url.Values{"post_api_key": {c.PostAPIKey}, "application_id": {strconv.FormatInt(data.ApplicationID, 10)}, "profile_id": {data.UserID}, "session_type": {"foreground"}, "event_timestamp": {strconv.FormatInt(timestamp, 10)}, "revenue_event_type": {"one_time_purchase"}, "price": {conversion.Amount}, "currency": {conversion.Currency}, "product_id": {data.LinkID}, "quantity": {"1"}, "payload": {string(payload)}}
	// A stable reconciliation identifier; don't assume upstream deduplication.
	q.Set("transaction_id", data.ClickID)
	q.Set("order_id", conversion.LeadID)
	return p.deliver(ctx, appmetrica.RevenueURL, q)
}

// Match the client event dimensions without exposing signed bearer contexts.
func analyticsAttributes(data clickData) map[string]any {
	attributes := map[string]any{"clickData": data, "experiments": data.AnalyticsExperiments, "attributionBasis": data.AttributionBasis, "clickId": data.ClickID, "linkId": data.LinkID}
	if len(data.Exposures) > 0 {
		shown := make([]map[string]any, 0, len(data.Exposures))
		for _, e := range data.Exposures {
			shown = append(shown, map[string]any{"id": e.ID, "collection": e.Collection, "record": e.Record, "revision": e.Revision, "set": e.Decision.Set, "version": e.Decision.Version})
		}
		attributes["exposures"] = shown
	}
	return attributes
}
