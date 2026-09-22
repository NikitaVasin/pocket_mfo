package partnerlinks

// ProviderPreset supplies the same defaults to Go applications and the admin UI.
// Each call returns independent maps that callers may customize.
type ProviderPreset struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Provider Provider `json:"provider"`
}

const PresetRafinadNew = "rafinad_new"

// RafinadNew configures new.rafinad.io. Supply a stable provider ID and a random
// postback secret (16–1024 bytes). Revenue is opt-in; its amount is the publisher
// commission, never the loan/order total. Rafinad dates are not Unix timestamps.
func RafinadNew(id, secret string) Provider {
	return Provider{
		Preset: PresetRafinadNew, ID: id, Name: "Rafinad New",
		URLTemplate: "{url}?p_click_id={clickData}",
		Secret:      secret, SecretLocation: "query", SecretName: "secret",
		Fields:        PostbackFields{Token: "p_click_id", Status: "status", LeadID: "order_id", Amount: "publisher_commission", Currency: "currency"},
		Statuses:      map[string]string{"1": "lead", "2": "hold", "3": "rejected", "4": "approved"},
		RevenueStatus: "approved", ExtraFields: map[string]string{},
	}
}

func ProviderPresets() []ProviderPreset {
	return []ProviderPreset{{ID: PresetRafinadNew, Name: "Rafinad New", Provider: RafinadNew("", "")}}
}
