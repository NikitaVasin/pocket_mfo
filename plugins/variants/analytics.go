package variants

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// ResolveAll returns a consistent, read-only snapshot for all configurations
// using this user's auth collection. No manually maintained collection list.
// When Variants isn't installed there are no assignments.
func ResolveAll(app core.App, user *core.Record) ([]Decision, error) {
	items := []Decision{}
	if user == nil || user.IsSuperuser() {
		return items, nil
	}
	if _, err := app.FindCollectionByNameOrId(configs); errors.Is(err, sql.ErrNoRows) {
		return items, nil
	} else if err != nil {
		return nil, err
	}
	err := app.RunInTransaction(func(tx core.App) error {
		cs, err := allConfigs(tx)
		if err != nil {
			return err
		}
		for _, c := range cs {
			if c.AuthCollection != user.Collection().Id {
				continue
			}
			d, err := Resolve(tx, c, user)
			if err != nil {
				return err
			}
			items = append(items, d)
		}
		return nil
	})
	return items, err
}

// AnalyticsExperiments converts resolved assignments into the shared analytics
// dimensions. Callers storing a conversion must persist this map at issuance.
func AnalyticsExperiments(app core.App, decisions []Decision) (map[string]string, error) {
	result := map[string]string{}
	for _, d := range decisions {
		if d.Reason == "variables_disabled" {
			continue
		}
		c, err := app.FindCollectionByNameOrId(d.Collection)
		if err != nil {
			return nil, err
		}
		value := d.Variant
		if d.Experiment != "" {
			value += "/" + d.Experiment + "/" + d.Group
		}
		result[c.Name] = value
	}
	return result, nil
}
