// Package schemalock protects the schema over HTTP while preserving content
// editing and server administration. Trusted Go code and migrations retain
// access to the schema.
package schemalock

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

//go:embed ui/*
var assets embed.FS

// Register enables the policy for every HTTP caller, including superusers.
// Call before Bootstrap/Start. Repeated registration is safe.
func Register(app core.App) {
	variants.LockAdminRules(app)
	// Request hooks also run for internal batch actions. Model hooks would
	// incorrectly block trusted plugin writes and migrations.
	protect := func(e *core.RecordRequestEvent) error {
		if e.Collection.System && e.Collection.Name != core.CollectionNameSuperusers {
			return e.ForbiddenError("Schema Lock: системные записи доступны только для чтения.", nil)
		}
		return e.Next()
	}
	app.OnRecordCreateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "schemalock", Priority: -100000, Func: protect})
	app.OnRecordUpdateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "schemalock", Priority: -100000, Func: protect})
	app.OnRecordDeleteRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "schemalock", Priority: -100000, Func: protect})
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: "schemalock", Priority: 1000, Func: func(e *core.ServeEvent) error {
		ui, err := fs.Sub(assets, "ui")
		if err != nil {
			return err
		}
		e.UIExtensions = append(e.UIExtensions, core.UIExtension{Name: "schemalock", FS: ui})
		e.Router.Bind(&hook.Handler[*core.RequestEvent]{Id: "schemalock", Priority: -100000, Func: func(r *core.RequestEvent) error {
			path := r.Request.URL.Path
			if (path == "/api" || strings.HasPrefix(path, "/api/")) && !allowed(r.Request) {
				return r.ForbiddenError("Schema Lock: это действие доступно только из серверного кода.", nil)
			}
			return r.Next()
		}})
		return e.Next()
	}})
}

// Match the router's resolved pattern, not arbitrary URL prefixes. In
// particular /collections/{collection} never grants access to schema writes.
func allowed(r *http.Request) bool {
	if r.Method == http.MethodOptions {
		return true // CORS preflight performs no application action.
	}
	switch r.Pattern {
	case "GET /api/health", "GET /api/settings",
		"PATCH /api/settings", "POST /api/settings/test/email", "POST /api/settings/test/s3",
		"POST /api/settings/apple/generate-client-secret",
		"GET /api/crons", "POST /api/crons/{id}",
		"GET /api/logs", "GET /api/logs/stats", "GET /api/logs/{id}", "DELETE /api/logs",
		"GET /api/backups", "POST /api/backups", "POST /api/backups/upload",
		"GET /api/backups/{key}", "DELETE /api/backups/{key}",
		"GET /api/collections", "GET /api/collections/{collection}",
		"GET /api/collections/meta/scaffolds", "GET /api/collections/meta/oauth2-providers",
		"GET /api/collections/{collection}/records", "GET /api/collections/{collection}/records/{id}",
		"POST /api/collections/{collection}/records", "PATCH /api/collections/{collection}/records/{id}",
		"DELETE /api/collections/{collection}/records/{id}",
		"GET /api/collections/{collection}/auth-methods",
		"POST /api/collections/{collection}/auth-refresh", "POST /api/collections/{collection}/auth-with-password",
		"POST /api/collections/{collection}/auth-with-oauth2", "POST /api/collections/{collection}/request-otp",
		"POST /api/collections/{collection}/auth-with-otp", "POST /api/collections/{collection}/request-password-reset",
		"POST /api/collections/{collection}/confirm-password-reset", "POST /api/collections/{collection}/request-verification",
		"POST /api/collections/{collection}/confirm-verification", "POST /api/collections/{collection}/request-email-change",
		"POST /api/collections/{collection}/confirm-email-change", "POST /api/collections/{collection}/impersonate/{id}",
		"GET /api/oauth2-redirect", "POST /api/oauth2-redirect",
		"POST /api/batch", "GET /api/realtime", "POST /api/realtime",
		"POST /api/files/token", "GET /api/files/{collection}/{recordId}/{filename}",
		"GET /api/variants/me", "GET /api/variants/me/history",
		"GET /api/variants/admin/users/{auth}/{id}", "GET /api/variants/admin/users/{auth}/{id}/history",
		"GET /api/variants/admin/collections/{collection}", "PUT /api/variants/admin/collections/{collection}",
		"GET /api/singleton/admin/collections/{collection}":
		return true
	}
	return false
}
