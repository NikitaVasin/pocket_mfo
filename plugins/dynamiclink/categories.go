package dynamiclink

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

const registeredStoreKey = "dynamicLink:registered"

// RegisterDefaultCategory lets another plugin contribute a category before
// Bootstrap/Start. It only takes effect when Dynamic Link is also registered.
// Existing categories with this key are preserved, including their options.
// Missing categories are added at startup and to newly created policy records;
// their empty options inherit the general policy of the same Variants set.
func RegisterDefaultCategory(app core.App, key, label string) {
	id := Type + ":default-category:" + key
	enabled := func(a core.App) bool { return a.Store().Get(registeredStoreKey) == true }
	add := func(r *core.Record) (bool, error) {
		if !categoryKey.MatchString(key) || strings.TrimSpace(label) == "" {
			return false, fmt.Errorf("dynamicLink: invalid default category")
		}
		categories, err := decodeCategories(r)
		if err != nil {
			return false, err
		}
		for _, category := range categories {
			if category.Key == key {
				return false, nil
			}
		}
		r.Set("categories", append(categories, Category{Key: key, Label: label}))
		return true, validatePolicy(r)
	}
	ensure := func(a core.App) error {
		if !enabled(a) {
			return nil
		}
		return a.RunInTransaction(func(tx core.App) error {
			collection, err := tx.FindCollectionByNameOrId(SettingsCollection)
			if errors.Is(err, sql.ErrNoRows) {
				return nil // Configure will create the policy later, e.g. in a migration.
			}
			if err != nil {
				return err
			}
			records, err := tx.FindAllRecords(collection)
			if err != nil {
				return err
			}
			for _, record := range records {
				changed, err := add(record)
				if err != nil {
					return err
				}
				if changed {
					if err := tx.Save(record); err != nil {
						return err
					}
				}
			}
			return nil
		})
	}
	app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{Id: id, Priority: -10, Func: func(e *core.RecordEvent) error {
		if enabled(e.App) && e.Record.Collection().Name == SettingsCollection {
			if _, err := add(e.Record); err != nil {
				return err
			}
		}
		return e.Next()
	}})
	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{Id: id, Func: func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return ensure(e.App)
	}})
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: id, Func: func(e *core.ServeEvent) error {
		if err := ensure(e.App); err != nil {
			return err
		}
		return e.Next()
	}})
}
