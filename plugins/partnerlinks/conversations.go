package partnerlinks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const ConversationsCollection = "conversations"
const conversationRule = "@request.auth.id != '' && userId = @request.auth.id && authCollection = @request.auth.collectionId"
const legacyConversationIndex = "CREATE UNIQUE INDEX idx_partner_conversation_lead ON conversations (provider, leadId)"
const conversationIndex = "CREATE UNIQUE INDEX idx_partner_conversation_lead ON conversations (provider, leadId) WHERE leadId != ''"
const tokenIndex = "CREATE UNIQUE INDEX idx_partner_conversation_token ON conversations (tokenHash) WHERE tokenHash != ''"

var errExpiredConversation = errors.New("Срок хранения конверсии истёк")

type invalidConversation string

func (e invalidConversation) Error() string { return string(e) }

func installConversations(app core.App, authNames []string) error {
	// PocketBase schema hooks update the shared cache inside transactions;
	// restore it after commit or rollback before any later schema operation.
	defer func() { _ = app.ReloadCachedCollections() }()
	return app.RunInTransaction(func(tx core.App) error {
		ids := []string{}
		for _, name := range authNames {
			c, err := tx.FindCollectionByNameOrId(name)
			if err != nil {
				return err
			}
			if !c.IsAuth() || c.System {
				return fmt.Errorf("partnerlinks: invalid conversation owner collection")
			}
			ids = append(ids, c.Id)
		}
		if len(ids) == 0 {
			return fmt.Errorf("partnerlinks: collecting conversations requires auth collections")
		}
		c, err := tx.FindCollectionByNameOrId(ConversationsCollection)
		if err == nil {
			f, ok := c.Fields.GetByName("user").(*polymorphicrelation.Field)
			if !ok || !c.IsBase() || !f.Required || f.OnDelete != polymorphicrelation.Cascade || len(f.CollectionIDs) != len(ids) || c.ListRule == nil || *c.ListRule != conversationRule || c.ViewRule == nil || *c.ViewRule != conversationRule || c.CreateRule != nil || c.UpdateRule != nil || c.DeleteRule != nil || (!slices.Contains(c.Indexes, conversationIndex) && !slices.Contains(c.Indexes, legacyConversationIndex)) {
				return fmt.Errorf("partnerlinks: incompatible conversations collection")
			}
			for _, id := range ids {
				found := false
				for _, v := range f.CollectionIDs {
					if id == v {
						found = true
					}
				}
				if !found {
					return fmt.Errorf("partnerlinks: conversations auth collections changed; migrate the owner field first")
				}
			}
			for name, kind := range map[string]string{"userId": core.FieldTypeText, "authCollection": core.FieldTypeText, "link": core.FieldTypeRelation, "provider": core.FieldTypeText, "leadId": core.FieldTypeText, "clickId": core.FieldTypeText, "status": core.FieldTypeSelect, "eventTimestamp": core.FieldTypeNumber, "clickTimestamp": core.FieldTypeNumber, "amount": core.FieldTypeText, "currency": core.FieldTypeText, "extra": core.FieldTypeJSON} {
				field := c.Fields.GetByName(name)
				if field == nil || field.Type() != kind {
					return fmt.Errorf("partnerlinks: incompatible conversations field %s", name)
				}
			}
			return migrateConversations(tx, c)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		links, err := tx.FindCollectionByNameOrId(LinksCollection)
		if err != nil {
			return err
		}
		c = core.NewBaseCollection(ConversationsCollection)
		rule := conversationRule
		c.ListRule = types.Pointer(rule)
		c.ViewRule = types.Pointer(rule)
		c.Fields.Add(&polymorphicrelation.Field{JSONField: core.JSONField{Name: "user", Required: true}, CollectionIDs: ids, OnDelete: polymorphicrelation.Cascade}, &core.TextField{Name: "userId", Required: true}, &core.TextField{Name: "authCollection", Required: true}, &core.RelationField{Name: "link", CollectionId: links.Id, MaxSelect: 1}, &core.TextField{Name: "provider", Required: true}, &core.TextField{Name: "leadId"}, &core.TextField{Name: "clickId", Required: true}, &core.SelectField{Name: "status", Required: true, MaxSelect: 1, Values: []string{"pending", "lead", "approved", "hold", "rejected"}}, &core.TextField{Name: "eventId"}, &core.NumberField{Name: "eventTimestamp", OnlyInt: true}, &core.NumberField{Name: "clickTimestamp", OnlyInt: true}, &core.TextField{Name: "amount"}, &core.TextField{Name: "currency"}, &core.JSONField{Name: "extra", MaxSize: 16384}, &core.AutodateField{Name: "created", OnCreate: true}, &core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		c.Indexes = []string{conversationIndex}
		return migrateConversations(tx, c)
	})
}

// migrateConversations preserves existing orders, IDs and owners. Old encrypted
// tokens have no stored digest and cannot access these legacy orders.
func migrateConversations(app core.App, c *core.Collection) error {
	c.Fields.GetByName("leadId").(*core.TextField).Required = false
	status := c.Fields.GetByName("status").(*core.SelectField)
	if !slices.Contains(status.Values, "pending") {
		status.Values = append(status.Values, "pending")
	}
	fields := []core.Field{
		&core.JSONField{Name: deliveryField, Hidden: true, MaxSize: 262144},
		&core.TextField{Name: "tokenHash", Hidden: true, Max: 64},
		&core.JSONField{Name: "clickData", MaxSize: 262144},
		&core.NumberField{Name: "statusChangedAt", OnlyInt: true},
	}
	changed := c.IsNew() || !c.System || slices.Contains(c.Indexes, legacyConversationIndex)
	promote := !c.IsNew() && !c.System
	if !promote {
		c.System = true
	}
	for _, f := range fields {
		if old := c.Fields.GetByName(f.GetName()); old == nil {
			c.Fields.Add(f)
			changed = true
		} else if old.Type() != f.Type() || ((f.GetName() == "tokenHash" || f.GetName() == deliveryField) && !old.GetHidden()) {
			return fmt.Errorf("partnerlinks: incompatible conversations field %s", f.GetName())
		}
	}
	c.Indexes = slices.DeleteFunc(c.Indexes, func(i string) bool { return i == legacyConversationIndex })
	for _, i := range []string{conversationIndex, tokenIndex, "CREATE INDEX idx_partner_conversation_retention ON conversations (status, statusChangedAt)"} {
		if !slices.Contains(c.Indexes, i) {
			c.Indexes = append(c.Indexes, i)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := save(app, c); err != nil {
		return err
	}
	if promote {
		// PocketBase forbids changing system on an existing collection during
		// validation. The complete schema was validated and saved above; only
		// this flag changes here, in the same private migration transaction.
		c.System = true
		if err := app.SaveNoValidateWithContext(context.WithValue(context.Background(), internalKey{}, true), c); err != nil {
			return err
		}
	}
	// Legacy orders did not store receipt time; their last event time is the
	// closest available baseline. Missing event times start retention at migration.
	_, err := app.DB().NewQuery("UPDATE conversations SET statusChangedAt = CASE WHEN eventTimestamp > 0 THEN eventTimestamp ELSE {:now} END WHERE statusChangedAt = 0").Bind(dbx.Params{"now": time.Now().Unix()}).Execute()
	return err
}

func createConversation(app core.App, data clickData, token string) error {
	digest, err := tokenDigest(token)
	if err != nil {
		return err
	}
	return app.RunInTransaction(func(tx core.App) error {
		c, err := tx.FindCollectionByNameOrId(ConversationsCollection)
		if err != nil {
			return err
		}
		r := core.NewRecord(c)
		r.Set("user", polymorphicrelation.Reference{CollectionID: data.AuthCollection, RecordID: data.UserID})
		r.Set("userId", data.UserID)
		r.Set("authCollection", data.AuthCollection)
		r.Set("provider", data.ProviderID)
		r.Set("link", data.LinkID)
		r.Set("clickId", data.ClickID)
		r.Set("clickTimestamp", data.IssuedAt)
		r.Set("statusChangedAt", data.IssuedAt)
		r.Set("status", "pending")
		r.Set(deliveryField, deliveryState{Version: 1, Items: map[string]deliveryItem{}})
		r.Set("clickData", data)
		r.Set("tokenHash", digest)
		return save(tx, r)
	})
}

func loadConversation(app core.App, token string) (*core.Record, clickData, error) {
	var data clickData
	digest, err := tokenDigest(token)
	if err != nil {
		return nil, data, invalidConversation("Некорректный clickData")
	}
	r, err := app.FindFirstRecordByData(ConversationsCollection, "tokenHash", digest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, data, invalidConversation("Конверсия не найдена")
	}
	if err != nil {
		return nil, data, err
	}
	err = json.Unmarshal([]byte(r.GetString("clickData")), &data)
	if err == nil && (data.ClickID == "" || data.ClickID != r.GetString("clickId") || data.UserID != r.GetString("userId") || data.AuthCollection != r.GetString("authCollection") || data.ProviderID != r.GetString("provider") || data.IssuedAt <= 0 || data.OpenUntil <= data.IssuedAt) {
		err = fmt.Errorf("partnerlinks: invalid stored click data")
	}
	return r, data, err
}

func expiredConversation(r *core.Record, c *Config, now int64) bool {
	days, start := c.ConversionRetentionDays, r.GetInt64("statusChangedAt")
	if r.GetString("status") == "pending" {
		days, start = c.PendingRetentionDays, r.GetInt64("clickTimestamp")
	}
	return days > 0 && now >= start+int64(days)*86400
}

func collect(app core.App, token string, timestamp int64, conversion conversionData, revenue bool) error {
	lead, status := conversion.LeadID, conversion.Status
	return app.RunInTransaction(func(tx core.App) error {
		r, data, err := loadConversation(tx, token)
		if err != nil {
			return err
		}
		cfg, err := Load(tx)
		if err != nil {
			return err
		}
		if expiredConversation(r, cfg, time.Now().Unix()) {
			return errExpiredConversation
		}
		delivery, err := readDelivery(r)
		if err != nil {
			return err
		}
		if lead != "" {
			if current := r.GetString("leadId"); current != "" && current != lead {
				return invalidConversation("Выданная ссылка уже связана с другой заявкой")
			}
			other, err := tx.FindFirstRecordByFilter(ConversationsCollection, "provider = {:provider} && leadId = {:lead}", dbx.Params{"provider": data.ProviderID, "lead": lead})
			if err == nil && other.Id != r.Id {
				return invalidConversation("ID заявки уже связан с другой ссылкой")
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		bindLead := lead != "" && r.GetString("leadId") == ""
		if bindLead {
			r.Set("leadId", lead)
		}
		stale := timestamp < r.GetInt64("eventTimestamp") || (timestamp == r.GetInt64("eventTimestamp") && status == "lead" && r.GetString("status") != "pending" && r.GetString("status") != "lead")
		if stale {
			// A delayed notification may first supply the application ID.
			// Bind it without rolling back the status or extending retention.
			if bindLead {
				return save(tx, r)
			}
			return nil
		}
		if status != r.GetString("status") {
			r.Set("statusChangedAt", time.Now().Unix())
		}
		r.Set("status", status)
		r.Set("eventTimestamp", timestamp)
		if lead != "" {
			r.Set("leadId", lead)
		}
		for key, value := range map[string]string{"eventId": conversion.EventID, "amount": conversion.Amount, "currency": conversion.Currency} {
			if value != "" {
				r.Set(key, value)
			}
		}
		if len(conversion.Extra) > 0 {
			r.Set("extra", conversion.Extra)
		}
		prepareDelivery(r, delivery, conversion, timestamp, revenue)
		return save(tx, r)
	})
}

// Cleanup deletes expired conversions in bounded transactions. Zero retention
// means unlimited. Both public endpoints also enforce retention before cron runs.
func Cleanup(app core.App) (int, error) { return cleanupAt(app, time.Now().Unix()) }
func cleanupAt(app core.App, now int64) (int, error) {
	total := 0
	for {
		removed := 0
		err := app.RunInTransaction(func(tx core.App) error {
			cfg, err := Load(tx)
			if err != nil {
				return err
			}
			conditions := []dbx.Expression{}
			if cfg.PendingRetentionDays > 0 {
				conditions = append(conditions, dbx.NewExp("status = 'pending' AND clickTimestamp <= {:cutoff}", dbx.Params{"cutoff": now - int64(cfg.PendingRetentionDays)*86400}))
			}
			if cfg.ConversionRetentionDays > 0 {
				conditions = append(conditions, dbx.NewExp("status != 'pending' AND statusChangedAt <= {:convertedCutoff}", dbx.Params{"convertedCutoff": now - int64(cfg.ConversionRetentionDays)*86400}))
			}
			if len(conditions) == 0 {
				return nil
			}
			c, err := tx.FindCollectionByNameOrId(ConversationsCollection)
			if err != nil {
				return err
			}
			var rows []*core.Record
			if err = tx.RecordQuery(c).AndWhere(dbx.Or(conditions...)).Limit(200).All(&rows); err != nil {
				return err
			}
			for _, r := range rows {
				if err = tx.Delete(r); err != nil {
					return err
				}
				removed++
			}
			return nil
		})
		if err != nil {
			return total, err
		}
		total += removed
		if removed < 200 {
			return total, nil
		}
	}
}
