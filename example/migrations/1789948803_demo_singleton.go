package migrations

import (
	"database/sql"
	"errors"

	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

const demoSingletonID = "demosingleton01"

func init() {
	migrations.Register(ensureDemoSingleton, func(app core.App) error { return nil })
}

func ensureDemoSingleton(app core.App) error {
	return app.RunInTransaction(func(tx core.App) error {
		// An existing demo belongs to its editor: reruns preserve content/settings.
		if _, err := tx.FindCollectionByNameOrId(demoSingletonID); err == nil {
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		c := core.NewBaseCollection("demo_homepage", demoSingletonID)
		c.Fields.Add(&core.TextField{Name: "title", Required: true, Presentable: true}, &core.TextField{Name: "description"}, &core.FileField{Name: "cover", MaxSelect: 1}, &core.BoolField{Name: "published"})
		c.ListRule = types.Pointer("")
		c.ViewRule = types.Pointer("")
		if err := tx.Save(c); err != nil {
			return err
		}
		cfg, err := variants.Load(tx, demoOffersID)
		if err != nil {
			return err
		}
		cfg.Collection = c.Id
		cfg.Version = 0
		if _, err = variants.Publish(tx, *cfg); err != nil {
			return err
		}
		if _, err = singleton.Configure(tx, singleton.Config{Collection: c.Id, Enabled: true}); err != nil {
			return err
		}
		c, err = tx.FindCollectionByNameOrId(c.Id)
		if err != nil {
			return err
		}
		sets, err := tx.FindAllRecords("pv_sets", dbx.HashExp{"collection": c.Id})
		if err != nil {
			return err
		}
		for _, set := range sets {
			r := core.NewRecord(c)
			r.Set("title", "Главная — "+set.GetString("name"))
			r.Set("description", "Одна запись на выбранный набор. Измените заголовок или переключите вариант над формой.")
			r.Set("published", true)
			r.Set(variants.SetField, set.Id)
			if err := tx.Save(r); err != nil {
				return err
			}
		}
		return nil
	})
}
