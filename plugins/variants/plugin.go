package variants

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/dbutils"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/security"
)

//go:embed ui/*
var assets embed.FS

// Register must be called before Bootstrap/Start. Hook IDs make registration idempotent.
func Register(app core.App) {
	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{Id: "variants", Func: func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return install(e.App)
	}})
	app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{Id: "variants", Func: recordSave})
	app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{Id: "variants", Func: recordSave})
	app.OnRecordDelete().Bind(&hook.Handler[*core.RecordEvent]{Id: "variants", Func: func(e *core.RecordEvent) error {
		if managedName(e.Record.Collection().Name) && !isInternal(e.Context) {
			return errInvalid("service records are managed by the plugin")
		}
		if !e.Record.Collection().IsAuth() {
			return e.Next()
		}
		return e.App.RunInTransaction(func(tx core.App) error {
			parent := e.App
			e.App = tx
			defer func() { e.App = parent }()
			for _, name := range []string{states, history} {
				if _, err := tx.DB().Delete(name, dbx.HashExp{"auth_collection": e.Record.Collection().Id, "user": e.Record.Id}).Execute(); err != nil {
					return err
				}
			}
			return e.Next()
		})
	}})
	app.OnRecordCreateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "variants", Func: protectRequest})
	app.OnRecordUpdateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "variants", Func: protectRequest})
	app.OnCollectionCreate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "variants", Func: func(e *core.CollectionEvent) error {
		if managedName(e.Collection.Name) && !isInternal(e.Context) {
			return errInvalid("reserved plugin collection name")
		}
		return e.Next()
	}})
	app.OnCollectionUpdate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "variants", Func: collectionUpdate})
	app.OnCollectionDelete().Bind(&hook.Handler[*core.CollectionEvent]{Id: "variants", Func: collectionDelete})
	app.OnRecordAuthRequest().Bind(&hook.Handler[*core.RecordAuthRequestEvent]{Id: "variants", Func: func(e *core.RecordAuthRequestEvent) error {
		if err := ensureUserBucket(e.App, e.Record); err != nil {
			return err
		}
		return e.Next()
	}})
	app.OnRecordsListRequest().Bind(&hook.Handler[*core.RecordsListRequestEvent]{Id: "variants", Func: func(e *core.RecordsListRequestEvent) error {
		if err := observeCollection(e.App, e.Auth, e.Collection.Id); err != nil {
			return err
		}
		return e.Next()
	}})
	app.OnRecordEnrich().Bind(&hook.Handler[*core.RecordEnrichEvent]{Id: "variants", Func: func(e *core.RecordEnrichEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		// Observe the final HTTP record tree once. Expanded records are visited by
		// their root so realtime expansions don't accidentally create HTTP history.
		if e.RequestInfo != nil && e.RequestInfo.Context != core.RequestInfoContextRealtime && e.RequestInfo.Context != core.RequestInfoContextExpand {
			return observeRecordTree(e.App, e.RequestInfo.Auth, e.Record)
		}
		return nil
	}})
	app.OnRealtimeMessageSend().Bind(&hook.Handler[*core.RealtimeMessageEvent]{Id: "variants", Func: func(e *core.RealtimeMessageEvent) error {
		if strings.HasPrefix(e.Message.Name, "PB_") {
			return e.Next()
		}
		user, _ := e.Client.Get(apis.RealtimeClientAuthKey).(*core.Record)
		var data struct {
			Record map[string]any `json:"record"`
		}
		if json.Unmarshal(e.Message.Data, &data) == nil {
			if err := e.Next(); err != nil {
				return err
			}
			// Topic remains available when fields= excludes collectionId.
			id := strings.Split(strings.Split(e.Message.Name, "?")[0], "/")[0]
			if col, err := e.App.FindCollectionByNameOrId(id); err == nil {
				if err := observeCollection(e.App, user, col.Id); err != nil {
					return err
				}
			}
			return observeJSONTree(e.App, user, data.Record)
		}
		return e.Next()
	}})
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: "variants", Func: func(e *core.ServeEvent) error {
		ui, err := fs.Sub(assets, "ui")
		if err != nil {
			return err
		}
		e.UIExtensions = append(e.UIExtensions, core.UIExtension{Name: "variants", FS: ui})
		e.Router.Bind(&hook.Handler[*core.RequestEvent]{Id: "variantsBucket", Priority: apis.DefaultLoadAuthTokenMiddlewarePriority + 1, Func: func(r *core.RequestEvent) error {
			if err := ensureUserBucket(r.App, r.Auth); err != nil {
				return err
			}
			return r.Next()
		}})
		bindRoutes(e)
		return e.Next()
	}})
}

func recordSave(e *core.RecordEvent) error {
	r := e.Record
	c := r.Collection()
	if managedName(c.Name) && !isInternal(e.Context) {
		return errInvalid("service records are managed by the plugin")
	}
	if c.IsAuth() && c.Fields.GetByName(BucketField) != nil {
		if r.IsNew() && r.Id == "" {
			r.Id = security.RandomString(15)
		}
		old := r.Original().GetInt(BucketField)
		if old > 0 {
			r.Set(BucketField, old)
		} else {
			r.Set(BucketField, Bucket(c.Id, r.Id))
		}
	}
	if c.Fields.GetByName(SetField) != nil && !managedName(c.Name) {
		cfg, err := Load(e.App, c.Id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if cfg != nil {
			if r.GetString(SetField) == "" {
				r.Set(SetField, setID(c.Id, "default", "", ""))
			}
			s, err := e.App.FindRecordById(sets, r.GetString(SetField))
			if err != nil || s.GetString("collection") != c.Id {
				return errInvalid("content_set must belong to this collection")
			}
		}
	}
	return e.Next()
}
func protectRequest(e *core.RecordRequestEvent) error {
	info, err := e.RequestInfo()
	if err != nil {
		return err
	}
	for k := range info.Body {
		if strings.Trim(k, "+-") == BucketField && e.Collection.Fields.GetByName(BucketField) != nil {
			if e.Record.IsNew() || e.Record.GetInt(BucketField) != e.Record.Original().GetInt(BucketField) {
				return e.BadRequestError("content_bucket is managed by the server", nil)
			}
		}
	}
	return e.Next()
}
func ensureUserBucket(app core.App, r *core.Record) error {
	if r == nil || r.IsSuperuser() || r.Collection().Fields.GetByName(BucketField) == nil || r.GetInt(BucketField) > 0 {
		return nil
	}
	return app.RunInTransaction(func(tx core.App) error {
		fresh, err := tx.FindRecordById(r.Collection().Id, r.Id)
		if err != nil {
			return err
		}
		if fresh.GetInt(BucketField) == 0 {
			if err := save(tx, fresh); err != nil {
				return err
			}
		}
		r.Set(BucketField, fresh.GetInt(BucketField))
		return nil
	})
}

func collectionUpdate(e *core.CollectionEvent) error {
	if isInternal(e.Context) {
		return e.Next()
	}
	parentApp := e.App
	defer func() { _ = parentApp.ReloadCachedCollections() }()
	oldCollection, oldErr := e.App.FindCollectionByNameOrId(e.Collection.Id)
	if oldErr != nil {
		return oldErr
	}
	if managedName(e.Collection.Name) || managedName(oldCollection.Name) {
		return errInvalid("service schema is managed by the plugin")
	}
	// During initial bootstrap the plugin collections may not exist yet.
	if _, err := e.App.FindCollectionByNameOrId(configs); errors.Is(err, sql.ErrNoRows) {
		return e.Next()
	} else if err != nil {
		return err
	}
	return e.App.RunInTransaction(func(tx core.App) error {
		parent := e.App
		e.App = tx
		defer func() { e.App = parent }()
		cs, err := allConfigs(tx)
		if err != nil {
			return err
		}
		personalized := map[string]bool{}
		for _, c := range cs {
			personalized[c.Collection] = true
		}
		for _, c := range cs {
			if c.AuthCollection == e.Collection.Id {
				if e.Collection.Fields.GetByName(BucketField) == nil {
					return errInvalid("cannot remove content_bucket")
				}
				if err := ensureBucket(tx, e.Collection); err != nil {
					return err
				}
			}
			if c.Collection == e.Collection.Id {
				before := oldCollection
				a := e.Collection.Fields.GetByName(SetField)
				b := before.Fields.GetByName(SetField)
				aj, _ := json.Marshal(a)
				bj, _ := json.Marshal(b)
				if a == nil || string(aj) != string(bj) {
					return errInvalid("content_set is managed by the plugin")
				}
				index := dbutils.ParseIndex(e.Collection.GetIndex(setIndexName(c.Collection)))
				if !index.IsValid() || index.Unique || index.Where != "" || len(index.Columns) != 1 ||
					index.Columns[0].Name != SetField || (index.Columns[0].Collate != "" && !strings.EqualFold(index.Columns[0].Collate, "binary")) {
					return errInvalid("content_set index is managed by the plugin")
				}
				if !sameRule(e.Collection.ListRule, before.ListRule) || !sameRule(e.Collection.ViewRule, before.ViewRule) {
					return errInvalid("edit original API rules in the Variants tab")
				}
				if e.Collection.CreateRule != nil || e.Collection.UpdateRule != nil || e.Collection.DeleteRule != nil {
					return errInvalid("personalized content is managed by superusers")
				}
			}
		}
		if err := e.Next(); err != nil {
			return err
		}
		// Schema changes rebuild validated views transactionally; data changes need no hooks.
		for _, c := range cs {
			a, err := tx.FindCollectionByNameOrId(c.AuthCollection)
			if err != nil {
				return err
			}
			query, err := compileConfig(&compiler{app: tx, personalized: personalized}, c, a)
			if err != nil {
				return err
			}
			if err := saveView(tx, c, query); err != nil {
				return err
			}
			if err := validateRules(tx, c); err != nil {
				return err
			}
		}
		return nil
	})
}
func sameRule(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
func collectionDelete(e *core.CollectionEvent) error {
	if isInternal(e.Context) {
		return e.Next()
	}
	if managedName(e.Collection.Name) {
		return errInvalid("cannot delete plugin service collections")
	}
	if _, err := e.App.FindCollectionByNameOrId(configs); errors.Is(err, sql.ErrNoRows) {
		return e.Next()
	}
	cs, err := allConfigs(e.App)
	if err != nil {
		return err
	}
	for _, c := range cs {
		if c.Collection == e.Collection.Id || c.AuthCollection == e.Collection.Id {
			return errInvalid("collection is used by variants")
		}
	}
	return e.App.RunInTransaction(func(tx core.App) error {
		parent := e.App
		e.App = tx
		defer func() { e.App = parent }()
		if err := e.Next(); err != nil {
			return err
		}
		p := map[string]bool{}
		for _, c := range cs {
			p[c.Collection] = true
		}
		for _, c := range cs {
			a, err := tx.FindCollectionByNameOrId(c.AuthCollection)
			if err != nil {
				return err
			}
			if _, err = compileConfig(&compiler{app: tx, personalized: p}, c, a); err != nil {
				return err
			}
		}
		return nil
	})
}
