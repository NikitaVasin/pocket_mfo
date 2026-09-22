package migrations

import (
	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

func init() {
	migrations.Register(func(app core.App) error { return dynamiclink.Configure(app, demoMembersID) }, func(core.App) error { return nil })
}
