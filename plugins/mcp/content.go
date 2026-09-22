package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

type contentProvider struct{ s *Server }
type contentInput struct {
	Collection string         `json:"collection"`
	ID         string         `json:"id,omitempty"`
	Page       int            `json:"page,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

func (p contentProvider) MCPTools() []Tool {
	text := map[string]any{"type": "string"}
	result := []Tool{}
	for _, operation := range []string{"schema", "list", "get", "create", "update", "delete"} {
		props := map[string]any{"collection": text}
		required := []string{"collection"}
		if operation == "get" || operation == "update" || operation == "delete" {
			props["id"] = text
			required = append(required, "id")
		}
		if operation == "list" {
			props["page"] = map[string]any{"type": "integer", "minimum": 1}
		}
		if operation == "create" || operation == "update" {
			props["data"] = map[string]any{"type": "object"}
			required = append(required, "data")
		}
		result = append(result, Tool{Name: "content_" + operation, Description: "Контент: " + operation + ". Только явно разрешённые коллекции. Сначала получите schema. Системные таблицы, пользователи, скрытые поля и схема недоступны.", InputSchema: Object(props, required...), ReadOnly: operation == "schema" || operation == "get" || operation == "list", Handle: func(ctx context.Context, call Call) (any, error) { return p.call(ctx, call, operation) }})
	}
	return result
}

func (p contentProvider) call(ctx context.Context, call Call, operation string) (any, error) {
	var in contentInput
	if err := Decode(bytes.NewReader(call.Arguments), &in); err != nil {
		return nil, fmt.Errorf("Некорректные параметры")
	}
	if !slices.Contains(p.s.opts.ContentCollections, in.Collection) || !slices.Contains(call.Key.Collections, in.Collection) {
		return nil, fmt.Errorf("Коллекция не разрешена этому ключу")
	}
	c, err := call.App.FindCollectionByNameOrId(in.Collection)
	if err != nil || !c.IsBase() || c.System {
		return nil, fmt.Errorf("Недоступная коллекция контента")
	}
	fields := []any{}
	names := []string{"collectionId", "collectionName"}
	for _, f := range c.Fields {
		if f.GetHidden() || f.Type() == core.FieldTypePassword {
			continue
		}
		fields = append(fields, f)
		names = append(names, f.GetName())
	}
	if operation == "schema" {
		return map[string]any{"collection": c.Name, "fields": fields}, nil
	}
	for name := range in.Data {
		f := c.Fields.GetByName(name)
		if f == nil || f.GetHidden() || f.Type() == core.FieldTypePassword || f.Type() == core.FieldTypeAutodate || (f.GetSystem() && name != "id") || (name == "id" && operation != "create") {
			return nil, fmt.Errorf("Поле %s недоступно", name)
		}
	}
	owner, err := call.App.FindRecordById(core.CollectionNameSuperusers, call.Key.Owner)
	if err != nil {
		return nil, fmt.Errorf("Владелец ключа недоступен")
	}
	token, err := owner.NewAuthToken()
	if err != nil {
		return nil, fmt.Errorf("Ошибка авторизации")
	}
	// Route through PocketBase's actual Records API so request hooks, validation,
	// singleton and variants restrictions are identical to the admin UI.
	router, err := apis.NewRouter(call.App)
	if err != nil {
		return nil, err
	}
	handler, err := router.BuildMux()
	if err != nil {
		return nil, err
	}
	path := "/api/collections/" + url.PathEscape(c.Id) + "/records"
	method := "GET"
	switch operation {
	case "create":
		method = "POST"
	case "update":
		method = "PATCH"
	case "delete":
		method = "DELETE"
	}
	if operation == "get" || operation == "update" || operation == "delete" {
		if !validRecordID(in.ID) {
			return nil, fmt.Errorf("Некорректный ID")
		}
		path += "/" + in.ID
	}
	if operation == "list" {
		path += "?perPage=50&page=" + strconv.Itoa(max(1, in.Page))
	}
	body, _ := json.Marshal(in.Data)
	req := httptest.NewRequest(method, path, bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code >= 400 {
		return nil, fmt.Errorf("PocketBase отклонил операцию (%d): проверьте поля и ограничения коллекции", w.Code)
	}
	if w.Code == 204 {
		return map[string]any{"deleted": in.ID}, nil
	}
	var value map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		return nil, fmt.Errorf("Некорректный ответ PocketBase")
	}
	strip := func(record map[string]any) {
		for name := range record {
			if !slices.Contains(names, name) {
				delete(record, name)
			}
		}
	}
	if operation == "list" {
		if items, ok := value["items"].([]any); ok {
			for _, item := range items {
				if record, ok := item.(map[string]any); ok {
					strip(record)
				}
			}
		}
	} else {
		strip(value)
	}
	return value, nil
}
func validRecordID(id string) bool {
	if len(id) != 15 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
