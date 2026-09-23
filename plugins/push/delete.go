package push

import (
	"context"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// DeleteAudience removes an unused saved audience. Run snapshots remain intact.
func (p *Plugin) DeleteAudience(app core.App, id string, version int) error {
	return deleteDefinition(app, AudiencesCollection, id, version, func(tx core.App) error {
		var references []struct {
			Name string `db:"name"`
		}
		err := tx.DB().NewQuery(`SELECT name FROM push_campaigns c
			WHERE EXISTS (SELECT 1 FROM json_each(c.definition, '$.audienceIds') WHERE value={:id})
			OR EXISTS (SELECT 1 FROM json_each(c.definition, '$.excludeAudienceIds') WHERE value={:id})
			ORDER BY id LIMIT 1`).Bind(dbx.Params{"id": id}).All(&references)
		if err != nil {
			return err
		}
		if len(references) > 0 {
			return fmt.Errorf("аудитория используется в кампании «%s»; сначала уберите её из получателей и исключений или удалите кампанию", references[0].Name)
		}
		return nil
	})
}

// DeleteCampaign removes the editable definition, preserving delivery history,
// snapshots and attribution. Pending and ambiguous runs must finish first.
func (p *Plugin) DeleteCampaign(app core.App, id string, version int) error {
	return deleteDefinition(app, CampaignsCollection, id, version, func(tx core.App) error {
		runs, err := tx.FindRecordsByFilter(RunsCollection,
			"campaignId={:id} && status!='sent' && status!='failed' && status!='empty' && status!='cancelled'", "", 1, 0, dbx.Params{"id": id})
		if err != nil {
			return err
		}
		if len(runs) > 0 {
			return textError("у кампании есть незавершённая рассылка; отмените запланированный запуск или дождитесь завершения отправки")
		}
		return nil
	})
}

func deleteDefinition(app core.App, collection, id string, version int, check func(core.App) error) error {
	if id == "" || version < 1 {
		return textError("для удаления нужны id и актуальная version")
	}
	return app.RunInTransaction(func(tx core.App) error {
		r, err := tx.FindRecordById(collection, id)
		if err != nil {
			return textError("запись не найдена")
		}
		var stored struct {
			Version int `json:"version"`
		}
		if err := decodeRecord(r, &stored); err != nil {
			return err
		}
		if stored.Version != version {
			return textError("запись изменена другим запросом; обновите страницу перед удалением")
		}
		if err := check(tx); err != nil {
			return err
		}
		return tx.DeleteWithContext(context.WithValue(context.Background(), internalKey{}, true), r)
	})
}
