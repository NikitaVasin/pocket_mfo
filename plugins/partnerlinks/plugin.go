package partnerlinks

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"slices"
	"strings"

	"github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"
	"github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/types"
)

//go:embed ui/*
var assets embed.FS

type plugin struct {
	options Options
	client  *http.Client
}

// Register installs the plugin. Repeated registration replaces the same hooks.
func Register(app core.App, options Options) {
	register(app, options, &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}

func register(app core.App, options Options, client *http.Client) {
	polymorphicrelation.Register(app, polymorphicrelation.Options{SystemCollections: []string{ConversationsCollection}})
	options.AuthCollections = slices.Clone(options.AuthCollections)
	setManaged(app, options.Managed)
	app.Store().Set(adminLockStoreKey, options.LockAdminConfig)
	p := &plugin{options: options, client: client}
	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{Id: "partnerlinks", Func: func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return e.App.RunInTransaction(func(tx core.App) error {
			if err := install(tx); err != nil {
				return err
			}
			return validateManaged(tx)
		})
	}})
	protectRecord := func(e *core.RecordEvent) error {
		if e.Record.Collection().Name == configsCollection && !internal(e.Context) {
			return fmt.Errorf("partnerlinks: settings are managed by Configure")
		}
		if e.Record.Collection().Name == LinksCollection {
			return e.App.RunInTransaction(func(tx core.App) error {
				parent := e.App
				e.App = tx
				defer func() { e.App = parent }()
				if err := validateLink(tx, e.Record); err != nil {
					return err
				}
				return e.Next()
			})
		}
		return e.Next()
	}
	app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{Id: "partnerlinks", Func: protectRecord})
	app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{Id: "partnerlinks", Func: protectRecord})
	app.OnRecordDelete().Bind(&hook.Handler[*core.RecordEvent]{Id: "partnerlinks", Func: func(e *core.RecordEvent) error {
		if e.Record.Collection().Name == configsCollection && !internal(e.Context) {
			return fmt.Errorf("partnerlinks: settings cannot be deleted")
		}
		return e.Next()
	}})
	protectConversation := func(e *core.RecordRequestEvent) error {
		if e.Collection.Name == ConversationsCollection {
			return e.ForbiddenError("Заказы изменяются только проверенными постбеками", nil)
		}
		return e.Next()
	}
	app.OnRecordCreateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "partnerlinks", Func: protectConversation})
	app.OnRecordUpdateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "partnerlinks", Func: protectConversation})
	app.OnRecordDeleteRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "partnerlinks", Func: protectConversation})
	protectCollection := func(e *core.CollectionEvent) error {
		if (e.Collection.Name == configsCollection || e.Collection.Name == ConversationsCollection) && !internal(e.Context) {
			return fmt.Errorf("partnerlinks: reserved service collection")
		}
		if original, err := e.App.FindCollectionByNameOrId(e.Collection.Id); err == nil && (original.Name == configsCollection || original.Name == ConversationsCollection) && !internal(e.Context) {
			return fmt.Errorf("partnerlinks: service schema cannot change")
		}
		return e.Next()
	}
	app.OnCollectionCreate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "partnerlinks", Func: protectCollection})
	app.OnCollectionUpdate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "partnerlinks", Func: protectCollection})
	app.OnCollectionDelete().Bind(&hook.Handler[*core.CollectionEvent]{Id: "partnerlinks", Func: protectCollection})
	app.OnRecordEnrich().Bind(&hook.Handler[*core.RecordEnrichEvent]{Id: "partnerlinks", Func: func(e *core.RecordEnrichEvent) error {
		if e.Record.Collection().Name == configsCollection {
			return fmt.Errorf("partnerlinks: use the settings API")
		}
		if err := e.Next(); err != nil {
			return err
		}
		// PocketBase automatically unhides fields for superusers during enrich.
		if e.Record.Collection().Name == ConversationsCollection {
			e.Record.Hide("tokenHash")
			e.Record.Hide(deliveryField)
		}
		return nil
	}})
	app.OnRecordsListRequest().Bind(&hook.Handler[*core.RecordsListRequestEvent]{Id: "partnerlinks", Func: func(e *core.RecordsListRequestEvent) error {
		if e.Collection.Name == configsCollection {
			return e.ForbiddenError("Используйте API настроек плагина", nil)
		}
		return e.Next()
	}})
	app.OnRecordViewRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "partnerlinks", Func: func(e *core.RecordRequestEvent) error {
		if e.Collection.Name == configsCollection {
			return e.ForbiddenError("Используйте API настроек плагина", nil)
		}
		return e.Next()
	}})
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: "partnerlinks", Func: func(e *core.ServeEvent) error {
		if err := validateManaged(e.App); err != nil {
			return err
		}
		if err := installConversations(e.App, options.AuthCollections); err != nil {
			return err
		}
		if err := e.App.Cron().Add("partnerlinks_cleanup", "0 * * * *", func() {
			if _, err := Cleanup(e.App); err != nil {
				e.App.Logger().Error("partnerlinks: conversion cleanup failed")
			}
		}); err != nil {
			return err
		}
		ui, err := fs.Sub(assets, "ui")
		if err != nil {
			return err
		}
		e.UIExtensions = append(e.UIExtensions, core.UIExtension{Name: "partnerlinks", FS: ui})
		// Truncate uses model deletes rather than RecordDeleteRequest hooks.
		// Protect it independently of Schema Lock, by collection name and ID.
		e.Router.Bind(&hook.Handler[*core.RequestEvent]{Id: "partnerlinks-service-records", Func: func(r *core.RequestEvent) error {
			if r.Request.Pattern == "DELETE /api/collections/{collection}/truncate" || r.Request.Pattern == "PUT /api/variants/admin/collections/{collection}" {
				c, err := r.App.FindCollectionByNameOrId(r.Request.PathValue("collection"))
				if err == nil && (c.Name == ConversationsCollection || c.Name == configsCollection) {
					return r.ForbiddenError("Служебные записи изменяются только сервером", nil)
				}
			}
			return r.Next()
		}})
		// Redact after handling but before PocketBase's activity logger observes
		// the request, including failures and query-secret postbacks.
		e.Router.Bind(&hook.Handler[*core.RequestEvent]{Id: "partnerlinks-redact", Priority: apis.DefaultActivityLoggerMiddlewarePriority + 1, Func: func(r *core.RequestEvent) error {
			defer func() {
				path := r.Request.URL.Path
				if strings.HasPrefix(path, "/api/partnerlinks/r/") || strings.HasPrefix(path, "/api/partnerlinks/postbacks/") {
					u := *r.Request.URL
					if strings.HasPrefix(path, "/api/partnerlinks/r/") {
						u.Path = "/api/partnerlinks/r/[redacted]"
					}
					u.RawQuery = ""
					u.RawPath = ""
					r.Request.URL = &u
					r.Request.RequestURI = u.RequestURI()
				}
			}()
			return r.Next()
		}})
		e.Router.GET("/api/partnerlinks/admin/config", func(r *core.RequestEvent) error {
			c, err := Load(r.App)
			if err != nil {
				return err
			}
			r.Response.Header().Set("Cache-Control", "no-store")
			return r.JSON(200, adminSettings(r.App, *c))
		}).Bind(apis.RequireSuperuserAuth())
		e.Router.PUT("/api/partnerlinks/admin/config", func(r *core.RequestEvent) error {
			if r.App.Store().Get(adminLockStoreKey) == true {
				return r.ForbiddenError("Настройки доступны только для просмотра: LockAdminConfig включён в Go-коде", nil)
			}
			var c adminConfig
			if err := decodeBody(r, &c); err != nil {
				return r.BadRequestError("Некорректные настройки", nil)
			}
			result, err := Configure(r.App, c.Config)
			if err != nil {
				return r.BadRequestError(err.Error(), nil)
			}
			r.Response.Header().Set("Cache-Control", "no-store")
			return r.JSON(200, adminSettings(r.App, *result))
		}).Bind(apis.RequireSuperuserAuth())
		e.Router.POST("/api/partnerlinks/links/{id}/resolve", p.resolve).Bind(apis.RequireAuth())
		e.Router.GET("/api/partnerlinks/r/{token}", p.redirect)
		e.Router.GET("/api/partnerlinks/postbacks/{provider}", p.postback)
		e.Router.POST("/api/partnerlinks/postbacks/{provider}", p.postback)
		return e.Next()
	}})
}

func install(app core.App) error {
	return app.RunInTransaction(func(tx core.App) error {
		if existing, err := tx.FindCollectionByNameOrId(configsCollection); errors.Is(err, sql.ErrNoRows) {
			c := core.NewBaseCollection(configsCollection)
			c.System = true
			c.Fields.Add(&core.JSONField{Name: "definition", Hidden: true, MaxSize: 262144})
			if err = save(tx, c); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if field, ok := existing.Fields.GetByName("definition").(*core.JSONField); !existing.System || !existing.IsBase() || !ok || !field.Hidden {
			return fmt.Errorf("partnerlinks: reserved collection %s has an incompatible schema", configsCollection)
		}
		if existing, err := tx.FindCollectionByNameOrId(LinksCollection); errors.Is(err, sql.ErrNoRows) {
			c := core.NewBaseCollection(LinksCollection)
			c.Fields.Add(&core.TextField{Name: "name", Required: true, Presentable: true}, &core.TextField{Name: "provider", Required: true}, &core.BoolField{Name: "active"}, &dynamiclink.Field{JSONField: core.JSONField{Name: "link", Required: true, MaxSize: dynamiclink.MaxSize, Help: "Исходная партнёрская ссылка и параметры открытия в приложении."}})
			c.ListRule = types.Pointer("@request.auth.id != ''")
			c.ViewRule = types.Pointer("@request.auth.id != ''")
			return tx.Save(c)
		} else if err != nil {
			return err
		} else {
			if !existing.IsBase() || existing.System {
				return fmt.Errorf("partnerlinks: incompatible partner_links collection")
			}
			for _, name := range []string{"name", "provider", "active"} {
				if existing.Fields.GetByName(name) == nil {
					return fmt.Errorf("partnerlinks: missing link field %s", name)
				}
			}
			return migrateLinkField(tx, existing)
		}
	})
}

func opening(r *core.Record) (Link, error) {
	value, err := dynamiclink.Decode([]byte(r.GetString("link")))
	if err != nil {
		return Link{}, err
	}
	if value == nil {
		return Link{}, fmt.Errorf("partnerlinks: link is required")
	}
	return *value, nil
}

func validateLink(app core.App, r *core.Record) error {
	link, err := opening(r)
	if err != nil {
		return err
	}
	c, err := Load(app)
	if err != nil {
		return err
	}
	p, err := provider(c, r.GetString("provider"))
	if err != nil {
		return err
	}
	_, err = renderURL(p, link.URL, "testClickData")
	return err
}
