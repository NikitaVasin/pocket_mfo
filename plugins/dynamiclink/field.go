// Package dynamiclink adds a reusable, validated URL and opening-options field.
package dynamiclink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const Type = "dynamicLink"
const MaxSize = 16384

type Field struct{ core.JSONField }

func init()                                  { core.Fields[Type] = func() core.Field { return &Field{} } }
func (f *Field) Type() string                { return Type }
func (f *Field) CalculateMaxBodySize() int64 { return MaxSize }

// Decode validates a field value and fills omitted opening options. A null value
// represents an empty optional field; Required is checked by Field.ValidateValue.
func Decode(raw []byte) (*Value, error) {
	if len(raw) > MaxSize {
		return nil, fmt.Errorf("dynamicLink: value exceeds 16384 bytes")
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("dynamicLink: expected an object")
	}
	for key, value := range obj {
		if key != "warningDialog" && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("dynamicLink: %s cannot be null", key)
		}
	}
	v := Value{Mode: "appView", SaveCooke: true, ShowLoader: true}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&v); err != nil {
		return nil, fmt.Errorf("dynamicLink: invalid or unknown parameter")
	}
	u, err := url.Parse(v.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(v.URL, "\r\n\t") {
		return nil, fmt.Errorf("dynamicLink: URL must be HTTP(S) without credentials")
	}
	if v.Mode != "appView" && v.Mode != "view" && v.Mode != "browser" {
		return nil, fmt.Errorf("dynamicLink: mode must be appView, view or browser")
	}
	if v.WarningDialog != nil && (strings.TrimSpace(v.WarningDialog.Title) == "" || strings.TrimSpace(v.WarningDialog.Content) == "") {
		return nil, fmt.Errorf("dynamicLink: warning requires title and content")
	}
	return &v, nil
}

func (f *Field) PrepareValue(r *core.Record, raw any) (any, error) {
	prepared, err := f.JSONField.PrepareValue(r, raw)
	if err != nil {
		return prepared, err
	}
	data, ok := prepared.(types.JSONRaw)
	if !ok {
		return prepared, nil
	}
	value, err := Decode(data)
	// Keep invalid input intact so normal field validation reports it.
	if err != nil || value == nil {
		return prepared, nil
	}
	return types.ParseJSONRaw(value)
}
func (f *Field) ValidateSettings(ctx context.Context, app core.App, c *core.Collection) error {
	if err := f.JSONField.ValidateSettings(ctx, app, c); err != nil {
		return err
	}
	if f.MaxSize != 0 && f.MaxSize != MaxSize {
		return fmt.Errorf("dynamicLink: maxSize is fixed at 16384 bytes")
	}
	return nil
}
func (f *Field) ValidateValue(ctx context.Context, app core.App, r *core.Record) error {
	if err := f.JSONField.ValidateValue(ctx, app, r); err != nil {
		return err
	}
	value, err := Decode(r.GetRaw(f.Name).(types.JSONRaw))
	if err != nil {
		return err
	}
	if value == nil && f.Required {
		return fmt.Errorf("dynamicLink: URL is required")
	}
	return nil
}
