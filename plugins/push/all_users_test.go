package push

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestAllUsersSelectionAndLimits(t *testing.T) {
	x := setup(t, true)
	c, err := x.p.SaveCampaign(x.app, Campaign{Name: "Everyone", AllUsers: true, Message: Message{Title: "Title", Text: "Hello", Action: "app"}})
	must(t, err)
	check := func(users, devices int) {
		t.Helper()
		preview, err := x.p.Preview(x.app, c.ID)
		must(t, err)
		if preview.Users != users || preview.Devices != devices {
			t.Fatalf("preview: %+v, want %d/%d", preview, users, devices)
		}
	}
	check(2, 2)
	// One user may own multiple devices; all means all enabled devices by default.
	extra := DeviceInput{ID: "device000000003", DeviceID: "333", Secret: secret(), Platform: "android", Enabled: true, NotificationPermission: "authorized"}
	_, err = x.p.RegisterDevice(x.app, x.user, extra)
	must(t, err)
	check(2, 3)
	c.LastDeviceOnly = true
	c, err = x.p.SaveCampaign(x.app, c)
	must(t, err)
	check(2, 2)
	c.LastDeviceOnly = false
	excluded, err := x.p.SaveAudience(x.app, Audience{Name: "Exclude", AuthCollection: "members", UserIDs: []string{x.other.Id}})
	must(t, err)
	c.ExcludeAudienceIDs = []string{excluded.ID}
	c, err = x.p.SaveCampaign(x.app, c)
	must(t, err)
	check(1, 2)
	must(t, x.p.DisableDevice(x.app, x.user, extra.ID, extra.Secret))
	check(1, 1)
	c.CooldownHours = 24
	c, err = x.p.SaveCampaign(x.app, c)
	must(t, err)
	run, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "everyone-limits"})
	must(t, err)
	if run["recipients"] != 1 {
		t.Fatal(run)
	}
	check(0, 0)
	// Snapshot and sending use the same selection as preview.
	must(t, x.p.Process(t.Context()))
	jobs, err := x.app.FindAllRecords(jobsCollection)
	must(t, err)
	var job jobDefinition
	must(t, decodeRecord(jobs[0], &job))
	if len(job.Recipients) != 1 || job.Recipients[0].ID != x.device.ID {
		t.Fatalf("wrong recipients: %+v", job)
	}
	if len(x.sent) != 1 {
		t.Fatal("campaign was not dispatched")
	}
}

func TestAllUsersOnlyConnectedCollectionsAndExistingOwners(t *testing.T) {
	x := setup(t, false)
	addUser := func(name, deviceID string) {
		t.Helper()
		col := core.NewAuthCollection(name)
		must(t, x.app.Save(col))
		user := core.NewRecord(col)
		user.SetEmail(name + "@example.test")
		user.SetPassword("password-12345")
		must(t, x.app.Save(user))
		x.p.options.AuthCollections = append(x.p.options.AuthCollections, name)
		_, err := x.p.RegisterDevice(x.app, user, DeviceInput{ID: deviceID, DeviceID: strings.TrimPrefix(deviceID, "device"), Secret: secret(), Platform: "ios", Enabled: true, NotificationPermission: "authorized"})
		must(t, err)
	}
	addUser("connected", "device000000003")
	addUser("removed", "device000000004")
	x.p.options.AuthCollections = []string{"members", "connected", "members"}
	must(t, x.app.Delete(x.other))
	c, err := x.p.SaveCampaign(x.app, Campaign{Name: "Connected", AllUsers: true, Message: Message{Title: "Title", Text: "Hello", Action: "app"}})
	must(t, err)
	preview, err := x.p.Preview(x.app, c.ID)
	must(t, err)
	if preview.Users != 2 || preview.Devices != 2 {
		t.Fatalf("included removed owner/collection or duplicated recipients: %+v", preview)
	}
}

func TestAllUsersRequiresExplicitChoiceAndScheduledSnapshot(t *testing.T) {
	x := setup(t, true)
	old := x.campaign(t)
	before, err := x.app.FindRecordById(CampaignsCollection, old.ID)
	must(t, err)
	for _, invalid := range []Campaign{
		{ID: old.ID, Version: old.Version, Name: old.Name, Message: old.Message},
		{ID: old.ID, Version: old.Version, Name: old.Name, Message: old.Message, AllUsers: true, AudienceIDs: old.AudienceIDs},
	} {
		response := x.request("POST", "/api/push/admin/campaign_save", x.admin, invalid)
		if response.Code != 400 {
			t.Fatal("ambiguous recipient choice accepted", response.Code)
		}
		after, err := x.app.FindRecordById(CampaignsCollection, old.ID)
		must(t, err)
		if before.GetString("definition") != after.GetString("definition") {
			t.Fatal("invalid save changed campaign")
		}
	}
	body := map[string]any{"name": "Everyone", "version": 0, "allUsers": true, "message": old.Message}
	if response := x.request("POST", "/api/push/admin/campaign_save", x.auth, body); response.Code != 403 {
		t.Fatal("ordinary user created broadcast")
	}
	response := x.request("POST", "/api/push/admin/campaign_save", x.admin, body)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var c Campaign
	must(t, json.Unmarshal(response.Body.Bytes(), &c))
	run, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "everyone-scheduled", ScheduledAt: time.Now().Add(time.Hour).Format(time.RFC3339)})
	must(t, err)
	c.AllUsers, c.AudienceIDs = false, old.AudienceIDs
	_, err = x.p.SaveCampaign(x.app, c)
	must(t, err)
	// A device registered after scheduling is included when the frozen all-users run starts.
	_, err = x.p.RegisterDevice(x.app, x.user, DeviceInput{ID: "device000000003", DeviceID: "333", Secret: secret(), Platform: "android", Enabled: true, NotificationPermission: "authorized"})
	must(t, err)
	r, err := x.app.FindRecordById(RunsCollection, run["id"].(string))
	must(t, err)
	r.Set("scheduledAt", types.NowDateTime())
	must(t, save(x.app, r))
	must(t, x.p.Process(t.Context()))
	r, err = x.app.FindRecordById(RunsCollection, r.Id)
	must(t, err)
	if r.GetInt("recipients") != 3 {
		t.Fatal("scheduled broadcast lost its recipient mode", r.GetInt("recipients"))
	}
}
