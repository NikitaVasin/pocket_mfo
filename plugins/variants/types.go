// Package variants adds audience-specific content sets and bucket experiments to PocketBase.
package variants

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	SetField    = "content_set"
	BucketField = "content_bucket"
	configs     = "pv_configs"
	sets        = "pv_sets"
	states      = "pv_states"
	history     = "pv_history"
)

// Condition is either an all/any group, an exists relation scope, or a typed field comparison.
// Relation accepts a forward relation name or PocketBase's collection_via_field notation.
// All children of exists are evaluated against the SAME related record.
type Condition struct {
	Kind     string      `json:"kind"`
	Field    string      `json:"field,omitempty"`
	Op       string      `json:"op,omitempty"`
	Value    any         `json:"value,omitempty"`
	Relation string      `json:"relation,omitempty"`
	Children []Condition `json:"children,omitempty"`
}

type Group struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	From int    `json:"from"`
	To   int    `json:"to"`
}
type Experiment struct {
	Key    string  `json:"key"`
	Name   string  `json:"name"`
	Active bool    `json:"active"`
	Groups []Group `json:"groups"`
}
type Variant struct {
	Key         string       `json:"key"`
	Name        string       `json:"name"`
	Condition   *Condition   `json:"condition,omitempty"`
	Experiments []Experiment `json:"experiments,omitempty"`
}
type Config struct {
	Collection     string `json:"collection"`
	AuthCollection string `json:"authCollection"`
	Variables      bool   `json:"variables"`
	Experiments    bool   `json:"experiments"`
	Version        int    `json:"version"`
	// Default has no audience condition and always uses key "default".
	Default  Variant   `json:"default"`
	Variants []Variant `json:"variants"`
	ListRule *string   `json:"listRule"`
	ViewRule *string   `json:"viewRule"`
}
type Decision struct {
	Collection string `json:"collection"`
	Variant    string `json:"variant"`
	Experiment string `json:"experiment,omitempty"`
	Group      string `json:"group,omitempty"`
	Set        string `json:"set"`
	Version    int    `json:"version"`
	Bucket     int    `json:"bucket"`
	Reason     string `json:"reason"`
}

type internalKey struct{}

func internal(ctx context.Context) context.Context {
	return context.WithValue(ctx, internalKey{}, true)
}
func isInternal(ctx context.Context) bool { return ctx != nil && ctx.Value(internalKey{}) == true }
func save(app core.App, m core.Model) error {
	return app.SaveWithContext(internal(context.Background()), m)
}
func digest(parts ...string) string {
	s := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(s[:])
}
func setID(collection, variant, experiment, group string) string {
	return digest(collection, variant, experiment, group)[:15]
}
func viewName(collection string) string { return "pv_choice_" + digest(collection)[:16] }

// Bucket is stable across processes and experiments; it is not an authorization credential.
func Bucket(authCollection, user string) int {
	s := sha256.Sum256([]byte(authCollection + "\x00" + user))
	return int(binary.BigEndian.Uint64(s[:8])%10000) + 1
}
func sqlString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func ident(s string) string     { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func decode(r *core.Record) (*Config, error) {
	var c Config
	err := json.Unmarshal([]byte(r.GetString("definition")), &c)
	return &c, err
}
func allConfigs(app core.App) ([]*Config, error) {
	rr, err := app.FindAllRecords(configs)
	if err != nil {
		return nil, err
	}
	result := make([]*Config, 0, len(rr))
	for _, r := range rr {
		c, err := decode(r)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}
func Load(app core.App, collection string) (*Config, error) {
	c, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		return nil, err
	}
	r, err := app.FindRecordById(configs, digest(c.Id)[:15])
	if err != nil {
		return nil, err
	}
	return decode(r)
}
func managedName(name string) bool {
	return name == configs || name == sets || name == states || name == history || strings.HasPrefix(name, "pv_choice_")
}
func wrapRule(original *string, guard string) *string {
	if original == nil {
		return nil
	}
	s := guard
	if *original != "" {
		s = "(" + *original + ") && (" + guard + ")"
	}
	return &s
}
func errInvalid(format string, args ...any) error { return fmt.Errorf("variants: "+format, args...) }
