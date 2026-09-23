package push

import (
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase/core"
)

func (p *Plugin) audienceSQL(app core.App, a Audience) (string, *core.Collection, error) {
	// Normalize Go integer values to the same representation as HTTP JSON.
	a = normalizeCondition(a)
	c, err := app.FindCollectionByNameOrId(a.AuthCollection)
	if err != nil || !c.IsAuth() || c.System || (!slices.Contains(p.options.AuthCollections, c.Id) && !slices.Contains(p.options.AuthCollections, c.Name)) {
		return "", nil, textError("auth-коллекция не подключена")
	}
	d, err := app.FindCollectionByNameOrId(DevicesCollection)
	if err != nil {
		return "", nil, err
	}
	where := []string{eligibleDevice("d"), "d.authCollection=" + literal(c.Id), "d.userId=u.id"}
	if a.Condition != nil {
		nodes := 0
		expr, err := conditionSQL(app, c, d, *a.Condition, 0, &nodes, false)
		if err != nil {
			return "", nil, err
		}
		where = append(where, expr)
	}
	for _, set := range []struct {
		ids       []string
		field, op string
	}{{a.UserIDs, "u.id", "IN"}, {a.ExcludeUserIDs, "u.id", "NOT IN"}, {a.DeviceIDs, "d.id", "IN"}} {
		if len(set.ids) > 10000 {
			return "", nil, textError("слишком большой ручной список")
		}
		if len(set.ids) == 0 {
			continue
		}
		values := []string{}
		for _, id := range set.ids {
			if len(id) != 15 {
				return "", nil, textError("некорректный ID")
			}
			values = append(values, literal(id))
		}
		where = append(where, set.field+" "+set.op+" ("+strings.Join(values, ",")+")")
	}
	return "SELECT d.id FROM push_devices d JOIN " + ident(c.Name) + " u ON u.id=d.userId WHERE " + strings.Join(where, " AND "), c, nil
}
func conditionSQL(app core.App, user, device *core.Collection, n Condition, depth int, nodes *int, inConversion bool) (string, error) {
	*nodes++
	if *nodes > 100 || depth > 6 {
		return "", textError("максимум 100 условий и 6 уровней")
	}
	switch n.Kind {
	case "all", "any", "not":
		if len(n.Children) == 0 || (n.Kind == "not" && len(n.Children) != 1) {
			return "", textError("пустая или некорректная группа условий")
		}
		parts := []string{}
		for _, child := range n.Children {
			v, err := conditionSQL(app, user, device, child, depth+1, nodes, inConversion)
			if err != nil {
				return "", err
			}
			parts = append(parts, "("+v+")")
		}
		if n.Kind == "not" {
			return "NOT " + parts[0], nil
		}
		sep := " AND "
		if n.Kind == "any" {
			sep = " OR "
		}
		return "(" + strings.Join(parts, sep) + ")", nil
	case "conversion":
		if inConversion || len(n.Children) != 1 {
			return "", textError("конверсия требует одно дерево условий")
		}
		if _, err := app.FindCollectionByNameOrId("conversations"); err != nil {
			return "", textError("Partner Links не подключён")
		}
		expr, err := conditionSQL(app, user, device, n.Children[0], depth+1, nodes, true)
		if err != nil {
			return "", err
		}
		return "EXISTS (SELECT 1 FROM conversations c WHERE c.userId=u.id AND c.authCollection=" + literal(user.Id) + " AND (" + expr + "))", nil
	case "variant":
		if inConversion {
			return "", textError("Variants не может быть условием строки конверсии")
		}
		query, err := variants.UserSelection(app, n.Collection, user.Id, n.Variant, n.Experiment, n.Group)
		if err != nil {
			return "", textError("некорректное назначение Variants")
		}
		return "u.id IN (" + query + ")", nil
	case "field":
		c, alias := user, "u"
		switch n.Source {
		case "user":
		case "device":
			c, alias = device, "d"
		case "conversion":
			if !inConversion {
				return "", textError("поля конверсии доступны внутри conversion")
			}
			var err error
			c, err = app.FindCollectionByNameOrId("conversations")
			if err != nil {
				return "", err
			}
			alias = "c"
		default:
			return "", textError("неизвестный источник условия")
		}
		f := c.Fields.GetByName(n.Field)
		if f == nil || f.GetHidden() || f.Type() == core.FieldTypePassword || n.Field == "tokenKey" {
			return "", textError("поле недоступно для аудитории")
		}
		field := alias + "." + ident(f.GetName())
		switch f.Type() {
		case core.FieldTypeText, core.FieldTypeEmail, core.FieldTypeURL, core.FieldTypeDate, core.FieldTypeAutodate, core.FieldTypeBool, core.FieldTypeNumber:
		case core.FieldTypeSelect:
			if f.(*core.SelectField).MaxSelect > 1 {
				return "", textError("множественный select не поддержан")
			}
		default:
			return "", textError("тип поля не поддержан в условии")
		}
		if n.Op == "empty" {
			return "(" + field + " IS NULL OR " + field + "='')", nil
		}
		if n.Op == "withinHours" || n.Op == "olderHours" {
			hours, ok := n.Value.(float64)
			if !ok || hours < 0 || hours > 87600 || math.IsNaN(hours) || (f.Type() != core.FieldTypeDate && f.Type() != core.FieldTypeAutodate) {
				return "", textError("относительное время требует поле даты и число часов")
			}
			op := ">="
			if n.Op == "olderHours" {
				op = "<"
			}
			return field + op + literal(time.Now().UTC().Add(-time.Duration(hours*float64(time.Hour))).Format("2006-01-02 15:04:05.000Z")), nil
		}
		var value string
		switch f.Type() {
		case core.FieldTypeBool:
			v, ok := n.Value.(bool)
			if !ok {
				return "", textError("ожидалось логическое значение")
			}
			value = "0"
			if v {
				value = "1"
			}
		case core.FieldTypeNumber:
			v, ok := n.Value.(float64)
			if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
				return "", textError("ожидалось число")
			}
			value = strconv.FormatFloat(v, 'g', -1, 64)
		default:
			v, ok := n.Value.(string)
			if !ok || len(v) > 4096 || strings.ContainsRune(v, 0) {
				return "", textError("ожидался текст")
			}
			value = literal(v)
		}
		ops := map[string]string{"eq": "=", "ne": "!=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
		op, ok := ops[n.Op]
		if !ok {
			return "", textError("неизвестная операция")
		}
		return field + op + value, nil
	default:
		return "", textError("неизвестный тип условия")
	}
}

func (p *Plugin) selection(app core.App, c Campaign, audiences map[string]Audience) (string, error) {
	union := func(ids []string) (string, error) {
		parts := []string{}
		for _, id := range ids {
			a, ok := audiences[id]
			if !ok {
				return "", textError("аудитория отсутствует")
			}
			q, _, err := p.audienceSQL(app, a)
			if err != nil {
				return "", err
			}
			parts = append(parts, q)
		}
		if len(parts) == 0 {
			return "SELECT id FROM push_devices WHERE 0", nil
		}
		return strings.Join(parts, " UNION "), nil
	}
	include, err := union(c.AudienceIDs)
	if err != nil {
		return "", err
	}
	if c.AllUsers {
		if len(c.AudienceIDs) > 0 {
			return "", textError("выберите всех пользователей или отдельные аудитории")
		}
		parts := []string{}
		for _, collection := range p.options.AuthCollections {
			q, _, err := p.audienceSQL(app, Audience{AuthCollection: collection})
			if err != nil {
				return "", err
			}
			parts = append(parts, q)
		}
		if len(parts) > 0 {
			include = strings.Join(parts, " UNION ")
		}
	}
	exclude, err := union(c.ExcludeAudienceIDs)
	if err != nil {
		return "", err
	}
	where := eligibleDevice("d") + " AND d.id IN (" + include + ") AND d.id NOT IN (" + exclude + ")"
	if c.CooldownHours > 0 {
		where += " AND (d.lastSent='' OR d.lastSent<" + literal(time.Now().UTC().Add(-time.Duration(c.CooldownHours)*time.Hour).Format("2006-01-02 15:04:05.000Z")) + ")"
	}
	if c.LastDeviceOnly {
		where += " AND NOT EXISTS (SELECT 1 FROM push_devices newer WHERE " + eligibleDevice("newer") + " AND newer.authCollection=d.authCollection AND newer.userId=d.userId AND (newer.lastSeen>d.lastSeen OR (newer.lastSeen=d.lastSeen AND newer.id>d.id)))"
	}
	return "SELECT d.id,d.deviceId,d.userId,d.authCollection,d.generation,d.platform FROM push_devices d WHERE " + where, nil
}
func loadCampaign(app core.App, id string) (Campaign, map[string]Audience, error) {
	r, err := app.FindRecordById(CampaignsCollection, id)
	if err != nil {
		return Campaign{}, nil, textError("кампания не найдена")
	}
	var c Campaign
	if err = decodeRecord(r, &c); err != nil {
		return c, nil, err
	}
	audiences := map[string]Audience{}
	for _, aid := range append(slices.Clone(c.AudienceIDs), c.ExcludeAudienceIDs...) {
		r, err := app.FindRecordById(AudiencesCollection, aid)
		if err != nil {
			return c, nil, textError("аудитория не найдена")
		}
		var a Audience
		if err = decodeRecord(r, &a); err != nil {
			return c, nil, err
		}
		audiences[aid] = a
	}
	return c, audiences, nil
}
func (p *Plugin) Preview(app core.App, id string) (Preview, error) {
	c, a, err := loadCampaign(app, id)
	if err != nil {
		return Preview{}, err
	}
	query, err := p.selection(app, c, a)
	if err != nil {
		return Preview{}, err
	}
	var result Preview
	err = app.DB().NewQuery("SELECT count(*) devices,count(DISTINCT authCollection||'/'||userId) users FROM (" + query + ")").One(&result)
	return result, err
}
func (p *Plugin) AudiencePreview(app core.App, a Audience) (Preview, error) {
	q, auth, err := p.audienceSQL(app, a)
	if err != nil {
		return Preview{}, err
	}
	var result Preview
	err = app.DB().NewQuery("SELECT count(*) devices,count(DISTINCT authCollection||'/'||userId) users,(SELECT count(*) FROM " + ident(auth.Name) + ") AS totalUsers FROM push_devices WHERE id IN (" + q + ")").One(&result)
	return result, err
}
func normalizeCondition(a Audience) Audience {
	b, _ := json.Marshal(a)
	_ = json.Unmarshal(b, &a)
	return a
}

func (p *Plugin) AudienceFields(app core.App) map[string]any {
	result := map[string]any{}
	for _, name := range append(slices.Clone(p.options.AuthCollections), DevicesCollection, "conversations") {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			continue
		}
		fields := []any{}
		for _, f := range c.Fields {
			if !f.GetHidden() && f.Type() != core.FieldTypePassword && f.GetName() != "tokenKey" {
				fields = append(fields, map[string]any{"name": f.GetName(), "type": f.Type()})
			}
		}
		result[c.Name] = map[string]any{"id": c.Id, "fields": fields}
	}
	return result
}
