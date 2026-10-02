package partnerlinks

import (
	"fmt"
	"reflect"
)

// Only connection values can be supplied by an administrator. Provider
// definitions, event names and retention are always owned by trusted Go code.
func checkAdminConfig(c, old *Config) error {
	if c.OpenTTLSeconds != old.OpenTTLSeconds || c.PendingRetentionDays != old.PendingRetentionDays || c.ConversionRetentionDays != old.ConversionRetentionDays || !reflect.DeepEqual(c.EventNames, old.EventNames) {
		return fmt.Errorf("События и сроки хранения задаются только в коде проекта")
	}
	if len(c.Providers) != len(old.Providers) {
		return fmt.Errorf("Провайдеры подключаются только в коде проекта")
	}
	for _, p := range c.Providers {
		previous, err := provider(old, p.ID)
		if err != nil {
			return fmt.Errorf("Провайдеры подключаются только в коде проекта")
		}
		a, b := p, *previous
		if p.Preset == PresetRafinadNew {
			a.Secret, b.Secret = "", ""
		}
		if !sameProvider(a, b) {
			return fmt.Errorf("Параметры провайдера задаются только пресетом в коде проекта")
		}
	}
	return nil
}

type configReadiness struct {
	Ready   bool          `json:"ready"`
	Missing []configIssue `json:"missing"`
}

type configIssue struct {
	Field      string `json:"field"`
	ProviderID string `json:"providerId,omitempty"`
	Message    string `json:"message"`
}

func configurationReadiness(c Config) configReadiness {
	r := configReadiness{Missing: []configIssue{}}
	if c.BaseURL == "" {
		r.Missing = append(r.Missing, configIssue{Field: "baseUrl", Message: "Не задан публичный URL сервера."})
	}
	if c.ApplicationID <= 0 {
		r.Missing = append(r.Missing, configIssue{Field: "applicationId", Message: "Не задан Application ID AppMetrica."})
	}
	if c.PostAPIKey == "" {
		r.Missing = append(r.Missing, configIssue{Field: "postApiKey", Message: "Не задан Post API key AppMetrica."})
	}
	if len(c.Providers) == 0 {
		r.Missing = append(r.Missing, configIssue{Field: "providers", Message: "В коде проекта не подключён ни один провайдер."})
	}
	for _, p := range c.Providers {
		if p.Secret == "" {
			r.Missing = append(r.Missing, configIssue{Field: "secret", ProviderID: p.ID, Message: "Не задан секрет постбека: " + p.Name + "."})
		}
	}
	r.Ready = len(r.Missing) == 0
	return r
}
