// Seeds only the disposable database created by browser/server.mjs.
package main

import (
	"log"
	"os"

	"github.com/NikitaVasin/pocket_mfo/plugins/partnerlinks"
	_ "github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
	_ "github.com/NikitaVasin/pocket_mfo/plugins/typedconfig"
	"github.com/pocketbase/pocketbase"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("expected disposable data directory and fixture mode")
	}
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: os.Args[1]})
	if err := app.Bootstrap(); err != nil {
		log.Fatal(err)
	}
	defer app.ClearBootstrap()
	config, err := partnerlinks.Load(app)
	if err != nil {
		log.Fatal(err)
	}
	config.BaseURL = "https://links.example.test"
	config.ApplicationID = 1234
	config.PostAPIKey = "fake-browser-key"
	if os.Args[2] == "locked" {
		provider := config.Providers[0]
		provider.ID, provider.Name = "browser-selector", "Другой партнёр"
		config.Providers = append(config.Providers, provider)
	}
	if _, err := partnerlinks.Configure(app, *config); err != nil {
		log.Fatal(err)
	}
}
