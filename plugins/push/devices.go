package push

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

type DeviceInput struct {
	ID         string `json:"id,omitempty"`
	Secret     string `json:"secret"`
	DeviceID   string `json:"deviceId"`
	Platform   string `json:"platform"`
	Language   string `json:"language"`
	AppVersion string `json:"appVersion"`
	Enabled    bool   `json:"enabled"`
}

var deviceIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)
var secretPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var recordIDPattern = regexp.MustCompile(`^[a-z0-9]{15}$`)

func (p *Plugin) RegisterDevice(app core.App, user *core.Record, in DeviceInput) (string, error) {
	if !p.allowedUser(user) {
		return "", textError("требуется пользователь подключённой auth-коллекции")
	}
	if !secretPattern.MatchString(in.Secret) || !deviceIDPattern.MatchString(in.DeviceID) || !slices.Contains([]string{"android", "ios"}, in.Platform) || len(in.Language) > 32 || len(in.AppVersion) > 64 {
		return "", textError("некорректные данные устройства")
	}
	id := ""
	err := app.RunInTransaction(func(tx core.App) error {
		c, err := tx.FindCollectionByNameOrId(DevicesCollection)
		if err != nil {
			return err
		}
		var r *core.Record
		if in.ID == "" {
			r = core.NewRecord(c)
		} else {
			r, err = tx.FindRecordById(c, in.ID)
			if errors.Is(err, sql.ErrNoRows) && recordIDPattern.MatchString(in.ID) {
				r = core.NewRecord(c)
				r.Id = in.ID
			} else if err != nil || subtle.ConstantTimeCompare([]byte(r.GetString("secretHash")), []byte(hash(in.Secret))) != 1 {
				return textError("устройство недоступно")
			}
		}
		if r.GetString("userId") != user.Id || r.GetString("authCollection") != user.Collection().Id || r.GetBool("enabled") != in.Enabled || r.GetString("deviceId") != in.DeviceID {
			r.Set("generation", secret())
		}
		r.Set("deviceId", in.DeviceID)
		r.Set("userId", user.Id)
		r.Set("authCollection", user.Collection().Id)
		r.Set("platform", in.Platform)
		r.Set("language", in.Language)
		r.Set("appVersion", in.AppVersion)
		r.Set("enabled", in.Enabled)
		r.Set("secretHash", hash(in.Secret))
		r.Set("lastSeen", types.NowDateTime())
		if err = save(tx, r); err != nil {
			return textError("не удалось зарегистрировать устройство; проверьте привязку")
		}
		id = r.Id
		return nil
	})
	return id, err
}
func (p *Plugin) allowedUser(user *core.Record) bool {
	return user != nil && !user.IsSuperuser() && (slices.Contains(p.options.AuthCollections, user.Collection().Id) || slices.Contains(p.options.AuthCollections, user.Collection().Name))
}
func (p *Plugin) device(app core.App, user *core.Record, id, credential string) (*core.Record, error) {
	if !p.allowedUser(user) {
		return nil, textError("требуется пользователь")
	}
	r, err := app.FindRecordById(DevicesCollection, id)
	if err != nil || r.GetString("userId") != user.Id || r.GetString("authCollection") != user.Collection().Id || subtle.ConstantTimeCompare([]byte(r.GetString("secretHash")), []byte(hash(credential))) != 1 {
		return nil, textError("устройство недоступно")
	}
	return r, nil
}
func (p *Plugin) DisableDevice(app core.App, user *core.Record, id, credential string) error {
	if !p.allowedUser(user) || !recordIDPattern.MatchString(id) || !secretPattern.MatchString(credential) {
		return textError("устройство недоступно")
	}
	return app.RunInTransaction(func(tx core.App) error {
		// Credentials are persisted before registration; its request may never
		// reach the server. There is no binding to retire in that case.
		if _, err := tx.FindRecordById(DevicesCollection, id); errors.Is(err, sql.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		r, err := p.device(tx, user, id, credential)
		if err != nil {
			return err
		}
		r.Set("enabled", false)
		r.Set("generation", secret())
		return save(tx, r)
	})
}

type OpenInput struct {
	DeviceID string `json:"deviceId"`
	Secret   string `json:"secret"`
	RunID    string `json:"runId"`
	Token    string `json:"token"`
}

func (p *Plugin) TrackOpen(app core.App, user *core.Record, in OpenInput) error {
	return app.RunInTransaction(func(tx core.App) error {
		d, err := p.device(tx, user, in.DeviceID, in.Secret)
		if err != nil {
			return err
		}
		r, err := tx.FindRecordById(RunsCollection, in.RunID)
		if err != nil || subtle.ConstantTimeCompare([]byte(r.GetString("openHash")), []byte(hash(in.Token))) != 1 {
			return textError("отправка недоступна")
		}
		var definition runDefinition
		if err = decodeRecord(r, &definition); err != nil {
			return err
		}
		if definition.Test {
			return nil
		}
		var count int
		err = tx.DB().NewQuery(`SELECT count(*) FROM push_jobs j, json_each(j.definition,'$.recipients') r WHERE j.runId={:run} AND j.status IN ('submitted','sent','unknown') AND json_extract(r.value,'$.id')={:device} AND json_extract(r.value,'$.generation')={:generation} AND json_extract(r.value,'$.userId')={:user} AND json_extract(r.value,'$.authCollection')={:auth}`).Bind(dbx.Params{"run": r.Id, "device": d.Id, "generation": d.GetString("generation"), "user": user.Id, "auth": user.Collection().Id}).Row(&count)
		if err != nil {
			return err
		}
		if count == 0 {
			return textError("устройство не входит в отправку")
		}
		_, err = tx.FindFirstRecordByFilter(opensCollection, "runId={:run} && deviceId={:device}", dbx.Params{"run": r.Id, "device": d.Id})
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		c, err := tx.FindCollectionByNameOrId(opensCollection)
		if err != nil {
			return err
		}
		o := core.NewRecord(c)
		o.Set("runId", r.Id)
		o.Set("userId", user.Id)
		o.Set("authCollection", user.Collection().Id)
		o.Set("deviceId", d.Id)
		if definition.Campaign.Message.Action == "partner" {
			o.Set("target", definition.Campaign.Message.Target)
		}
		return save(tx, o)
	})
}

// Attribution is suitable for partnerlinks.Options.Attribution. The snapshot is
// taken when the partner link is issued and remains attached to later postbacks.
func Attribution(app core.App, user *core.Record, linkID string) (map[string]string, error) {
	if user == nil {
		return nil, nil
	}
	rr, err := app.FindRecordsByFilter(opensCollection, "userId={:user} && authCollection={:auth} && (target='' || target={:link}) && created>={:since}", "-created,-id", 1, 0, dbx.Params{"user": user.Id, "auth": user.Collection().Id, "link": linkID, "since": time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)})
	if err != nil {
		return nil, err
	}
	if len(rr) == 0 {
		return nil, nil
	}
	r, err := app.FindRecordById(RunsCollection, rr[0].GetString("runId"))
	if err != nil {
		return nil, err
	}
	return map[string]string{"campaignId": r.GetString("campaignId"), "runId": r.Id}, nil
}
