package push

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

type runDefinition struct {
	Campaign      Campaign            `json:"campaign"`
	Audiences     map[string]Audience `json:"audiences"`
	ApplicationID int64               `json:"applicationId"`
	SendRate      int                 `json:"sendRate"`
	GroupID       int64               `json:"groupId"`
	OpenToken     string              `json:"openToken"`
	InputHash     string              `json:"inputHash"`
	Test          bool                `json:"test"`
	TestDeviceIDs []string            `json:"testDeviceIds,omitempty"`
}
type jobDefinition struct {
	Recipients []Recipient `json:"recipients"`
}

func (p *Plugin) Launch(app core.App, in Launch) (map[string]any, error) {
	if len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 120 {
		return nil, textError("ключ запуска должен содержать 8–120 символов")
	}
	if len(in.TestDeviceIDs) > 20 {
		return nil, textError("не более 20 тестовых устройств")
	}
	when := time.Now().UTC()
	if in.ScheduledAt != "" {
		var err error
		when, err = time.Parse(time.RFC3339, in.ScheduledAt)
		if err != nil {
			return nil, textError("некорректное время запуска")
		}
	}
	input, _ := json.Marshal(in)
	fingerprint := hash(string(input))
	var result map[string]any
	err := app.RunInTransaction(func(tx core.App) error {
		old, err := tx.FindFirstRecordByFilter(RunsCollection, "requestKey={:key}", dbx.Params{"key": in.IdempotencyKey})
		if err == nil {
			var d runDefinition
			if err = decodeRecord(old, &d); err != nil {
				return err
			}
			if d.InputHash != fingerprint {
				return textError("ключ запуска уже использован с другими параметрами")
			}
			result = runSummary(old)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		c, a, err := loadCampaign(tx, in.CampaignID)
		if err != nil {
			return err
		}
		if c.Version != in.Version {
			return textError("версия кампании изменилась")
		}
		cfg, err := Load(tx)
		if err != nil {
			return err
		}
		if cfg.ApplicationID <= 0 || cfg.OAuthToken == "" {
			return textError("сначала настройте AppMetrica")
		}
		coll, err := tx.FindCollectionByNameOrId(RunsCollection)
		if err != nil {
			return err
		}
		r := core.NewRecord(coll)
		d := runDefinition{Campaign: c, Audiences: a, ApplicationID: cfg.ApplicationID, SendRate: cfg.SendRate, OpenToken: secret(), InputHash: fingerprint, Test: len(in.TestDeviceIDs) > 0, TestDeviceIDs: in.TestDeviceIDs}
		r.Set("campaignId", c.ID)
		r.Set("name", c.Name)
		r.Set("requestKey", in.IdempotencyKey)
		r.Set("status", "scheduled")
		r.Set("scheduledAt", when)
		r.Set("openHash", hash(d.OpenToken))
		r.Set("definition", d)
		if err = save(tx, r); err != nil {
			return err
		}
		if !when.After(time.Now()) {
			if err = p.prepare(tx, r, d); err != nil {
				return err
			}
		}
		result = runSummary(r)
		return nil
	})
	return result, err
}
func (p *Plugin) prepare(app core.App, run *core.Record, d runDefinition) error {
	query, err := p.selection(app, d.Campaign, d.Audiences)
	if err != nil {
		return err
	}
	if d.Test {
		ids := []string{}
		for _, id := range d.TestDeviceIDs {
			if len(id) != 15 {
				return textError("некорректный ID тестового устройства")
			}
			ids = append(ids, literal(id))
		}
		query = "SELECT d.id,d.deviceId,d.userId,d.authCollection,d.generation,d.platform FROM push_devices d WHERE " + eligibleDevice("d") + " AND d.id IN (" + strings.Join(ids, ",") + ")"
	}
	collection, err := app.FindCollectionByNameOrId(jobsCollection)
	if err != nil {
		return err
	}
	last := ""
	total := 0
	for {
		var recipients []Recipient
		if err = app.DB().NewQuery(query + " AND d.id>{:last} ORDER BY d.id LIMIT " + strconv.Itoa(batchSize)).Bind(dbx.Params{"last": last}).All(&recipients); err != nil {
			return err
		}
		if len(recipients) == 0 {
			break
		}
		last = recipients[len(recipients)-1].ID
		total += len(recipients)
		job := core.NewRecord(collection)
		job.Set("runId", run.Id)
		job.Set("status", "queued")
		job.Set("definition", jobDefinition{recipients})
		var b [8]byte
		if _, err = rand.Read(b[:]); err != nil {
			return err
		}
		job.Set("clientId", strconv.FormatUint((binary.BigEndian.Uint64(b[:])&((1<<63)-1))|1, 10))
		if err = save(app, job); err != nil {
			return err
		}
		if !d.Test {
			snapshot, _ := json.Marshal(recipients)
			_, err = app.DB().NewQuery(`UPDATE push_devices SET lastSent={:now} WHERE id IN (SELECT json_extract(value,'$.id') FROM json_each({:recipients}))`).Bind(dbx.Params{"now": types.NowDateTime(), "recipients": string(snapshot)}).Execute()
			if err != nil {
				return err
			}
		}
	}
	status := "queued"
	if total == 0 {
		status = "empty"
	}
	run.Set("status", status)
	run.Set("recipients", total)
	return save(app, run)
}
func runSummary(r *core.Record) map[string]any {
	return map[string]any{"id": r.Id, "campaignId": r.GetString("campaignId"), "name": r.GetString("name"), "status": r.GetString("status"), "recipients": r.GetInt("recipients"), "scheduledAt": r.GetString("scheduledAt"), "created": r.GetString("created"), "error": r.GetString("error")}
}

// Process services a bounded amount of persistent work. No HTTP request waits
// for a campaign; the cron owns dispatch. Unknown sends are never blindly retried.
func (p *Plugin) Process(ctx context.Context) error {
	if !p.worker.TryLock() {
		return nil
	}
	defer p.worker.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	runs, err := p.app.FindRecordsByFilter(RunsCollection, "status='scheduled' && scheduledAt<={:now}", "scheduledAt", 5, 0, dbx.Params{"now": types.NowDateTime()})
	if err != nil {
		return err
	}
	for _, run := range runs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = p.app.RunInTransaction(func(tx core.App) error {
			r, err := tx.FindRecordById(RunsCollection, run.Id)
			if err != nil {
				return err
			}
			if r.GetString("status") != "scheduled" {
				return nil
			}
			var d runDefinition
			if err = decodeRecord(r, &d); err != nil {
				return err
			}
			return p.prepare(tx, r, d)
		})
		if err != nil {
			return err
		}
	}
	jobs, err := p.app.FindRecordsByFilter(jobsCollection, "(status='queued'||status='unknown'||status='submitted') && (nextAttempt=''||nextAttempt<={:now})", "nextAttempt,created", 10, 0, dbx.Params{"now": types.NowDateTime()})
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = p.processJob(ctx, job.Id); err != nil {
			return err
		}
	}
	return nil
}
func (p *Plugin) processJob(ctx context.Context, id string) error {
	job, err := p.app.FindRecordById(jobsCollection, id)
	if err != nil {
		return err
	}
	run, err := p.app.FindRecordById(RunsCollection, job.GetString("runId"))
	if err != nil {
		return err
	}
	if run.GetString("status") == "cancelled" {
		return nil
	}
	var d runDefinition
	if err = decodeRecord(run, &d); err != nil {
		return err
	}
	cfg, err := Load(p.app)
	if err != nil {
		return err
	}
	if cfg.ApplicationID != d.ApplicationID {
		return textError("приложение изменилось")
	}
	status := job.GetString("status")
	if status == "queued" {
		if d.GroupID == 0 {
			group, err := p.ensureGroup(ctx, cfg, d, run.Id)
			if err != nil {
				return p.deferJob(job, diagnostic("Подготовка группы: "+err.Error(), cfg.OAuthToken, d.OpenToken))
			}
			d.GroupID = group
			run.Set("definition", d)
			if err = save(p.app, run); err != nil {
				return err
			}
		}
		// Claim transaction also rechecks device ownership and consent immediately
		// before sending. Network I/O never runs inside the SQLite transaction.
		claimed := false
		err = p.app.RunInTransaction(func(tx core.App) error {
			fresh, err := tx.FindRecordById(jobsCollection, id)
			if err != nil {
				return err
			}
			if fresh.GetString("status") != "queued" {
				return nil
			}
			job = fresh
			var j jobDefinition
			if err = decodeRecord(job, &j); err != nil {
				return err
			}
			valid := make([]Recipient, 0, len(j.Recipients))
			snapshot, _ := json.Marshal(j.Recipients)
			owners := []string{"0"}
			for _, name := range p.options.AuthCollections {
				c, err := tx.FindCollectionByNameOrId(name)
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				if err != nil {
					return err
				}
				if c.IsAuth() && !c.System {
					owners = append(owners, "(d.authCollection="+literal(c.Id)+" AND EXISTS (SELECT 1 FROM "+ident(c.Name)+" u WHERE u.id=d.userId))")
				}
			}
			err = tx.DB().NewQuery(`SELECT d.id,d.deviceId,d.userId,d.authCollection,d.generation,d.platform FROM json_each({:recipients}) r JOIN push_devices d ON d.id=json_extract(r.value,'$.id') WHERE ` + eligibleDevice("d") + ` AND d.userId=json_extract(r.value,'$.userId') AND d.authCollection=json_extract(r.value,'$.authCollection') AND d.generation=json_extract(r.value,'$.generation') AND (` + strings.Join(owners, " OR ") + `)`).Bind(dbx.Params{"recipients": string(snapshot)}).All(&valid)
			if err != nil {
				return err
			}
			j.Recipients = valid
			job.Set("definition", j)
			job.Set("groupId", strconv.FormatInt(d.GroupID, 10))
			job.Set("status", "unknown")
			if len(valid) == 0 {
				job.Set("status", "sent")
			} else {
				claimed = true
			}
			job.Set("nextAttempt", time.Now().Add(time.Minute))
			return save(tx, job)
		})
		if err != nil {
			return err
		}
		if claimed {
			transfer, err := p.sendBatch(ctx, cfg, d, run.Id, job)
			if err != nil {
				var remote *remoteError
				if errors.As(err, &remote) && remote.status == 429 {
					job.Set("status", "queued")
				}
				if errors.As(err, &remote) && remote.status >= 400 && remote.status < 500 && remote.status != 429 && remote.status != 408 {
					job.Set("status", "failed")
				}
				message := "Отправка не подтверждена; проверяем статус без повторной отправки. "
				if job.GetString("status") == "failed" {
					message = "Отправка отклонена. "
				}
				if job.GetString("status") == "queued" {
					message = "Лимит запросов; отправка будет повторена. "
				}
				job.Set("error", diagnostic(message+err.Error(), cfg.OAuthToken, d.OpenToken))
			} else {
				job.Set("status", "submitted")
				job.Set("transferId", strconv.FormatInt(transfer, 10))
				job.Set("error", "")
			}
			if err = save(p.app, job); err != nil {
				return err
			}
		}
	} else {
		group, _ := strconv.ParseInt(job.GetString("groupId"), 10, 64)
		transfer, state, detail, err := p.transferStatus(ctx, cfg, group, job.GetString("clientId"), d.OpenToken)
		if err != nil {
			return p.deferJob(job, diagnostic("Проверка статуса: "+err.Error(), cfg.OAuthToken, d.OpenToken))
		}
		switch state {
		case "sent", "failed":
			job.Set("status", state)
		case "pending", "in_progress":
			job.Set("status", "submitted")
		default:
			return p.deferJob(job, "AppMetrica: неизвестный статус")
		}
		job.Set("transferId", strconv.FormatInt(transfer, 10))
		message := ""
		if state == "failed" {
			message = "AppMetrica отклонила отправку: " + detail
			if detail == "" {
				message = "AppMetrica отклонила отправку, но не вернула причину. Проверьте настройки отправителей FCM/APNs в AppMetrica."
			}
		}
		job.Set("error", diagnostic(message, cfg.OAuthToken, d.OpenToken))
		job.Set("nextAttempt", time.Now().Add(time.Minute))
		if err = save(p.app, job); err != nil {
			return err
		}
	}
	return p.updateRun(run.Id)
}
func (p *Plugin) deferJob(job *core.Record, message string) error {
	job.Set("error", message)
	attempts := job.GetInt("attempts") + 1
	job.Set("attempts", attempts)
	job.Set("nextAttempt", time.Now().Add(time.Duration(min(attempts, 30))*time.Minute))
	if err := save(p.app, job); err != nil {
		return err
	}
	return p.updateRun(job.GetString("runId"))
}
func (p *Plugin) updateRun(id string) error {
	return p.app.RunInTransaction(func(tx core.App) error {
		r, err := tx.FindRecordById(RunsCollection, id)
		if err != nil {
			return err
		}
		if r.GetString("status") == "cancelled" {
			return nil
		}
		rows, err := tx.FindRecordsByFilter(jobsCollection, "runId={:run}", "", 0, 0, dbx.Params{"run": id})
		if err != nil {
			return err
		}
		state := "sent"
		message := ""
		for _, j := range rows {
			if j.GetString("error") != "" {
				message = j.GetString("error")
			}
			switch j.GetString("status") {
			case "unknown":
				state = "unknown"
			case "queued", "submitted":
				if state != "unknown" {
					state = "sending"
				}
			case "failed":
				if state == "sent" {
					state = "failed"
				}
			}
		}
		r.Set("status", state)
		r.Set("error", message)
		return save(tx, r)
	})
}
func (p *Plugin) Cancel(app core.App, id string) error {
	return app.RunInTransaction(func(tx core.App) error {
		r, err := tx.FindRecordById(RunsCollection, id)
		if err != nil {
			return err
		}
		if r.GetString("status") != "scheduled" {
			return textError("отменить можно только запланированный запуск до начала отправки")
		}
		r.Set("status", "cancelled")
		return save(tx, r)
	})
}
func (p *Plugin) Runs(app core.App) ([]map[string]any, error) {
	rr, err := app.FindRecordsByFilter(RunsCollection, "", "-created", 100, 0)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rr))
	for _, r := range rr {
		out = append(out, runSummary(r))
	}
	return out, nil
}
func (p *Plugin) Report(app core.App, id string) (map[string]any, error) {
	r, err := app.FindRecordById(RunsCollection, id)
	if err != nil {
		return nil, textError("запуск не найден")
	}
	result := runSummary(r)
	var opens int
	if err = app.DB().NewQuery("SELECT count(*) FROM push_opens WHERE runId={:id}").Bind(dbx.Params{"id": id}).Row(&opens); err != nil {
		return nil, err
	}
	result["opens"] = opens
	var d runDefinition
	if err = decodeRecord(r, &d); err != nil {
		return nil, err
	}
	result["appmetricaGroupId"] = strconv.FormatInt(d.GroupID, 10)
	result["test"] = d.Test
	jobs, err := app.FindRecordsByFilter(jobsCollection, "runId={:id}", "created,id", 0, 0, dbx.Params{"id": id})
	if err != nil {
		return nil, err
	}
	batches := make([]map[string]any, 0, len(jobs))
	counts := map[string]int{}
	for _, job := range jobs {
		var definition jobDefinition
		if err = decodeRecord(job, &definition); err != nil {
			return nil, err
		}
		status := job.GetString("status")
		counts[status]++
		next := ""
		if status == "queued" || status == "submitted" || status == "unknown" {
			next = job.GetString("nextAttempt")
		}
		batches = append(batches, map[string]any{
			"id": job.Id, "status": status, "recipients": len(definition.Recipients),
			"error": job.GetString("error"), "clientTransferId": job.GetString("clientId"),
			"transferId": job.GetString("transferId"), "appmetricaGroupId": job.GetString("groupId"),
			"deferrals": job.GetInt("attempts"), "nextAttempt": next, "updated": job.GetString("updated"),
		})
	}
	result["jobs"] = batches
	result["jobCounts"] = counts
	if _, err := app.FindCollectionByNameOrId("conversations"); err == nil {
		type metric struct {
			Status   string  `db:"status" json:"status"`
			Currency string  `db:"currency" json:"currency"`
			Count    int     `db:"count" json:"count"`
			Amount   float64 `db:"amount" json:"amount"`
		}
		rows := []metric{}
		err = app.DB().NewQuery(`SELECT status,currency,count(*) count,sum(CAST(amount AS REAL)) amount FROM conversations WHERE json_extract(clickData,'$.push.runId')={:id} GROUP BY status,currency`).Bind(dbx.Params{"id": id}).All(&rows)
		if err != nil {
			return nil, err
		}
		result["conversions"] = rows
	}
	return result, nil
}

// RefreshReport recovers diagnostics for failed transfers, including legacy runs.
// It only queries provider status and never sends or retries a notification.
func (p *Plugin) RefreshReport(ctx context.Context, app core.App, id string) (map[string]any, error) {
	if !p.worker.TryLock() {
		return nil, textError("обрабатывается очередь; повторите обновление позже")
	}
	defer p.worker.Unlock()
	run, err := app.FindRecordById(RunsCollection, id)
	if err != nil {
		return nil, textError("запуск не найден")
	}
	var d runDefinition
	if err = decodeRecord(run, &d); err != nil {
		return nil, err
	}
	cfg, err := Load(app)
	if err != nil {
		return nil, err
	}
	if cfg.ApplicationID != d.ApplicationID {
		return nil, textError("приложение изменилось")
	}
	jobs, err := app.FindRecordsByFilter(jobsCollection, "runId={:id} && status='failed' && error=''", "created,id", 20, 0, dbx.Params{"id": id})
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		group, _ := strconv.ParseInt(job.GetString("groupId"), 10, 64)
		if group == 0 || job.GetString("clientId") == "" {
			continue
		}
		_, state, detail, err := p.transferStatus(ctx, cfg, group, job.GetString("clientId"), d.OpenToken)
		if err != nil {
			return nil, textError(diagnostic("Не удалось запросить причину: "+err.Error(), cfg.OAuthToken, d.OpenToken))
		}
		if state != "failed" {
			continue
		}
		if detail == "" {
			detail = "провайдер не вернул причину; проверьте настройки отправителей FCM/APNs в AppMetrica"
		}
		job.Set("error", diagnostic("AppMetrica отклонила отправку: "+detail, cfg.OAuthToken, d.OpenToken))
		if err = save(app, job); err != nil {
			return nil, err
		}
	}
	if len(jobs) > 0 {
		if err = p.updateRun(id); err != nil {
			return nil, err
		}
	}
	return p.Report(app, id)
}
