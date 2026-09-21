package variants

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Resolve uses the same virtual view that native rules use. It never records history.
func Resolve(app core.App, c *Config, user *core.Record) (Decision, error) {
	if user != nil && user.Collection().Id != c.AuthCollection {
		return Decision{}, errInvalid("user is not from the configured auth collection")
	}
	id := setID(c.Collection, "default", "", "")
	bucket := 0
	if user != nil && user.Collection().Id == c.AuthCollection {
		bucket = user.GetInt(BucketField)
		source := ident(viewName(c.Collection))
		if bucket == 0 {
			fresh, err := app.FindRecordById(c.AuthCollection, user.Id)
			if err != nil {
				return Decision{}, err
			}
			bucket = fresh.GetInt(BucketField)
			if bucket == 0 {
				bucket = Bucket(c.AuthCollection, user.Id)
				query, err := compileConfig(&compiler{app: app, personalized: map[string]bool{c.Collection: true}, previewBucket: bucket}, c, fresh.Collection())
				if err != nil {
					return Decision{}, err
				}
				source = "(" + query + ")"
			}
		}
		err := app.DB().NewQuery("SELECT content_set FROM " + source + " WHERE id = {:id}").Bind(dbx.Params{"id": user.Id}).Row(&id)
		if err != nil {
			return Decision{}, err
		}
	}
	s, err := app.FindRecordById(sets, id)
	if err != nil {
		return Decision{}, err
	}
	d := Decision{Collection: c.Collection, Variant: s.GetString("variant"), Experiment: s.GetString("experiment"), Group: s.GetString("group"), Set: id, Version: c.Version, Bucket: bucket, Reason: "default"}
	if !c.Variables {
		d.Reason = "variables_disabled"
	} else if d.Variant != "default" {
		d.Reason = "first_matching_audience"
	}
	if d.Experiment != "" {
		d.Reason = "experiment_bucket"
	}
	return d, nil
}
func observeCollection(app core.App, user *core.Record, collection string) error {
	if user == nil || user.IsSuperuser() {
		return nil
	}
	c, err := Load(app, collection)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if user.Collection().Id != c.AuthCollection {
		return nil
	}
	_, err = currentDecision(app, c, user, true)
	return err
}
func currentDecision(app core.App, c *Config, user *core.Record, recordHistory bool) (Decision, error) {
	d, err := Resolve(app, c, user)
	if err != nil {
		return d, err
	}
	if !recordHistory || user == nil || user.IsSuperuser() {
		return d, nil
	}
	unchanged, err := sameObservation(app, user, d)
	if err != nil || unchanged {
		return d, err
	}
	err = app.RunInTransaction(func(tx core.App) error {
		// Read config/decision inside the write transaction to serialize concurrent observations.
		c, err = Load(tx, c.Collection)
		if err != nil {
			return err
		}
		d, err = Resolve(tx, c, user)
		if err != nil {
			return err
		}
		return observe(tx, user, d)
	})
	return d, err
}
func sameObservation(app core.App, user *core.Record, d Decision) (bool, error) {
	payload, err := json.Marshal(d)
	if err != nil {
		return false, err
	}
	id := digest(user.Collection().Id, user.Id, d.Collection)[:15]
	s, err := app.FindRecordById(states, id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return s.GetString("fingerprint") == digest(string(payload)), nil
}
func observeRecordTree(app core.App, user *core.Record, r *core.Record) error {
	if err := observeCollection(app, user, r.Collection().Id); err != nil {
		return err
	}
	for _, value := range r.Expand() {
		switch v := value.(type) {
		case *core.Record:
			if err := observeRecordTree(app, user, v); err != nil {
				return err
			}
		case []*core.Record:
			for _, child := range v {
				if err := observeRecordTree(app, user, child); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func observeJSONTree(app core.App, user *core.Record, value any) error {
	switch v := value.(type) {
	case map[string]any:
		if id, ok := v["collectionId"].(string); ok {
			if err := observeCollection(app, user, id); err != nil {
				return err
			}
		}
		if expand, ok := v["expand"].(map[string]any); ok {
			for _, child := range expand {
				if err := observeJSONTree(app, user, child); err != nil {
					return err
				}
			}
		}
	case []any:
		for _, child := range v {
			if err := observeJSONTree(app, user, child); err != nil {
				return err
			}
		}
	}
	return nil
}
func observe(app core.App, user *core.Record, d Decision) error {
	payload, err := json.Marshal(d)
	if err != nil {
		return err
	}
	fingerprint := digest(string(payload))
	sc, err := app.FindCollectionByNameOrId(states)
	if err != nil {
		return err
	}
	id := digest(user.Collection().Id, user.Id, d.Collection)[:15]
	s, err := app.FindRecordById(sc, id)
	if errors.Is(err, sql.ErrNoRows) {
		s = core.NewRecord(sc)
		s.Id = id
	} else if err != nil {
		return err
	}
	if s.GetString("fingerprint") == fingerprint {
		return nil
	}
	hc, err := app.FindCollectionByNameOrId(history)
	if err != nil {
		return err
	}
	h := core.NewRecord(hc)
	for _, r := range []*core.Record{s, h} {
		r.Set("auth_collection", user.Collection().Id)
		r.Set("user", user.Id)
		r.Set("collection", d.Collection)
		r.Set("fingerprint", fingerprint)
		r.Set("decision", d)
		if err := save(app, r); err != nil {
			return err
		}
	}
	return nil
}
func bindRoutes(e *core.ServeEvent) {
	e.Router.GET("/api/variants/me", func(r *core.RequestEvent) error { return decisionsResponse(r, r.Auth, true) }).Bind(apis.RequireAuth())
	e.Router.GET("/api/variants/me/history", func(r *core.RequestEvent) error { return historyResponse(r, r.Auth) }).Bind(apis.RequireAuth())
	e.Router.GET("/api/variants/admin/users/{auth}/{id}", func(r *core.RequestEvent) error {
		u, err := adminUser(r)
		if err != nil {
			return err
		}
		return decisionsResponse(r, u, false)
	}).Bind(apis.RequireSuperuserAuth())
	e.Router.GET("/api/variants/admin/users/{auth}/{id}/history", func(r *core.RequestEvent) error {
		u, err := adminUser(r)
		if err != nil {
			return err
		}
		return historyResponse(r, u)
	}).Bind(apis.RequireSuperuserAuth())
	e.Router.GET("/api/variants/admin/collections/{collection}", func(r *core.RequestEvent) error {
		col, err := r.App.FindCollectionByNameOrId(r.Request.PathValue("collection"))
		if err != nil {
			return r.NotFoundError("Collection not found", err)
		}
		c, err := Load(r.App, col.Id)
		if errors.Is(err, sql.ErrNoRows) {
			return r.JSON(http.StatusOK, map[string]any{"config": nil, "sets": []any{}})
		}
		if err != nil {
			return err
		}
		ss, err := r.App.FindAllRecords(sets, dbx.HashExp{"collection": col.Id})
		if err != nil {
			return err
		}
		return r.JSON(http.StatusOK, map[string]any{"config": c, "sets": ss})
	}).Bind(apis.RequireSuperuserAuth())
	e.Router.PUT("/api/variants/admin/collections/{collection}", func(r *core.RequestEvent) error {
		var c Config
		if err := r.BindBody(&c); err != nil {
			return r.BadRequestError("Invalid configuration", err)
		}
		c.Collection = r.Request.PathValue("collection")
		out, err := Publish(r.App, c)
		if err != nil {
			return r.BadRequestError(err.Error(), nil)
		}
		return r.JSON(http.StatusOK, out)
	}).Bind(apis.RequireSuperuserAuth())
}
func adminUser(r *core.RequestEvent) (*core.Record, error) {
	c, err := r.App.FindCollectionByNameOrId(r.Request.PathValue("auth"))
	if err != nil || !c.IsAuth() || c.System {
		return nil, r.NotFoundError("Auth collection not found", err)
	}
	u, err := r.App.FindRecordById(c, r.Request.PathValue("id"))
	if err != nil {
		return nil, r.NotFoundError("User not found", err)
	}
	return u, nil
}
func decisionsResponse(r *core.RequestEvent, user *core.Record, recordHistory bool) error {
	cs, err := allConfigs(r.App)
	if err != nil {
		return err
	}
	items := []Decision{}
	filter := r.Request.URL.Query().Get("collection")
	if filter != "" {
		col, err := r.App.FindCollectionByNameOrId(filter)
		if err != nil {
			return r.NotFoundError("Collection not found", err)
		}
		filter = col.Id
	}
	for _, c := range cs {
		if user.Collection().Id != c.AuthCollection {
			continue
		}
		if filter != "" && filter != c.Collection {
			continue
		}
		d, err := currentDecision(r.App, c, user, recordHistory)
		if err != nil {
			return err
		}
		items = append(items, d)
	}
	return r.JSON(http.StatusOK, map[string]any{"items": items})
}
func historyResponse(r *core.RequestEvent, user *core.Record) error {
	page, _ := strconv.Atoi(r.Request.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.Request.URL.Query().Get("perPage"))
	if perPage < 1 {
		perPage = 30
	}
	if perPage > 100 {
		perPage = 100
	}
	if page > 1000000 {
		return r.BadRequestError("Page too large", nil)
	}
	where := dbx.HashExp{"auth_collection": user.Collection().Id, "user": user.Id}
	if filter := r.Request.URL.Query().Get("collection"); filter != "" {
		c, err := r.App.FindCollectionByNameOrId(filter)
		if err != nil {
			return r.NotFoundError("Collection not found", err)
		}
		where["collection"] = c.Id
	}
	var count int
	if err := r.App.RecordQuery(history).AndWhere(where).Select("count(*)").Row(&count); err != nil {
		return err
	}
	items := []*core.Record{}
	if err := r.App.RecordQuery(history).AndWhere(where).OrderBy("created DESC", "id DESC").Limit(int64(perPage)).Offset(int64((page - 1) * perPage)).All(&items); err != nil {
		return err
	}
	return r.JSON(http.StatusOK, map[string]any{"page": page, "perPage": perPage, "totalItems": count, "totalPages": (count + perPage - 1) / perPage, "items": items})
}
