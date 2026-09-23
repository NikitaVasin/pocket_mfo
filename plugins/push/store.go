package push

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/security"
)

var serviceCollections = []string{configCollection, DevicesCollection, AudiencesCollection, CampaignsCollection, RunsCollection, jobsCollection, opensCollection}

func install(app core.App) error {
	return app.RunInTransaction(func(tx core.App) error {
		for _, name := range serviceCollections {
			if old, err := tx.FindCollectionByNameOrId(name); err == nil {
				if !old.System || !old.IsBase() || old.Fields.GetByName("definition") == nil {
					return textError("конфликт служебной коллекции " + name)
				}
				if name == DevicesCollection && old.Fields.GetByName("notificationPermission") == nil {
					old.Fields.Add(&core.SelectField{Name: "notificationPermission", Values: notificationPermissions, MaxSelect: 1})
					if err := save(tx, old); err != nil {
						return err
					}
					if _, err := tx.DB().NewQuery("UPDATE push_devices SET notificationPermission='unknown'").Execute(); err != nil {
						return err
					}
				}
				continue
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			c := core.NewBaseCollection(name)
			c.System = true
			c.Fields.Add(&core.JSONField{Name: "definition", MaxSize: 4 << 20, Hidden: name == configCollection || name == jobsCollection || name == RunsCollection}, &core.TextField{Name: "name", Presentable: true}, &core.AutodateField{Name: "created", OnCreate: true}, &core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
			switch name {
			case DevicesCollection:
				for _, field := range []string{"deviceId", "userId", "authCollection", "platform", "language", "appVersion", "generation"} {
					c.Fields.Add(&core.TextField{Name: field})
				}
				c.Fields.Add(&core.TextField{Name: "secretHash", Hidden: true}, &core.BoolField{Name: "enabled"}, &core.DateField{Name: "lastSeen"}, &core.DateField{Name: "lastSent"})
				c.Fields.Add(&core.SelectField{Name: "notificationPermission", Values: notificationPermissions, MaxSelect: 1})
				c.AddIndex("idx_push_device_identity", true, "deviceId", "")
				c.AddIndex("idx_push_device_user", false, "authCollection,userId", "")
			case RunsCollection:
				for _, field := range []string{"campaignId", "requestKey", "status", "error"} {
					c.Fields.Add(&core.TextField{Name: field})
				}
				c.Fields.Add(&core.TextField{Name: "openHash", Hidden: true}, &core.NumberField{Name: "recipients"}, &core.DateField{Name: "scheduledAt"})
				c.AddIndex("idx_push_run_request", true, "requestKey", "")
			case jobsCollection:
				for _, field := range []string{"runId", "status", "clientId", "groupId", "transferId", "error"} {
					c.Fields.Add(&core.TextField{Name: field})
				}
				c.Fields.Add(&core.NumberField{Name: "attempts"}, &core.DateField{Name: "nextAttempt"})
				c.AddIndex("idx_push_job_run", false, "runId,status", "")
			case opensCollection:
				for _, field := range []string{"runId", "userId", "authCollection", "deviceId", "target"} {
					c.Fields.Add(&core.TextField{Name: field})
				}
				c.AddIndex("idx_push_open_identity", true, "runId,deviceId", "")
				c.AddIndex("idx_push_open_owner", false, "authCollection,userId,created", "")
			}
			if err := save(tx, c); err != nil {
				return err
			}
		}
		return nil
	})
}
func protect(app core.App) {
	guard := func(e *core.RecordEvent) error {
		if slices.Contains(serviceCollections, e.Record.Collection().Name) && !internal(e.Context) {
			return textError("используйте API плагина")
		}
		return e.Next()
	}
	app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{Id: "push", Func: guard})
	app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{Id: "push", Func: guard})
	app.OnRecordDelete().Bind(&hook.Handler[*core.RecordEvent]{Id: "push", Func: guard})
	app.OnRecordEnrich().Bind(&hook.Handler[*core.RecordEnrichEvent]{Id: "push", Func: func(e *core.RecordEnrichEvent) error {
		if e.Record.Collection().Name == configCollection || e.Record.Collection().Name == jobsCollection {
			return textError("служебные данные недоступны")
		}
		if err := e.Next(); err != nil {
			return err
		}
		if slices.Contains(serviceCollections, e.Record.Collection().Name) {
			e.Record.Hide("secretHash", "openHash")
			if e.Record.Collection().Name == RunsCollection {
				e.Record.Hide("definition")
			}
		}
		return nil
	}})
	guardSchema := func(e *core.CollectionEvent) error {
		old, _ := e.App.FindCollectionByNameOrId(e.Collection.Id)
		if (slices.Contains(serviceCollections, e.Collection.Name) || (old != nil && slices.Contains(serviceCollections, old.Name))) && !internal(e.Context) {
			return textError("служебная схема защищена")
		}
		return e.Next()
	}
	app.OnCollectionCreate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "push", Func: guardSchema})
	app.OnCollectionUpdate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "push", Func: guardSchema})
	app.OnCollectionDelete().Bind(&hook.Handler[*core.CollectionEvent]{Id: "push", Func: guardSchema})
}
func loadStored(app core.App) (Config, error) {
	r, err := app.FindRecordById(configCollection, configID)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{SendRate: 1000}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	err = decodeRecord(r, &c)
	c.HasOAuthToken = c.OAuthToken != ""
	return c, err
}

// Load returns effective settings, including values pinned by Go code.
func Load(app core.App) (Config, error) {
	c, err := loadStored(app)
	overlayManaged(app, &c)
	return c, err
}
func Configure(app core.App, in Config) (Config, error) {
	var result Config
	err := app.RunInTransaction(func(tx core.App) error {
		old, err := Load(tx)
		if err != nil {
			return err
		}
		if in.Version != old.Version {
			return textError("настройки изменились; обновите страницу")
		}
		if in.OAuthToken == "" {
			in.OAuthToken = old.OAuthToken
		}
		if err := validateConfig(in); err != nil {
			return err
		}
		if err := checkManaged(tx, in, old); err != nil {
			return err
		}
		if err := checkApplicationChange(tx, old.ApplicationID, in.ApplicationID); err != nil {
			return err
		}

		c, err := tx.FindCollectionByNameOrId(configCollection)
		if err != nil {
			return err
		}
		r, err := tx.FindRecordById(c, configID)
		if errors.Is(err, sql.ErrNoRows) {
			r = core.NewRecord(c)
			r.Id = configID
		} else if err != nil {
			return err
		}
		in.Version++
		in.HasOAuthToken = in.OAuthToken != ""
		stored := in
		if m := managed(tx); m != nil && m.OAuthToken != nil {
			raw, err := loadStored(tx)
			if err != nil {
				return err
			}
			stored.OAuthToken = raw.OAuthToken
			stored.HasOAuthToken = stored.OAuthToken != ""
		}
		r.Set("definition", stored)
		if err = save(tx, r); err != nil {
			return err
		}
		result = in
		return nil
	})
	return result, err
}
func (p *Plugin) SaveAudience(app core.App, in Audience) (Audience, error) {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 120 {
		return in, textError("укажите название аудитории")
	}
	if _, _, err := p.audienceSQL(app, in); err != nil {
		return in, err
	}
	err := saveDefinition(app, AudiencesCollection, &in.ID, &in.Version, in.Name, func() any { return in })
	return in, err
}
func (p *Plugin) SaveCampaign(app core.App, in Campaign) (Campaign, error) {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 120 {
		return in, textError("укажите название кампании")
	}
	if in.AllUsers && len(in.AudienceIDs) > 0 {
		return in, textError("выберите всех пользователей или отдельные аудитории")
	}
	if (!in.AllUsers && len(in.AudienceIDs) == 0) || len(in.AudienceIDs) > 30 || len(in.ExcludeAudienceIDs) > 30 {
		return in, textError("выберите 1–30 аудиторий")
	}
	if in.CooldownHours < 0 || in.CooldownHours > 8760 {
		return in, textError("интервал должен быть от 0 до 8760 часов")
	}
	if err := validateMessage(in.Message); err != nil {
		return in, err
	}
	for _, id := range append(slices.Clone(in.AudienceIDs), in.ExcludeAudienceIDs...) {
		if _, err := app.FindRecordById(AudiencesCollection, id); err != nil {
			return in, textError("аудитория не найдена")
		}
	}
	err := saveDefinition(app, CampaignsCollection, &in.ID, &in.Version, in.Name, func() any { return in })
	return in, err
}
func validateMessage(m Message) error {
	if strings.TrimSpace(m.Title) == "" || strings.TrimSpace(m.Text) == "" || len(m.Title)+len(m.Text)+len(m.Target)+len(m.Image) > 2800 {
		return textError("укажите заголовок и текст, общий размер до 2800 байт")
	}
	if m.Image != "" {
		u, err := url.Parse(m.Image)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return textError("изображение должно иметь HTTPS URL")
		}
	}
	switch m.Action {
	case "app":
		if m.Target != "" {
			return textError("для открытия приложения цель не нужна")
		}
	case "route":
		if !strings.HasPrefix(m.Target, "/") || strings.HasPrefix(m.Target, "//") {
			return textError("маршрут должен начинаться с одного /")
		}
	case "partner":
		if len(m.Target) != 15 {
			return textError("укажите ID партнёрской ссылки")
		}
	default:
		return textError("неизвестное действие")
	}
	return nil
}
func saveDefinition(app core.App, collection string, id *string, version *int, name string, value func() any) error {
	return app.RunInTransaction(func(tx core.App) error {
		c, err := tx.FindCollectionByNameOrId(collection)
		if err != nil {
			return err
		}
		var r *core.Record
		if *id == "" {
			if *version != 0 {
				return textError("новая запись должна иметь version=0")
			}
			r = core.NewRecord(c)
		} else {
			r, err = tx.FindRecordById(c, *id)
			if err != nil {
				return textError("запись не найдена")
			}
			var old struct {
				Version int `json:"version"`
			}
			if err = decodeRecord(r, &old); err != nil {
				return err
			}
			if old.Version != *version {
				return textError("запись изменена другим запросом")
			}
		}
		// PocketBase assigns an ID on NewRecord; assign explicitly before serializing.
		if r.Id == "" {
			r.Id = security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
		}
		*id = r.Id
		*version++
		r.Set("name", name)
		r.Set("definition", value())
		return save(tx, r)
	})
}
func definitions(app core.App, collection string) ([]any, error) {
	rr, err := app.FindRecordsByFilter(collection, "", "-created", 500, 0)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rr))
	for _, r := range rr {
		var v any
		if err = decodeRecord(r, &v); err != nil {
			return nil, fmt.Errorf("invalid stored definition")
		}
		out = append(out, v)
	}
	return out, nil
}
