package appmetrica

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

type Application struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type ConnectInput struct {
	Version       int    `json:"version"`
	OAuthToken    string `json:"oauthToken"`
	ApplicationID int64  `json:"applicationId,omitempty"`
}
type ConnectResult struct {
	Applications []Application `json:"applications,omitempty"`
	Settings     any           `json:"settings,omitempty"`
}

type applicationCredentials struct {
	ID         int64  `json:"id"`
	SDKAPIKey  string `json:"api_key128"`
	PostAPIKey string `json:"import_token"`
}

func (p *Plugin) readApplication(ctx context.Context, token string, id int64) (applicationCredentials, error) {
	var response struct {
		Application applicationCredentials `json:"application"`
	}
	if err := p.management(ctx, token, "application/"+strconv.FormatInt(id, 10), &response); err != nil {
		return applicationCredentials{}, err
	}
	if response.Application.ID != id {
		return applicationCredentials{}, fmt.Errorf("AppMetrica вернула другое приложение")
	}
	return response.Application, nil
}

func (p *Plugin) management(ctx context.Context, token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.appmetrica.yandex.ru/management/v1/"+path, nil)
	if err != nil {
		return fmt.Errorf("Не удалось подготовить запрос AppMetrica")
	}
	req.Header.Set("Authorization", "OAuth "+token)
	response, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("AppMetrica недоступна; повторите подключение")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("AppMetrica не разрешила чтение настроек (HTTP %d). Проверьте права OAuth-токена", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 || json.Unmarshal(body, out) != nil {
		return fmt.Errorf("Некорректный ответ AppMetrica")
	}
	return nil
}

// Connect discovers application credentials using OAuth and saves them only
// after a complete response, version check and application identity guards.
// Multiple applications require an explicit choice; no credentials are saved
// during discovery. All external requests are read-only.
func (p *Plugin) Connect(parent context.Context, app core.App, in ConnectInput) (ConnectResult, error) {
	out := ConnectResult{}
	old, err := Load(app)
	if err != nil {
		return out, err
	}
	if old.Version != in.Version {
		return out, fmt.Errorf("Настройки изменились; обновите страницу")
	}
	if in.OAuthToken == "" {
		in.OAuthToken = old.OAuthToken
	}
	if in.OAuthToken == "" {
		return out, fmt.Errorf("Укажите OAuth-токен")
	}
	candidate := old
	candidate.OAuthToken = in.OAuthToken
	if err = validate(candidate); err != nil {
		return out, err
	}
	if m := p.options.Managed; m != nil && m.OAuthToken != nil && candidate.OAuthToken != old.OAuthToken {
		return out, fmt.Errorf("OAuth-токен задан кодом проекта")
	}
	id := in.ApplicationID
	if id == 0 {
		id = old.ApplicationID
	}
	if id < 0 {
		return out, fmt.Errorf("Некорректное приложение")
	}
	if m := p.options.Managed; m != nil && m.ApplicationID != nil && id != old.ApplicationID {
		return out, fmt.Errorf("Приложение задано кодом проекта")
	}
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	if id == 0 {
		var list struct {
			Applications []Application `json:"applications"`
		}
		if err = p.management(ctx, in.OAuthToken, "applications", &list); err != nil {
			return out, err
		}
		if len(list.Applications) == 0 {
			return out, fmt.Errorf("У токена нет доступных приложений AppMetrica")
		}
		for _, application := range list.Applications {
			if application.ID <= 0 {
				return out, fmt.Errorf("AppMetrica вернула некорректный список приложений")
			}
		}
		if len(list.Applications) > 1 {
			out.Applications = list.Applications
			return out, nil
		}
		id = list.Applications[0].ID
	}
	application, err := p.readApplication(ctx, in.OAuthToken, id)
	if err != nil {
		return out, err
	}
	if application.SDKAPIKey == "" || application.PostAPIKey == "" {
		return out, fmt.Errorf("AppMetrica не вернула ключи приложения. Нужен OAuth-токен аккаунта с правом чтения настроек и API-ключей")
	}
	candidate.ApplicationID = id
	candidate.SDKAPIKey = application.SDKAPIKey
	candidate.PostAPIKey = application.PostAPIKey
	err = app.RunInTransaction(func(tx core.App) error {
		current, err := Load(tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, old) {
			return fmt.Errorf("Настройки изменились во время подключения; повторите запрос")
		}
		if reflect.DeepEqual(candidate, current) {
			out.Settings = adminSettings(tx, current)
			return nil
		}
		saved, err := Configure(tx, candidate)
		if err != nil {
			return err
		}
		out.Settings = adminSettings(tx, saved)
		return nil
	})
	return out, err
}
