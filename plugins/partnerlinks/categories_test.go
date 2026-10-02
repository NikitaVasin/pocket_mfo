package partnerlinks

import (
	"testing"

	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

func TestOffersCategoryRequiresBothPlugins(t *testing.T) {
	for _, order := range []string{"dynamic-first", "partner-first", "partner-only"} {
		t.Run(order, func(t *testing.T) {
			app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
			variants.Register(app)
			singleton.Register(app)
			if order == "dynamic-first" {
				dynamiclink.Register(app)
			}
			Register(app, Options{AuthCollections: []string{"members"}})
			Register(app, Options{AuthCollections: []string{"members"}})
			if order == "partner-first" {
				dynamiclink.Register(app)
			}
			must(t, app.Bootstrap())
			defer app.ClearBootstrap()
			auth := core.NewAuthCollection("members")
			must(t, app.Save(auth))
			must(t, dynamiclink.Configure(app, auth.Id))
			must(t, dynamiclink.Configure(app, auth.Id))
			rows, err := app.FindAllRecords(dynamiclink.SettingsCollection)
			must(t, err)
			if len(rows) != 1 {
				t.Fatal("expected default policy")
			}
			var categories []dynamiclink.Category
			must(t, rows[0].UnmarshalJSONField("categories", &categories))
			if order == "partner-only" {
				if len(categories) != 0 {
					t.Fatal("category added without Dynamic Link")
				}
			} else if len(categories) != 1 || categories[0].Key != "offers" || categories[0].Label != "Офферы" || categories[0].Options != (dynamiclink.OpeningOptions{}) {
				t.Fatal("missing default offers category", categories)
			}
		})
	}
}
