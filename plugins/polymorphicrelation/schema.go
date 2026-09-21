package polymorphicrelation

import (
	"fmt"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func indexName(c *core.Collection, name string) string { return "idx_" + c.Id + "_" + name }

func saveCollection(e *core.CollectionEvent) error {
	var old *core.Collection
	if !e.Collection.IsNew() {
		var err error
		old, err = e.App.FindCollectionByNameOrId(e.Collection.Id)
		if err != nil {
			return err
		}
	}
	if len(fields(e.Collection)) == 0 && (old == nil || len(fields(old)) == 0) {
		return e.Next()
	}
	parent := e.App
	defer func() { e.App = parent; _ = parent.ReloadCachedCollections() }()
	return parent.RunInTransaction(func(tx core.App) error {
		e.App = tx
		c := e.Collection
		oldManaged := map[string]*Field{}
		if old != nil {
			oldManaged = managed(old)
		}
		for _, f := range fields(c) {
			// Target existence is checked by Field.ValidateSettings. PocketBase schema
			// import first saves all collections without validation, then validates them;
			// checking existence here would reject forward references in that first pass.
			f.OnDelete = f.policy()
			f.Relations = map[string]string{}
			for _, id := range f.CollectionIDs {
				f.Relations[id] = ServiceFieldName(f.Id, id)
			}
		}
		wanted := managed(c)
		for name, owner := range oldManaged {
			if wanted[name] != nil {
				continue
			}
			// Removing a complete logical field is explicit; narrowing its targets must not lose data.
			if current, ok := c.Fields.GetById(owner.Id).(*Field); ok && !slices.Contains(current.CollectionIDs, targetFor(owner, name)) {
				var count int
				if err := tx.RecordQuery(old).Select("count(*)").AndWhere(dbx.Not(dbx.HashExp{name: ""})).Row(&count); err != nil {
					return err
				}
				if count > 0 {
					return fmt.Errorf("cannot remove target from %s: %d records still reference it", current.Name, count)
				}
			}
			c.Fields.RemoveByName(name)
			c.Fields.RemoveById(name)
			c.RemoveIndex(indexName(old, name))
		}
		for _, f := range fields(c) {
			for _, id := range f.CollectionIDs {
				name := ServiceFieldName(f.Id, id)
				for _, existing := range []core.Field{c.Fields.GetByName(name), c.Fields.GetById(name)} {
					if existing == nil {
						continue
					}
					rel, ok := existing.(*core.RelationField)
					// Export/import includes generated relations; accept only matching ownership.
					if !ok || rel.Id != name || rel.Name != name || rel.CollectionId != id {
						return fmt.Errorf("managed field collision: %s", name)
					}
				}
				c.Fields.Add(&core.RelationField{
					Id: name, Name: name, CollectionId: id, MaxSelect: 1,
					Hidden: f.Hidden, CascadeDelete: f.policy() == Cascade,
				})
				c.AddIndex(indexName(c, name), false, name, "")
			}
		}
		return e.Next()
	})
}

func targetFor(f *Field, name string) string {
	for _, id := range f.CollectionIDs {
		if ServiceFieldName(f.Id, id) == name {
			return id
		}
	}
	return ""
}
