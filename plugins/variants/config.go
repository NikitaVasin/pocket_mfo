package variants

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/search"
)

var validKey = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)

func setIndexName(collection string) string { return "idx_pv_" + digest(collection)[:16] }

func install(app core.App) error {
	return app.RunInTransaction(func(tx core.App) error {
		for _, name := range []string{configs, sets, states, history} {
			if existing, err := tx.FindCollectionByNameOrId(name); err == nil {
				expected := map[string]string{configs: "definition", sets: "variant", states: "fingerprint", history: "decision"}[name]
				if !existing.System || !existing.IsBase() || existing.Fields.GetByName(expected) == nil {
					return errInvalid("reserved collection name collision: %s", name)
				}
				continue
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			c := core.NewBaseCollection(name)
			c.System = true
			switch name {
			case configs:
				c.Fields.Add(&core.JSONField{Name: "definition", MaxSize: 1024 * 1024})
			case sets:
				for _, n := range []string{"collection", "variant", "experiment", "group", "name"} {
					c.Fields.Add(&core.TextField{Name: n, Presentable: n == "name"})
				}
				c.Fields.Add(&core.BoolField{Name: "active"})
				c.AddIndex("idx_pv_sets_collection", false, "collection", "")
			case states, history:
				for _, n := range []string{"auth_collection", "user", "collection", "fingerprint"} {
					c.Fields.Add(&core.TextField{Name: n})
				}
				c.Fields.Add(&core.JSONField{Name: "decision"}, &core.AutodateField{Name: "created", OnCreate: true})
				if name == states {
					c.AddIndex("idx_pv_state_identity", true, "auth_collection, user, collection", "")
				} else {
					c.AddIndex("idx_pv_history_user", false, "auth_collection, user, created", "")
				}
			}
			if err := save(tx, c); err != nil {
				return err
			}
		}
		return nil
	})
}

// Publish atomically installs the content field, sets, decision view and native rules.
// Version is optimistic concurrency control: use 0 for a new configuration.
func Publish(app core.App, input Config) (*Config, error) {
	return publish(app, input, false)
}

func publish(app core.App, input Config, lockRules bool) (*Config, error) {
	defer func() { _ = app.ReloadCachedCollections() }()
	var result *Config
	err := app.RunInTransaction(func(tx core.App) error {
		c := input
		target, err := tx.FindCollectionByNameOrId(c.Collection)
		if err != nil {
			return err
		}
		if !target.IsBase() || target.System || managedName(target.Name) {
			return errInvalid("only ordinary base collections support variants")
		}
		c.Collection = target.Id
		auth, err := tx.FindCollectionByNameOrId(c.AuthCollection)
		if err != nil {
			return err
		}
		if !auth.IsAuth() || auth.System {
			return errInvalid("select a non-system auth collection")
		}
		c.AuthCollection = auth.Id
		old, loadErr := Load(tx, target.Id)
		if loadErr != nil && !errors.Is(loadErr, sql.ErrNoRows) {
			return loadErr
		}
		if lockRules {
			list, view := target.ListRule, target.ViewRule
			if old != nil {
				list, view = old.ListRule, old.ViewRule
			}
			if !sameRule(c.ListRule, list) || !sameRule(c.ViewRule, view) {
				return ErrAdminRulesLocked
			}
		}
		if old == nil {
			if c.Version != 0 {
				return errInvalid("configuration changed; reload before publishing")
			}
			if target.Fields.GetByName(SetField) != nil {
				return errInvalid("field %s already exists", SetField)
			}
			c.ListRule = target.ListRule
			c.ViewRule = target.ViewRule
		} else {
			if c.Version != old.Version {
				return errInvalid("configuration changed; reload before publishing")
			}
			if old.AuthCollection != c.AuthCollection {
				return errInvalid("auth collection cannot change after initialization")
			}
		}
		c.Version++
		if c.Default.Key == "" {
			c.Default.Key = "default"
		}
		if c.Default.Name == "" {
			c.Default.Name = "Default"
		}
		if c.Default.Key != "default" || c.Default.Condition != nil {
			return errInvalid("default cannot have a condition or another key")
		}
		if c.Experiments && !c.Variables {
			return errInvalid("experiments require variables")
		}
		if len(c.Variants) > 30 {
			return errInvalid("at most 30 audience variants per collection")
		}
		if err := ensureBucket(tx, auth); err != nil {
			return err
		}
		cs, err := allConfigs(tx)
		if err != nil {
			return err
		}
		personalized := map[string]bool{target.Id: true}
		for _, x := range cs {
			personalized[x.Collection] = true
		}
		b := &compiler{app: tx, personalized: personalized}
		query, err := compileConfig(b, &c, auth)
		if err != nil {
			return err
		}
		setCollection, err := tx.FindCollectionByNameOrId(sets)
		if err != nil {
			return err
		}
		// Old sets remain addressable to preserve records and experiment history.
		existing, err := tx.FindAllRecords(sets, dbx.HashExp{"collection": target.Id})
		if err != nil {
			return err
		}
		for _, s := range existing {
			s.Set("active", false)
			if err := save(tx, s); err != nil {
				return err
			}
		}
		for _, v := range append([]Variant{c.Default}, c.Variants...) {
			if err := upsertSet(tx, setCollection, c.Collection, v, "", "", v.Name+" / base"); err != nil {
				return err
			}
			for _, ex := range v.Experiments {
				for _, g := range ex.Groups {
					if err := upsertSet(tx, setCollection, c.Collection, v, ex.Key, g.Key, v.Name+" / "+ex.Name+" / "+g.Name); err != nil {
						return err
					}
				}
			}
		}
		if err := saveView(tx, &c, query); err != nil {
			return err
		}
		if old == nil {
			target.Fields.Add(&core.RelationField{Name: SetField, CollectionId: setCollection.Id, MaxSelect: 1})
			target.AddIndex(setIndexName(target.Id), false, SetField, "")
		}
		guard := accessGuard(c)
		target.ListRule = wrapRule(c.ListRule, guard)
		target.ViewRule = wrapRule(c.ViewRule, guard)
		target.CreateRule = nil
		target.UpdateRule = nil
		target.DeleteRule = nil
		if err := save(tx, target); err != nil {
			return err
		}
		if old == nil {
			_, err = tx.DB().NewQuery("UPDATE " + ident(target.Name) + " SET " + ident(SetField) + " = {:set} WHERE " + ident(SetField) + " = '' OR " + ident(SetField) + " IS NULL").Bind(dbx.Params{"set": setID(c.Collection, "default", "", "")}).Execute()
			if err != nil {
				return err
			}
		}
		cr, err := tx.FindRecordById(configs, digest(target.Id)[:15])
		if errors.Is(err, sql.ErrNoRows) {
			cc, e := tx.FindCollectionByNameOrId(configs)
			if e != nil {
				return e
			}
			cr = core.NewRecord(cc)
			cr.Id = digest(target.Id)[:15]
		} else if err != nil {
			return err
		}
		cr.Set("definition", c)
		if err := save(tx, cr); err != nil {
			return err
		}
		// A newly personalized collection may invalidate an existing audience dependency.
		for _, other := range cs {
			if other.Collection == target.Id {
				continue
			}
			a, e := tx.FindCollectionByNameOrId(other.AuthCollection)
			if e != nil {
				return e
			}
			if _, e = compileConfig(&compiler{app: tx, personalized: personalized}, other, a); e != nil {
				return e
			}
		}
		if err := validateRules(tx, &c); err != nil {
			return err
		}
		result = &c
		return nil
	})
	return result, err
}

func ensureBucket(app core.App, auth *core.Collection) error {
	if f := auth.Fields.GetByName(BucketField); f != nil {
		n, ok := f.(*core.NumberField)
		if !ok || !n.Hidden || !n.OnlyInt || n.Min == nil || *n.Min != 0 || n.Max == nil || *n.Max != 10000 {
			return errInvalid("incompatible existing content_bucket field")
		}
		return nil
	}
	min, max := float64(0), float64(10000)
	auth.Fields.Add(&core.NumberField{Name: BucketField, Hidden: true, OnlyInt: true, Min: &min, Max: &max})
	return save(app, auth)
}
func upsertSet(app core.App, sc *core.Collection, collection string, v Variant, ex, g, name string) error {
	id := setID(collection, v.Key, ex, g)
	r, err := app.FindRecordById(sc, id)
	if errors.Is(err, sql.ErrNoRows) {
		r = core.NewRecord(sc)
		r.Id = id
	} else if err != nil {
		return err
	}
	r.Set("collection", collection)
	r.Set("variant", v.Key)
	r.Set("experiment", ex)
	r.Set("group", g)
	r.Set("name", name)
	r.Set("active", true)
	return save(app, r)
}
func saveView(app core.App, c *Config, query string) error {
	v, err := app.FindCollectionByNameOrId(viewName(c.Collection))
	if errors.Is(err, sql.ErrNoRows) {
		v = core.NewViewCollection(viewName(c.Collection))
	} else if err != nil {
		return err
	}
	v.ViewQuery = query
	v.System = true
	return save(app, v)
}
func accessGuard(c Config) string {
	base := setID(c.Collection, "default", "", "")
	// The identity predicate is null-rejecting for authenticated users, allowing
	// SQLite to flatten the view into indexed joins even with native DISTINCT.
	// For guests the resolver reduces both operands to empty values without joins.
	v := "@request.auth." + viewName(c.Collection) + "_via_user"
	return fmt.Sprintf(`%s.user ?= @request.auth.id && (((@request.auth.id = "") && content_set = "%s") || (@request.auth.id != "" && @request.auth.collectionId = "%s" && content_set ?= %s.content_set))`, v, base, c.AuthCollection, v)
}
func compileConfig(b *compiler, c *Config, auth *core.Collection) (string, error) {
	bucketExpr := "u." + ident(BucketField)
	if b.previewBucket > 0 {
		bucketExpr = fmt.Sprint(b.previewBucket)
	}
	seen := map[string]bool{"default": true}
	for _, v := range c.Variants {
		if !validKey.MatchString(v.Key) || seen[v.Key] || v.Condition == nil {
			return "", errInvalid("variants require unique keys and conditions")
		}
		seen[v.Key] = true
	}
	choices := make(map[string]string)
	for _, v := range append([]Variant{c.Default}, c.Variants...) {
		base := sqlString(setID(c.Collection, v.Key, "", ""))
		arms := []string{}
		active := 0
		exKeys := map[string]bool{}
		if len(v.Experiments) > 30 {
			return "", errInvalid("at most 30 experiments per variant")
		}
		for _, ex := range v.Experiments {
			if !validKey.MatchString(ex.Key) || exKeys[ex.Key] {
				return "", errInvalid("experiment keys must be unique within variant")
			}
			exKeys[ex.Key] = true
			if ex.Active {
				active++
			}
			if active > 1 {
				return "", errInvalid("only one active experiment per variant")
			}
			if len(ex.Groups) < 2 || len(ex.Groups) > 30 {
				return "", errInvalid("an experiment needs 2–30 groups")
			}
			occupied := make([]bool, 10001)
			gKeys := map[string]bool{}
			for _, g := range ex.Groups {
				if !validKey.MatchString(g.Key) || gKeys[g.Key] || g.From < 1 || g.To > 10000 || g.From > g.To {
					return "", errInvalid("invalid group key or bucket range")
				}
				gKeys[g.Key] = true
				for i := g.From; i <= g.To; i++ {
					if occupied[i] {
						return "", errInvalid("overlapping bucket ranges")
					}
					occupied[i] = true
				}
				if c.Variables && c.Experiments && ex.Active {
					arms = append(arms, fmt.Sprintf("WHEN %s BETWEEN %d AND %d THEN %s", bucketExpr, g.From, g.To, sqlString(setID(c.Collection, v.Key, ex.Key, g.Key))))
				}
			}
		}
		choices[v.Key] = base
		if len(arms) > 0 {
			choices[v.Key] = "CASE " + strings.Join(arms, " ") + " ELSE " + base + " END"
		}
	}
	clauses := []string{}
	for _, v := range c.Variants {
		p, err := b.condition(auth, "u", *v.Condition, 0)
		if err != nil {
			return "", err
		}
		if c.Variables {
			clauses = append(clauses, "WHEN ("+p+") THEN "+choices[v.Key])
		}
	}
	expr := choices["default"]
	if len(clauses) > 0 {
		expr = "CASE " + strings.Join(clauses, " ") + " ELSE " + expr + " END"
	}
	return "SELECT u.id AS id, u.id AS user, CAST((" + expr + ") AS TEXT) AS content_set FROM " + ident(auth.Name) + " AS u", nil
}

// validateRules additionally exercises the same parser used by native HTTP and realtime.
func validateRules(app core.App, c *Config) error {
	target, err := app.FindCollectionByNameOrId(c.Collection)
	if err != nil {
		return err
	}
	auth, err := app.FindCollectionByNameOrId(c.AuthCollection)
	if err != nil {
		return err
	}
	for _, a := range []*core.Record{nil, core.NewRecord(auth)} {
		for _, rule := range []*string{target.ListRule, target.ViewRule} {
			if rule == nil {
				continue
			}
			r := core.NewRecordFieldResolver(app, target, &core.RequestInfo{Auth: a}, true)
			if _, err := search.FilterData(*rule).BuildExpr(r); err != nil {
				return err
			}
		}
	}
	return nil
}
