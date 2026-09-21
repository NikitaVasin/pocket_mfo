package migrations

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

func init() {
	migrations.Register(ensureDemoAdmin, func(app core.App) error {
		// Keep the account on rollback: it may already contain user changes.
		return nil
	})
}

func ensureDemoAdmin(app core.App) error {
	const email = "admin@admin.com"
	if _, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, email); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	collection, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return err
	}
	admin := core.NewRecord(collection)
	admin.SetEmail(email)
	admin.SetPassword("123456")
	admin.SetVerified(true)
	// This example explicitly uses a six-character password. SetPassword hashes
	// it normally; skip validation only here, keeping the default password policy.
	return app.SaveNoValidate(admin)
}
