// Unrestricted fixture for plugin editor tests. Production example always locks
// the schema; no runtime escape flag is shipped with it.
package main

import (
	_ "github.com/NikitaVasin/pocket_mfo/example/migrations"
	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/mcp"
	"github.com/NikitaVasin/pocket_mfo/plugins/partnerlinks"
	"github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
	"github.com/NikitaVasin/pocket_mfo/plugins/push"
	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"log"
)

func main() {
	app := pocketbase.New()
	polymorphicrelation.Register(app)
	variants.Register(app)
	singleton.Register(app)
	dynamiclink.Register(app)
	partnerOptions := partnerlinks.Options{AuthCollections: []string{"users"}, VariantCollections: []string{"demo_offers", dynamiclink.SettingsCollection}}
	partnerOptions.Attribution = push.Attribution
	partnerlinks.Register(app, partnerOptions)
	bridge := mcp.Register(app, mcp.Options{ContentCollections: []string{"demo_offers", "partner_links", dynamiclink.SettingsCollection}})
	push.Register(app, push.Options{AuthCollections: []string{"users"}, MCP: bridge})
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{Automigrate: false})
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
