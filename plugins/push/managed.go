package push

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const managedStoreKey = "push.managed"
const adminLockStoreKey = "push.adminConfigLocked"

func detached[T any](v *T) *T {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}

func setManaged(app core.App, m *ManagedConfig) {
	var copy *ManagedConfig
	if m != nil {
		copy = &ManagedConfig{ApplicationID: detached(m.ApplicationID), OAuthToken: detached(m.OAuthToken), SendRate: detached(m.SendRate)}
	}
	app.Store().Set(managedStoreKey, copy)
}

func managed(app core.App) *ManagedConfig {
	m, _ := app.Store().Get(managedStoreKey).(*ManagedConfig)
	return m
}

func overlayManaged(app core.App, c *Config) {
	if m := managed(app); m != nil {
		if m.ApplicationID != nil {
			c.ApplicationID = *m.ApplicationID
		}
		if m.OAuthToken != nil {
			c.OAuthToken = *m.OAuthToken
		}
		if m.SendRate != nil {
			c.SendRate = *m.SendRate
		}
	}
	c.HasOAuthToken = c.OAuthToken != ""
}

func validateConfig(c Config) error {
	if c.ApplicationID <= 0 || c.SendRate < 100 || c.SendRate > 5000 {
		return textError("Application ID > 0, скорость 100–5000")
	}
	if strings.ContainsAny(c.OAuthToken, "\r\n") || len(c.OAuthToken) > 4096 {
		return textError("некорректный OAuth token")
	}
	return nil
}

func checkManaged(app core.App, in, old Config) error {
	if m := managed(app); m != nil {
		if m.ApplicationID != nil && in.ApplicationID != old.ApplicationID ||
			m.OAuthToken != nil && in.OAuthToken != old.OAuthToken ||
			m.SendRate != nil && in.SendRate != old.SendRate {
			return textError("настройка закреплена в Go-коде")
		}
	}
	return nil
}

func checkApplicationChange(app core.App, old, next int64) error {
	if old == 0 || old == next {
		return nil
	}
	var devices, runs int
	if err := app.DB().NewQuery("SELECT count(*) FROM push_devices").Row(&devices); err != nil {
		return err
	}
	if devices > 0 {
		return textError("смена приложения требует миграции привязок устройств")
	}
	if err := app.DB().NewQuery("SELECT count(*) FROM push_runs WHERE status NOT IN ('sent','failed','cancelled','empty')").Row(&runs); err != nil {
		return err
	}
	if runs > 0 {
		return textError("дождитесь завершения запусков перед сменой приложения")
	}
	return nil
}

// Persist the non-secret application identity so changing code on a subsequent
// restart cannot silently reassign existing devices to another application.
func applyManaged(app core.App) error {
	if managed(app) == nil {
		return nil
	}
	return app.RunInTransaction(func(tx core.App) error {
		raw, err := loadStored(tx)
		if err != nil {
			return err
		}
		effective := raw
		overlayManaged(tx, &effective)
		if err := validateConfig(effective); err != nil {
			return err
		}
		if err := checkApplicationChange(tx, raw.ApplicationID, effective.ApplicationID); err != nil {
			return err
		}
		if raw.ApplicationID == effective.ApplicationID {
			return nil
		}
		_, err = Configure(tx, effective)
		return err
	})
}

type configLocks struct {
	All    bool     `json:"all"`
	Fields []string `json:"fields"`
}

func settingsLocks(app core.App) configLocks {
	locks := configLocks{All: app.Store().Get(adminLockStoreKey) == true, Fields: []string{}}
	if m := managed(app); m != nil {
		if m.ApplicationID != nil {
			locks.Fields = append(locks.Fields, "applicationId")
		}
		if m.OAuthToken != nil {
			locks.Fields = append(locks.Fields, "oauthToken")
		}
		if m.SendRate != nil {
			locks.Fields = append(locks.Fields, "sendRate")
		}
	}
	return locks
}
