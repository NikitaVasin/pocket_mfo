// Package adminui provides shared navigation for the PocketBase plugins.
package adminui

import (
	"embed"
	"io/fs"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

//go:embed ui/*
var assets embed.FS

// Register hides plugin-owned collections in the native sidebar, preserving
// the complete schema store for plugin pages, record forms and relation pickers.
// The shared hook ID ensures one extension regardless of registration order.
func Register(app core.App) {
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: "pocket-mfo-admin", Priority: -1000, Func: func(e *core.ServeEvent) error {
		ui, err := fs.Sub(assets, "ui")
		if err != nil {
			return err
		}
		e.UIExtensions = append(e.UIExtensions, core.UIExtension{Name: "pocket-mfo-admin", FS: ui})
		return e.Next()
	}})
}
