// Package partnerlinks issues affiliate links backed by stored conversions and forwards conversions to AppMetrica.
package partnerlinks

import (
	"fmt"
	"time"

	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
)

const LinksCollection = "partner_links"
const configsCollection = "pl_config"
const configID = "partnerconfig01"

// Options contains trusted server configuration. Auth collections must exist at Start.
type Options struct {
	AuthCollections    []string
	VariantCollections []string
}

type Config struct {
	ProfileIDField          string            `json:"profileIdField"`
	Version                 int               `json:"version"`
	BaseURL                 string            `json:"baseUrl"`
	ApplicationID           int64             `json:"applicationId"`
	PostAPIKey              string            `json:"postApiKey,omitempty"`
	HasPostAPIKey           bool              `json:"hasPostApiKey,omitempty"`
	OpenTTLSeconds          int64             `json:"openTtlSeconds"`
	PendingRetentionDays    int               `json:"pendingRetentionDays"`
	ConversionRetentionDays int               `json:"conversionRetentionDays"`
	EventNames              map[string]string `json:"eventNames"`
	Providers               []Provider        `json:"providers"`
}

type Provider struct {
	RevenueStatus  string            `json:"revenueStatus"`
	SendRevenue    bool              `json:"sendRevenue"`
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	URLTemplate    string            `json:"urlTemplate"`
	MaxTokenLength int               `json:"maxTokenLength"`
	Secret         string            `json:"secret,omitempty"`
	HasSecret      bool              `json:"hasSecret,omitempty"`
	SecretLocation string            `json:"secretLocation"`
	SecretName     string            `json:"secretName"`
	Fields         PostbackFields    `json:"fields"`
	Statuses       map[string]string `json:"statuses"`
	ExtraFields    map[string]string `json:"extraFields"`
}

type PostbackFields struct {
	Token     string `json:"token"`
	Status    string `json:"status"`
	LeadID    string `json:"leadId"`
	EventID   string `json:"eventId"`
	Timestamp string `json:"timestamp"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
}

type Link = dynamiclink.Value
type WarningDialog = dynamiclink.WarningDialog

type ResolveResponse struct {
	ClickID   string    `json:"clickId"`
	ExpiresAt time.Time `json:"expiresAt"`
	Link      Link      `json:"link"`
}

type clickData struct {
	ClickID        string              `json:"clickId"`
	UserID         string              `json:"userId"`
	AuthCollection string              `json:"authCollection"`
	ProfileID      string              `json:"profileId"`
	ApplicationID  int64               `json:"applicationId"`
	LinkID         string              `json:"linkId"`
	ProviderID     string              `json:"providerId"`
	Experiments    []variants.Decision `json:"experiments"`
	IssuedAt       int64               `json:"issuedAt"`
	OpenUntil      int64               `json:"openUntil"`
}

func DefaultConfig() Config {
	return Config{OpenTTLSeconds: 86400, PendingRetentionDays: 14,
		EventNames: map[string]string{"click": "offer_click", "lead": "offer_lead", "approved": "offer_approved", "hold": "offer_hold", "rejected": "offer_rejected"}, Providers: []Provider{}}
}

func provider(c *Config, id string) (*Provider, error) {
	for i := range c.Providers {
		if c.Providers[i].ID == id {
			return &c.Providers[i], nil
		}
	}
	return nil, fmt.Errorf("partnerlinks: unknown provider")
}
