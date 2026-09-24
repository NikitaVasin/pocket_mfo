package dynamiclink

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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
			c.Fields.Add(&core.SelectField{Name: "mode", MaxSelect: 1, Values: []string{"appView", "view", "browser"}, Help: "Пусто — внешний браузер. Общий режим для ссылок без переопределения в категории."}, &core.SelectField{Name: "warningPolicy", MaxSelect: 1, Values: []string{"inherit", "replace", "disabled"}, Help: "inherit и disabled — без предупреждения; replace — общее предупреждение. Категория может переопределить его."}, &core.TextField{Name: "warningTitle", Help: "Заголовок общего предупреждения (replace)."}, &core.TextField{Name: "warningContent", Help: "Текст общего предупреждения (replace)."})
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
		// Additive upgrade for existing installations; Configure remains atomic.
		changed := false
		for _, name := range []string{"openingOptions", "categories"} {
			if field := c.Fields.GetByName(name); field == nil {
				c.Fields.Add(&core.JSONField{Name: name, MaxSize: 65536})
				changed = true
			} else if field.Type() != core.FieldTypeJSON {
				return fmt.Errorf("dynamicLink: incompatible settings field %s", name)
			}
		}
		if changed {
			if err = tx.Save(c); err != nil {
				return err
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
			r.Set("mode", "browser")
			r.Set("warningPolicy", "disabled")
			return tx.Save(r)
		}
		return nil
	})
}

// OpeningOptions are category overrides; nil flags inherit the global value.
type OpeningOptions struct {
	Mode              string `json:"mode,omitempty"`
	SaveCooke         *bool  `json:"saveCooke,omitempty"`
	ChangeClient      *bool  `json:"changeClient,omitempty"`
	ShowLoader        *bool  `json:"showLoader,omitempty"`
	OpenURLsInBrowser *bool  `json:"openUrlsInBrowser,omitempty"`
	WarningPolicy     string `json:"warningPolicy,omitempty"`
	WarningTitle      string `json:"warningTitle,omitempty"`
	WarningContent    string `json:"warningContent,omitempty"`
}
type Category struct {
	Key     string         `json:"key"`
	Label   string         `json:"label"`
	Options OpeningOptions `json:"options"`
}
type Policy struct {
	Mode, WarningPolicy, WarningTitle, WarningContent string
	Options                                           OpeningOptions
	Categories                                        []Category
	configured                                        bool
}

var categoryKey = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func decodeOptions(raw string, target any) error {
	if raw == "" || raw == "null" {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("dynamicLink: invalid opening settings: %w", err)
	}
	return nil
}
func decodeCategories(r *core.Record) ([]Category, error) {
	var categories []Category
	err := decodeOptions(r.GetString("categories"), &categories)
	return categories, err
}
func validateOptions(o OpeningOptions) error {
	if o.Mode != "" && o.Mode != "browser" && o.Mode != "view" && o.Mode != "appView" {
		return fmt.Errorf("dynamicLink: invalid policy mode")
	}
	switch o.WarningPolicy {
	case "", "inherit", "disabled":
		return nil
	case "replace":
		if strings.TrimSpace(o.WarningTitle) != "" && strings.TrimSpace(o.WarningContent) != "" {
			return nil
		}
	}
	return fmt.Errorf("dynamicLink: replace requires warningTitle and warningContent")
}

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
		return Policy{configured: true}, nil
	}
	if len(records) != 1 {
		return Policy{}, fmt.Errorf("dynamicLink: multiple policies in one set")
	}
	r := records[0]
	if err = validatePolicy(r); err != nil {
		return Policy{}, err
	}
	p := Policy{Mode: r.GetString("mode"), WarningPolicy: r.GetString("warningPolicy"), WarningTitle: r.GetString("warningTitle"), WarningContent: r.GetString("warningContent"), configured: true}
	if err := decodeOptions(r.GetString("openingOptions"), &p.Options); err != nil {
		return Policy{}, err
	}
	p.Categories, err = decodeCategories(r)
	return p, err
}
func validatePolicy(r *core.Record) error {
	if err := validateOptions(OpeningOptions{Mode: r.GetString("mode"), WarningPolicy: r.GetString("warningPolicy"), WarningTitle: r.GetString("warningTitle"), WarningContent: r.GetString("warningContent")}); err != nil {
		return err
	}
	var options OpeningOptions
	if err := decodeOptions(r.GetString("openingOptions"), &options); err != nil {
		return err
	}
	if err := validateOptions(options); err != nil {
		return err
	}
	categories, err := decodeCategories(r)
	if err != nil {
		return err
	}
	if len(categories) > 100 {
		return fmt.Errorf("dynamicLink: at most 100 categories")
	}
	keys := map[string]bool{}
	for _, c := range categories {
		if !categoryKey.MatchString(c.Key) || strings.TrimSpace(c.Label) == "" || keys[c.Key] {
			return fmt.Errorf("dynamicLink: categories require unique keys and labels")
		}
		keys[c.Key] = true
		if err := validateOptions(c.Options); err != nil {
			return err
		}
	}
	return nil
}
func (o OpeningOptions) apply(value Value) Value {
	if o.Mode != "" {
		value.Mode = o.Mode
	}
	if o.SaveCooke != nil {
		value.SaveCooke = *o.SaveCooke
	}
	if o.ChangeClient != nil {
		value.ChangeClient = *o.ChangeClient
	}
	if o.ShowLoader != nil {
		value.ShowLoader = *o.ShowLoader
	}
	if o.OpenURLsInBrowser != nil {
		value.OpenURLsInBrowser = *o.OpenURLsInBrowser
	}
	switch o.WarningPolicy {
	case "disabled":
		value.WarningDialog = nil
		value.SkipWarningDialog = true
	case "replace":
		value.WarningDialog = &WarningDialog{Title: o.WarningTitle, Content: o.WarningContent}
		value.SkipWarningDialog = false
	}
	return value
}
func (p Policy) apply(value Value) Value {
	if !p.configured {
		return value
	}
	// Opening behavior belongs to the policy, including previously saved links.
	value.Mode = "browser"
	value.SaveCooke, value.ShowLoader = true, true
	value.ChangeClient, value.OpenURLsInBrowser = false, false
	value.WarningDialog, value.SkipWarningDialog = nil, true
	value = p.Options.apply(value)
	value = (OpeningOptions{Mode: p.Mode, WarningPolicy: p.WarningPolicy, WarningTitle: p.WarningTitle, WarningContent: p.WarningContent}).apply(value)
	for _, category := range p.Categories {
		if category.Key == value.Category {
			return category.Options.apply(value)
		}
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
