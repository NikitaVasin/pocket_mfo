package appmetrica

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"reflect"
	"time"

	"github.com/NikitaVasin/pocket_mfo/internal/adminui"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

//go:embed ui/*
var assets embed.FS

type Plugin struct {
	options Options
	client  *http.Client
}

// Register installs the shared module before its consumers and Bootstrap.
func Register(app core.App, options Options) *Plugin {
	adminui.Register(app)
	b, _ := json.Marshal(options.Managed)
	var managed *ManagedConfig
	_ = json.Unmarshal(b, &managed)
	options.Managed = managed
	p := &Plugin{options: options, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	app.Store().Set(storeKey, p)
	protect(app)
	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{Id: "appmetrica", Func: func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return initialize(e.App)
	}})
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: "appmetrica", Func: func(e *core.ServeEvent) error {
		if err := initialize(e.App); err != nil {
			return err
		}
		// Code-configured credentials need no manual setup in the dashboard.
		if m := p.options.Managed; m != nil && m.ApplicationID != nil && *m.ApplicationID > 0 && m.OAuthToken != nil && *m.OAuthToken != "" {
			c, err := Load(e.App)
			if err != nil {
				return err
			}
			if _, err = p.Connect(context.Background(), e.App, ConnectInput{Version: c.Version}); err != nil {
				e.App.Logger().Warn("AppMetrica: не удалось обновить ключи приложения; повторите подключение в админке")
			}
		}
		ui, err := fs.Sub(assets, "ui")
		if err != nil {
			return err
		}
		e.UIExtensions = append(e.UIExtensions, core.UIExtension{Name: "appmetrica", FS: ui})
		e.Router.GET("/api/appmetrica/admin/config", func(r *core.RequestEvent) error {
			c, err := Load(r.App)
			if err != nil {
				return err
			}
			r.Response.Header().Set("Cache-Control", "no-store")
			return r.JSON(200, adminSettings(r.App, c))
		}).Bind(apis.RequireSuperuserAuth())
		e.Router.PUT("/api/appmetrica/admin/config", func(r *core.RequestEvent) error {
			if p.options.LockAdminConfig {
				return r.ForbiddenError("Настройки заданы кодом", nil)
			}
			var c Config
			dec := json.NewDecoder(http.MaxBytesReader(r.Response, r.Request.Body, 64<<10))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&c); err != nil {
				return r.BadRequestError("Некорректные настройки", nil)
			}
			var result Config
			err := r.App.RunInTransaction(func(tx core.App) error {
				old, err := Load(tx)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(c.EventNames, old.EventNames) {
					return fmt.Errorf("Имена событий задаются только кодом")
				}
				result, err = Configure(tx, c)
				return err
			})
			if err != nil {
				return r.BadRequestError(err.Error(), nil)
			}
			r.Response.Header().Set("Cache-Control", "no-store")
			return r.JSON(200, adminSettings(r.App, result))
		}).Bind(apis.RequireSuperuserAuth())
		e.Router.POST("/api/appmetrica/admin/connect", func(r *core.RequestEvent) error {
			if p.options.LockAdminConfig {
				return r.ForbiddenError("Настройки заданы кодом", nil)
			}
			var in ConnectInput
			dec := json.NewDecoder(http.MaxBytesReader(r.Response, r.Request.Body, 16<<10))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in); err != nil {
				return r.BadRequestError("Некорректные параметры подключения", nil)
			}
			result, err := p.Connect(r.Request.Context(), r.App, in)
			if err != nil {
				return r.BadRequestError(err.Error(), nil)
			}
			r.Response.Header().Set("Cache-Control", "no-store")
			return r.JSON(200, result)
		}).Bind(apis.RequireSuperuserAuth())
		e.Router.POST("/api/appmetrica/admin/check", func(r *core.RequestEvent) error {
			c, err := Load(r.App)
			if err != nil {
				return err
			}
			result := p.Check(r.Request.Context(), c)
			current, err := Load(r.App)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, c) {
				return r.BadRequestError("Настройки изменились; повторите проверку", nil)
			}
			r.Response.Header().Set("Cache-Control", "no-store")
			return r.JSON(200, result)
		}).Bind(apis.RequireSuperuserAuth())
		e.Router.Bind(&hook.Handler[*core.RequestEvent]{Id: "appmetrica-service", Func: func(r *core.RequestEvent) error {
			if r.Request.Pattern == "DELETE /api/collections/{collection}/truncate" || r.Request.Pattern == "PUT /api/variants/admin/collections/{collection}" {
				c, err := r.App.FindCollectionByNameOrId(r.Request.PathValue("collection"))
				if err == nil && c.Name == Collection {
					return r.ForbiddenError("Служебная коллекция защищена", nil)
				}
			}
			return r.Next()
		}})
		return e.Next()
	}})
	return p
}

func protect(app core.App) {
	guard := func(e *core.RecordEvent) error {
		if e.Record.Collection().Name == Collection && (e.Context == nil || e.Context.Value(internalKey{}) != true) {
			return fmt.Errorf("AppMetrica: используйте Configure")
		}
		return e.Next()
	}
	app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{Id: "appmetrica", Func: guard})
	app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{Id: "appmetrica", Func: guard})
	app.OnRecordDelete().Bind(&hook.Handler[*core.RecordEvent]{Id: "appmetrica", Func: guard})
	schema := func(e *core.CollectionEvent) error {
		old, _ := e.App.FindCollectionByNameOrId(e.Collection.Id)
		if (e.Collection.Name == Collection || (old != nil && old.Name == Collection)) && (e.Context == nil || e.Context.Value(internalKey{}) != true) {
			return fmt.Errorf("AppMetrica: служебная схема защищена")
		}
		return e.Next()
	}
	app.OnCollectionCreate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "appmetrica", Func: schema})
	app.OnCollectionUpdate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "appmetrica", Func: schema})
	app.OnCollectionDelete().Bind(&hook.Handler[*core.CollectionEvent]{Id: "appmetrica", Func: schema})
	app.OnRecordEnrich().Bind(&hook.Handler[*core.RecordEnrichEvent]{Id: "appmetrica", Func: func(e *core.RecordEnrichEvent) error {
		if e.Record.Collection().Name == Collection {
			return fmt.Errorf("AppMetrica: служебные данные недоступны")
		}
		return e.Next()
	}})
	app.OnRecordsListRequest().Bind(&hook.Handler[*core.RecordsListRequestEvent]{Id: "appmetrica", Func: func(e *core.RecordsListRequestEvent) error {
		if e.Collection.Name == Collection {
			return e.ForbiddenError("Используйте настройки AppMetrica", nil)
		}
		return e.Next()
	}})
}

// RegisterLegacySource imports an existing consumer's settings once, atomically.
// Sources are trusted Go callbacks registered before Bootstrap.
func RegisterLegacySource(app core.App, name string, source func(core.App) (Config, error)) {
	sources, _ := app.Store().Get("appmetrica.sources").(map[string]func(core.App) (Config, error))
	if sources == nil {
		sources = map[string]func(core.App) (Config, error){}
	}
	sources[name] = source
	app.Store().Set("appmetrica.sources", sources)
}
func Initialized(app core.App) bool {
	if !Enabled(app) {
		return false
	}
	_, err := app.FindRecordById(Collection, configID)
	return !errors.Is(err, sql.ErrNoRows)
}

func initialize(app core.App) error {
	return app.RunInTransaction(func(tx core.App) error {
		col, err := tx.FindCollectionByNameOrId(Collection)
		if errors.Is(err, sql.ErrNoRows) {
			col = core.NewBaseCollection(Collection)
			col.System = true
			col.Fields.Add(&core.JSONField{Name: "definition", Hidden: true, MaxSize: 64 << 10})
			if err = save(tx, col); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !col.System || !col.IsBase() || col.Fields.GetByName("definition") == nil {
			return fmt.Errorf("AppMetrica: конфликт служебной коллекции")
		}
		raw, err := rawConfig(tx)
		if err != nil {
			return err
		}
		if raw.Version == 0 {
			merged := DefaultConfig()
			sources, _ := tx.Store().Get("appmetrica.sources").(map[string]func(core.App) (Config, error))
			for _, source := range sources {
				legacy, err := source(tx)
				if err != nil {
					return err
				}
				if legacy.ApplicationID > 0 {
					if merged.ApplicationID > 0 && merged.ApplicationID != legacy.ApplicationID {
						return fmt.Errorf("AppMetrica: разные Application ID в плагинах; сначала согласуйте настройки")
					}
					merged.ApplicationID = legacy.ApplicationID
				}
				if legacy.PostAPIKey != "" {
					merged.PostAPIKey = legacy.PostAPIKey
				}
				if legacy.OAuthToken != "" {
					merged.OAuthToken = legacy.OAuthToken
				}
				if legacy.OAuthClientID != "" {
					merged.OAuthClientID = legacy.OAuthClientID
				}
				if legacy.EventNames != nil {
					merged.EventNames = legacy.EventNames
				}
			}
			fields := object(merged)
			for key, value := range pinned(tx) {
				if key == "applicationId" && merged.ApplicationID > 0 {
					var id int64
					_ = json.Unmarshal(value, &id)
					if id != merged.ApplicationID {
						return fmt.Errorf("AppMetrica: Application ID в коде отличается от прежних настроек")
					}
				}
				fields[key] = value
			}
			b, _ := json.Marshal(fields)
			_ = json.Unmarshal(b, &merged)
			// Initialize canonical storage without persisting managed secrets.
			seed := core.NewRecord(col)
			seed.Id = configID
			// Configure reads the managed overlay, so save imported non-managed values first.
			stored := object(merged)
			for key := range pinned(tx) {
				if key != "applicationId" {
					delete(stored, key)
				}
			}
			seed.Set("definition", stored)
			if err = save(tx, seed); err != nil {
				return err
			}
			_, err = Configure(tx, merged)
			return err
		}
		effective, err := Load(tx)
		if err != nil {
			return err
		}
		if err = validate(effective); err != nil {
			return err
		}
		if raw.ApplicationID != effective.ApplicationID {
			_, err = Configure(tx, effective)
		}
		return err
	})
}
