package push

import (
	"errors"
	"fmt"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func TestNotificationPermissionSelection(t *testing.T) {
	x := setup(t, true)
	audienceCampaign := x.campaign(t)
	all, err := x.p.SaveCampaign(x.app, Campaign{Name: "Everyone", AllUsers: true, Message: audienceCampaign.Message})
	must(t, err)
	for _, permission := range append([]string{""}, notificationPermissions...) {
		t.Run(permission, func(t *testing.T) {
			input := x.device
			input.NotificationPermission = permission
			response := x.request("POST", "/api/push/devices", x.auth, input)
			if response.Code != 200 {
				t.Fatal(response.Body.String())
			}
			expected := 0
			if permission == "authorized" || permission == "provisional" {
				expected = 1
			}
			for _, c := range []Campaign{audienceCampaign, all} {
				preview, err := x.p.Preview(x.app, c.ID)
				must(t, err)
				want := expected
				if c.AllUsers {
					want++
				}
				if preview.Devices != want {
					t.Fatalf("permission %q preview %+v, want %d", permission, preview, want)
				}
			}
			run, err := x.p.Launch(x.app, Launch{CampaignID: audienceCampaign.ID, Version: audienceCampaign.Version, IdempotencyKey: "permission-" + permission, TestDeviceIDs: []string{x.device.ID}})
			must(t, err)
			if run["recipients"] != expected {
				t.Fatal(run)
			}
		})
	}
	// Invalid input cannot modify a binding or its generation.
	before, err := x.app.FindRecordById(DevicesCollection, x.device.ID)
	must(t, err)
	invalid := x.device
	invalid.NotificationPermission = "allowed"
	invalid.Language = "changed"
	if response := x.request("POST", "/api/push/devices", x.auth, invalid); response.Code != 400 {
		t.Fatal(response.Code)
	}
	after, err := x.app.FindRecordById(DevicesCollection, x.device.ID)
	must(t, err)
	if before.GetString("generation") != after.GetString("generation") || before.GetString("language") != after.GetString("language") || before.GetString("notificationPermission") != after.GetString("notificationPermission") {
		t.Fatal("invalid input changed device")
	}
	for _, auth := range []string{"", x.auth} {
		if response := x.request("GET", "/api/collections/push_devices/records", auth, nil); response.Code == 200 {
			t.Fatal("device data exposed to user")
		}
	}
	if response := x.request("GET", "/api/collections/push_devices/records", x.admin, nil); response.Code != 200 {
		t.Fatal(response.Body.String())
	}
}

func TestRevokedPermissionBeforeDispatch(t *testing.T) {
	for _, regrant := range []bool{false, true} {
		t.Run(fmt.Sprint(regrant), func(t *testing.T) {
			x := setup(t, true)
			c := x.campaign(t)
			_, err := x.p.Launch(x.app, Launch{CampaignID: c.ID, Version: c.Version, IdempotencyKey: "revoke-before-send"})
			must(t, err)
			input := x.device
			input.NotificationPermission = "denied"
			_, err = x.p.RegisterDevice(x.app, x.user, input)
			must(t, err)
			if regrant {
				_, err = x.p.RegisterDevice(x.app, x.user, x.device)
				must(t, err)
			}
			must(t, x.p.Process(t.Context()))
			if len(x.sent) != 0 {
				t.Fatal("revoked snapshot dispatched")
			}
		})
	}
}

func TestLastDeviceOnlyIgnoresDeniedNewerDevice(t *testing.T) {
	x := setup(t, true)
	c := x.campaign(t)
	c.LastDeviceOnly = true
	c, err := x.p.SaveCampaign(x.app, c)
	must(t, err)
	_, err = x.p.RegisterDevice(x.app, x.user, DeviceInput{ID: "device000000003", DeviceID: "333", Secret: secret(), Platform: "android", Enabled: true, NotificationPermission: "denied"})
	must(t, err)
	preview, err := x.p.Preview(x.app, c.ID)
	must(t, err)
	if preview.Devices != 1 {
		t.Fatal(preview)
	}
}

func TestPermissionMigrationIsAtomicAndIdempotent(t *testing.T) {
	x := setup(t, true)
	c, err := x.app.FindCollectionByNameOrId(DevicesCollection)
	must(t, err)
	c.Fields.RemoveByName("notificationPermission")
	must(t, save(x.app, c))
	rollback := errors.New("rollback")
	err = x.app.RunInTransaction(func(tx core.App) error {
		if err := install(tx); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	c, err = x.app.FindCollectionByNameOrId(DevicesCollection)
	must(t, err)
	if c.Fields.GetByName("notificationPermission") != nil {
		t.Fatal("schema migration escaped rollback")
	}
	must(t, install(x.app))
	d, err := x.app.FindRecordById(DevicesCollection, x.device.ID)
	must(t, err)
	if d.GetString("notificationPermission") != "unknown" || !d.GetBool("enabled") {
		t.Fatal("legacy device was not preserved with unknown permission")
	}
	preview, err := x.p.Preview(x.app, x.campaign(t).ID)
	must(t, err)
	if preview.Devices != 0 {
		t.Fatal("legacy device selected before permission sync")
	}
	_, err = x.p.RegisterDevice(x.app, x.user, x.device)
	must(t, err)
	must(t, install(x.app))
	d, err = x.app.FindRecordById(DevicesCollection, x.device.ID)
	must(t, err)
	if d.GetString("notificationPermission") != "authorized" {
		t.Fatal("reinstall reset permission")
	}
}
