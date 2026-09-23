package main

import (
	_ "github.com/NikitaVasin/pocket_mfo/example/migrations"
	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/mcp"
	"github.com/NikitaVasin/pocket_mfo/plugins/partnerlinks"
	"github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
	"github.com/NikitaVasin/pocket_mfo/plugins/push"
	"github.com/NikitaVasin/pocket_mfo/plugins/schemalock"
	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"log"
	"os"
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
	applicationID, sendRate := int64(6361870), 1000
	oauthToken := os.Getenv("APPMETRICA_PUSH_OAUTH_TOKEN")
	if oauthToken == "" {
		oauthToken = "y0__wgBEOWvgdEBGLajSiCKoMOQGWqgD3q4-aW1vPzTAenfuy2vivMx"
	}
	push.Register(app, push.Options{
		OAuthClientID:   "8e1f79cf905d4a70b30507ea80e0730f",
		AuthCollections: []string{"users"}, MCP: bridge,
		Managed:         &push.ManagedConfig{ApplicationID: &applicationID, OAuthToken: &oauthToken, SendRate: &sendRate},
		LockAdminConfig: true,
	})
	schemalock.Register(app)
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{Automigrate: false})
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
