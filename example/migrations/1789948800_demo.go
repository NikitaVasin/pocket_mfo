package migrations

import (
	"github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

func init() {
	migrations.Register(func(app core.App) error {
		articles := core.NewBaseCollection("articles", "demoarticles001")
		videos := core.NewBaseCollection("videos", "demovideos00001")
		for _, c := range []*core.Collection{articles, videos} {
			c.Fields.Add(&core.TextField{Name: "title", Required: true, Presentable: true})
			c.ListRule = types.Pointer("")
			c.ViewRule = types.Pointer("")
			if err := app.Save(c); err != nil {
				return err
			}
		}
		comments := core.NewBaseCollection("comments", "democomments001")
		comments.Fields.Add(&core.TextField{Name: "text", Required: true, Presentable: true})
		comments.Fields.Add(&polymorphicrelation.Field{
			JSONField:     core.JSONField{Id: "subject00000001", Name: "subject"},
			CollectionIDs: []string{articles.Id, videos.Id}, OnDelete: polymorphicrelation.Restrict,
		})
		comments.ListRule = types.Pointer("")
		comments.ViewRule = types.Pointer("")
		if err := app.Save(comments); err != nil {
			return err
		}
		for i, c := range []*core.Collection{articles, videos} {
			r := core.NewRecord(c)
			r.Set("title", []string{"PocketBase plugins", "Polymorphic relations demo"}[i])
			if err := app.Save(r); err != nil {
				return err
			}
			comment := core.NewRecord(comments)
			comment.Set("text", []string{"Комментарий к статье", "Комментарий к видео"}[i])
			comment.Set("subject", polymorphicrelation.Reference{CollectionID: c.Id, RecordID: r.Id})
			if err := app.Save(comment); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		for _, name := range []string{"comments", "videos", "articles"} {
			c, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				return err
			}
			if err := app.Delete(c); err != nil {
				return err
			}
		}
		return nil
	})
}
