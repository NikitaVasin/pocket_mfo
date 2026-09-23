package typedconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

type Field struct {
	core.JSONField
	Schema    Schema            `json:"schema"`
	Relations map[string]string `json:"relations,omitempty"`
}

func init()                                  { core.Fields[Type] = func() core.Field { return &Field{} } }
func (f *Field) Type() string                { return Type }
func (f *Field) CalculateMaxBodySize() int64 { return MaxSize }
func (f *Field) PrepareValue(r *core.Record, raw any) (any, error) {
	v, err := f.JSONField.PrepareValue(r, raw)
	if err != nil {
		return v, err
	}
	if b, ok := v.(types.JSONRaw); ok && (len(b) == 0 || string(b) == "null") {
		return types.ParseJSONRaw([]Item{})
	}
	return v, nil
}
func (f *Field) ValidateSettings(ctx context.Context, app core.App, c *core.Collection) error {
	if err := f.JSONField.ValidateSettings(ctx, app, c); err != nil {
		return err
	}
	if !c.IsBase() || c.System || f.Required {
		return fmt.Errorf("typedConfig: use an optional field on a non-system base collection; deletion may empty the list")
	}
	if f.MaxSize != 0 && f.MaxSize != MaxSize {
		return fmt.Errorf("typedConfig: maxSize is fixed at %d", MaxSize)
	}
	return f.Schema.validate(app, c, nil)
}
func (f *Field) ValidateValue(ctx context.Context, app core.App, r *core.Record) error {
	_, _, err := f.evaluate(app, r, nil, false)
	return err
}
func Decode(raw []byte) ([]Item, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []Item{}, nil
	}
	if len(raw) > MaxSize {
		return nil, fmt.Errorf("typedConfig: too large")
	}
	var items []Item
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&items); err != nil {
		return nil, fmt.Errorf("typedConfig: expected a list of items: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("typedConfig: expected one document")
	}
	if len(items) > MaxItems {
		return nil, fmt.Errorf("typedConfig: at most %d items", MaxItems)
	}
	seen := map[string]bool{}
	for _, it := range items {
		if !validKey(it.ID) || seen[it.ID] {
			return nil, fmt.Errorf("typedConfig: invalid or duplicate item id")
		}
		seen[it.ID] = true
	}
	return items, nil
}

type denied struct{}

func (denied) Error() string { return "source unavailable" }

type evaluator struct {
	app    core.App
	schema Schema
	info   *core.RequestInfo
	render bool
	count  int
	refs   map[string][]string
}

func (f *Field) evaluate(app core.App, r *core.Record, info *core.RequestInfo, render bool) ([]Item, map[string][]string, error) {
	items, err := Decode([]byte(r.GetString(f.Name)))
	if err != nil {
		return nil, nil, err
	}
	e := evaluator{app: app, schema: f.Schema, info: info, render: render, refs: map[string][]string{}}
	result := []Item{}
	for _, item := range items {
		b := f.Schema.block(item.Type)
		if b == nil {
			return nil, nil, fmt.Errorf("items.%s: unknown type %q", item.ID, item.Type)
		}
		data, err := e.object(b.Fields, item.Data, "items."+item.ID, 0)
		if _, ok := err.(denied); ok && render {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		item.Data = data
		result = append(result, item)
	}
	for k, v := range e.refs {
		slices.Sort(v)
		e.refs[k] = slices.Compact(v)
	}
	return result, e.refs, nil
}
func (e *evaluator) object(ps []Property, value map[string]any, path string, depth int) (map[string]any, error) {
	if value == nil {
		return nil, fmt.Errorf("%s: expected object", path)
	}
	out := map[string]any{}
	known := map[string]bool{}
	for _, p := range ps {
		known[p.Name] = true
		v, ok := value[p.Name]
		if !ok || v == nil || p.Required && v == "" {
			if p.Required {
				return nil, fmt.Errorf("%s.%s: required", path, p.Name)
			}
			continue
		}
		n, err := e.value(p.Node, v, path+"."+p.Name, depth+1)
		if err != nil {
			return nil, err
		}
		out[p.Name] = n
	}
	for k := range value {
		if !known[k] {
			return nil, fmt.Errorf("%s.%s: unknown field", path, k)
		}
	}
	return out, nil
}
func (e *evaluator) value(n Node, v any, path string, depth int) (any, error) {
	e.count++
	if depth > 12 || e.count > 10000 {
		return nil, fmt.Errorf("typedConfig: content too complex")
	}
	if n.Ref != "" {
		return e.value(e.schema.Definitions[n.Ref], v, path, depth+1)
	}
	bad := func() (any, error) { return nil, fmt.Errorf("%s: invalid %s", path, n.Kind) }
	switch n.Kind {
	case "string", "enum":
		s, ok := v.(string)
		if !ok || len([]rune(s)) > maxLength(n) || (n.Kind == "enum" && !slices.Contains(n.Options, s)) {
			return bad()
		}
	case "number", "integer":
		x, ok := v.(float64)
		if !ok || math.IsNaN(x) || math.IsInf(x, 0) || (n.Kind == "integer" && math.Trunc(x) != x) || (n.Min != nil && x < *n.Min) || (n.Max != nil && x > *n.Max) {
			return bad()
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return bad()
		}
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return bad()
		}
		return e.object(n.Fields, obj, path, depth)
	case "array":
		items, ok := v.([]any)
		if !ok || len(items) > MaxItems {
			return bad()
		}
		out := []any{}
		for i, it := range items {
			vv, err := e.value(*n.Items, it, fmt.Sprintf("%s.%d", path, i), depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, vv)
		}
		return out, nil
	case "reference":
		ref, ok := v.(map[string]any)
		if !ok || len(ref) != 1 {
			return bad()
		}
		id, ok := ref["id"].(string)
		if !ok || len(id) != 15 {
			return bad()
		}
		c, err := e.app.FindCollectionByNameOrId(n.Source.Collection)
		if err != nil {
			return nil, err
		}
		record, err := e.app.FindRecordById(c, id)
		if err != nil {
			if e.render && e.info != nil {
				return nil, denied{}
			}
			return nil, fmt.Errorf("%s: referenced record is missing", path)
		}
		e.refs[c.Id] = append(e.refs[c.Id], id)
		if !e.render {
			return v, nil
		}
		if e.info != nil {
			allowed, err := e.app.CanAccessRecord(record, e.info, c.ViewRule)
			if err != nil {
				return nil, err
			}
			if !allowed {
				return nil, denied{}
			}
		}
		if len(n.Source.Mapping) == 0 {
			return map[string]any{"id": id, "collectionId": c.Id}, nil
		}
		result := map[string]any{}
		for key, source := range n.Source.Mapping {
			f := sourceField(c, source)
			if f == nil || f.GetHidden() {
				return nil, fmt.Errorf("%s: source schema changed", path)
			}
			result[key] = record.Get(f.GetName())
		}
		// Normalize PocketBase values to JSON scalars before validating the projection.
		b, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &result); err != nil {
			return nil, err
		}
		return e.object(n.Fields, result, path, depth)
	default:
		return bad()
	}
	return v, nil
}
func maxLength(n Node) int {
	if n.MaxLength > 0 {
		return n.MaxLength
	}
	return MaxSize
}
func fields(c *core.Collection) []*Field {
	out := []*Field{}
	for _, raw := range c.Fields {
		if f, ok := raw.(*Field); ok {
			out = append(out, f)
		}
	}
	return out
}
func (f *Field) depends(item Item, collection, id string) bool {
	var check func(Node, any) bool
	check = func(n Node, v any) bool {
		if n.Ref != "" {
			return check(f.Schema.Definitions[n.Ref], v)
		}
		switch n.Kind {
		case "reference":
			m, _ := v.(map[string]any)
			return n.Source.Collection == collection && m["id"] == id
		case "object":
			m, _ := v.(map[string]any)
			for _, p := range n.Fields {
				if check(p.Node, m[p.Name]) {
					return true
				}
			}
		case "array":
			a, _ := v.([]any)
			for _, x := range a {
				if check(*n.Items, x) {
					return true
				}
			}
		}
		return false
	}
	b := f.Schema.block(item.Type)
	if b == nil {
		return false
	}
	for _, p := range b.Fields {
		if check(p.Node, item.Data[p.Name]) {
			return true
		}
	}
	return false
}
func cleanName(name string) string { return strings.Trim(name, "+-") }
