package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/pocketbase/pocketbase/core"
)

const pushHost = "https://push.api.appmetrica.yandex.net"

type remoteError struct{ status int }

func (e *remoteError) Error() string { return fmt.Sprintf("AppMetrica HTTP %d", e.status) }
func (p *Plugin) request(ctx context.Context, c Config, method, path string, body, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, pushHost+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "OAuth "+c.OAuthToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return textError("AppMetrica недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &remoteError{resp.StatusCode}
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return textError("некорректный ответ AppMetrica")
	}
	return nil
}
func (p *Plugin) ensureGroup(ctx context.Context, c Config, d runDefinition, runID string) (int64, error) {
	name := "PocketBase " + d.Campaign.ID + " / " + runID
	var list struct {
		Groups []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"groups"`
	}
	if err := p.request(ctx, c, "GET", "/push/v1/management/groups?app_id="+strconv.FormatInt(d.ApplicationID, 10), nil, &list); err != nil {
		return 0, err
	}
	for _, g := range list.Groups {
		if g.Name == name {
			return g.ID, nil
		}
	}
	var result struct {
		Group struct {
			ID int64 `json:"id"`
		} `json:"group"`
	}
	err := p.request(ctx, c, "POST", "/push/v1/management/groups", map[string]any{"group": map[string]any{"app_id": d.ApplicationID, "name": name, "send_rate": d.SendRate}}, &result)
	if err == nil && result.Group.ID <= 0 {
		return 0, textError("AppMetrica не вернула ID группы")
	}
	return result.Group.ID, err
}
func (p *Plugin) sendBatch(ctx context.Context, c Config, d runDefinition, runID string, job *core.Record) (int64, error) {
	var j jobDefinition
	if err := decodeRecord(job, &j); err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(j.Recipients))
	for _, r := range j.Recipients {
		ids = append(ids, r.DeviceID)
	}
	payload := map[string]any{"type": d.Campaign.Message.Action, "pushRunId": runID, "pushToken": d.OpenToken}
	if d.Campaign.Message.Action == "route" {
		payload["url"] = d.Campaign.Message.Target
	}
	if d.Campaign.Message.Action == "partner" {
		payload["id"] = d.Campaign.Message.Target
	}
	data, _ := json.Marshal(payload)
	content := map[string]any{"title": d.Campaign.Message.Title, "text": d.Campaign.Message.Text, "data": string(data)}
	android := map[string]any{}
	ios := map[string]any{}
	for k, v := range content {
		android[k] = v
		ios[k] = v
	}
	if d.Campaign.Message.Image != "" {
		android["image"] = d.Campaign.Message.Image
		ios["mutable_content"] = 1
		ios["attachments"] = []any{map[string]any{"id": "image", "file_url": d.Campaign.Message.Image}}
	}
	clientID, err := strconv.ParseInt(job.GetString("clientId"), 10, 64)
	if err != nil {
		return 0, err
	}
	body := map[string]any{"push_batch_request": map[string]any{"group_id": d.GroupID, "client_transfer_id": clientID, "tag": runID, "batch": []any{map[string]any{"messages": map[string]any{"android": map[string]any{"silent": false, "content": android}, "iOS": map[string]any{"silent": false, "content": ios}}, "devices": []any{map[string]any{"id_type": "appmetrica_device_id", "id_values": ids}}}}}}
	var result struct {
		Response struct {
			ID int64 `json:"transfer_id"`
		} `json:"push_response"`
	}
	err = p.request(ctx, c, "POST", "/push/v1/send-batch", body, &result)
	if err == nil && result.Response.ID <= 0 {
		return 0, textError("AppMetrica не вернула ID отправки")
	}
	return result.Response.ID, err
}
func (p *Plugin) transferStatus(ctx context.Context, c Config, group int64, client string) (int64, string, error) {
	var result struct {
		Transfer struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"transfer"`
	}
	err := p.request(ctx, c, "GET", "/push/v1/status/"+strconv.FormatInt(group, 10)+"/"+client, nil, &result)
	return result.Transfer.ID, result.Transfer.Status, err
}
