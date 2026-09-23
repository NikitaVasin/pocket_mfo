package push

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/NikitaVasin/pocket_mfo/plugins/mcp"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func TestDeleteDefinitionsPermissionsReferencesAndHistory(t *testing.T) {
	for _, locked := range []bool{false, true} {
		t.Run(fmt.Sprint(locked), func(t *testing.T) {
			x := setup(t, locked)
			c := x.campaign(t)
			aRecord, err := x.app.FindRecordById(AudiencesCollection, c.AudienceIDs[0])
			must(t, err)
			var a Audience
			must(t, decodeRecord(aRecord, &a))
			for action, id := range map[string]string{"campaign_delete": c.ID, "audience_delete": a.ID} {
				for _, token := range []string{"", x.auth} {
					response := x.request("POST", "/api/push/admin/"+action, token, map[string]any{"id": id, "version": 1})
					if response.Code < 400 {
						t.Fatal("unauthorized delete", action, response.Code)
					}
				}
			}
			reject := func(action, id string, version int, message string) {
				t.Helper()
				response := x.request("POST", "/api/push/admin/"+action, x.admin, map[string]any{"id": id, "version": version})
				if response.Code != 400 || !strings.Contains(strings.ToLower(response.Body.String()), strings.ToLower(message)) {
					t.Fatalf("%s: %d %s", action, response.Code, response.Body.String())
				}
			}
			reject("campaign_delete", c.ID, 0, "version")
			reject("audience_delete", a.ID, 2, "изменена")
			reject("audience_delete", a.ID, a.Version, "используется")
			c.AllUsers = true
			c.AudienceIDs = nil
			c.ExcludeAudienceIDs = []string{a.ID}
			c, err = x.p.SaveCampaign(x.app, c)
			must(t, err)
			reject("audience_delete", a.ID, a.Version, "используется")
			reject("campaign_delete", c.ID, 1, "изменена")
			c.AllUsers = false
			c.AudienceIDs = []string{a.ID}
			c.ExcludeAudienceIDs = nil
			c, err = x.p.SaveCampaign(x.app, c)
			must(t, err)
			run, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "delete-history"})
			must(t, err)
			runID := run["id"].(string)
			runRecord, err := x.app.FindRecordById(RunsCollection, runID)
			must(t, err)
			for _, status := range []string{"scheduled", "queued", "sending", "submitted", "unknown", "unrecognized"} {
				runRecord.Set("status", status)
				must(t, save(x.app, runRecord))
				reject("campaign_delete", c.ID, c.Version, "незавершённая")
				_, err = x.app.FindRecordById(CampaignsCollection, c.ID)
				must(t, err)
			}
			runRecord.Set("status", "queued")
			must(t, save(x.app, runRecord))
			must(t, x.p.Process(t.Context()))
			x.due(t)
			must(t, x.p.Process(t.Context()))
			before, err := x.p.Report(x.app, runID)
			must(t, err)
			if before["status"] != "sent" {
				t.Fatal(before)
			}
			// An unrelated hook failure must roll back deletion.
			x.app.OnRecordDelete().BindFunc(func(e *core.RecordEvent) error {
				if e.Record.Id == c.ID {
					if err := e.Next(); err != nil {
						return err
					}
					return errors.New("rollback deletion")
				}
				return e.Next()
			})
			reject("campaign_delete", c.ID, c.Version, "rollback deletion")
			_, err = x.app.FindRecordById(CampaignsCollection, c.ID)
			must(t, err)
		})
	}
}

func TestDeleteCampaignPreservesHistoryAndAllowsAudienceRemoval(t *testing.T) {
	x := setup(t, true)
	c := x.campaign(t)
	run, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "delete-complete"})
	must(t, err)
	must(t, x.p.Process(t.Context()))
	x.due(t)
	must(t, x.p.Process(t.Context()))
	id := run["id"].(string)
	before, err := x.p.Report(x.app, id)
	must(t, err)
	response := x.request("POST", "/api/push/admin/campaign_delete", x.admin, map[string]any{"id": c.ID, "version": c.Version})
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}

	_, err = x.app.FindRecordById(CampaignsCollection, c.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("campaign remains: %v", err)
	}
	response = x.request("POST", "/api/push/admin/audience_delete", x.admin, map[string]any{"id": c.AudienceIDs[0], "version": 1})
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	after, err := x.p.Report(x.app, id)
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("history changed on deletion")
	}
	// Deletion cannot revive a stale draft or break the launch idempotency receipt.
	if _, err = x.p.SaveCampaign(x.app, c); err == nil {
		t.Fatal("deleted campaign resurrected")
	}
	again, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "delete-complete"})
	must(t, err)
	if again["id"] != id {
		t.Fatal("launch receipt changed")
	}
	_, err = x.app.FindRecordById(AudiencesCollection, c.AudienceIDs[0])
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("audience remains: %v", err)
	}
}

func TestDeleteAudienceRacingCampaignSaveKeepsReferencesValid(t *testing.T) {
	x := setup(t, true)
	a, err := x.p.SaveAudience(x.app, Audience{Name: "Concurrent", AuthCollection: "members"})
	must(t, err)
	var wg sync.WaitGroup
	start := make(chan struct{})
	var c Campaign
	var saveErr, deleteErr error
	wg.Go(func() {
		<-start
		c, saveErr = x.p.SaveCampaign(x.app, Campaign{Name: "Concurrent", AudienceIDs: []string{a.ID}, Message: Message{Title: "t", Text: "t", Action: "app"}})
	})
	wg.Go(func() { <-start; deleteErr = x.p.DeleteAudience(x.app, a.ID, a.Version) })
	close(start)
	wg.Wait()
	if saveErr == nil && deleteErr == nil {
		t.Fatal("dangling audience reference")
	}
	if saveErr == nil {
		_, err = x.app.FindRecordById(AudiencesCollection, a.ID)
		must(t, err)
		_, err = x.app.FindRecordById(CampaignsCollection, c.ID)
		must(t, err)
	}
}

func TestDeleteTerminalCampaignsViaMCP(t *testing.T) {
	x := setup(t, true)
	var tool mcp.Tool
	for _, candidate := range x.p.MCPTools() {
		if candidate.Name == "push_campaign_delete" {
			tool = candidate
		}
	}
	if tool.Handle == nil || tool.ReadOnly {
		t.Fatal("missing mutation tool")
	}
	for _, status := range []string{"failed", "empty", "cancelled"} {
		c := x.campaign(t)
		collection, err := x.app.FindCollectionByNameOrId(RunsCollection)
		must(t, err)
		run := core.NewRecord(collection)
		run.Set("campaignId", c.ID)
		run.Set("status", status)
		run.Set("requestKey", secret())
		run.Set("definition", runDefinition{Campaign: c})
		must(t, save(x.app, run))
		args, err := json.Marshal(map[string]any{"id": c.ID, "version": c.Version})
		must(t, err)
		_, err = tool.Handle(t.Context(), mcp.Call{App: x.app, Arguments: args})
		must(t, err)
		_, err = x.app.FindRecordById(RunsCollection, run.Id)
		must(t, err)
		if _, err = x.app.FindRecordById(CampaignsCollection, c.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("campaign was not deleted", err)
		}
	}
}
