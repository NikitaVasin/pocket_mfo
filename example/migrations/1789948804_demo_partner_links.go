package migrations

import (
	"database/sql"
	"errors"

	"github.com/NikitaVasin/pocket_mfo/plugins/partnerlinks"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

func init() { migrations.Register(ensureDemoPartnerLinks, func(core.App) error { return nil }) }

// Demo credentials are public. No AppMetrica key is seeded.
func ensureDemoPartnerLinks(app core.App) error {
	return app.RunInTransaction(func(tx core.App) error {
		links, err := tx.FindCollectionByNameOrId(partnerlinks.LinksCollection)
		if err != nil {
			return err
		}
		if _, err = tx.FindRecordById(links, "demopartner0001"); err == nil {
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		cfg, err := partnerlinks.Load(tx)
		if err != nil {
			return err
		}
		found := false
		for _, p := range cfg.Providers {
			if p.ID == "demo" {
				found = true
			}
		}
		if !found {
			cfg.Providers = append(cfg.Providers, partnerlinks.Provider{ID: "demo", Name: "Демонстрационный партнёр", URLTemplate: "{url}?subid={clickData}", Secret: "public-demo-postback-secret", SecretLocation: "query", SecretName: "secret", Fields: partnerlinks.PostbackFields{Token: "subid", Status: "status", LeadID: "lead_id"}, Statuses: map[string]string{"lead": "lead", "approved": "approved", "hold": "hold", "rejected": "rejected"}, ExtraFields: map[string]string{}})
			if _, err = partnerlinks.Configure(tx, *cfg); err != nil {
				return err
			}
		}
		r := core.NewRecord(links)
		r.Id = "demopartner0001"
		r.Set("name", "Демонстрационный оффер")
		r.Set("provider", "demo")
		r.Set("link", map[string]any{"url": "https://example.com/offer"})
		r.Set("active", true)
		return tx.Save(r)
	})
}
