package partnerlinks

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase/core"
)

const appMetricaURL = "https://api.appmetrica.yandex.ru/logs/v1/import/events"
const maxBody = 262144

func decodeBody(r *core.RequestEvent, out any) error {
	b, err := io.ReadAll(io.LimitReader(r.Request.Body, maxBody+1))
	if err != nil {
		return err
	}
	if len(b) > maxBody {
		return fmt.Errorf("request too large")
	}
	// PocketBase's RequestInfo subsequently reads the body to evaluate ViewRule.
	r.Request.Body = io.NopCloser(bytes.NewReader(b))
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err = d.Decode(out); err != nil {
		return err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON document")
	}
	return nil
}

func (p *plugin) resolve(r *core.RequestEvent) error {
	if r.Auth == nil || r.Auth.IsSuperuser() {
		return r.ForbiddenError("Нужен пользователь разрешённой auth-коллекции", nil)
	}
	allowed := false
	for _, id := range p.options.AuthCollections {
		if id == r.Auth.Collection().Id || id == r.Auth.Collection().Name {
			allowed = true
		}
	}
	if !allowed {
		return r.ForbiddenError("Auth-коллекция не подключена", nil)
	}
	var input struct{}
	if err := decodeBody(r, &input); err != nil && err != io.EOF {
		return r.BadRequestError("Запрос не должен содержать profileId или device", nil)
	}
	c, err := Load(r.App)
	if err != nil {
		return err
	}
	if c.BaseURL == "" || c.ApplicationID <= 0 || c.PostAPIKey == "" {
		return r.Error(503, "Партнёрские ссылки не настроены", nil)
	}
	record, err := r.App.FindRecordById(LinksCollection, r.Request.PathValue("id"))
	if err != nil || !record.GetBool("active") {
		return r.NotFoundError("Ссылка недоступна", nil)
	}
	info, err := r.RequestInfo()
	if err != nil {
		return err
	}
	if ok, err := r.App.CanAccessRecord(record, info, record.Collection().ViewRule); err != nil || !ok {
		return r.NotFoundError("Ссылка недоступна", nil)
	}
	prov, err := provider(c, record.GetString("provider"))
	if err != nil {
		return r.NotFoundError("Провайдер недоступен", nil)
	}

	now := time.Now().Unix()
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return err
	}
	data := clickData{ClickID: base64.RawURLEncoding.EncodeToString(id), UserID: r.Auth.Id, AuthCollection: r.Auth.Collection().Id, ApplicationID: c.ApplicationID, LinkID: record.Id, ProviderID: prov.ID, Experiments: []variants.Decision{}, IssuedAt: now, OpenUntil: now + c.OpenTTLSeconds}
	for _, collection := range p.options.VariantCollections {
		cfg, err := variants.Load(r.App, collection)
		if err != nil {
			return r.InternalServerError("Не удалось загрузить конфигурацию экспериментов", nil)
		}
		if cfg.AuthCollection != r.Auth.Collection().Id {
			continue
		}
		d, err := variants.Resolve(r.App, cfg, r.Auth)
		if err != nil {
			return r.InternalServerError("Не удалось определить эксперименты", nil)
		}
		data.Experiments = append(data.Experiments, d)
	}
	data.AnalyticsExperiments, err = variants.AnalyticsExperiments(r.App, data.Experiments)
	if err != nil {
		return r.InternalServerError("Не удалось подготовить эксперименты", nil)
	}
	token, err := newToken()
	if err != nil {
		return r.BadRequestError(err.Error(), nil)
	}
	if prov.MaxTokenLength > 0 && len(token) > prov.MaxTokenLength {
		return r.BadRequestError("clickData превышает лимит провайдера", nil)
	}
	link, err := opening(record)
	if err != nil {
		return r.BadRequestError("Некорректная партнёрская ссылка", nil)
	}
	if _, err = renderURL(prov, link.URL, token); err != nil {
		return r.BadRequestError(err.Error(), nil)
	}
	link, err = dynamiclink.Apply(r.App, r.Auth, link)
	if err != nil {
		return r.InternalServerError("Не удалось применить настройки Dynamic Link", nil)
	}
	if err = createConversation(r.App, data, token); err != nil {
		return r.Error(503, "Не удалось сохранить конверсию", nil)
	}
	link.URL = c.BaseURL + "/api/partnerlinks/r/" + token
	r.Response.Header().Set("Cache-Control", "no-store")
	return r.JSON(200, ResolveResponse{ClickID: data.ClickID, ExpiresAt: time.Unix(data.OpenUntil, 0).UTC(), Link: link})
}

func (p *plugin) redirect(r *core.RequestEvent) error {
	r.Response.Header().Set("Cache-Control", "no-store")
	r.Response.Header().Set("Referrer-Policy", "no-referrer")
	if r.Request.Method != http.MethodGet {
		return r.NoContent(http.StatusMethodNotAllowed)
	}
	token := r.Request.PathValue("token")
	stored, data, err := loadConversation(r.App, token)
	if err != nil {
		if _, ok := err.(invalidConversation); ok {
			return r.BadRequestError("Некорректная ссылка", nil)
		}
		return r.Error(503, "Не удалось прочитать конверсию", nil)
	}
	if time.Now().Unix() >= data.OpenUntil {
		return r.Error(410, "Срок открытия ссылки истёк", nil)
	}
	c, err := Load(r.App)
	if err != nil {
		return err
	}
	if expiredConversation(stored, c, time.Now().Unix()) {
		return r.Error(410, "Срок хранения конверсии истёк", nil)
	}
	if c.ApplicationID != data.ApplicationID {
		return r.Error(410, "Приложение ссылки больше не настроено", nil)
	}
	prov, err := provider(c, data.ProviderID)
	if err != nil {
		return r.NotFoundError("Провайдер недоступен", nil)
	}
	record, err := r.App.FindRecordById(LinksCollection, data.LinkID)
	if err != nil || !record.GetBool("active") || record.GetString("provider") != data.ProviderID {
		return r.NotFoundError("Ссылка недоступна", nil)
	}
	link, err := opening(record)
	if err != nil {
		return r.BadRequestError("Некорректная партнёрская ссылка", nil)
	}
	target, err := renderURL(prov, link.URL, token)
	if err != nil {
		return r.BadRequestError("Некорректная партнёрская ссылка", nil)
	}
	if err = p.send(r.Request.Context(), c, data, "click", time.Now().Unix(), nil); err != nil {
		r.App.Logger().Warn("partnerlinks: click delivery failed", "provider", data.ProviderID)
	}
	return r.Redirect(http.StatusFound, target)
}

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

func scalar(values map[string]any, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	var value any = values
	// Form/query names are literal; nested paths apply only when no literal key exists.
	if direct, ok := values[path]; ok {
		value = direct
	} else {
		for _, part := range strings.Split(path, ".") {
			m, ok := value.(map[string]any)
			if !ok {
				return "", nil
			}
			value = m[part]
		}
	}
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		if len(v) > 4096 {
			return "", fmt.Errorf("field too long")
		}
		return v, nil
	case json.Number:
		return v.String(), nil
	case bool:
		return strconv.FormatBool(v), nil
	default:
		return "", fmt.Errorf("expected scalar postback field")
	}
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
	// The token may exceed the ordinary attribute limit.
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
	rawStatus, err := scalar(values, prov.Fields.Status)
	if err != nil {
		return r.BadRequestError("Некорректный статус", nil)
	}
	status := prov.Statuses[rawStatus]
	if status == "" {
		return r.BadRequestError("Неизвестный статус", nil)
	}
	conversion := map[string]any{"status": status, "rawStatus": rawStatus}
	for key, path := range map[string]string{"leadId": prov.Fields.LeadID, "eventId": prov.Fields.EventID, "amount": prov.Fields.Amount, "currency": prov.Fields.Currency} {
		v, err := scalar(values, path)
		if err != nil {
			return r.BadRequestError("Некорректное поле конверсии", nil)
		}
		if v != "" {
			conversion[key] = v
		}
	}
	if raw, ok := conversion["amount"].(string); ok {
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return r.BadRequestError("Некорректная сумма", nil)
		}
	}
	extra := map[string]string{}
	for key, path := range prov.ExtraFields {
		v, err := scalar(values, path)
		if err != nil {
			return r.BadRequestError("Некорректный дополнительный параметр", nil)
		}
		if v != "" {
			extra[key] = v
		}
	}
	if len(extra) > 0 {
		conversion["extra"] = extra
	}
	timestamp := now
	rawTime, err := scalar(values, prov.Fields.Timestamp)
	if err != nil {
		return r.BadRequestError("Некорректное время события", nil)
	}
	if rawTime != "" {
		timestamp, err = strconv.ParseInt(rawTime, 10, 64)
		if err != nil {
			return r.BadRequestError("Время события должно быть Unix timestamp в секундах", nil)
		}
	}
	if timestamp < now-14*86400 || timestamp > now {
		return r.BadRequestError("Время события должно быть в пределах последних 14 дней", nil)
	}
	if prov.SendRevenue && status == prov.RevenueStatus {
		payload, _ := json.Marshal(map[string]any{"clickData": data, "conversion": conversion, "experiments": data.AnalyticsExperiments})
		if len(payload) > 30*1024 {
			return r.BadRequestError("Revenue payload превышает 30 KiB", nil)
		}
		if err = validateRevenue(conversion); err != nil {
			return r.BadRequestError(err.Error(), nil)
		}
	}
	if err = collect(r.App, token, status, timestamp, conversion); err != nil {
		if errors.Is(err, errExpiredConversation) {
			return r.Error(410, err.Error(), nil)
		}
		if _, ok := err.(invalidConversation); ok {
			return r.BadRequestError(err.Error(), nil)
		}
		return r.Error(503, "Не удалось сохранить заказ", nil)
	}
	if err = p.send(r.Request.Context(), c, data, status, timestamp, conversion); err != nil {
		return r.JSON(http.StatusBadGateway, map[string]string{"error": "appmetrica_delivery_failed"})
	}
	if prov.SendRevenue && status == prov.RevenueStatus {
		if err = p.sendRevenue(r.Request.Context(), c, data, timestamp, conversion); err != nil {
			return r.JSON(http.StatusBadGateway, map[string]string{"error": "appmetrica_revenue_delivery_failed"})
		}
	}
	return r.JSON(200, map[string]bool{"ok": true})
}

func (p *plugin) send(parent context.Context, c *Config, data clickData, event string, timestamp int64, conversion map[string]any) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	attributes := map[string]any{"clickData": data, "experiments": data.AnalyticsExperiments}
	if conversion != nil {
		attributes["conversion"] = conversion
	}
	b, err := json.Marshal(attributes)
	if err != nil {
		return err
	}
	q := url.Values{"post_api_key": {c.PostAPIKey}, "application_id": {strconv.FormatInt(data.ApplicationID, 10)}, "profile_id": {data.UserID}, "session_type": {"foreground"}, "event_name": {c.EventNames[event]}, "event_timestamp": {strconv.FormatInt(timestamp, 10)}, "event_json": {string(b)}}
	return p.deliver(ctx, appMetricaURL, q)
}

func (p *plugin) deliver(ctx context.Context, endpoint string, q url.Values) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("appmetrica: invalid request")
	}
	response, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("appmetrica: transport failure")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("appmetrica: HTTP %d", response.StatusCode)
	}
	return nil
}

var revenueAmount = regexp.MustCompile(`^(0|[1-9][0-9]{0,9})(\.[0-9]{1,8})?$`)
var revenueCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

func validateRevenue(conversion map[string]any) error {
	amount, _ := conversion["amount"].(string)
	currency, _ := conversion["currency"].(string)
	lead, _ := conversion["leadId"].(string)
	if !revenueAmount.MatchString(amount) || !revenueCurrency.MatchString(currency) || strings.TrimSpace(lead) == "" {
		return fmt.Errorf("Для Revenue нужны ID заявки, сумма дохода decimal(10,8) и код валюты из трёх заглавных букв")
	}
	return nil
}

func (p *plugin) sendRevenue(parent context.Context, c *Config, data clickData, timestamp int64, conversion map[string]any) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	payload, err := json.Marshal(map[string]any{"clickData": data, "conversion": conversion, "experiments": data.AnalyticsExperiments})
	if err != nil {
		return err
	}
	// Prevent AppMetrica's documented payload truncation.
	if len(payload) > 30*1024 {
		return fmt.Errorf("appmetrica: revenue payload too large")
	}
	q := url.Values{"post_api_key": {c.PostAPIKey}, "application_id": {strconv.FormatInt(data.ApplicationID, 10)}, "profile_id": {data.UserID}, "session_type": {"foreground"}, "event_timestamp": {strconv.FormatInt(timestamp, 10)}, "revenue_event_type": {"one_time_purchase"}, "price": {conversion["amount"].(string)}, "currency": {conversion["currency"].(string)}, "product_id": {data.LinkID}, "quantity": {"1"}, "payload": {string(payload)}}
	return p.deliver(ctx, "https://api.appmetrica.yandex.ru/logs/v1/import/revenue", q)
}
