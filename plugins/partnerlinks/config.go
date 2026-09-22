package partnerlinks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type internalKey struct{}

func save(app core.App, model core.Model) error {
	return app.SaveWithContext(context.WithValue(context.Background(), internalKey{}, true), model)
}
func internal(ctx context.Context) bool { return ctx != nil && ctx.Value(internalKey{}) == true }

// Load returns secrets to trusted Go code. HTTP callers receive a redacted copy.
func Load(app core.App) (*Config, error) {
	r, err := app.FindRecordById(configsCollection, configID)
	if errors.Is(err, sql.ErrNoRows) {
		c := DefaultConfig()
		return &c, nil
	}
	if err != nil {
		return nil, err
	}
	c := DefaultConfig()
	if err = json.Unmarshal([]byte(r.GetString("definition")), &c); err != nil {
		return nil, err
	}
	defaultRevenueStatuses(&c)
	return &c, nil
}

// Configure atomically replaces settings, preserving omitted secrets. Version
// must match Load; stale writes fail. Schema and rules cannot be changed here.
func Configure(app core.App, c Config) (*Config, error) {
	// Detach maps/slices so secret preservation never mutates caller-owned state.
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	c = Config{}
	if err = json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	err = app.RunInTransaction(func(tx core.App) error {
		old, err := Load(tx)
		if err != nil {
			return err
		}
		if c.Version != old.Version {
			return fmt.Errorf("partnerlinks: configuration version conflict; reload settings")
		}
		if c.PostAPIKey == "" {
			c.PostAPIKey = old.PostAPIKey
		}
		c.HasPostAPIKey = false
		for i := range c.Providers {
			p := &c.Providers[i]
			if p.Secret == "" {
				if previous, err := provider(old, p.ID); err == nil {
					p.Secret = previous.Secret
				}
			}
			p.HasSecret = false
		}
		if err = validateConfig(&c); err != nil {
			return err
		}
		for _, p := range old.Providers {
			if _, err := provider(&c, p.ID); err != nil {
				count, err := tx.CountRecords(LinksCollection, dbx.HashExp{"provider": p.ID})
				if err != nil {
					return err
				}
				if count > 0 {
					return fmt.Errorf("partnerlinks: provider %s is referenced by links", p.ID)
				}
			}
		}
		col, err := tx.FindCollectionByNameOrId(configsCollection)
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
		c.Version++
		r.Set("definition", c)
		return save(tx, r)
	})
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func redacted(c Config) Config {
	// Load/Configure own this instance; HTTP must never return plaintext secrets.
	c.HasPostAPIKey = c.PostAPIKey != ""
	c.PostAPIKey = ""
	for i := range c.Providers {
		c.Providers[i].HasSecret = c.Providers[i].Secret != ""
		c.Providers[i].Secret = ""
	}
	return c
}

func webURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || strings.ContainsAny(value, "\r\n") {
		return nil, fmt.Errorf("partnerlinks: expected an absolute HTTP(S) URL without credentials")
	}
	return u, nil
}

func renderURL(p *Provider, source, token string) (string, error) {
	u, err := webURL(source)
	if err != nil {
		return "", err
	}
	tmpl := p.URLTemplate
	if strings.ContainsAny(strings.ReplaceAll(strings.ReplaceAll(tmpl, "{url}", ""), "{clickData}", ""), "{}") {
		return "", fmt.Errorf("partnerlinks: unknown template placeholder")
	}
	if strings.Count(tmpl, "{clickData}") != 1 || strings.Count(tmpl, "{url}") > 1 {
		return "", fmt.Errorf("partnerlinks: template requires exactly one {clickData}")
	}
	var result string
	if strings.HasPrefix(tmpl, "{url}") {
		suffix := strings.TrimPrefix(tmpl, "{url}")
		if !strings.HasPrefix(suffix, "?") && !strings.HasPrefix(suffix, "&") {
			return "", fmt.Errorf("partnerlinks: {url} prefix must be followed by query parameters")
		}
		query, err := url.ParseQuery(strings.ReplaceAll(suffix[1:], "{clickData}", token))
		if err != nil {
			return "", fmt.Errorf("partnerlinks: invalid query template")
		}
		original, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return "", err
		}
		for key, values := range query {
			original[key] = values
		}
		u.RawQuery = original.Encode()
		result = u.String()
	} else {
		// Nested destination URLs are query-encoded; the token is already URL-safe.
		result = strings.ReplaceAll(strings.ReplaceAll(tmpl, "{url}", url.QueryEscape(source)), "{clickData}", token)
	}
	u, err = webURL(result)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(u.Host, "{}") || strings.Contains(u.Host, token) || strings.Contains(u.Fragment, token) || strings.ContainsAny(result, "{}") {
		return "", fmt.Errorf("partnerlinks: placeholders must be in the URL path or query")
	}
	return result, nil
}

// Missing values preserve the behavior of configurations created before status selection.
func defaultRevenueStatuses(c *Config) {
	for i := range c.Providers {
		if c.Providers[i].RevenueStatus == "" {
			c.Providers[i].RevenueStatus = "approved"
		}
	}
}

func validateConfig(c *Config) error {
	defaultRevenueStatuses(c)
	if c.ProfileIDField != "" && !regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,255}$`).MatchString(c.ProfileIDField) {
		return fmt.Errorf("partnerlinks: invalid profileIdField name")
	}
	if c.BaseURL != "" {
		u, err := webURL(c.BaseURL)
		if err != nil {
			return err
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("partnerlinks: base URL cannot contain query or fragment")
		}
		c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	}
	if c.ApplicationID < 0 || c.OpenTTLSeconds < 1 || c.OpenTTLSeconds > 365*86400 || c.PendingRetentionDays < 0 || c.PendingRetentionDays > 36500 || c.ConversionRetentionDays < 0 || c.ConversionRetentionDays > 36500 {
		return fmt.Errorf("partnerlinks: invalid application ID or retention (opening: 1–31536000 seconds; retention: 0–36500 days)")
	}
	for _, event := range []string{"click", "lead", "approved", "hold", "rejected"} {
		if strings.TrimSpace(c.EventNames[event]) == "" || len(c.EventNames[event]) > 256 {
			return fmt.Errorf("partnerlinks: invalid event name for %s", event)
		}
	}
	ids := map[string]bool{}
	for _, p := range c.Providers {
		if p.RevenueStatus != "approved" && p.RevenueStatus != "hold" {
			return fmt.Errorf("partnerlinks: revenueStatus must be approved or hold")
		}
		if !safeID.MatchString(p.ID) || ids[p.ID] || strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("partnerlinks: provider needs a unique stable ID and name")
		}
		ids[p.ID] = true
		if len(p.Secret) < 16 || len(p.Secret) > 1024 {
			return fmt.Errorf("partnerlinks: provider secret must contain 16–1024 bytes")
		}
		if p.SecretLocation != "header" && p.SecretLocation != "query" && p.SecretLocation != "body" {
			return fmt.Errorf("partnerlinks: secretLocation must be header, query or body")
		}
		if p.SecretName == "" || len(p.SecretName) > 256 || strings.ContainsAny(p.SecretName, "\r\n\t :?&=#") || (p.SecretLocation == "header" && !validHeaderName(p.SecretName)) {
			return fmt.Errorf("partnerlinks: invalid secret name")
		}
		if p.SecretLocation == "body" {
			for _, part := range strings.Split(p.SecretName, ".") {
				if part == "" {
					return fmt.Errorf("partnerlinks: body secret path must not have empty segments")
				}
			}
		}
		if p.MaxTokenLength < 0 || p.MaxTokenLength > maxTokenBytes {
			return fmt.Errorf("partnerlinks: invalid token length limit")
		}
		if p.SendRevenue && (p.Fields.Amount == "" || p.Fields.Currency == "" || p.Fields.LeadID == "") {
			return fmt.Errorf("partnerlinks: Revenue requires amount, currency and lead ID mappings")
		}
		if p.Fields.Token == "" || p.Fields.Status == "" || len(p.Statuses) == 0 {
			return fmt.Errorf("partnerlinks: token/status mappings are required")
		}
		for raw, status := range p.Statuses {
			if raw == "" || (status != "lead" && status != "approved" && status != "hold" && status != "rejected") {
				return fmt.Errorf("partnerlinks: invalid status mapping")
			}
		}
		paths := []string{p.Fields.Token, p.Fields.Status, p.Fields.LeadID, p.Fields.EventID, p.Fields.Timestamp, p.Fields.Amount, p.Fields.Currency}
		for key, path := range p.ExtraFields {
			if !safeID.MatchString(key) || path == "" {
				return fmt.Errorf("partnerlinks: invalid extra field")
			}
			paths = append(paths, path)
		}
		for _, path := range paths {
			if len(path) > 256 || strings.ContainsAny(path, "\r\n") || (path != "" && path == p.SecretName) {
				return fmt.Errorf("partnerlinks: invalid field path or secret mapped as event data")
			}
		}
		if strings.Contains(p.URLTemplate, p.Secret) {
			return fmt.Errorf("partnerlinks: secret must not appear in the public URL")
		}
		if _, err := renderURL(&p, "https://partner.example/offer?existing=1", "testClickData"); err != nil {
			return err
		}
	}
	return nil
}

func validHeaderName(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("!#$%&'*+-.^_`|~0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", c) {
			return false
		}
	}
	return http.CanonicalHeaderKey(s) != ""
}
