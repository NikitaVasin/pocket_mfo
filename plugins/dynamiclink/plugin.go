package dynamiclink

import (
	"embed"
	"io/fs"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

//go:embed ui/*
var assets embed.FS

// Register enables the field editor and user-specific response policies.
// For independent use, call Register before Bootstrap/Start.
func Register(app core.App) {
	validate := func(e *core.RecordEvent) error {
		if e.Record.Collection().Name == SettingsCollection {
			if err := validatePolicy(e.Record); err != nil {
				return err
			}
		}
		return e.Next()
	}
	app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{Id: Type, Func: validate})
	app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{Id: Type, Func: validate})
	app.OnRecordEnrich().Bind(&hook.Handler[*core.RecordEnrichEvent]{Id: Type, Func: func(e *core.RecordEnrichEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		if e.RequestInfo == nil || e.RequestInfo.Auth == nil || e.RequestInfo.Auth.IsSuperuser() {
			return nil
		}
		p, err := policyFor(e.App, e.RequestInfo.Auth)
		if err != nil {
			return err
		}
		return enrichTree(e.Record, p, map[*core.Record]bool{})
	}})
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: Type, Func: func(e *core.ServeEvent) error {
		ui, err := fs.Sub(assets, "ui")
		if err != nil {
			return err
		}
		e.UIExtensions = append(e.UIExtensions, core.UIExtension{Name: Type, FS: ui})
		return e.Next()
	}})
}
