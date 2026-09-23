package typedconfig

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

//go:embed ui/*
var assets embed.FS

// Register installs atomic integrity hooks and a native item-card field editor.
// Call before Bootstrap; content collections remain compatible with Variants.
func Register(app core.App) {
	app.OnCollectionCreate().Bind(&hook.Handler[*core.CollectionEvent]{Id: Type, Func: saveCollection})
	app.OnCollectionUpdate().Bind(&hook.Handler[*core.CollectionEvent]{Id: Type, Func: saveCollection})
	app.OnCollectionDelete().Bind(&hook.Handler[*core.CollectionEvent]{Id: Type, Func: func(e *core.CollectionEvent) error {
		all, err := e.App.FindAllCollections()
		if err != nil {
			return err
		}
		for _, c := range all {
			if c.Id == e.Collection.Id {
				continue
			}
			for _, f := range fields(c) {
				if f.Relations[e.Collection.Id] != "" {
					return fmt.Errorf("typedConfig: remove source from %s.%s schema before deleting its collection", c.Name, f.Name)
				}
			}
		}
		return e.Next()
	}})
	app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{Id: Type, Func: saveRecord})
	app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{Id: Type, Func: saveRecord})
	app.OnRecordDeleteExecute().Bind(&hook.Handler[*core.RecordEvent]{Id: Type, Priority: -20, Func: deleteSource})
	app.OnRecordCreateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: Type, Func: protectInput})
	app.OnRecordUpdateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: Type, Func: protectInput})
	app.OnRecordDeleteRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: Type, Func: protectInput})
	// Complete projection before Variants signs the public material after Next.
	app.OnRecordEnrich().Bind(&hook.Handler[*core.RecordEnrichEvent]{Id: Type, Priority: 100, Func: func(e *core.RecordEnrichEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		for _, f := range fields(e.Record.Collection()) {
			for _, name := range f.Relations {
				e.Record.Hide(name)
			}
			if e.RequestInfo == nil || (e.RequestInfo.Auth != nil && e.RequestInfo.Auth.IsSuperuser()) {
				continue
			}
			result, _, err := f.evaluate(e.App, e.Record, e.RequestInfo, true)
			if err != nil {
				return err
			}
			e.Record.Set(f.Name, result)
		}
		return nil
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

// ServiceFieldName is stable across display-name changes. Do not write it directly.
func ServiceFieldName(fieldID, collectionID string) string {
	h := sha256.Sum256([]byte(fieldID + "\x00" + collectionID))
	return "tc_" + hex.EncodeToString(h[:])[:12]
}
func companions(c *core.Collection) map[string]string {
	m := map[string]string{}
	for _, f := range fields(c) {
		for id, name := range f.Relations {
			m[name] = id
		}
	}
	return m
}
func saveCollection(e *core.CollectionEvent) error {
	parent := e.App
	var old *core.Collection
	if !e.Collection.IsNew() {
		old, _ = parent.FindCollectionByNameOrId(e.Collection.Id)
	}
	// Source schema changes must not invalidate existing projections or reveal hidden data.
	all, err := parent.FindAllCollections()
	if err != nil {
		return err
	}
	for _, c := range all {
		if c.Id == e.Collection.Id {
			continue
		}
		for _, f := range fields(c) {
			if err := f.Schema.validate(parent, c, e.Collection); err != nil {
				return err
			}
		}
	}
	if len(fields(e.Collection)) == 0 && (old == nil || len(fields(old)) == 0) {
		return e.Next()
	}
	defer func() { e.App = parent; _ = parent.ReloadCachedCollections() }()
	return parent.RunInTransaction(func(tx core.App) error {
		e.App = tx
		c := e.Collection
		for _, f := range fields(c) {
			if err := f.ValidateSettings(e.Context, tx, c); err != nil {
				return err
			}
			if old != nil {
				if previous, ok := old.Fields.GetById(f.Id).(*Field); ok {
					a, _ := json.Marshal(previous.Schema)
					b, _ := json.Marshal(f.Schema)
					if string(a) != string(b) && f.Schema.Version <= previous.Schema.Version {
						return fmt.Errorf("typedConfig: increase schema version before changing types")
					}
				}
			}
			f.Relations = map[string]string{}
			f.Schema.walk(func(n *Node) {
				if n.Source == nil {
					return
				}
				source, _ := tx.FindCollectionByNameOrId(n.Source.Collection)
				n.Source.Collection = source.Id
				if label := sourceField(source, n.Source.LabelField); label != nil {
					n.Source.LabelField = label.GetId()
				}
				for key, name := range n.Source.Mapping {
					n.Source.Mapping[key] = sourceField(source, name).GetId()
				}
				f.Relations[source.Id] = ServiceFieldName(f.Id, source.Id)
			})
		}
		wanted := companions(c)
		if old != nil {
			for name := range companions(old) {
				if _, ok := wanted[name]; !ok {
					c.Fields.RemoveByName(name)
				}
			}
		}
		for name, id := range wanted {
			for _, existing := range []core.Field{c.Fields.GetByName(name), c.Fields.GetById(name)} {
				if existing != nil {
					rel, ok := existing.(*core.RelationField)
					if !ok || rel.Id != name || rel.Name != name || rel.CollectionId != id {
						return fmt.Errorf("typedConfig: managed relation collision")
					}
				}
			}
			c.Fields.Add(&core.RelationField{Id: name, Name: name, CollectionId: id, MaxSelect: 10000, Hidden: true})
		}
		if err := e.Next(); err != nil {
			return err
		}
		// Validate and rebuild links for existing content in the same schema transaction.
		// Bound memory; rejecting a schema migration rolls back schema and companions.
		offset := 0
		for {
			var rows []*core.Record
			if err := tx.RecordQuery(c).OrderBy("id").Limit(100).Offset(int64(offset)).All(&rows); err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			for _, r := range rows {
				if err := tx.SaveNoValidate(r); err != nil {
					return err
				}
			}
			offset += len(rows)
		}
		return nil
	})
}
func saveRecord(e *core.RecordEvent) error {
	related, err := e.App.FindCachedCollectionReferences(e.Record.Collection())
	if err != nil {
		return err
	}
	hasDependants := false
	for c := range related {
		for _, f := range fields(c) {
			if f.Relations[e.Record.Collection().Id] != "" {
				hasDependants = true
			}
		}
	}
	if len(fields(e.Record.Collection())) == 0 && !hasDependants {
		return e.Next()
	}
	parent := e.App
	defer func() { e.App = parent }()
	return parent.RunInTransaction(func(tx core.App) error {
		e.App = tx
		for _, f := range fields(e.Record.Collection()) {
			result, refs, err := f.evaluate(tx, e.Record, nil, false)
			if err != nil {
				return validation.Errors{f.Name: validation.NewError("invalid_items", err.Error())}
			}
			e.Record.Set(f.Name, result)
			for id, name := range f.Relations {
				values := refs[id]
				if values == nil {
					values = []string{}
				}
				e.Record.Set(name, values)
			}
		}
		if err := e.Next(); err != nil {
			return err
		}
		for _, f := range fields(e.Record.Collection()) {
			if _, _, err := f.evaluate(tx, e.Record, nil, true); err != nil {
				return err
			}
		}
		// A source edit must not invalidate mapped values in already saved blocks.
		// Validation runs after the new source is stored, inside the same transaction.
		for c := range related {
			for _, f := range fields(c) {
				name := f.Relations[e.Record.Collection().Id]
				if name == "" {
					continue
				}
				offset := 0
				for {
					var rows []*core.Record
					err := tx.RecordQuery(c).AndWhere(dbx.NewExp("EXISTS (SELECT 1 FROM json_each([["+name+"]]) WHERE value = {:target})", dbx.Params{"target": e.Record.Id})).OrderBy("id").Limit(100).Offset(int64(offset)).All(&rows)
					if err != nil {
						return err
					}
					if len(rows) == 0 {
						break
					}
					for _, r := range rows {
						if _, _, err := f.evaluate(tx, r, nil, true); err != nil {
							return fmt.Errorf("typedConfig: source edit invalidates %s.%s: %w", c.Name, f.Name, err)
						}
					}
					offset += len(rows)
				}
			}
		}
		return nil
	})
}
func protectInput(e *core.RecordRequestEvent) error {
	fs := fields(e.Record.Collection())
	if len(fs) == 0 {
		return e.Next()
	}
	if e.Auth == nil || !e.Auth.IsSuperuser() {
		return e.ForbiddenError("Конфигурации изменяет администратор", nil)
	}
	info, err := e.RequestInfo()
	if err != nil {
		return err
	}
	managed := companions(e.Record.Collection())
	for key := range info.Body {
		if _, ok := managed[cleanName(key)]; ok {
			return e.BadRequestError("Служебные связи изменяются автоматически", nil)
		}
	}
	return e.Next()
}
func deleteSource(e *core.RecordEvent) error {
	parent := e.App
	defer func() { e.App = parent }()
	return parent.RunInTransaction(func(tx core.App) error {
		e.App = tx
		refs, err := tx.FindCachedCollectionReferences(e.Record.Collection())
		if err != nil {
			return err
		}
		for c := range refs {
			for _, f := range fields(c) {
				name := f.Relations[e.Record.Collection().Id]
				if name == "" {
					continue
				}
				for {
					var parents []*core.Record
					query := tx.RecordQuery(c).AndWhere(dbx.NewExp("EXISTS (SELECT 1 FROM json_each([["+name+"]]) WHERE value = {:target})", dbx.Params{"target": e.Record.Id})).Limit(100)
					if err := query.All(&parents); err != nil {
						return err
					}
					if len(parents) == 0 {
						break
					}
					for _, owner := range parents {
						items, err := Decode([]byte(owner.GetString(f.Name)))
						if err != nil {
							return err
						}
						items = slices.DeleteFunc(items, func(item Item) bool { return f.depends(item, e.Record.Collection().Id, e.Record.Id) })
						owner.Set(f.Name, items)
						if err := tx.SaveNoValidate(owner); err != nil {
							return err
						}
					}
				}
			}
		}
		return e.Next()
	})
}
