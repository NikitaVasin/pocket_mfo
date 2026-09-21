package main

import (
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"log"
	_ "pocket_mfo/example/migrations"
	"pocket_mfo/plugins/polymorphicrelation"
)

func main() {
	app := pocketbase.New()
	polymorphicrelation.Register(app)
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{Automigrate: false})
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
