package push

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestManagedBootstrapAndRestart(t *testing.T) {
	dir := t.TempDir()
	id, token, rate := int64(6361870), "environment-secret", 1500
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dir})
	p := Register(app, Options{AuthCollections: []string{"members"}, Managed: &ManagedConfig{ApplicationID: &id, OAuthToken: &token, SendRate: &rate}, LockAdminConfig: true})
	// Registration owns a detached snapshot of caller pointers.
	id, token, rate = 999, "changed", 2000
	must(t, app.Bootstrap())
	c, err := Load(app)
	must(t, err)
	if c.ApplicationID != 6361870 || c.OAuthToken != "environment-secret" || c.SendRate != 1500 {
		t.Fatal("managed snapshot changed")
	}
	raw, err := loadStored(app)
	must(t, err)
	if raw.ApplicationID != 6361870 || raw.OAuthToken != "" || raw.HasOAuthToken {
		t.Fatal("identity not anchored or managed secret persisted")
	}
	must(t, applyManaged(app))
	again, err := Load(app)
	must(t, err)
	if again.Version != c.Version {
		t.Fatal("unchanged startup rewrote settings")
	}
	col := core.NewAuthCollection("members")
	must(t, app.Save(col))
	user := core.NewRecord(col)
	user.SetEmail("device@example.test")
	user.SetPassword("password-12345")
	must(t, app.Save(user))
	// Registration requires neither a server token nor permission to send.
	setManaged(app, &ManagedConfig{OAuthToken: types.Pointer("")})
	_, err = p.RegisterDevice(app, user, DeviceInput{ID: "device000000001", Secret: secret(), DeviceID: "1234", Platform: "android", Enabled: false})
	must(t, err)
	must(t, app.ClearBootstrap())

	// A code change on restart must not silently move registered devices.
	reopened := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dir})
	Register(reopened, Options{Managed: &ManagedConfig{ApplicationID: types.Pointer(int64(999))}})
	if err := reopened.Bootstrap(); err == nil || !strings.Contains(err.Error(), "миграции") {
		t.Fatalf("expected migration error, got %v", err)
	}
	_ = reopened.ClearBootstrap()
	restored := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dir})
	Register(restored, Options{})
	must(t, restored.Bootstrap())
	defer restored.ClearBootstrap()
	raw, err = loadStored(restored)
	must(t, err)
	if raw.ApplicationID != 6361870 || raw.Version != c.Version || raw.OAuthToken != "" {
		t.Fatal("failed restart changed stored configuration")
	}
}

func TestManagedSettingsPermissionsAndRedaction(t *testing.T) {
	x := setup(t, true)
	setManaged(x.app, &ManagedConfig{ApplicationID: types.Pointer(int64(123)), OAuthToken: types.Pointer("managed-secret")})
	for _, field := range []string{"applicationId", "oauthToken"} {
		c, err := Load(x.app)
		must(t, err)
		if field == "applicationId" {
			c.ApplicationID = 999
		} else {
			c.OAuthToken = "override"
		}
		before, err := loadStored(x.app)
		must(t, err)
		for _, auth := range []string{x.auth, x.admin} {
			response := x.request("PUT", "/api/push/admin/config", auth, c)
			if response.Code < 400 {
				t.Fatalf("override allowed for %s", field)
			}
		}
		after, err := loadStored(x.app)
		must(t, err)
		if after != before {
			t.Fatal("rejected update changed data")
		}
	}
	c, err := Load(x.app)
	must(t, err)
	c.OAuthToken, c.SendRate = "", 1200
	response := x.request("PUT", "/api/push/admin/config", x.admin, c)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	raw, err := loadStored(x.app)
	must(t, err)
	if raw.SendRate != 1200 || raw.OAuthToken != "server-only-token" {
		t.Fatal("editable field not saved or managed secret persisted")
	}
	state := x.request("GET", "/api/push/admin/state", x.admin, nil)
	if state.Code != 200 || strings.Contains(state.Body.String(), "managed-secret") || strings.Contains(response.Body.String(), "managed-secret") {
		t.Fatal("secret leaked")
	}
	var data struct {
		Config Config      `json:"config"`
		Locks  configLocks `json:"configLocks"`
	}
	must(t, json.Unmarshal(state.Body.Bytes(), &data))
	if !data.Config.HasOAuthToken || data.Config.OAuthToken != "" || len(data.Locks.Fields) != 2 {
		t.Fatal("missing safe metadata")
	}
	x.app.Store().Set(adminLockStoreKey, true)
	c, err = Load(x.app)
	must(t, err)
	c.SendRate = 1300
	for _, auth := range []string{x.auth, x.admin} {
		response = x.request("PUT", "/api/push/admin/config", auth, c)
		if response.Code != 403 {
			t.Fatalf("full lock: %d", response.Code)
		}
	}
	after, err := loadStored(x.app)
	must(t, err)
	if after != raw {
		t.Fatal("full lock changed settings")
	}
	// Trusted Go may still change unpinned fields.
	_, err = Configure(x.app, c)
	must(t, err)
}

func TestManagedValidationRollback(t *testing.T) {
	for _, m := range []*ManagedConfig{
		{ApplicationID: types.Pointer(int64(0))},
		{SendRate: types.Pointer(1)},
		{OAuthToken: types.Pointer("bad\ntoken")},
		{ApplicationID: types.Pointer(int64(456))},
	} {
		x := setup(t, false)
		before, err := loadStored(x.app)
		must(t, err)
		setManaged(x.app, m)
		if err := applyManaged(x.app); err == nil {
			t.Fatal("invalid managed settings accepted")
		}
		after, err := loadStored(x.app)
		must(t, err)
		if after != before {
			t.Fatal("failed managed settings changed storage")
		}
	}
}
