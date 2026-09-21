package variants

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Conditions are compiled to schema-validated, quoted SQL inside a native view.
// EXISTS scopes prevent separate related rows satisfying parts of one condition.
type compiler struct {
	app           core.App
	personalized  map[string]bool
	seq, nodes    int
	previewBucket int
}

func (b *compiler) condition(c *core.Collection, alias string, n Condition, depth int) (string, error) {
	b.nodes++
	if depth > 6 || b.nodes > 200 {
		return "", errInvalid("condition exceeds 6 levels or 200 nodes")
	}
	if b.personalized[c.Id] || managedName(c.Name) || c.IsView() {
		return "", errInvalid("audiences cannot depend on personalized, service or view collections")
	}
	switch n.Kind {
	case "all", "any":
		if len(n.Children) == 0 {
			return "", errInvalid("empty condition group")
		}
		parts := []string{}
		for _, child := range n.Children {
			p, err := b.condition(c, alias, child, depth+1)
			if err != nil {
				return "", err
			}
			parts = append(parts, "("+p+")")
		}
		sep := " AND "
		if n.Kind == "any" {
			sep = " OR "
		}
		return strings.Join(parts, sep), nil
	case "exists", "notExists":
		if len(n.Children) != 1 {
			return "", errInvalid("relation scope requires one condition or group")
		}
		b.seq++
		childAlias := fmt.Sprintf("r%d", b.seq)
		var target *core.Collection
		var link string
		var err error
		if f, ok := c.Fields.GetByName(n.Relation).(*core.RelationField); ok {
			target, err = b.app.FindCollectionByNameOrId(f.CollectionId)
			if f.MaxSelect <= 1 {
				link = ident(childAlias) + ".id = " + ident(alias) + "." + ident(f.Name)
			} else {
				link = ident(childAlias) + ".id IN (SELECT value FROM json_each(" + ident(alias) + "." + ident(f.Name) + "))"
			}
		} else {
			parts := strings.Split(n.Relation, "_via_")
			if len(parts) != 2 {
				return "", errInvalid("unknown relation %q", n.Relation)
			}
			target, err = b.app.FindCollectionByNameOrId(parts[0])
			if err != nil {
				return "", err
			}
			f, ok := target.Fields.GetByName(parts[1]).(*core.RelationField)
			if !ok || f.CollectionId != c.Id {
				return "", errInvalid("invalid reverse relation %q", n.Relation)
			}
			if f.MaxSelect <= 1 {
				link = ident(childAlias) + "." + ident(f.Name) + " = " + ident(alias) + ".id"
			} else {
				link = ident(alias) + ".id IN (SELECT value FROM json_each(" + ident(childAlias) + "." + ident(f.Name) + "))"
			}
		}
		if err != nil {
			return "", err
		}
		p, err := b.condition(target, childAlias, n.Children[0], depth+1)
		if err != nil {
			return "", err
		}
		prefix := "EXISTS"
		if n.Kind == "notExists" {
			prefix = "NOT EXISTS"
		}
		return prefix + " (SELECT 1 FROM " + ident(target.Name) + " AS " + ident(childAlias) + " WHERE " + link + " AND (" + p + "))", nil
	case "field":
		f := c.Fields.GetByName(n.Field)
		if f == nil || f.GetHidden() || f.Type() == core.FieldTypePassword || n.Field == "tokenKey" {
			return "", errInvalid("unavailable audience field %q", n.Field)
		}
		col := ident(alias) + "." + ident(f.GetName())
		switch f.Type() {
		case core.FieldTypeText, core.FieldTypeEmail, core.FieldTypeURL, core.FieldTypeDate, core.FieldTypeAutodate, core.FieldTypeNumber, core.FieldTypeBool, core.FieldTypeSelect:
		default:
			return "", errInvalid("use a relation scope for relations; unsupported field %q", n.Field)
		}
		if f, ok := f.(*core.SelectField); ok && f.MaxSelect > 1 {
			return "", errInvalid("multi-select audience comparisons are unsupported")
		}
		if n.Op == "empty" {
			return "(" + col + " IS NULL OR " + col + " = '')", nil
		}
		if n.Op == "notEmpty" {
			return "(" + col + " IS NOT NULL AND " + col + " != '')", nil
		}
		value, err := literal(f, n.Value)
		if err != nil {
			return "", err
		}
		ops := map[string]string{"eq": "=", "ne": "!=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
		if op, ok := ops[n.Op]; ok {
			return "COALESCE(" + col + " " + op + " " + value + ", 0)", nil
		}
		if n.Op == "contains" || n.Op == "notContains" {
			if _, ok := n.Value.(string); !ok {
				return "", errInvalid("contains requires text")
			}
			op := "> 0"
			if n.Op == "notContains" {
				op = "= 0"
			}
			return "instr(COALESCE(" + col + ", ''), " + value + ") " + op, nil
		}
		return "", errInvalid("unknown operator %q", n.Op)
	default:
		return "", errInvalid("unknown condition kind %q", n.Kind)
	}
}
func literal(f core.Field, value any) (string, error) {
	switch f.Type() {
	case core.FieldTypeNumber:
		var n float64
		switch v := value.(type) {
		case float64:
			n = v
		case int:
			n = float64(v)
		case json.Number:
			var err error
			n, err = v.Float64()
			if err != nil {
				return "", err
			}
		default:
			return "", errInvalid("%s requires a number", f.GetName())
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return "", errInvalid("invalid number")
		}
		return strconv.FormatFloat(n, 'g', -1, 64), nil
	case core.FieldTypeBool:
		v, ok := value.(bool)
		if !ok {
			return "", errInvalid("%s requires a boolean", f.GetName())
		}
		if v {
			return "1", nil
		}
		return "0", nil
	default:
		v, ok := value.(string)
		if !ok {
			return "", errInvalid("%s requires text", f.GetName())
		}
		if len(v) > 4096 || strings.ContainsRune(v, 0) {
			return "", errInvalid("invalid text value")
		}
		return sqlString(v), nil
	}
}
