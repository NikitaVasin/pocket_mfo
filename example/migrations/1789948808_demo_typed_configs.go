package migrations

import (
	"database/sql"
	"errors"

	"github.com/NikitaVasin/pocket_mfo/plugins/typedconfig"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

func init() { migrations.Register(ensureDemoTypedConfigs, func(core.App) error { return nil }) }
func ensureDemoTypedConfigs(app core.App) error {
	return app.RunInTransaction(func(tx core.App) error {
		if _, err := tx.FindCollectionByNameOrId("demo_screen_configs"); err == nil {
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		source, err := tx.FindCollectionByNameOrId("partner_links")
		if err != nil {
			return err
		}
		c := core.NewBaseCollection("demo_screen_configs")
		c.ListRule = types.Pointer("")
		c.ViewRule = types.Pointer("")
		c.Fields.Add(&core.TextField{Name: "title", Required: true, Presentable: true}, &typedconfig.Field{JSONField: core.JSONField{Name: "items", Help: "Блоки главного экрана"}, Schema: typedconfig.Schema{Version: 1, Definitions: map[string]typedconfig.Node{
			"style":    {Kind: "object", Fields: []typedconfig.Property{{Name: "layout", Label: "Расположение", Required: true, Node: typedconfig.Node{Kind: "enum", Options: []string{"Карточки", "Список"}}}, {Name: "compact", Label: "Компактный вид", Required: true, Node: typedconfig.Node{Kind: "boolean"}}}},
			"question": {Kind: "object", Fields: []typedconfig.Property{{Name: "question", Label: "Вопрос", Required: true, Node: typedconfig.Node{Kind: "string", MaxLength: 150}}, {Name: "answer", Label: "Ответ", Required: true, Node: typedconfig.Node{Kind: "string", MaxLength: 2000}}}},
		}, Types: []typedconfig.Block{
			{Key: "heading", Label: "Заголовок", Description: "Текст и оформление раздела", Fields: []typedconfig.Property{{Name: "text", Label: "Текст заголовка", Required: true, Node: typedconfig.Node{Kind: "string", MaxLength: 150}}, {Name: "style", Label: "Оформление", Node: typedconfig.Node{Ref: "style"}}}},
			{Key: "offerCard", Label: "Карточка оффера", Description: "Предложение из общего каталога", Fields: []typedconfig.Property{{Name: "offer", Label: "Оффер", Required: true, Node: typedconfig.Node{Kind: "reference", Source: &typedconfig.Source{Collection: source.Id, LabelField: "name", Mapping: map[string]string{"title": "name", "partnerLinkId": "id"}}, Fields: []typedconfig.Property{{Name: "title", Label: "Название", Required: true, Node: typedconfig.Node{Kind: "string"}}, {Name: "partnerLinkId", Label: "Партнёрская ссылка", Required: true, Node: typedconfig.Node{Kind: "string"}}}}}, {Name: "badge", Label: "Метка карточки", Node: typedconfig.Node{Kind: "enum", Options: []string{"Рекомендуем", "Популярное", "Новое"}}}}},
			{Key: "faq", Label: "Вопросы и ответы", Description: "Список ответов на частые вопросы", Fields: []typedconfig.Property{{Name: "entries", Label: "Вопросы", Required: true, Node: typedconfig.Node{Kind: "array", Items: &typedconfig.Node{Ref: "question"}}}}},
		}}})
		if err := tx.Save(c); err != nil {
			return err
		}
		if _, err = variants.Publish(tx, variants.Config{Collection: c.Id, AuthCollection: demoMembersID, Variables: true, Experiments: true, Default: variants.Variant{Key: "default"}}); err != nil {
			return err
		}
		c, err = tx.FindCollectionByNameOrId(c.Id)
		if err != nil {
			return err
		}
		r := core.NewRecord(c)
		r.Id = "demohomeconfig1"
		r.Set("title", "Главная витрина")
		r.Set("items", []typedconfig.Item{
			{ID: "welcome", Type: "heading", Data: map[string]any{"text": "Подберите подходящее предложение", "style": map[string]any{"layout": "Карточки", "compact": false}}},
			{ID: "featured", Type: "offerCard", Data: map[string]any{"offer": map[string]any{"id": "demopartner0001"}, "badge": "Рекомендуем"}},
			{ID: "help", Type: "faq", Data: map[string]any{"entries": []any{map[string]any{"question": "Как выбрать предложение?", "answer": "Сравните условия и откройте карточку."}}}},
		})
		return tx.Save(r)
	})
}
