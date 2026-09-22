package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

const keysCollection = "mcp_keys"

type internalKey struct{}

func save(app core.App, m core.Model) error {
	return app.SaveWithContext(context.WithValue(context.Background(), internalKey{}, true), m)
}
func digest(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }

type Key struct {
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name"`
	Tools       []string `json:"tools"`
	Collections []string `json:"collections"`
	ExpiresAt   string   `json:"expiresAt,omitempty"`
	Revoked     bool     `json:"revoked"`
	Created     string   `json:"created,omitempty"`
	Owner       string   `json:"-"`
}

func install(app core.App) error {
	existing, err := app.FindCollectionByNameOrId(keysCollection)
	if err == nil {
		f := existing.Fields.GetByName("hash")
		if !existing.System || !existing.IsBase() || f == nil || !f.GetHidden() {
			return fmt.Errorf("mcp: reserved collection collision")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	c := core.NewBaseCollection(keysCollection)
	c.System = true
	c.Fields.Add(&core.TextField{Name: "name", Required: true, Max: 100}, &core.TextField{Name: "hash", Hidden: true, Required: true}, &core.TextField{Name: "owner", Hidden: true, Required: true}, &core.JSONField{Name: "tools", MaxSize: 65536}, &core.JSONField{Name: "collections", MaxSize: 65536}, &core.DateField{Name: "expiresAt"}, &core.BoolField{Name: "revoked"}, &core.AutodateField{Name: "created", OnCreate: true})
	c.Indexes = []string{"CREATE UNIQUE INDEX idx_mcp_key_hash ON mcp_keys (hash)"}
	return save(app, c)
}

func (s *Server) CreateKey(in Key) (Key, string, error) {
	if in.Name == "" || len(in.Name) > 100 || len(in.Tools) == 0 {
		return Key{}, "", fmt.Errorf("Укажите название и инструменты")
	}
	available := map[string]bool{}
	for _, t := range s.Tools() {
		available[t.Name] = true
	}
	for _, name := range in.Tools {
		if !available[name] {
			return Key{}, "", fmt.Errorf("Неизвестный инструмент %s", name)
		}
	}
	for _, name := range in.Collections {
		if !slices.Contains(s.opts.ContentCollections, name) {
			return Key{}, "", fmt.Errorf("Коллекция не разрешена")
		}
	}
	if in.ExpiresAt != "" {
		at, err := time.Parse(time.RFC3339, in.ExpiresAt)
		if err != nil || !at.After(time.Now()) {
			return Key{}, "", fmt.Errorf("Срок действия должен быть в будущем")
		}
	}
	if _, err := s.app.FindRecordById(core.CollectionNameSuperusers, in.Owner); err != nil {
		return Key{}, "", fmt.Errorf("Владелец ключа недействителен")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return Key{}, "", err
	}
	token := "pmcp_" + base64.RawURLEncoding.EncodeToString(b)
	c, err := s.app.FindCollectionByNameOrId(keysCollection)
	if err != nil {
		return Key{}, "", err
	}
	r := core.NewRecord(c)
	r.Set("name", in.Name)
	r.Set("hash", digest(token))
	r.Set("owner", in.Owner)
	r.Set("tools", in.Tools)
	r.Set("collections", in.Collections)
	r.Set("expiresAt", in.ExpiresAt)
	if err = save(s.app, r); err != nil {
		return Key{}, "", fmt.Errorf("Не удалось сохранить ключ")
	}
	return keyFrom(r), token, nil
}

func keyFrom(r *core.Record) Key {
	k := Key{ID: r.Id, Name: r.GetString("name"), Owner: r.GetString("owner"), ExpiresAt: r.GetString("expiresAt"), Revoked: r.GetBool("revoked"), Created: r.GetString("created")}
	_ = json.Unmarshal([]byte(r.GetString("tools")), &k.Tools)
	_ = json.Unmarshal([]byte(r.GetString("collections")), &k.Collections)
	return k
}
func authenticate(app core.App, token string) (Key, error) {
	if len(token) != 48 {
		return Key{}, fmt.Errorf("invalid key")
	}
	r, err := app.FindFirstRecordByFilter(keysCollection, "hash={:hash}", dbx.Params{"hash": digest(token)})
	if err != nil {
		return Key{}, err
	}
	if r.GetBool("revoked") || (!r.GetDateTime("expiresAt").IsZero() && !r.GetDateTime("expiresAt").Time().After(time.Now())) {
		return Key{}, fmt.Errorf("expired key")
	}
	k := keyFrom(r)
	if _, err := app.FindRecordById(core.CollectionNameSuperusers, k.Owner); err != nil {
		return Key{}, err
	}
	return k, nil
}
func listKeys(app core.App) ([]Key, error) {
	rr, err := app.FindRecordsByFilter(keysCollection, "", "-created", 500, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Key, 0, len(rr))
	for _, r := range rr {
		out = append(out, keyFrom(r))
	}
	return out, nil
}
func Revoke(app core.App, id string) error {
	r, err := app.FindRecordById(keysCollection, id)
	if err != nil {
		return err
	}
	r.Set("revoked", true)
	return save(app, r)
}

func protect(app core.App) {
	guard := func(e *core.RecordEvent) error {
		if e.Record.Collection().Name == keysCollection && (e.Context == nil || e.Context.Value(internalKey{}) != true) {
			return fmt.Errorf("mcp: use key management API")
		}
		return e.Next()
	}
	app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{Id: "mcp", Func: guard})
	app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{Id: "mcp", Func: guard})
	app.OnRecordDelete().Bind(&hook.Handler[*core.RecordEvent]{Id: "mcp", Func: guard})
	app.OnRecordEnrich().Bind(&hook.Handler[*core.RecordEnrichEvent]{Id: "mcp", Func: func(e *core.RecordEnrichEvent) error {
		if e.Record.Collection().Name == keysCollection {
			return fmt.Errorf("mcp: use key management API")
		}
		return e.Next()
	}})
	collectionGuard := func(e *core.CollectionEvent) error {
		old, _ := e.App.FindCollectionByNameOrId(e.Collection.Id)
		if (e.Collection.Name == keysCollection || (old != nil && old.Name == keysCollection)) && (e.Context == nil || e.Context.Value(internalKey{}) != true) {
			return fmt.Errorf("mcp: protected schema")
		}
		return e.Next()
	}
	app.OnCollectionCreate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "mcp", Func: collectionGuard})
	app.OnCollectionUpdate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "mcp", Func: collectionGuard})
	app.OnCollectionDelete().Bind(&hook.Handler[*core.CollectionEvent]{Id: "mcp", Func: collectionGuard})
}
