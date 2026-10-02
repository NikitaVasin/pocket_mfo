package migrations

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"

	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/partnerlinks"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

func init() { migrations.Register(removeLegacyDemoPartner, func(core.App) error { return nil }) }

// Remove only the bundled, unmodified fixture. Historical conversions and any
// user-created partners/links remain intact. Integration test rigs opt in.
func removeLegacyDemoPartner(app core.App) error {
	if os.Getenv("DEMO_PARTNER_ENABLED") == "1" {
		return nil
	}
	return app.RunInTransaction(func(tx core.App) error {
		cfg, err := partnerlinks.Load(tx)
		if err != nil {
			return err
		}
		index := -1
		for i, p := range cfg.Providers {
			if isLegacyDemoProvider(p) {
				index = i
				break
			}
		}
		if index < 0 {
			return nil
		}
		links, err := tx.FindAllRecords(partnerlinks.LinksCollection, dbx.HashExp{"provider": "demo"})
		if err != nil {
			return err
		}
		// Don't remove a provider that the project has started using for its own links.
		for _, link := range links {
			if link.Id != "demopartner0001" || link.GetString("name") != "Демонстрационный оффер" || !link.GetBool("active") {
				return nil
			}
			raw, err := json.Marshal(link.Get("link"))
			if err != nil {
				return err
			}
			value, err := dynamiclink.Decode(raw)
			if err != nil {
				return err
			}
			base := strings.TrimRight(os.Getenv("DEMO_PARTNER_PUBLIC_URL"), "/")
			if base == "" {
				base = "http://127.0.0.1:8091"
			}
			if value.URL != "https://example.com/offer" && value.URL != base+"/click" {
				return nil
			}
			original, err := dynamiclink.Decode([]byte(`{"url":"https://example.com/offer"}`))
			if err != nil {
				return err
			}
			original.URL = value.URL
			if !reflect.DeepEqual(value, original) {
				return nil
			}
		}
		for _, link := range links {
			if err := tx.Delete(link); err != nil {
				return err
			}
		}
		cfg.Providers = append(cfg.Providers[:index], cfg.Providers[index+1:]...)
		delete(cfg.ProviderCredentials, "demo")
		_, err = partnerlinks.Configure(tx, *cfg)
		return err
	})
}

// Accept both historical seed revisions, but never edited mapping or options.
func isLegacyDemoProvider(p partnerlinks.Provider) bool {
	p.HasSecret = false
	if len(p.ExtraFields) == 0 {
		p.ExtraFields = map[string]string{}
	}
	expected := legacyDemoProvider()
	expected.RevenueStatus = "approved"
	if reflect.DeepEqual(p, expected) {
		return true
	}
	expected.Fields.EventID = "event_id"
	expected.Fields.Timestamp = "timestamp"
	expected.Fields.Amount = "amount"
	expected.Fields.Currency = "currency"
	return reflect.DeepEqual(p, expected)
}
