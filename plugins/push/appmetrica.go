package push

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/NikitaVasin/pocket_mfo/plugins/appmetrica"
	"github.com/pocketbase/pocketbase/core"
)

type remoteError = appmetrica.RemoteError

var diagnostic = appmetrica.Diagnostic

func (p *Plugin) request(ctx context.Context, c Config, method, path string, body, out any, secrets ...string) error {
	return appmetrica.PushRequest(ctx, p.client, c.OAuthToken, method, path, body, out, secrets...)
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
	idsByPlatform := map[string][]string{}
	for _, r := range j.Recipients {
		idsByPlatform[r.Platform] = append(idsByPlatform[r.Platform], r.DeviceID)
	}
	batch := []any{}
	for _, platform := range []string{"android", "ios"} {
		ids := idsByPlatform[platform]
		if len(ids) == 0 {
			continue
		}
		key, content := "android", android
		if platform == "ios" {
			key, content = "iOS", ios
		}
		batch = append(batch, map[string]any{
			"messages": map[string]any{key: map[string]any{"silent": false, "content": content}},
			"devices":  []any{map[string]any{"id_type": "appmetrica_device_id", "id_values": ids}},
		})
	}
	body := map[string]any{"push_batch_request": map[string]any{"group_id": d.GroupID, "client_transfer_id": clientID, "tag": runID, "batch": batch}}
	var result struct {
		Response struct {
			ID int64 `json:"transfer_id"`
		} `json:"push_response"`
	}
	err = p.request(ctx, c, "POST", "/push/v1/send-batch", body, &result, d.OpenToken)
	if err == nil && result.Response.ID <= 0 {
		return 0, textError("AppMetrica не вернула ID отправки")
	}
	return result.Response.ID, err
}
func (p *Plugin) transferStatus(ctx context.Context, c Config, group int64, client, openToken string) (int64, string, string, error) {
	var result struct {
		Transfer struct {
			ID     int64    `json:"id"`
			Status string   `json:"status"`
			Errors []string `json:"errors"`
		} `json:"transfer"`
	}
	err := p.request(ctx, c, "GET", "/push/v1/status/"+strconv.FormatInt(group, 10)+"/"+client, nil, &result)
	return result.Transfer.ID, result.Transfer.Status, diagnostic(strings.Join(result.Transfer.Errors, "; "), c.OAuthToken, openToken), err
}
