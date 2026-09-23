// Package push manages audiences and durable AppMetrica push campaigns.
package push

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/NikitaVasin/pocket_mfo/plugins/mcp"
	"github.com/pocketbase/pocketbase/core"
)

const (
	DevicesCollection   = "push_devices"
	AudiencesCollection = "push_audiences"
	CampaignsCollection = "push_campaigns"
	RunsCollection      = "push_runs"
	jobsCollection      = "push_jobs"
	opensCollection     = "push_opens"
	configCollection    = "push_config"
	configID            = "pushsettings001"
	batchSize           = 10000
)

type Options struct {
	// OAuthClientID is the public Yandex OAuth app ID used for the authorization link.
	// It is not the AppMetrica Application ID or a sending credential.
	OAuthClientID   string
	AuthCollections []string
	MCP             *mcp.Server
	Managed         *ManagedConfig
	// LockAdminConfig prevents HTTP changes, including by superusers.
	LockAdminConfig bool
}

// ManagedConfig pins non-nil settings to a detached snapshot supplied by Go code.
// OAuthToken may be explicitly empty; managed secrets are never persisted.
type ManagedConfig struct {
	ApplicationID *int64
	OAuthToken    *string
	SendRate      *int
}
type Plugin struct {
	app     core.App
	options Options
	client  *http.Client
	worker  sync.Mutex
}
type Config struct {
	Version       int    `json:"version"`
	ApplicationID int64  `json:"applicationId"`
	OAuthToken    string `json:"oauthToken,omitempty"`
	HasOAuthToken bool   `json:"hasOAuthToken"`
	SendRate      int    `json:"sendRate"`
}
type Condition struct {
	Kind       string      `json:"kind"`             // all, any, not, field, conversion, variant
	Source     string      `json:"source,omitempty"` // user, device, conversion
	Field      string      `json:"field,omitempty"`
	Op         string      `json:"op,omitempty"`
	Value      any         `json:"value,omitempty"`
	Children   []Condition `json:"children,omitempty"`
	Collection string      `json:"collection,omitempty"`
	Variant    string      `json:"variant,omitempty"`
	Experiment string      `json:"experiment,omitempty"`
	Group      string      `json:"group,omitempty"`
}
type Audience struct {
	ID             string     `json:"id,omitempty"`
	Version        int        `json:"version"`
	Name           string     `json:"name"`
	AuthCollection string     `json:"authCollection"`
	Condition      *Condition `json:"condition,omitempty"`
	UserIDs        []string   `json:"userIds,omitempty"`
	ExcludeUserIDs []string   `json:"excludeUserIds,omitempty"`
	DeviceIDs      []string   `json:"deviceIds,omitempty"`
}
type Message struct {
	Title  string `json:"title"`
	Text   string `json:"text"`
	Image  string `json:"image,omitempty"`
	Action string `json:"action"` // app, route, partner
	Target string `json:"target,omitempty"`
}
type Campaign struct {
	AllUsers           bool     `json:"allUsers"` // Explicit opt-in to all connected auth collections.
	ID                 string   `json:"id,omitempty"`
	Version            int      `json:"version"`
	Name               string   `json:"name"`
	AudienceIDs        []string `json:"audienceIds"`
	ExcludeAudienceIDs []string `json:"excludeAudienceIds,omitempty"`
	LastDeviceOnly     bool     `json:"lastDeviceOnly"`
	CooldownHours      int      `json:"cooldownHours"`
	Message            Message  `json:"message"`
}
type Launch struct {
	CampaignID     string   `json:"campaignId"`
	Version        int      `json:"version"`
	IdempotencyKey string   `json:"idempotencyKey"`
	ScheduledAt    string   `json:"scheduledAt,omitempty"`
	TestDeviceIDs  []string `json:"testDeviceIds,omitempty"`
}
type Preview struct {
	Users      int  `json:"users"`
	Devices    int  `json:"devices"`
	TotalUsers *int `json:"totalUsers,omitempty" db:"totalUsers"`
}
type Recipient struct {
	Platform       string `json:"platform" db:"platform"`
	ID             string `json:"id" db:"id"`
	DeviceID       string `json:"deviceId" db:"deviceId"`
	UserID         string `json:"userId" db:"userId"`
	AuthCollection string `json:"authCollection" db:"authCollection"`
	Generation     string `json:"generation" db:"generation"`
}
type internalKey struct{}

func save(app core.App, m core.Model) error {
	return app.SaveWithContext(context.WithValue(context.Background(), internalKey{}, true), m)
}
func internal(ctx context.Context) bool { return ctx != nil && ctx.Value(internalKey{}) == true }
func hash(v string) string              { b := sha256.Sum256([]byte(v)); return hex.EncodeToString(b[:]) }
func secret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeRecord(r *core.Record, out any) error {
	return json.Unmarshal([]byte(r.GetString("definition")), out)
}
func textError(message string) error { return fmt.Errorf("push: %s", message) }
func ident(v string) string          { return `"` + strings.ReplaceAll(v, `"`, `""`) + `"` }
func literal(v string) string { // SQL strings, never identifiers.
	result := "'"
	for _, c := range v {
		if c == '\'' {
			result += "''"
		} else {
			result += string(c)
		}
	}
	return result + "'"
}
