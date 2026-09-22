package partnerlinks

import (
	"fmt"
	"github.com/pocketbase/pocketbase/core"
	"slices"
)

type profileField struct {
	Name        string   `json:"name"`
	Collections []string `json:"collections"`
}

func (p *plugin) profileFields(app core.App) ([]profileField, error) {
	result := []profileField{}
	counts := map[string]int{}
	names := map[string][]string{}
	seen := map[string]bool{}
	for _, name := range p.options.AuthCollections {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			return nil, err
		}
		if seen[c.Id] {
			continue
		}
		seen[c.Id] = true
		for _, field := range c.Fields {
			if f, ok := field.(*core.TextField); ok && f.Name != core.FieldNameTokenKey {
				counts[f.Name]++
				names[f.Name] = append(names[f.Name], c.Name)
			}
		}
	}
	for name, count := range counts {
		if count == len(seen) {
			result = append(result, profileField{Name: name, Collections: names[name]})
		}
	}
	slices.SortFunc(result, func(a, b profileField) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return result, nil
}
func (p *plugin) validateProfileField(app core.App, name string) error {
	if name == "" {
		return nil
	}
	fields, err := p.profileFields(app)
	if err != nil {
		return err
	}
	for _, field := range fields {
		if field.Name == name {
			return nil
		}
	}
	return fmt.Errorf("Выберите существующее текстовое поле разрешённых auth-коллекций")
}
