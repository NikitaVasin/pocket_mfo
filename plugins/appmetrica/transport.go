package appmetrica

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

const PushHost = "https://push.api.appmetrica.yandex.net"
const EventsURL = "https://api.appmetrica.yandex.ru/logs/v1/import/events"
const RevenueURL = "https://api.appmetrica.yandex.ru/logs/v1/import/revenue"
const AnalyticsURL = "https://api.appmetrica.yandex.com/stat/v1/data"

type DeliveryHTTPError int

func (e DeliveryHTTPError) Error() string { return fmt.Sprintf("appmetrica: HTTP %d", int(e)) }
func Deliver(ctx context.Context, client *http.Client, endpoint string, q url.Values) error {
	if endpoint != EventsURL && endpoint != RevenueURL {
		return fmt.Errorf("AppMetrica: неизвестный адрес загрузки")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("appmetrica: invalid request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("appmetrica: transport failure")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != 200 {
		return DeliveryHTTPError(resp.StatusCode)
	}
	return nil
}
func ReadAnalytics(ctx context.Context, client *http.Client, token string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, AnalyticsURL+"?"+params.Encode(), nil)
	if err != nil {
		return fmt.Errorf("не удалось подготовить запрос AppMetrica")
	}
	req.Header.Set("Authorization", "OAuth "+token)
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("AppMetrica недоступна или истекло время ожидания")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		hint := "повторите позже"
		switch response.StatusCode {
		case 401, 403:
			hint = "проверьте права OAuth-токена на чтение статистики приложения"
		case 429:
			hint = "исчерпана квота API, повторите позже"
		}
		return fmt.Errorf("AppMetrica HTTP %d: %s", response.StatusCode, hint)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 || json.Unmarshal(body, out) != nil {
		return fmt.Errorf("некорректный ответ аналитики AppMetrica")
	}
	return nil
}

type RemoteError struct {
	Status  int
	Message string
}

func (e *RemoteError) Error() string {
	message := fmt.Sprintf("AppMetrica HTTP %d", e.Status)
	if e.Message != "" {
		message += ": " + e.Message
	}
	return message
}

// Only diagnostic fields are retained; response bodies and credentials are not stored.
func providerErrors(data []byte) string {
	var envelope struct {
		Errors  []json.RawMessage `json:"errors"`
		Message string            `json:"message"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return ""
	}
	messages := []string{}
	for _, raw := range envelope.Errors {
		var message string
		if json.Unmarshal(raw, &message) != nil {
			var detail struct {
				Message   string `json:"message"`
				ErrorType string `json:"error_type"`
			}
			if json.Unmarshal(raw, &detail) != nil {
				continue
			}
			message = detail.Message
			if detail.ErrorType != "" {
				message = detail.ErrorType + ": " + message
			}
		}
		if strings.TrimSpace(message) != "" {
			messages = append(messages, message)
		}
	}
	if len(messages) == 0 && envelope.Message != "" {
		messages = append(messages, envelope.Message)
	}
	return strings.Join(messages, "; ")
}
func Diagnostic(message string, secrets ...string) string {
	for _, value := range secrets {
		if value != "" {
			message = strings.ReplaceAll(message, value, "[скрыто]")
		}
	}
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, message)
	chars := []rune(strings.TrimSpace(message))
	if len(chars) > 2000 {
		return string(chars[:2000]) + "…"
	}
	return string(chars)
}
func PushRequest(ctx context.Context, client *http.Client, token, method, path string, body, out any, secrets ...string) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, PushHost+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "OAuth "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("AppMetrica недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		return &RemoteError{Status: resp.StatusCode, Message: Diagnostic(providerErrors(data), append(secrets, token)...)}
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("некорректный ответ AppMetrica")
	}
	return nil
}
