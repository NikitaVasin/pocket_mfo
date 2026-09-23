package partnerlinks

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase/core"
)

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
	var input struct {
		ExposureTokens []string `json:"exposureTokens"`
	}
	if err := decodeBody(r, &input); err != nil && err != io.EOF {
		return r.BadRequestError("Запрос не должен содержать profileId или device", nil)
	}
	exposures, err := variants.VerifyExposures(r.Auth, input.ExposureTokens)
	if err != nil {
		return r.BadRequestError("Некорректный контекст показанного контента", nil)
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

	var data clickData
	var link dynamiclink.Value
	err = r.App.RunInTransaction(func(tx core.App) error {
		now := time.Now().Unix()
		id := make([]byte, 16)
		if _, err = rand.Read(id); err != nil {
			return err
		}
		data = clickData{ClickID: base64.RawURLEncoding.EncodeToString(id), UserID: r.Auth.Id, AuthCollection: r.Auth.Collection().Id, ApplicationID: c.ApplicationID, LinkID: record.Id, ProviderID: prov.ID, Experiments: []variants.Decision{}, IssuedAt: now, OpenUntil: now + c.OpenTTLSeconds}
		data.Experiments, err = variants.ResolveAll(tx, r.Auth)
		if err != nil {
			return r.InternalServerError("Не удалось определить эксперименты", nil)
		}
		data.AnalyticsExperiments, err = variants.AnalyticsExperiments(tx, data.Experiments)
		if err != nil {
			return r.InternalServerError("Не удалось подготовить эксперименты", nil)
		}
		currentDecisions := data.Experiments
		data.AttributionBasis = "current_assignment"
		if len(exposures) > 0 {
			data.Exposures = exposures
			data.AttributionBasis = "content_exposure"
			// Attribute only explicitly supplied shown content, not unobserved tests.
			data.Experiments = []variants.Decision{}
			data.AnalyticsExperiments = map[string]string{}
			seen := map[string]bool{}
			for _, exposure := range exposures {
				if exposure.Collection == dynamiclink.SettingsCollection {
					// Opening behavior is executed now. A stale screen context must
					// not label a different runtime policy as the old experiment.
					current, err := tx.FindRecordById(exposure.Decision.Collection, exposure.Record)
					if err != nil {
						return r.Error(409, "Обновите настройки открытия и контекст экрана", nil)
					}
					revision, err := variants.ContentRevision(current)
					if err != nil {
						return err
					}
					matches := false
					for _, decision := range currentDecisions {
						if decision.Collection == exposure.Decision.Collection && decision.Set == exposure.Decision.Set && decision.Version == exposure.Decision.Version {
							matches = true
						}
					}
					if !matches || revision != exposure.Revision {
						return r.Error(409, "Обновите настройки открытия и контекст экрана", nil)
					}
				}
				if !seen[exposure.Decision.Collection] {
					data.Experiments = append(data.Experiments, exposure.Decision)
					seen[exposure.Decision.Collection] = true
				}
				for key, value := range exposure.Experiments {
					data.AnalyticsExperiments[key] = value
				}
			}
		}
		if p.options.Attribution != nil {
			data.Push, err = p.options.Attribution(tx, r.Auth, record.Id)
			if err != nil {
				return r.InternalServerError("Не удалось определить источник перехода", nil)
			}
		}
		token, err := newToken()
		if err != nil {
			return r.BadRequestError(err.Error(), nil)
		}
		if prov.MaxTokenLength > 0 && len(token) > prov.MaxTokenLength {
			return r.BadRequestError("clickData превышает лимит провайдера", nil)
		}
		link, err = opening(record)
		if err != nil {
			return r.BadRequestError("Некорректная партнёрская ссылка", nil)
		}
		if _, err = renderURL(prov, link.URL, token); err != nil {
			return r.BadRequestError(err.Error(), nil)
		}
		link, err = dynamiclink.Apply(tx, r.Auth, link)
		if err != nil {
			return r.InternalServerError("Не удалось применить настройки Dynamic Link", nil)
		}
		if err = createConversation(tx, data, token); err != nil {
			return r.Error(503, "Не удалось сохранить конверсию", nil)
		}
		link.URL = c.BaseURL + "/api/partnerlinks/r/" + token
		return nil
	})
	if err != nil {
		return err
	}
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
