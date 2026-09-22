package migrations

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/partnerlinks"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

func init() { migrations.Register(ensureDemoPartnerService, func(core.App) error { return nil }) }

// Only upgrade the bundled demo offer; keep real analytics keys and edited URLs.
func ensureDemoPartnerService(app core.App) error {
	return app.RunInTransaction(func(tx core.App) error {
		cfg, err := partnerlinks.Load(tx)
		if err != nil {
			return err
		}
		if cfg.BaseURL == "" {
			cfg.BaseURL = strings.TrimRight(os.Getenv("DEMO_PUBLIC_URL"), "/")
			if cfg.BaseURL == "" {
				cfg.BaseURL = "http://127.0.0.1:8090"
			}
		}
		for i := range cfg.Providers {
			p := &cfg.Providers[i]
			if p.ID != "demo" {
				continue
			}
			if p.Fields.EventID == "" {
				p.Fields.EventID = "event_id"
			}
			if p.Fields.Timestamp == "" {
				p.Fields.Timestamp = "timestamp"
			}
			if p.Fields.Amount == "" {
				p.Fields.Amount = "amount"
			}
			if p.Fields.Currency == "" {
				p.Fields.Currency = "currency"
			}
		}
		if _, err = partnerlinks.Configure(tx, *cfg); err != nil {
			return err
		}
		row, err := tx.FindRecordById(partnerlinks.LinksCollection, "demopartner0001")
		if err != nil {
			return err
		}
		raw, err := json.Marshal(row.Get("link"))
		if err != nil {
			return err
		}
		link, err := dynamiclink.Decode(raw)
		if err != nil {
			return err
		}
		if link.URL != "https://example.com/offer" {
			return nil
		}
		base := strings.TrimRight(os.Getenv("DEMO_PARTNER_PUBLIC_URL"), "/")
		if base == "" {
			base = "http://127.0.0.1:8091"
		}
		link.URL = base + "/click"
		row.Set("link", link)
		return tx.Save(row)
	})
}
