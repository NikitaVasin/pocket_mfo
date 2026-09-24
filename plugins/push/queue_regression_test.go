package push

import (
	"errors"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

func TestInvalidScheduledAudienceDoesNotBlockOtherRuns(t *testing.T) {
	x := setup(t, false)
	invalid := x.campaign(t)
	broken, err := x.p.Launch(x.app, Launch{
		CampaignID: invalid.ID, Version: invalid.Version, IdempotencyKey: "scheduled-invalid",
		ScheduledAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
	must(t, err)
	// A trusted migration can remove a field after a launch was scheduled.
	users, err := x.app.FindCollectionByNameOrId("members")
	must(t, err)
	users.Fields.RemoveByName("tier")
	must(t, x.app.Save(users))
	run, err := x.app.FindRecordById(RunsCollection, broken["id"].(string))
	must(t, err)
	run.Set("scheduledAt", time.Now().Add(-time.Minute))
	must(t, save(x.app, run))
	valid, err := x.p.SaveCampaign(x.app, Campaign{
		Name: "Unaffected", AllUsers: true,
		Message: Message{Title: "Hello", Text: "World", Action: "route", Target: "/orders"},
	})
	must(t, err)
	_, err = x.p.Launch(x.app, Launch{CampaignID: valid.ID, Version: valid.Version, IdempotencyKey: "unaffected-launch"})
	must(t, err)
	must(t, x.p.Process(t.Context()))
	run, err = x.app.FindRecordById(RunsCollection, run.Id)
	must(t, err)
	if run.GetString("status") != "failed" || run.GetString("error") == "" {
		t.Fatal("invalid scheduled audience should have a terminal diagnostic")
	}
	jobs, err := x.app.FindAllRecords(jobsCollection)
	must(t, err)
	for _, job := range jobs {
		if job.GetString("runId") == run.Id {
			t.Fatal("invalid audience left partial jobs")
		}
	}
	if len(x.sent) != 1 {
		t.Fatal("invalid scheduled run blocked an unrelated send")
	}
	x.due(t)
	must(t, x.p.Process(t.Context()))
	if len(x.sent) != 1 {
		t.Fatal("processing retried a completed send")
	}
}

func TestScheduledLaunchRejectsInvalidDeviceBeforeSaving(t *testing.T) {
	x := setup(t, false)
	c := x.campaign(t)
	_, err := x.p.Launch(x.app, Launch{
		CampaignID: c.ID, Version: c.Version, IdempotencyKey: "invalid-test-device",
		ScheduledAt:   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		TestDeviceIDs: []string{"invalid"},
	})
	if err == nil {
		t.Fatal("invalid scheduled test device was accepted")
	}
	runs, err := x.app.FindAllRecords(RunsCollection)
	must(t, err)
	if len(runs) != 0 {
		t.Fatal("rejected launch left a run")
	}
}

func TestScheduledStorageFailureRollsBackAndRemainsRetryable(t *testing.T) {
	x := setup(t, false)
	c := x.campaign(t)
	result, err := x.p.Launch(x.app, Launch{
		CampaignID: c.ID, Version: c.Version, IdempotencyKey: "scheduled-storage",
		ScheduledAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
	must(t, err)
	id := result["id"].(string)
	run, err := x.app.FindRecordById(RunsCollection, id)
	must(t, err)
	run.Set("scheduledAt", time.Now().Add(-time.Minute))
	must(t, save(x.app, run))
	failure := errors.New("temporary storage failure")
	x.app.OnRecordUpdateExecute(RunsCollection).Bind(&hook.Handler[*core.RecordEvent]{Id: "fail-prepare", Func: func(e *core.RecordEvent) error {
		if e.Record.Id == id && e.Record.GetString("status") == "queued" {
			return failure
		}
		return e.Next()
	}})
	if err := x.p.Process(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("lost storage failure: %v", err)
	}
	run, err = x.app.FindRecordById(RunsCollection, id)
	must(t, err)
	jobs, err := x.app.FindAllRecords(jobsCollection)
	must(t, err)
	device, err := x.app.FindRecordById(DevicesCollection, x.device.ID)
	must(t, err)
	if run.GetString("status") != "scheduled" || len(jobs) != 0 || device.GetString("lastSent") != "" || len(x.sent) != 0 {
		t.Fatal("failed preparation left side effects or became terminal")
	}
	x.app.OnRecordUpdateExecute(RunsCollection).Unbind("fail-prepare")
	must(t, x.p.Process(t.Context()))
	if len(x.sent) != 1 {
		t.Fatal("scheduled run did not recover after storage became available")
	}
}
