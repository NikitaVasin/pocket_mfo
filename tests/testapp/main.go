// Unrestricted fixture for plugin editor tests. Production example always locks
// the schema; no runtime escape flag is shipped with it.
package main

import (
	_ "github.com/NikitaVasin/pocket_mfo/example/migrations"
	"github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
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
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{Automigrate: false})
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
