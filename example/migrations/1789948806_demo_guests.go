package migrations

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
	"strings"
)

func init() { migrations.Register(ensureDemoGuests, func(core.App) error { return nil }) }

// Demo-only guest registration. Regular accounts are seeded by trusted Go.
// Passwords use PocketBase hashing; there is no second secret/binding table.
func ensureDemoGuests(app core.App) error {
	return app.RunInTransaction(func(tx core.App) error {
		c, err := tx.FindCollectionByNameOrId(demoMembersID)
		if err != nil {
			return err
		}
		if err := reserveUsersName(tx); err != nil {
			return err
		}
		c.Name = "users"
		c.AddIndex("idx_users_auth_id", true, "id", "")
		c.Fields.GetByName("email").(*core.EmailField).Required = false
		c.PasswordAuth.Enabled = true
		c.PasswordAuth.IdentityFields = []string{"email", "id"}
		c.CreateRule = types.Pointer(`@request.auth.id = "" && @request.body.email = "" && @request.body.verified = false && @request.body.tier:isset = false && @request.body.content_bucket:isset = false && @request.body.name:isset = false`)
		c.UpdateRule = nil
		// No public update: a guest cannot promote itself or set audience fields.
		return tx.Save(c)
	})
}

// Preserve the existing demo auth collection ID, hence all relations, buckets,
// user passwords and conversions. Only PocketBase's unused default users
// collection may be removed to free the requested name.
func reserveUsersName(app core.App) error {
	c, err := app.FindCollectionByNameOrId("users")
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if c.Id == demoMembersID {
		return nil
	}
	if c.Id != "_pb_users_auth_" {
		return fmt.Errorf("users is a custom collection; refusing to remove its schema")
	}
	count, err := app.CountRecords(c.Id)
	if err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("users already contains records; refusing to merge or remove accounts")
	}
	collections, err := app.FindAllCollections()
	if err != nil {
		return err
	}
	for _, other := range collections {
		if other.Id == c.Id {
			continue
		}
		data, err := json.Marshal(other.Fields)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), c.Id) {
			return fmt.Errorf("users is referenced by %s; refusing to remove it", other.Name)
		}
	}
	return app.Delete(c)
}
