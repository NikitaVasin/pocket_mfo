// Package singleton limits ordinary collections to one record, or one per Variants content set.
package singleton

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/dbutils"
	"github.com/pocketbase/pocketbase/tools/hook"
)

const configs = "ps_configs"
const setField = "content_set"

//go:embed ui/*
var assets embed.FS

type internalKey struct{}

func internal(ctx context.Context) bool { return ctx != nil && ctx.Value(internalKey{}) == true }
func save(app core.App, model core.Model) error {
	return app.SaveWithContext(context.WithValue(context.Background(), internalKey{}, true), model)
}
func key(id string) string       { return fmt.Sprintf("%x", sha256.Sum256([]byte(id)))[:15] }
func indexName(id string) string { return "idx_ps_" + key(id) }
func ident(s string) string      { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// Config is stored independently of the target collection's API rules.
type Config struct {
	Collection string `json:"collection"`
	Enabled    bool   `json:"enabled"`
}

type Conflict struct {
	Set   string `json:"set" db:"set"`
	Name  string `json:"name"`
	Count int    `json:"count" db:"count"`
}

// ConflictError identifies every content set that prevents enabling Singleton.
type ConflictError struct {
	Conflicts []Conflict `json:"conflicts"`
}

func (e *ConflictError) Error() string {
	return "Нельзя включить Singleton: в коллекции или отдельных наборах больше одной записи."
}

// Register is idempotent and must run before Bootstrap/Start.
func Register(app core.App) {
	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{Id: "singleton", Func: func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return install(e.App)
	}})
	app.OnCollectionCreate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "singleton", Func: protectCollection})
	app.OnCollectionUpdate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "singleton", Func: protectCollection})
	app.OnCollectionDelete().Bind(&hook.Handler[*core.CollectionEvent]{Id: "singleton", Func: func(e *core.CollectionEvent) error {
		if e.Collection.Name == configs {
			return fmt.Errorf("singleton: service collection cannot be deleted")
		}
		return e.App.RunInTransaction(func(tx core.App) error {
			parent := e.App
			e.App = tx
			defer func() { e.App = parent }()
			if _, err := tx.FindCollectionByNameOrId(configs); err == nil {
				if _, err = tx.DB().Delete(configs, dbx.HashExp{"collection": e.Collection.Id}).Execute(); err != nil {
					return err
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			return e.Next()
		})
	}})
	protectRecord := func(e *core.RecordEvent) error {
		if e.Record.Collection().Name == configs && !internal(e.Context) {
			return fmt.Errorf("singleton: service records are managed by the plugin")
		}
		return e.Next()
	}
	app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{Id: "singleton", Func: protectRecord})
	app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{Id: "singleton", Func: protectRecord})
	app.OnRecordDelete().Bind(&hook.Handler[*core.RecordEvent]{Id: "singleton", Func: protectRecord})
	constraintError := func(e *core.RecordEvent) error {
		err := e.Next()
		if err != nil && (strings.Contains(err.Error(), indexName(e.Record.Collection().Id)) || strings.Contains(err.Error(), e.Record.Collection().Name+"."+setField)) {
			cfg, loadErr := Load(e.App, e.Record.Collection().Id)
			if loadErr == nil && cfg.Enabled {
				return validation.Errors{"singleton": validation.NewError("singleton_conflict", "В этом наборе уже есть запись. Обновите форму, чтобы открыть её.")}
			}
		}
		return err
	}
	app.OnRecordCreateExecute().Bind(&hook.Handler[*core.RecordEvent]{Id: "singleton", Func: constraintError})
	app.OnRecordUpdateExecute().Bind(&hook.Handler[*core.RecordEvent]{Id: "singleton", Func: constraintError})
	// Load after Variants' UI so wrappers compose in either Go registration order.
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: "singleton", Priority: 50, Func: func(e *core.ServeEvent) error {
		ui, err := fs.Sub(assets, "ui")
		if err != nil {
			return err
		}
		e.UIExtensions = append(e.UIExtensions, core.UIExtension{Name: "singleton", FS: ui})
		e.Router.GET("/api/singleton/admin/collections/{collection}", func(r *core.RequestEvent) error {
			cfg, err := Load(r.App, r.Request.PathValue("collection"))
			if err != nil {
				return r.BadRequestError("Не удалось загрузить Singleton.", err)
			}
			return r.JSON(http.StatusOK, cfg)
		}).Bind(apis.RequireSuperuserAuth())
		e.Router.PUT("/api/singleton/admin/collections/{collection}", func(r *core.RequestEvent) error {
			var body struct {
				Enabled *bool `json:"enabled"`
			}
			if err := r.BindBody(&body); err != nil || body.Enabled == nil {
				return r.BadRequestError("Укажите enabled: true или false.", err)
			}
			cfg, err := Configure(r.App, Config{Collection: r.Request.PathValue("collection"), Enabled: *body.Enabled})
			var conflict *ConflictError
			if errors.As(err, &conflict) {
				return r.JSON(http.StatusBadRequest, map[string]any{"status": 400, "message": err.Error(), "data": conflict})
			}
			if err != nil {
				return r.BadRequestError("Не удалось изменить Singleton.", err)
			}
			return r.JSON(http.StatusOK, cfg)
		}).Bind(apis.RequireSuperuserAuth())
		return e.Next()
	}})
}

func install(app core.App) error {
	if c, err := app.FindCollectionByNameOrId(configs); err == nil {
		if !c.System || !c.IsBase() || c.Fields.GetByName("collection") == nil || c.Fields.GetByName("enabled") == nil {
			return fmt.Errorf("singleton: reserved collection name %s", configs)
		}
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	c := core.NewBaseCollection(configs)
	c.System = true
	c.Fields.Add(&core.TextField{Name: "collection", Required: true}, &core.BoolField{Name: "enabled"})
	c.AddIndex("idx_ps_configs_collection", true, "collection", "")
	return save(app, c)
}

// Load returns Enabled=false for collections that have never enabled Singleton.
func Load(app core.App, collection string) (*Config, error) {
	c, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		return nil, err
	}
	cfg := &Config{Collection: c.Id}
	if _, err := app.FindCollectionByNameOrId(configs); errors.Is(err, sql.ErrNoRows) {
		return cfg, nil
	} else if err != nil {
		return nil, err
	}
	r, err := app.FindRecordById(configs, key(c.Id))
	if errors.Is(err, sql.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	cfg.Enabled = r.GetBool("enabled")
	return cfg, nil
}

func hasVariants(app core.App, c *core.Collection) bool {
	f, ok := c.Fields.GetByName(setField).(*core.RelationField)
	if !ok {
		return false
	}
	target, err := app.FindCollectionByNameOrId(f.CollectionId)
	return err == nil && target.Name == "pv_sets" && target.System && f.MaxSelect == 1
}

func setIndex(app core.App, c *core.Collection) {
	expr := "(1)"
	if hasVariants(app, c) {
		expr = setField
	}
	c.AddIndex(indexName(c.Id), true, expr, "")
}

// Configure atomically changes the setting and its database constraint. Existing data is never removed.
func Configure(app core.App, input Config) (*Config, error) {
	defer func() { _ = app.ReloadCachedCollections() }()
	var result *Config
	err := app.RunInTransaction(func(tx core.App) error {
		if err := install(tx); err != nil {
			return err
		}
		c, err := tx.FindCollectionByNameOrId(input.Collection)
		if err != nil {
			return err
		}
		if !c.IsBase() || c.System {
			return fmt.Errorf("singleton: only ordinary base collections are supported")
		}
		input.Collection = c.Id
		if input.Enabled {
			expr := "''"
			if hasVariants(tx, c) {
				expr = ident(setField)
			}
			var conflicts []Conflict
			if err := tx.DB().NewQuery("SELECT " + expr + " AS `set`, count(*) AS `count` FROM " + ident(c.Name) + " GROUP BY " + expr + " HAVING count(*) > 1").All(&conflicts); err != nil {
				return err
			}
			for i := range conflicts {
				conflicts[i].Name = c.Name
				if conflicts[i].Set != "" {
					if s, err := tx.FindRecordById("pv_sets", conflicts[i].Set); err == nil {
						conflicts[i].Name = s.GetString("name")
					}
				}
			}
			if len(conflicts) > 0 {
				return &ConflictError{Conflicts: conflicts}
			}
			setIndex(tx, c)
		} else {
			c.RemoveIndex(indexName(c.Id))
		}
		if err := save(tx, c); err != nil {
			return err
		}
		sc, err := tx.FindCollectionByNameOrId(configs)
		if err != nil {
			return err
		}
		r, err := tx.FindRecordById(sc, key(c.Id))
		if errors.Is(err, sql.ErrNoRows) {
			r = core.NewRecord(sc)
			r.Id = key(c.Id)
		} else if err != nil {
			return err
		}
		r.Set("collection", c.Id)
		r.Set("enabled", input.Enabled)
		if err := save(tx, r); err != nil {
			return err
		}
		result = &input
		return nil
	})
	return result, err
}

func protectCollection(e *core.CollectionEvent) error {
	if internal(e.Context) {
		return e.Next()
	}
	c := e.Collection
	old, err := e.App.FindCollectionByNameOrId(c.Id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if c.Name == configs || old != nil && old.Name == configs {
		return fmt.Errorf("singleton: service schema is managed by the plugin")
	}
	if old == nil {
		if err := rejectReservedIndexes(c); err != nil {
			return err
		}
		return e.Next()
	}
	cfg, err := Load(e.App, c.Id)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		if err := rejectReservedIndexes(c); err != nil {
			return err
		}
		return e.Next()
	}
	if !c.IsBase() || c.System {
		return fmt.Errorf("singleton: disable Singleton before changing collection type")
	}
	// Compare the caller's index to the stored one, then adapt it if Variants
	// is being installed in this transaction (before its config record exists).
	a, b := dbutils.ParseIndex(c.GetIndex(indexName(c.Id))), dbutils.ParseIndex(old.GetIndex(indexName(c.Id)))
	a.TableName = b.TableName
	if !a.IsValid() || a.Build() != b.Build() {
		return fmt.Errorf("singleton: index is managed by the plugin; use the Singleton settings")
	}
	setIndex(e.App, c)
	return e.Next()
}

func rejectReservedIndexes(c *core.Collection) error {
	for _, raw := range c.Indexes {
		if strings.HasPrefix(dbutils.ParseIndex(raw).IndexName, "idx_ps_") {
			return fmt.Errorf("singleton: reserved index; enable Singleton in collection settings")
		}
	}
	return nil
}
