package appmetrica

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

type Capability struct {
	Configured bool   `json:"configured"`
	Verified   bool   `json:"verified"`
	Message    string `json:"message"`
}
type Readiness struct {
	Events    Capability `json:"events"`
	Push      Capability `json:"push"`
	Reports   Capability `json:"reports"`
	CheckedAt string     `json:"checkedAt,omitempty"`
}

func Status(c Config) Readiness {
	missing := func(key, value string) string {
		fields := []string{}
		if c.ApplicationID <= 0 {
			fields = append(fields, "Application ID")
		}
		if value == "" {
			fields = append(fields, key)
		}
		if len(fields) == 0 {
			return ""
		}
		return "Не задано: " + strings.Join(fields, ", ") + "."
	}
	events := Capability{Configured: c.ApplicationID > 0 && c.PostAPIKey != "", Message: missing("Post API key", c.PostAPIKey)}
	if events.Configured {
		events.Message = "Параметры заданы. Приём событий и доходов не проверен."
	}
	oauth := Capability{Configured: c.ApplicationID > 0 && c.OAuthToken != "", Message: missing("OAuth-токен", c.OAuthToken)}
	if oauth.Configured {
		oauth.Message = "Параметры заданы. Доступ ещё не проверен."
	}
	return Readiness{Events: events, Push: oauth, Reports: oauth}
}
func adminSettings(app core.App, c Config) any {
	fields := []string{}
	for key := range pinned(app) {
		fields = append(fields, key)
	}
	slices.Sort(fields)
	readiness := Status(c)
	post, oauth := c.PostAPIKey != "", c.OAuthToken != ""
	c.PostAPIKey = ""
	c.OAuthToken = ""
	p, _ := app.Store().Get(storeKey).(*Plugin)
	return map[string]any{"config": c, "hasPostApiKey": post, "hasOAuthToken": oauth, "locks": map[string]any{"all": p != nil && p.options.LockAdminConfig, "fields": fields}, "readiness": readiness}
}

// Check performs only GET requests. It does not send events, create groups or
// send push notifications. Reading groups does not verify Firebase delivery.
func (p *Plugin) Check(parent context.Context, c Config) Readiness {
	out := Status(c)
	out.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	if !out.Push.Configured {
		return out
	}
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	application, err := p.readApplication(ctx, c.OAuthToken, c.ApplicationID)
	if err != nil {
		out.Push.Message = err.Error()
		out.Reports.Message = err.Error()
		return out
	}
	if c.PostAPIKey != "" && application.PostAPIKey != "" {
		out.Events.Verified = subtle.ConstantTimeCompare([]byte(c.PostAPIKey), []byte(application.PostAPIKey)) == 1
		out.Events.Message = "Заданный Post API key не совпадает с ключом приложения."
		if out.Events.Verified {
			out.Events.Message = "Ключ сверён с настройками приложения. Приём событий и доходов не проверен."
		}
	}
	var groups struct {
		Groups *[]json.RawMessage `json:"groups"`
	}
	err = PushRequest(ctx, p.client, c.OAuthToken, "GET", "/push/v1/management/groups?app_id="+strconv.FormatInt(c.ApplicationID, 10), nil, &groups)
	out.Push.Message = "Не удалось подтвердить доступ к Push API. Проверьте права токена."
	if err == nil && groups.Groups != nil {
		out.Push.Verified = true
		out.Push.Message = "Чтение Push API доступно. Настройка Firebase и доставка на устройство не проверены."
	}
	var report struct {
		Totals *[]float64 `json:"totals"`
	}
	err = ReadAnalytics(ctx, p.client, c.OAuthToken, url.Values{"ids": {strconv.FormatInt(c.ApplicationID, 10)}, "metrics": {"ym:s:users"}, "date1": {"today"}, "date2": {"today"}, "limit": {"1"}}, &report)
	out.Reports.Message = "Не удалось подтвердить чтение отчётов. Проверьте права токена."
	if err == nil && report.Totals != nil && len(*report.Totals) > 0 {
		out.Reports.Verified = true
		out.Reports.Message = "Чтение отчётов доступно."
	}
	return out
}
