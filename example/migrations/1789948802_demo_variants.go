package migrations

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

const (
	demoMembersID        = "demomembers0001"
	demoSubscriptionsID  = "demosubscript01"
	demoOffersID         = "demooffers00001"
	demoVariantsPassword = "demo-variants-123"
)

type demoMember struct {
	index                             int
	email, name, tier, variant, group string
}

var demoMembers = []demoMember{
	{1, "default@variants.test", "Обычный пользователь", "standard", "default", ""},
	{2, "newcomer@variants.test", "Новый пользователь", "new", "newcomer", ""},
	{100, "premium-a@variants.test", "Premium — группа A", "premium", "premium", "a"},
	{200, "premium-b@variants.test", "Premium — группа B", "premium", "premium", "b"},
	{300, "subscriber@variants.test", "Premium с оплаченной подпиской", "premium", "subscriber", ""},
	{400, "split@variants.test", "Условия в разных подписках", "standard", "default", ""},
}

func init() {
	migrations.Register(ensureDemoVariants, func(app core.App) error {
		// Seed rollback keeps records and published configuration: the user may
		// already have edited them. Reapplying the migration only fills missing data.
		return nil
	})
}

func demoMemberID(member demoMember) string {
	for i := member.index; ; i++ {
		id := fmt.Sprintf("demouser%07d", i)
		bucket := variants.Bucket(demoMembersID, id)
		if member.group == "" || (member.group == "a" && bucket <= 5000) || (member.group == "b" && bucket > 5000) {
			return id
		}
	}
}

func ensureDemoVariants(app core.App) error {
	return app.RunInTransaction(func(tx core.App) error {
		members := core.NewAuthCollection("demo_members", demoMembersID)
		members.Fields.Add(&core.TextField{Name: "name", Presentable: true}, &core.SelectField{Name: "tier", MaxSelect: 1, Values: []string{"standard", "new", "premium"}})
		members.ListRule = types.Pointer("id = @request.auth.id")
		members.ViewRule = types.Pointer("id = @request.auth.id")
		var err error
		members, err = ensureDemoCollection(tx, members)
		if err != nil {
			return err
		}
		subscriptions := core.NewBaseCollection("demo_subscriptions", demoSubscriptionsID)
		subscriptions.Fields.Add(&core.RelationField{Name: "member", CollectionId: members.Id, MaxSelect: 1, Required: true}, &core.SelectField{Name: "status", MaxSelect: 1, Values: []string{"active", "expired"}}, &core.BoolField{Name: "paid"}, &core.TextField{Name: "note", Presentable: true})
		subscriptions.AddIndex("idx_demo_subscriptions_member", false, "member", "")
		subscriptions, err = ensureDemoCollection(tx, subscriptions)
		if err != nil {
			return err
		}
		offers := core.NewBaseCollection("demo_offers", demoOffersID)
		offers.Fields.Add(&core.TextField{Name: "title", Required: true, Presentable: true}, &core.TextField{Name: "description"}, &core.NumberField{Name: "position", OnlyInt: true})
		offers.ListRule = types.Pointer("")
		offers.ViewRule = types.Pointer("")
		offers, err = ensureDemoCollection(tx, offers)
		if err != nil {
			return err
		}
		if _, err = variants.Load(tx, offers.Id); errors.Is(err, sql.ErrNoRows) {
			_, err = variants.Publish(tx, variants.Config{
				Collection: offers.Id, AuthCollection: members.Id, Variables: true, Experiments: true,
				Default: variants.Variant{Key: "default", Name: "Для всех"},
				Variants: []variants.Variant{
					{Key: "subscriber", Name: "Оплаченная подписка", Condition: &variants.Condition{Kind: "exists", Relation: subscriptions.Name + "_via_member", Children: []variants.Condition{{Kind: "all", Children: []variants.Condition{{Kind: "field", Field: "status", Op: "eq", Value: "active"}, {Kind: "field", Field: "paid", Op: "eq", Value: true}}}}}},
					{Key: "premium", Name: "Premium", Condition: &variants.Condition{Kind: "field", Field: "tier", Op: "eq", Value: "premium"}, Experiments: []variants.Experiment{{Key: "offer_layout", Name: "Формат подборки", Active: true, Groups: []variants.Group{{Key: "a", Name: "A — коротко", From: 1, To: 5000}, {Key: "b", Name: "B — подробно", From: 5001, To: 10000}}}}},
					{Key: "newcomer", Name: "Новички", Condition: &variants.Condition{Kind: "field", Field: "tier", Op: "eq", Value: "new"}},
				},
			})
		}
		if err != nil {
			return err
		}
		// Publish adds managed fields; reload before creating records.
		members, err = tx.FindCollectionByNameOrId(members.Id)
		if err != nil {
			return err
		}
		offers, err = tx.FindCollectionByNameOrId(offers.Id)
		if err != nil {
			return err
		}
		for _, m := range demoMembers {
			r := core.NewRecord(members)
			r.Id = demoMemberID(m)
			r.SetEmail(m.email)
			r.SetPassword(demoVariantsPassword)
			r.SetVerified(true)
			r.Set("name", m.name)
			r.Set("tier", m.tier)
			if err := saveMissingDemoRecord(tx, r); err != nil {
				return err
			}
		}
		for i, s := range []struct {
			member int
			status string
			paid   bool
			note   string
		}{
			{4, "active", true, "Оба условия выполнены: вариант подписчика выше Premium"},
			{5, "active", false, "Активна, но не оплачена"},
			{5, "expired", true, "Оплачена, но истекла: вместе с другой строкой не должна давать доступ"},
		} {
			r := core.NewRecord(subscriptions)
			r.Id = fmt.Sprintf("demosub%08d", i+1)
			r.Set("member", demoMemberID(demoMembers[s.member]))
			r.Set("status", s.status)
			r.Set("paid", s.paid)
			r.Set("note", s.note)
			if err := saveMissingDemoRecord(tx, r); err != nil {
				return err
			}
		}
		for i, s := range []struct{ variant, experiment, group, title, description string }{
			{"default", "", "", "Базовая подборка", "Видна гостям и пользователям без совпавшего правила."},
			{"default", "", "", "Как начать", "Общие материалы для знакомства с сервисом."},
			{"newcomer", "", "", "Добро пожаловать", "Этот набор выбирается при tier = new."},
			{"newcomer", "", "", "Первые шаги", "Материалы для нового пользователя вместо базовой подборки."},
			{"premium", "", "", "Premium: базовая подборка", "Появится после отключения эксперимента или для непокрытых бакетов."},
			{"premium", "", "", "Premium: преимущества", "Базовый набор варианта Premium, сейчас заменён группами A/B."},
			{"premium", "offer_layout", "a", "Premium A: краткое предложение", "Бакеты 1–5000. Короткое описание с главным преимуществом."},
			{"premium", "offer_layout", "a", "Premium A: три преимущества", "Компактный формат подборки для группы A."},
			{"premium", "offer_layout", "b", "Premium B: подробное предложение", "Бакеты 5001–10000. Расширенное описание предложения."},
			{"premium", "offer_layout", "b", "Premium B: сравнение вариантов", "Подробный формат подборки для группы B."},
			{"subscriber", "", "", "Подписка: персональная подборка", "Выбрана по одной связанной записи: status = active И paid = true."},
			{"subscriber", "", "", "Подписка: дополнительные материалы", "Правило подписки имеет приоритет над правилом Premium."},
		} {
			set, err := tx.FindFirstRecordByFilter("pv_sets", "collection = {:collection} && variant = {:variant} && experiment = {:experiment} && group = {:group}", dbx.Params{"collection": offers.Id, "variant": s.variant, "experiment": s.experiment, "group": s.group})
			if err != nil {
				return err
			}
			r := core.NewRecord(offers)
			r.Id = fmt.Sprintf("demooffer%06d", i+1)
			r.Set("title", s.title)
			r.Set("description", s.description)
			r.Set("position", i%2+1)
			r.Set(variants.SetField, set.Id)
			if err := saveMissingDemoRecord(tx, r); err != nil {
				return err
			}
		}
		return nil
	})
}

func ensureDemoCollection(app core.App, c *core.Collection) (*core.Collection, error) {
	if existing, err := app.FindCollectionByNameOrId(c.Id); err == nil {
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err := app.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}
func saveMissingDemoRecord(app core.App, r *core.Record) error {
	if _, err := app.FindRecordById(r.Collection(), r.Id); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return app.Save(r)
}
