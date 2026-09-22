package dynamiclink

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const SettingsCollection = "dynamic_link_settings"

// Configure creates a singleton-per-Variants-set policy collection. Call from a
// migration after the auth collection exists. Repeated calls preserve all content.
func Configure(app core.App, authCollection string) error {
	return app.RunInTransaction(func(tx core.App) error {
		auth, err := tx.FindCollectionByNameOrId(authCollection)
		if err != nil {
			return err
		}
		if !auth.IsAuth() || auth.System {
			return fmt.Errorf("dynamicLink: non-system auth collection required")
		}
		c, err := tx.FindCollectionByNameOrId(SettingsCollection)
		created := errors.Is(err, sql.ErrNoRows)
		if created {
			c = core.NewBaseCollection(SettingsCollection)
			c.ListRule = types.Pointer("@request.auth.id != ''")
			c.ViewRule = types.Pointer("@request.auth.id != ''")
			c.Fields.Add(&core.SelectField{Name: "mode", MaxSelect: 1, Values: []string{"appView", "view", "browser"}, Help: "Пусто — режим самой ссылки. Выбранный режим переопределяет все Dynamic Link для этого набора пользователей."}, &core.SelectField{Name: "warningPolicy", MaxSelect: 1, Values: []string{"inherit", "replace", "disabled"}, Help: "inherit — настройки ссылки; replace — общее предупреждение, даже если ссылка отключает его; disabled — без предупреждений."}, &core.TextField{Name: "warningTitle", Help: "Заголовок общего предупреждения (replace)."}, &core.TextField{Name: "warningContent", Help: "Текст общего предупреждения (replace)."})
			if err = tx.Save(c); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		for name, kind := range map[string]string{"mode": core.FieldTypeSelect, "warningPolicy": core.FieldTypeSelect, "warningTitle": core.FieldTypeText, "warningContent": core.FieldTypeText} {
			f := c.Fields.GetByName(name)
			if f == nil || f.Type() != kind {
				return fmt.Errorf("dynamicLink: incompatible settings field %s", name)
			}
		}
		cfg, err := variants.Load(tx, c.Id)
		if errors.Is(err, sql.ErrNoRows) {
			cfg, err = variants.Publish(tx, variants.Config{Collection: c.Id, AuthCollection: auth.Id, Variables: true, Experiments: true, Default: variants.Variant{Key: "default"}})
		}
		if err != nil {
			return err
		}
		if cfg.AuthCollection != auth.Id {
			return fmt.Errorf("dynamicLink: policy already belongs to a different auth collection")
		}
		if _, err = singleton.Configure(tx, singleton.Config{Collection: c.Id, Enabled: true}); err != nil {
			return err
		}
		if created {
			c, err = tx.FindCollectionByNameOrId(c.Id)
			if err != nil {
				return err
			}
			r := core.NewRecord(c)
			r.Set("warningPolicy", "inherit")
			return tx.Save(r)
		}
		return nil
	})
}

type Policy struct{ Mode, WarningPolicy, WarningTitle, WarningContent string }

func policyFor(app core.App, user *core.Record) (Policy, error) {
	if user == nil || user.IsSuperuser() {
		return Policy{}, nil
	}
	cfg, err := variants.Load(app, SettingsCollection)
	if errors.Is(err, sql.ErrNoRows) {
		return Policy{}, nil
	}
	if err != nil {
		return Policy{}, err
	}
	if cfg.AuthCollection != user.Collection().Id {
		return Policy{}, nil
	}
	decision, err := variants.Resolve(app, cfg, user)
	if err != nil {
		return Policy{}, err
	}
	records, err := app.FindAllRecords(SettingsCollection, dbx.HashExp{variants.SetField: decision.Set})
	if err != nil {
		return Policy{}, err
	}
	if len(records) == 0 {
		return Policy{}, nil
	}
	if len(records) != 1 {
		return Policy{}, fmt.Errorf("dynamicLink: multiple policies in one set")
	}
	r := records[0]
	if err = validatePolicy(r); err != nil {
		return Policy{}, err
	}
	return Policy{r.GetString("mode"), r.GetString("warningPolicy"), r.GetString("warningTitle"), r.GetString("warningContent")}, nil
}
func validatePolicy(r *core.Record) error {
	mode := r.GetString("mode")
	if mode != "" && mode != "appView" && mode != "view" && mode != "browser" {
		return fmt.Errorf("dynamicLink: invalid policy mode")
	}
	switch r.GetString("warningPolicy") {
	case "", "inherit", "disabled":
		return nil
	case "replace":
		if strings.TrimSpace(r.GetString("warningTitle")) != "" && strings.TrimSpace(r.GetString("warningContent")) != "" {
			return nil
		}
	}
	return fmt.Errorf("dynamicLink: replace requires warningTitle and warningContent")
}
func (p Policy) apply(value Value) Value {
	if p.Mode != "" {
		value.Mode = p.Mode
	}
	switch p.WarningPolicy {
	case "disabled":
		value.WarningDialog = nil
		value.SkipWarningDialog = true
	case "replace":
		value.WarningDialog = &WarningDialog{Title: p.WarningTitle, Content: p.WarningContent}
		value.SkipWarningDialog = false
	}
	return value
}

// Apply resolves the user's Variants policy for custom endpoints such as resolve.
// It never changes stored source values or creates a Variants observation.
func Apply(app core.App, user *core.Record, value Value) (Value, error) {
	p, err := policyFor(app, user)
	if err != nil {
		return Value{}, err
	}
	return p.apply(value), nil
}

func enrichTree(r *core.Record, p Policy, seen map[*core.Record]bool) error {
	if r == nil || seen[r] {
		return nil
	}
	seen[r] = true
	for _, field := range r.Collection().Fields {
		if f, ok := field.(*Field); ok {
			value, err := Decode([]byte(r.GetString(f.Name)))
			if err != nil {
				return err
			}
			if value != nil {
				r.Set(f.Name, p.apply(*value))
			}
		}
	}
	for _, expanded := range r.Expand() {
		switch v := expanded.(type) {
		case *core.Record:
			if err := enrichTree(v, p, seen); err != nil {
				return err
			}
		case []*core.Record:
			for _, item := range v {
				if err := enrichTree(item, p, seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
