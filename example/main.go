package main

import (
	_ "github.com/NikitaVasin/pocket_mfo/example/migrations"
	"github.com/NikitaVasin/pocket_mfo/plugins/appmetrica"
	"github.com/NikitaVasin/pocket_mfo/plugins/currencyrates"
	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/mcp"
	"github.com/NikitaVasin/pocket_mfo/plugins/partnerlinks"
	"github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
	"github.com/NikitaVasin/pocket_mfo/plugins/push"
	"github.com/NikitaVasin/pocket_mfo/plugins/schemalock"
	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/typedconfig"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"log"
	"os"
)

func main() {
	app := pocketbase.New()
	applicationID, sendRate := int64(6361870), 1000
	oauthToken := os.Getenv("APPMETRICA_PUSH_OAUTH_TOKEN")
	shared := &appmetrica.ManagedConfig{ApplicationID: &applicationID, OAuthToken: &oauthToken}
	appmetrica.Register(app, appmetrica.Options{Managed: shared})
	polymorphicrelation.Register(app)
	variants.Register(app)
	typedconfig.Register(app)
	singleton.Register(app)
	dynamiclink.Register(app)
	partnerOptions := partnerlinks.Options{AuthCollections: []string{"users"}, Managed: &partnerlinks.ManagedConfig{Providers: []partnerlinks.Provider{partnerlinks.RafinadNew("rafinad-new", "")}}}
	partnerOptions.Attribution = push.Attribution
	partnerlinks.Register(app, partnerOptions)
	bridge := mcp.Register(app, mcp.Options{ContentCollections: []string{"demo_offers", "partner_links", "demo_screen_configs", dynamiclink.SettingsCollection}})

	push.Register(app, push.Options{
		AuthCollections: []string{"users"}, MCP: bridge,
		Managed:         &push.ManagedConfig{SendRate: &sendRate},
		LockAdminConfig: true,
	})
	currencyrates.Register(app)
	schemalock.Register(app)
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{Automigrate: false})
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
