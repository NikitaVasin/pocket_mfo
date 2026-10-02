// Package appmetrica owns shared AppMetrica configuration and API access.
package appmetrica

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const Collection = "appmetrica_config"
const configID = "appmetricaconf1"
const storeKey = "appmetrica.plugin"

type Config struct {
	Version       int               `json:"version"`
	ApplicationID int64             `json:"applicationId"`
	SDKAPIKey     string            `json:"sdkApiKey"`
	OAuthClientID string            `json:"oauthClientId"`
	PostAPIKey    string            `json:"postApiKey,omitempty"`
	OAuthToken    string            `json:"oauthToken,omitempty"`
	EventNames    map[string]string `json:"eventNames"`
}

type ManagedConfig struct {
	ApplicationID *int64            `json:"applicationId,omitempty"`
	SDKAPIKey     *string           `json:"sdkApiKey,omitempty"`
	OAuthClientID *string           `json:"oauthClientId,omitempty"`
	PostAPIKey    *string           `json:"postApiKey,omitempty"`
	OAuthToken    *string           `json:"oauthToken,omitempty"`
	EventNames    map[string]string `json:"eventNames,omitempty"`
}

type Options struct {
	Managed         *ManagedConfig
	LockAdminConfig bool
}

type internalKey struct{}

func save(app core.App, model core.Model) error {
	return app.SaveWithContext(context.WithValue(context.Background(), internalKey{}, true), model)
}

func Enabled(app core.App) bool { return app.Store().Get(storeKey) != nil }

func DefaultConfig() Config {
	return Config{EventNames: map[string]string{"click": "offer_click", "lead": "offer_lead", "approved": "offer_approved", "hold": "offer_hold", "rejected": "offer_rejected"}}
}

func object(value any) map[string]json.RawMessage {
	b, _ := json.Marshal(value)
	var out map[string]json.RawMessage
	_ = json.Unmarshal(b, &out)
	return out
}

func pinned(app core.App) map[string]json.RawMessage {
	if p, ok := app.Store().Get(storeKey).(*Plugin); ok && p.options.Managed != nil {
		return object(p.options.Managed)
	}
	return nil
}

func rawConfig(app core.App) (Config, error) {
	c := DefaultConfig()
	if _, err := app.FindCollectionByNameOrId(Collection); errors.Is(err, sql.ErrNoRows) {
		return c, nil
	} else if err != nil {
		return c, err
	}
	r, err := app.FindRecordById(Collection, configID)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal([]byte(r.GetString("definition")), &c)
	return c, err
}

// Load exposes credentials only to trusted server code.
func Load(app core.App) (Config, error) {
	c, err := rawConfig(app)
	if err != nil {
		return c, err
	}
	fields := object(c)
	for key, value := range pinned(app) {
		fields[key] = value
	}
	b, _ := json.Marshal(fields)
	err = json.Unmarshal(b, &c)
	return c, err
}

func validate(c Config) error {
	if c.ApplicationID < 0 {
		return fmt.Errorf("AppMetrica: Application ID не может быть отрицательным")
	}
	for _, v := range []string{c.SDKAPIKey, c.OAuthClientID, c.PostAPIKey, c.OAuthToken} {
		if len(v) > 4096 || strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("AppMetrica: некорректный параметр подключения")
		}
	}
	for _, event := range []string{"click", "lead", "approved", "hold", "rejected"} {
		if strings.TrimSpace(c.EventNames[event]) == "" || len(c.EventNames[event]) > 256 {
			return fmt.Errorf("AppMetrica: некорректное имя события %s", event)
		}
	}
	return nil
}

// GuardApplicationChange registers a domain guard without importing consumer plugins.
// Register guards before Bootstrap. They run in the configuration transaction.
func GuardApplicationChange(app core.App, name string, guard func(core.App, int64, int64) error) {
	const key = "appmetrica.guards"
	guards, _ := app.Store().Get(key).(map[string]func(core.App, int64, int64) error)
	if guards == nil {
		guards = map[string]func(core.App, int64, int64) error{}
	}
	guards[name] = guard
	app.Store().Set(key, guards)
}

func checkIdentity(app core.App, old, next int64) error {
	if old == next || old == 0 {
		return nil
	}
	guards, _ := app.Store().Get("appmetrica.guards").(map[string]func(core.App, int64, int64) error)
	for _, guard := range guards {
		if err := guard(app, old, next); err != nil {
			return err
		}
	}
	return nil
}

// Configure is atomic and preserves blank secrets. Managed values are immutable.
func Configure(app core.App, in Config) (Config, error) {
	var result Config
	err := app.RunInTransaction(func(tx core.App) error {
		old, err := Load(tx)
		if err != nil {
			return err
		}
		raw, err := rawConfig(tx)
		if err != nil {
			return err
		}
		if in.Version != old.Version {
			return fmt.Errorf("AppMetrica: настройки изменились; обновите страницу")
		}
		if in.PostAPIKey == "" {
			in.PostAPIKey = old.PostAPIKey
		}
		if in.OAuthToken == "" {
			in.OAuthToken = old.OAuthToken
		}
		// Derived keys belong to an application. Do not carry old keys to a
		// different ID when only the identity was changed in code or via API.
		if raw.ApplicationID != 0 && raw.ApplicationID != in.ApplicationID {
			fields := pinned(tx)
			if _, fixed := fields["postApiKey"]; !fixed && in.PostAPIKey == raw.PostAPIKey {
				in.PostAPIKey = ""
			}
			if _, fixed := fields["sdkApiKey"]; !fixed && in.SDKAPIKey == raw.SDKAPIKey {
				in.SDKAPIKey = ""
			}
		}
		if err := validate(in); err != nil {
			return err
		}
		current, previous := object(in), object(old)
		for key := range pinned(tx) {
			var a, b any
			_ = json.Unmarshal(current[key], &a)
			_ = json.Unmarshal(previous[key], &b)
			if !reflect.DeepEqual(a, b) {
				return fmt.Errorf("AppMetrica: параметр %s задан кодом", key)
			}
		}
		if err := checkIdentity(tx, raw.ApplicationID, in.ApplicationID); err != nil {
			return err
		}
		in.Version++
		stored := object(in)
		original := object(raw)
		for key := range pinned(tx) {
			if key != "applicationId" {
				delete(stored, key)
				if v, ok := original[key]; ok {
					stored[key] = v
				}
			}
		}
		col, err := tx.FindCollectionByNameOrId(Collection)
		if err != nil {
			return err
		}
		r, err := tx.FindRecordById(col, configID)
		if errors.Is(err, sql.ErrNoRows) {
			r = core.NewRecord(col)
			r.Id = configID
		} else if err != nil {
			return err
		}
		r.Set("definition", stored)
		if err := save(tx, r); err != nil {
			return err
		}
		result = in
		return nil
	})
	return result, err
}
