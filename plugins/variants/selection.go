package variants

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// UserSelection returns a read-only subquery of user IDs with this effective
// assignment and a persisted bucket. Users predating Variants initialization
// acquire their bucket on their next authenticated API request. This query
// neither creates assignments nor records history.
// The returned SQL is schema-derived and safely quotes the supplied keys.
func UserSelection(app core.App, collection, authCollection, variant, experiment, group string) (string, error) {
	c, err := Load(app, collection)
	if err != nil {
		return "", err
	}
	if c.AuthCollection != authCollection {
		return "", fmt.Errorf("variants: different auth collection")
	}
	auth, err := app.FindCollectionByNameOrId(c.AuthCollection)
	if err != nil {
		return "", err
	}
	return "SELECT id FROM " + ident(viewName(c.Collection)) + " WHERE content_set = " + sqlString(setID(c.Collection, variant, experiment, group)) + " AND id IN (SELECT id FROM " + ident(auth.Name) + " WHERE " + ident(BucketField) + " > 0)", nil
}
