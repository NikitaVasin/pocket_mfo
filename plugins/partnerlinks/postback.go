package partnerlinks

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pocketbase/pocketbase/core"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func readPostback(r *core.RequestEvent) (map[string]any, error) {
	values := map[string]any{}
	if r.Request.Method == http.MethodGet {
		query, err := url.ParseQuery(r.Request.URL.RawQuery)
		if err != nil {
			return nil, err
		}
		for key, v := range query {
			if len(v) != 1 {
				return nil, fmt.Errorf("duplicate query field")
			}
			values[key] = v[0]
		}
		return values, nil
	}
	media, _, err := mime.ParseMediaType(r.Request.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	if media == "application/json" {
		err = decodeBody(r, &values)
		if values == nil {
			return nil, fmt.Errorf("expected JSON object")
		}
		return values, err
	}
	if media != "application/x-www-form-urlencoded" {
		return nil, fmt.Errorf("unsupported postback content type")
	}
	body, err := io.ReadAll(io.LimitReader(r.Request.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("request too large")
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	for key, v := range form {
		if len(v) != 1 {
			return nil, fmt.Errorf("duplicate form field")
		}
		values[key] = v[0]
	}
	return values, nil
}

func (p *plugin) postback(r *core.RequestEvent) error {
	r.Response.Header().Set("Cache-Control", "no-store")
	c, err := Load(r.App)
	if err != nil {
		return err
	}
	prov, err := provider(c, r.Request.PathValue("provider"))
	if err != nil {
		return r.NotFoundError("Неизвестный провайдер", nil)
	}
	var secret string
	var values map[string]any
	switch prov.SecretLocation {
	case "header":
		values := r.Request.Header.Values(prov.SecretName)
		if len(values) == 1 {
			secret = values[0]
		}
	case "query":
		query, err := url.ParseQuery(r.Request.URL.RawQuery)
		if err == nil && len(query[prov.SecretName]) == 1 {
			secret = query.Get(prov.SecretName)
		}
	case "body":
		if r.Request.Method != http.MethodPost {
			return r.ForbiddenError("Секрет в body принимается только через POST", nil)
		}
		// Body authentication requires parsing first; readPostback enforces the
		// size and content-type limits. Reuse the parsed values after verification.
		values, err = readPostback(r)
		if err != nil {
			return r.BadRequestError("Некорректный формат постбека", nil)
		}
		secret, err = scalar(values, prov.SecretName)
		if err != nil {
			return r.ForbiddenError("Неверный секрет провайдера", nil)
		}
	}
	a, b := sha256.Sum256([]byte(secret)), sha256.Sum256([]byte(prov.Secret))
	if prov.Secret == "" || subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
		return r.ForbiddenError("Неверный секрет провайдера", nil)
	}
	if values == nil {
		values, err = readPostback(r)
		if err != nil {
			return r.BadRequestError("Некорректный формат постбека", nil)
		}
	}
	// Tokens must remain strings; never coerce numeric postback values.
	var token string
	if raw, ok := values[prov.Fields.Token].(string); ok {
		token = raw
	} else {
		v := any(values)
		for _, part := range strings.Split(prov.Fields.Token, ".") {
			m, ok := v.(map[string]any)
			if !ok {
				v = nil
				break
			}
			v = m[part]
		}
		token, _ = v.(string)
	}
	stored, data, err := loadConversation(r.App, token)
	if err != nil {
		if _, ok := err.(invalidConversation); ok {
			return r.BadRequestError("Некорректный clickData", nil)
		}
		return r.Error(503, "Не удалось прочитать конверсию", nil)
	}
	if data.ProviderID != prov.ID {
		return r.BadRequestError("Некорректный clickData", nil)
	}
	now := time.Now().Unix()
	if expiredConversation(stored, c, now) {
		return r.Error(410, "Срок хранения конверсии истёк", nil)
	}
	if c.ApplicationID != data.ApplicationID || c.PostAPIKey == "" {
		return r.Error(503, "Приложение AppMetrica не настроено для этого токена", nil)
	}
	conversion, timestamp, err := parseConversion(values, prov, now)
	if err != nil {
		return r.BadRequestError(err.Error(), nil)
	}
	if prov.SendRevenue && conversion.Status == prov.RevenueStatus {
		attributes := analyticsAttributes(data)
		attributes["conversion"] = conversion
		payload, _ := json.Marshal(attributes)
		if len(payload) > 30*1024 {
			return r.BadRequestError("Revenue payload превышает 30 KiB", nil)
		}
		if err = validateRevenue(conversion); err != nil {
			return r.BadRequestError(err.Error(), nil)
		}
	}
	if err = collect(r.App, token, timestamp, conversion, prov.SendRevenue && conversion.Status == prov.RevenueStatus); err != nil {
		if errors.Is(err, errExpiredConversation) {
			return r.Error(410, err.Error(), nil)
		}
		if _, ok := err.(invalidConversation); ok {
			return r.BadRequestError(err.Error(), nil)
		}
		return r.Error(503, "Не удалось сохранить заказ", nil)
	}
	if err = p.deliverOnce(r.Request.Context(), r.App, c, data, token, "event_"+conversion.Status); err != nil {
		return r.JSON(http.StatusBadGateway, map[string]string{"error": "appmetrica_delivery_failed"})
	}
	if prov.SendRevenue && conversion.Status == prov.RevenueStatus {
		if err = p.deliverOnce(r.Request.Context(), r.App, c, data, token, "revenue"); err != nil {
			return r.JSON(http.StatusBadGateway, map[string]string{"error": "appmetrica_revenue_delivery_failed"})
		}
	}
	return r.JSON(200, map[string]bool{"ok": true})
}
