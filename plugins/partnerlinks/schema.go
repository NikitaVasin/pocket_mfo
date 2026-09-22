package partnerlinks

import (
	"encoding/json"
	"fmt"

	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/pocketbase/pocketbase/core"
)

// Called inside install's transaction. Existing record IDs, relations, rules and
// unrelated fields remain intact. Any malformed legacy value rolls back all work.
func migrateLinkField(tx core.App, c *core.Collection) error {
	if field, ok := c.Fields.GetByName("link").(*dynamiclink.Field); ok {
		if !field.Required || c.Fields.GetByName("url") != nil || c.Fields.GetByName("opening") != nil {
			return fmt.Errorf("partnerlinks: incompatible dynamic link schema")
		}
		return nil
	}
	if c.Fields.GetByName("link") != nil {
		return fmt.Errorf("partnerlinks: link field already exists with another type")
	}
	if _, ok := c.Fields.GetByName("url").(*core.URLField); !ok {
		return fmt.Errorf("partnerlinks: missing legacy URL field")
	}
	if _, ok := c.Fields.GetByName("opening").(*core.JSONField); !ok {
		return fmt.Errorf("partnerlinks: missing legacy opening field")
	}
	c.Fields.Add(&dynamiclink.Field{JSONField: core.JSONField{Name: "link", MaxSize: dynamiclink.MaxSize, Help: "Исходная партнёрская ссылка и параметры открытия в приложении."}})
	if err := tx.Save(c); err != nil {
		return err
	}
	records, err := tx.FindAllRecords(c)
	if err != nil {
		return err
	}
	for _, r := range records {
		value := Link{Mode: "appView", SaveCooke: true, ShowLoader: true}
		if raw := r.GetString("opening"); raw != "" && raw != "null" {
			if err := json.Unmarshal([]byte(raw), &value); err != nil {
				return fmt.Errorf("partnerlinks: invalid legacy opening in record %s", r.Id)
			}
		}
		value.URL = r.GetString("url")
		r.Set("link", value)
		if err := tx.Save(r); err != nil {
			return fmt.Errorf("partnerlinks: cannot migrate link %s: %w", r.Id, err)
		}
	}
	c.Fields.RemoveByName("url")
	c.Fields.RemoveByName("opening")
	c.Fields.GetByName("link").(*dynamiclink.Field).Required = true
	return tx.Save(c)
}
