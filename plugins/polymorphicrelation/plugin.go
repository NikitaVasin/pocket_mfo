package polymorphicrelation

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

//go:embed ui/*
var assets embed.FS

// Options configures additional server-owned hosts for polymorphic fields.
// The owning plugin must protect their schema and HTTP record writes itself.
type Options struct {
	SystemCollections []string // exact base collection names; never system auth targets
}

const systemCollectionKey = "polymorphicrelation.systemCollection."

// Register installs model hooks and the embedded admin extension. Call before app.Start.
// Stable hook IDs make repeated registration on the same app harmless.
func Register(app core.App, options ...Options) {
	// Registration is trusted Go code. No record, schema field or HTTP parameter
	// can opt a system collection in. Re-registration retains earlier grants.
	for _, o := range options {
		for _, name := range o.SystemCollections {
			app.Store().Set(systemCollectionKey+name, true)
		}
	}
	app.OnCollectionCreate().Bind(&hook.Handler[*core.CollectionEvent]{Id: Type, Func: saveCollection})
	app.OnCollectionUpdate().Bind(&hook.Handler[*core.CollectionEvent]{Id: Type, Func: saveCollection})
	app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{Id: Type, Func: saveRecord})
	app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{Id: Type, Func: saveRecord})
	app.OnRecordDeleteExecute().Bind(&hook.Handler[*core.RecordEvent]{Id: Type, Func: restrictDelete})
	app.OnRecordCreateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: Type, Func: protectInput})
	app.OnRecordUpdateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: Type, Func: protectInput})
	app.OnRecordAuthRequest().Bind(&hook.Handler[*core.RecordAuthRequestEvent]{Id: Type, Func: expandAuthResponse})
	app.OnRecordEnrich().Bind(&hook.Handler[*core.RecordEnrichEvent]{Id: Type, Func: enrich})
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: Type, Func: func(e *core.ServeEvent) error {
		ui, err := fs.Sub(assets, "ui")
		if err != nil {
			return err
		}
		e.UIExtensions = append(e.UIExtensions, core.UIExtension{Name: Type, FS: ui})
		return e.Next()
	}})
}

func fields(c *core.Collection) []*Field {
	var result []*Field
	for _, raw := range c.Fields {
		if f, ok := raw.(*Field); ok {
			result = append(result, f)
		}
	}
	return result
}

func managed(c *core.Collection) map[string]*Field {
	result := map[string]*Field{}
	for _, f := range fields(c) {
		for _, id := range f.CollectionIDs {
			result[ServiceFieldName(f.Id, id)] = f
		}
	}
	return result
}

func saveRecord(e *core.RecordEvent) error {
	if len(fields(e.Record.Collection())) == 0 {
		return e.Next()
	}
	parent := e.App
	defer func() { e.App = parent }()
	return parent.RunInTransaction(func(tx core.App) error {
		e.App = tx
		for _, f := range fields(e.Record.Collection()) {
			r := e.Record
			ref, err := f.reference(r)
			if err != nil {
				return validation.Errors{f.Name: validation.NewError("invalid_reference", err.Error())}
			}
			if err := f.ValidateValue(e.Context, tx, r); err != nil {
				return validation.Errors{f.Name: err}
			}
			for _, id := range f.CollectionIDs {
				value := ""
				if ref != nil && ref.CollectionID == id {
					value = ref.RecordID
				}
				r.Set(ServiceFieldName(f.Id, id), value)
			}
		}
		return e.Next()
	})
}

func restrictDelete(e *core.RecordEvent) error {
	parent := e.App
	defer func() { e.App = parent }()
	return parent.RunInTransaction(func(tx core.App) error {
		e.App = tx
		refs, err := tx.FindCachedCollectionReferences(e.Record.Collection())
		if err != nil {
			return err
		}
		for c, rels := range refs {
			owners := managed(c)
			for _, rel := range rels {
				f := owners[rel.GetName()]
				if f == nil || f.policy() != Restrict {
					continue
				}
				query := tx.RecordQuery(c).AndWhere(dbx.HashExp{rel.GetName(): e.Record.Id})
				if c.Id == e.Record.Collection().Id {
					query.AndWhere(dbx.Not(dbx.HashExp{"id": e.Record.Id}))
				}
				var count int
				if err := query.Select("count(*)").Row(&count); err != nil {
					return err
				}
				if count > 0 {
					return fmt.Errorf("cannot delete parent: %s.%s has %d references", c.Name, f.Name, count)
				}
			}
		}
		// Clear all setNull fields on each child before the parent disappears.
		// Native cleanup saves one companion at a time without validation, which
		// would temporarily leave other polymorphic fields pointing at a deleted row.
		for c, rels := range refs {
			owners := managed(c)
			for _, rel := range rels {
				f := owners[rel.GetName()]
				if f == nil || f.policy() != SetNull {
					continue
				}
				for {
					var children []*core.Record
					query := tx.RecordQuery(c).AndWhere(dbx.HashExp{rel.GetName(): e.Record.Id}).Limit(500)
					if c.Id == e.Record.Collection().Id {
						query.AndWhere(dbx.Not(dbx.HashExp{"id": e.Record.Id}))
					}
					if err := query.All(&children); err != nil {
						return err
					}
					if len(children) == 0 {
						break
					}
					for _, child := range children {
						for _, field := range fields(c) {
							ref, err := field.reference(child)
							if err != nil {
								return err
							}
							if field.policy() == SetNull && ref != nil && ref.CollectionID == e.Record.Collection().Id && ref.RecordID == e.Record.Id {
								child.Set(field.Name, nil)
							}
						}
						if err := tx.SaveNoValidate(child); err != nil {
							return err
						}
					}
				}
			}
		}
		return e.Next()
	})
}

func protectInput(e *core.RecordRequestEvent) error {
	info, err := e.RequestInfo()
	if err != nil {
		return err
	}
	for name := range managed(e.Record.Collection()) {
		for key := range info.Body {
			if strings.Trim(key, "+-") == name {
				return e.BadRequestError("Managed relation fields are read-only; write the polymorphic field instead.", nil)
			}
		}
	}
	return e.Next()
}
