// Package typedconfig provides typed, composable content lists with native forms.
package typedconfig

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/pocketbase/pocketbase/core"
)

const Type = "typedConfig"
const MaxSize = 262144
const MaxItems = 100

var keyPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

// Schema is an immutable application contract. Definitions can be reused by Ref.
type Schema struct {
	Version     int             `json:"version"`
	Definitions map[string]Node `json:"definitions,omitempty"`
	Types       []Block         `json:"types"`
}
type Block struct {
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	Description string     `json:"description,omitempty"`
	Fields      []Property `json:"fields"`
}
type Property struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Help     string `json:"help,omitempty"`
	Required bool   `json:"required,omitempty"`
	Node     Node   `json:"node"`
}

// Kind: string, number, integer, boolean, enum, object, array or reference.
// Ref names a Definition and cannot be combined with inline kind/settings.
type Node struct {
	Kind      string     `json:"kind,omitempty"`
	Ref       string     `json:"ref,omitempty"`
	Fields    []Property `json:"fields,omitempty"`
	Items     *Node      `json:"items,omitempty"`
	Options   []string   `json:"options,omitempty"`
	Min       *float64   `json:"min,omitempty"`
	Max       *float64   `json:"max,omitempty"`
	MaxLength int        `json:"maxLength,omitempty"`
	Source    *Source    `json:"source,omitempty"`
}

// Mapping maps output property names to source field IDs (names accepted initially).
// With no mapping the output is a typed record reference, without source data.
type Source struct {
	Collection string            `json:"collection"`
	LabelField string            `json:"labelField,omitempty"`
	Mapping    map[string]string `json:"mapping,omitempty"`
}
type Item struct {
	ID   string         `json:"id"`
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

func validKey(key string) bool {
	return keyPattern.MatchString(key) && key != "__proto__" && key != "constructor" && key != "prototype"
}
func (s Schema) node(n Node) Node {
	for n.Ref != "" {
		n = s.Definitions[n.Ref]
	}
	return n
}
func (s Schema) block(key string) *Block {
	for _, b := range s.Types {
		if b.Key == key {
			return &b
		}
	}
	return nil
}

func (s Schema) validate(app core.App, host, override *core.Collection) error {
	if s.Version < 1 || len(s.Types) == 0 || len(s.Types) > 30 || len(s.Definitions) > 100 {
		return fmt.Errorf("typedConfig: schema needs a version and 1–30 item types")
	}
	count := 0
	var node func(Node, int, map[string]bool) error
	var properties func([]Property, int, map[string]bool) error
	properties = func(ps []Property, depth int, seen map[string]bool) error {
		names := map[string]bool{}
		for _, p := range ps {
			if !validKey(p.Name) || names[p.Name] || p.Label == "" {
				return fmt.Errorf("typedConfig: invalid/duplicate property %q or missing label", p.Name)
			}
			names[p.Name] = true
			if err := node(p.Node, depth+1, seen); err != nil {
				return fmt.Errorf("%s: %w", p.Name, err)
			}
		}
		return nil
	}
	node = func(n Node, depth int, seen map[string]bool) error {
		count++
		if depth > 10 || count > 1000 {
			return fmt.Errorf("typedConfig: schema too deep or large")
		}
		if n.Ref != "" {
			if n.Kind != "" || n.Source != nil || n.Items != nil || len(n.Fields) > 0 || len(n.Options) > 0 || n.Min != nil || n.Max != nil || n.MaxLength != 0 {
				return fmt.Errorf("typedConfig: ref cannot override a type")
			}
			d, ok := s.Definitions[n.Ref]
			if !ok || seen[n.Ref] {
				return fmt.Errorf("typedConfig: missing or cyclic type %q", n.Ref)
			}
			next := map[string]bool{}
			for k, v := range seen {
				next[k] = v
			}
			next[n.Ref] = true
			return node(d, depth+1, next)
		}
		if n.Kind != "reference" && n.Source != nil || n.Kind != "array" && n.Items != nil || n.Kind != "enum" && len(n.Options) > 0 || n.Kind != "object" && n.Kind != "reference" && len(n.Fields) > 0 || n.Kind != "string" && n.Kind != "enum" && n.MaxLength != 0 || n.Kind != "number" && n.Kind != "integer" && (n.Min != nil || n.Max != nil) {
			return fmt.Errorf("typedConfig: options do not match kind %q", n.Kind)
		}
		if n.Min != nil && n.Max != nil && *n.Min > *n.Max {
			return fmt.Errorf("typedConfig: min exceeds max")
		}
		if n.MaxLength < 0 || n.MaxLength > MaxSize {
			return fmt.Errorf("typedConfig: invalid maxLength")
		}
		switch n.Kind {
		case "string", "number", "integer", "boolean":
		case "enum":
			if len(n.Options) == 0 || len(n.Options) > 100 {
				return fmt.Errorf("typedConfig: enum needs choices")
			}
			if len(slices.Compact(slices.Sorted(slices.Values(n.Options)))) != len(n.Options) {
				return fmt.Errorf("typedConfig: duplicate choices")
			}
		case "object":
			return properties(n.Fields, depth, seen)
		case "array":
			if n.Items == nil {
				return fmt.Errorf("typedConfig: array needs an item type")
			}
			return node(*n.Items, depth+1, seen)
		case "reference":
			if n.Source == nil {
				return fmt.Errorf("typedConfig: reference needs a source")
			}
			c, err := app.FindCollectionByNameOrId(n.Source.Collection)
			if err != nil {
				return err
			}
			if override != nil && override.Id == c.Id {
				c = override
			}
			if c.IsView() || c.System || c.IsAuth() || c.Id == host.Id {
				return fmt.Errorf("typedConfig: source must be another non-system base collection")
			}
			if n.Source.LabelField != "" {
				f := sourceField(c, n.Source.LabelField)
				if f == nil || f.GetHidden() {
					return fmt.Errorf("typedConfig: invalid source label field")
				}
			}
			if len(n.Source.Mapping) == 0 {
				if len(n.Fields) > 0 {
					return fmt.Errorf("typedConfig: projected fields require mappings")
				}
				return nil
			}
			if err := properties(n.Fields, depth, seen); err != nil {
				return err
			}
			if len(n.Source.Mapping) != len(n.Fields) {
				return fmt.Errorf("typedConfig: each projected property needs a mapping")
			}
			for _, p := range n.Fields {
				f := sourceField(c, n.Source.Mapping[p.Name])
				if f == nil || f.GetHidden() || !compatible(s.node(p.Node), f) {
					return fmt.Errorf("typedConfig: incompatible or hidden mapping %s", p.Name)
				}
			}
		default:
			return fmt.Errorf("typedConfig: unknown kind %q", n.Kind)
		}
		return nil
	}
	names := map[string]bool{}
	for _, b := range s.Types {
		if !validKey(b.Key) || b.Label == "" || names[b.Key] {
			return fmt.Errorf("typedConfig: invalid item type")
		}
		names[b.Key] = true
		if err := properties(b.Fields, 0, map[string]bool{}); err != nil {
			return err
		}
	}
	// Validate unused definitions too; a typo must not wait until first use.
	for key, n := range s.Definitions {
		if !validKey(key) {
			return fmt.Errorf("typedConfig: invalid definition key")
		}
		if err := node(n, 0, map[string]bool{key: true}); err != nil {
			return err
		}
	}
	return nil
}
func sourceField(c *core.Collection, name string) core.Field {
	if f := c.Fields.GetById(name); f != nil {
		return f
	}
	return c.Fields.GetByName(name)
}
func compatible(n Node, f core.Field) bool {
	switch f.Type() {
	case core.FieldTypeText, core.FieldTypeEditor, core.FieldTypeEmail, core.FieldTypeURL, core.FieldTypeDate, core.FieldTypeAutodate:
		return n.Kind == "string"
	case core.FieldTypeBool:
		return n.Kind == "boolean"
	case core.FieldTypeNumber:
		v := f.(*core.NumberField)
		return n.Kind == "number" || n.Kind == "integer" && v.OnlyInt
	case core.FieldTypeSelect:
		v := f.(*core.SelectField)
		if v.MaxSelect > 1 {
			return n.Kind == "array" && n.Items != nil && n.Items.Kind == "string"
		}
		return n.Kind == "string" || n.Kind == "enum" && slices.Equal(n.Options, v.Values)
	}
	return false
}

// walk also visits named definitions so reference bindings remain stable on rename.
func (s *Schema) walk(edit func(*Node)) {
	var visit func(*Node)
	visit = func(n *Node) {
		edit(n)
		for i := range n.Fields {
			visit(&n.Fields[i].Node)
		}
		if n.Items != nil {
			visit(n.Items)
		}
	}
	for i := range s.Types {
		for j := range s.Types[i].Fields {
			visit(&s.Types[i].Fields[j].Node)
		}
	}
	for key, n := range s.Definitions {
		visit(&n)
		s.Definitions[key] = n
	}
}
