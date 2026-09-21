package variants

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

const adminRulesLockKey = "variants.adminRulesLocked"

// ErrAdminRulesLocked indicates an HTTP attempt to change code-managed rules.
var ErrAdminRulesLocked = errors.New("Variants: базовые правила доступа изменяются только из серверного кода")

// LockAdminRules disables rule editing through the Variants HTTP API for this
// app instance. Call before Start. Publish from Go remains unrestricted.
func LockAdminRules(app core.App) {
	app.Store().Set(adminRulesLockKey, true)
}

func adminRulesLocked(app core.App) bool {
	return app.Store().Get(adminRulesLockKey) == true
}
