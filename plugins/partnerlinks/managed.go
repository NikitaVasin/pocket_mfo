package partnerlinks

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const managedStoreKey = "partnerlinks.managed"
const adminLockStoreKey = "partnerlinks.adminConfigLocked"

// Registration owns a detached snapshot, including maps, slices and pointers.
func setManaged(app core.App, source *ManagedConfig) {
	var snapshot *ManagedConfig
	b, _ := json.Marshal(source)
	_ = json.Unmarshal(b, &snapshot)
	if snapshot != nil && snapshot.BaseURL != nil {
		*snapshot.BaseURL = strings.TrimRight(*snapshot.BaseURL, "/")
	}
	app.Store().Set(managedStoreKey, snapshot)
}

func managed(app core.App) *ManagedConfig {
	m, _ := app.Store().Get(managedStoreKey).(*ManagedConfig)
	return m
}

func jsonFields(value any) map[string]json.RawMessage {
	b, _ := json.Marshal(value)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	return fields
}

func managedScalars(m *ManagedConfig) map[string]json.RawMessage {
	fields := jsonFields(m)
	delete(fields, "eventNames")
	delete(fields, "providers")
	return fields
}

func overlayManaged(c *Config, m *ManagedConfig) {
	if m == nil {
		return
	}
	fields := jsonFields(c)
	for key, value := range managedScalars(m) {
		fields[key] = value
	}
	b, _ := json.Marshal(fields)
	_ = json.Unmarshal(b, c)
	if c.EventNames == nil {
		c.EventNames = map[string]string{}
	}
	for key, value := range m.EventNames {
		c.EventNames[key] = value
	}
	// Detach managed maps from both callers and successive Load results.
	b, _ = json.Marshal(m.Providers)
	var providers []Provider
	_ = json.Unmarshal(b, &providers)
	for _, p := range providers {
		if old, err := provider(c, p.ID); err == nil {
			*old = p
		} else {
			c.Providers = append(c.Providers, p)
		}
	}
	defaultRevenueStatuses(c)
}

func checkManaged(c, old *Config, m *ManagedConfig) error {
	if m == nil {
		return nil
	}
	current, previous := jsonFields(c), jsonFields(old)
	for key := range managedScalars(m) {
		if string(current[key]) != string(previous[key]) {
			return fmt.Errorf("partnerlinks: %s is managed by Go code", key)
		}
	}
	for key := range m.EventNames {
		if c.EventNames[key] != old.EventNames[key] {
			return fmt.Errorf("partnerlinks: event %s is managed by Go code", key)
		}
	}
	for _, p := range m.Providers {
		current, err := provider(c, p.ID)
		previous, _ := provider(old, p.ID)
		if err != nil || !sameProvider(*current, *previous) {
			return fmt.Errorf("partnerlinks: provider %s is managed by Go code", p.ID)
		}
	}
	return nil
}

func sameProvider(a, b Provider) bool {
	a.HasSecret, b.HasSecret = false, false
	if len(a.ExtraFields) == 0 {
		a.ExtraFields = nil
	}
	if len(b.ExtraFields) == 0 {
		b.ExtraFields = nil
	}
	return reflect.DeepEqual(a, b)
}

// Persist only editable values. Removing Managed on restart reveals the last
// stored settings; values/secrets supplied by code never enter pl_config.
func storedConfig(c Config, raw *Config, m *ManagedConfig) Config {
	if m == nil {
		return c
	}
	fields, previous := jsonFields(c), jsonFields(raw)
	for key := range managedScalars(m) {
		delete(fields, key)
		if value, ok := previous[key]; ok {
			fields[key] = value
		}
	}
	b, _ := json.Marshal(fields)
	var stored Config
	_ = json.Unmarshal(b, &stored)
	for key := range m.EventNames {
		delete(stored.EventNames, key)
		if value, ok := raw.EventNames[key]; ok {
			stored.EventNames[key] = value
		}
	}
	stored.Providers = []Provider{}
	ids := map[string]bool{}
	for _, p := range m.Providers {
		ids[p.ID] = true
	}
	for _, p := range c.Providers {
		if !ids[p.ID] {
			stored.Providers = append(stored.Providers, p)
		}
	}
	for _, p := range raw.Providers {
		if ids[p.ID] {
			stored.Providers = append(stored.Providers, p)
		}
	}
	return stored
}

type configLocks struct {
	All        bool     `json:"all"`
	Fields     []string `json:"fields"`
	EventNames []string `json:"eventNames"`
	Providers  []string `json:"providers"`
}

type adminConfig struct {
	Config
	Locks   configLocks      `json:"locks"`
	Presets []ProviderPreset `json:"presets"`
}

func adminSettings(app core.App, c Config) adminConfig {
	result := adminConfig{Config: redacted(c), Presets: ProviderPresets()}
	result.Locks.All = app.Store().Get(adminLockStoreKey) == true
	if m := managed(app); m != nil {
		for key := range managedScalars(m) {
			result.Locks.Fields = append(result.Locks.Fields, key)
		}
		for key := range m.EventNames {
			result.Locks.EventNames = append(result.Locks.EventNames, key)
		}
		for _, p := range m.Providers {
			result.Locks.Providers = append(result.Locks.Providers, p.ID)
		}
	}
	sort.Strings(result.Locks.Fields)
	sort.Strings(result.Locks.EventNames)
	return result
}

func validateManaged(app core.App) error {
	m := managed(app)
	if m == nil {
		return nil
	}
	ids := map[string]bool{}
	for _, p := range m.Providers {
		if ids[p.ID] {
			return fmt.Errorf("partnerlinks: duplicate managed provider ID")
		}
		ids[p.ID] = true
	}
	c, err := Load(app)
	if err != nil {
		return err
	}
	return validateConfig(c)
}
