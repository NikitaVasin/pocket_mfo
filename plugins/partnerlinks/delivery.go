package partnerlinks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

const deliveryField = "deliveryState"

type deliveryItem struct {
	State      string         `json:"state"`
	Timestamp  int64          `json:"timestamp"`
	Conversion conversionData `json:"conversion"`
}
type deliveryState struct {
	Version int                     `json:"version"`
	Items   map[string]deliveryItem `json:"items"`
}

func readDelivery(r *core.Record) (deliveryState, error) {
	state := deliveryState{Version: 1, Items: map[string]deliveryItem{}}
	if raw := r.GetString(deliveryField); raw != "" && raw != "null" {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return state, err
		}
		if state.Version != 1 || state.Items == nil {
			return state, fmt.Errorf("partnerlinks: invalid delivery state")
		}
		return state, nil
	}
	// Existing converted records predate delivery receipts. Never assume either
	// success or failure, since the old implementation stored before sending.
	if status := r.GetString("status"); status != "pending" {
		for _, previous := range []string{"lead", "hold", "approved", "rejected"} {
			state.Items["event_"+previous] = deliveryItem{State: "unknown"}
		}
		state.Items["revenue"] = deliveryItem{State: "unknown"}
	}
	return state, nil
}

func prepareDelivery(r *core.Record, state deliveryState, conversion conversionData, timestamp int64, revenue bool) {
	keys := []string{"event_" + conversion.Status}
	if revenue {
		keys = append(keys, "revenue")
	}
	for _, key := range keys {
		item, exists := state.Items[key]
		if !exists {
			item.State = "pending"
		}
		// Keep the original event payload unchanged across retries.
		if item.Conversion.Status == "" {
			item.Conversion = conversion
			item.Timestamp = timestamp
		}
		state.Items[key] = item
	}
	r.Set(deliveryField, state)
}

func (p *plugin) deliverOnce(ctx context.Context, app core.App, c *Config, data clickData, token, key string) error {
	var item deliveryItem
	claimed := false
	err := app.RunInTransaction(func(tx core.App) error {
		r, _, err := loadConversation(tx, token)
		if err != nil {
			return err
		}
		state, err := readDelivery(r)
		if err != nil {
			return err
		}
		var ok bool
		item, ok = state.Items[key]
		if !ok || item.State == "sent" {
			return nil
		}
		if item.State != "pending" {
			return fmt.Errorf("partnerlinks: delivery outcome unknown")
		}
		item.State = "unknown" // Persist before the external call, including crashes.
		state.Items[key] = item
		r.Set(deliveryField, state)
		if err := save(tx, r); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	if err != nil || !claimed {
		return err
	}
	if key == "revenue" {
		err = p.sendRevenue(ctx, c, data, item.Timestamp, item.Conversion)
	} else {
		err = p.send(ctx, c, data, item.Conversion.Status, item.Timestamp, &item.Conversion)
	}
	sendErr := err
	stateName := "sent"
	if err != nil {
		var httpErr deliveryHTTPError
		if !errors.As(err, &httpErr) {
			return err
		} // Network failure stays unknown.
		stateName = "pending" // Explicit failure may be retried by the partner.
	}
	err = app.RunInTransaction(func(tx core.App) error {
		r, _, err := loadConversation(tx, token)
		if err != nil {
			return err
		}
		state, err := readDelivery(r)
		if err != nil {
			return err
		}
		item.State = stateName
		state.Items[key] = item
		r.Set(deliveryField, state)
		return save(tx, r)
	})
	return errors.Join(sendErr, err)
}

// ReconcileDelivery records an operator's verified outcome after an ambiguous
// send (or for a legacy conversion). Trusted Go only; it does not send anything.
// kind is event_lead/event_hold/event_approved/event_rejected or revenue.
// delivered=false permits the next partner retry. Verify externally first.
func ReconcileDelivery(app core.App, conversationID, kind string, delivered bool) error {
	return app.RunInTransaction(func(tx core.App) error {
		r, err := tx.FindRecordById(ConversationsCollection, conversationID)
		if err != nil {
			return err
		}
		state, err := readDelivery(r)
		if err != nil {
			return err
		}
		item, ok := state.Items[kind]
		if !ok || item.State != "unknown" {
			return fmt.Errorf("partnerlinks: only unknown deliveries can be reconciled")
		}
		item.State = "pending"
		if delivered {
			item.State = "sent"
		}
		state.Items[kind] = item
		r.Set(deliveryField, state)
		return save(tx, r)
	})
}
