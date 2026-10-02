package push

import (
	"testing"

	"github.com/NikitaVasin/pocket_mfo/plugins/appmetrica"
	"github.com/NikitaVasin/pocket_mfo/plugins/partnerlinks"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func TestSharedAppMetricaMigrationAndDeviceIdentity(t *testing.T) {
	x := setup(t, false)
	c, err := partnerlinks.Load(x.app)
	must(t, err)
	c.ApplicationID = 123
	c.PostAPIKey = "post-key"
	_, err = partnerlinks.Configure(x.app, *c)
	must(t, err)
	appmetrica.Register(x.app, appmetrica.Options{})
	router, err := apis.NewRouter(x.app)
	must(t, err)
	must(t, x.app.OnServe().Trigger(&core.ServeEvent{App: x.app, Router: router}, func(*core.ServeEvent) error { return nil }))
	shared, err := appmetrica.Load(x.app)
	must(t, err)
	if shared.ApplicationID != 123 || shared.PostAPIKey != "post-key" || shared.OAuthToken != "server-only-token" {
		t.Fatal("migration lost shared credentials")
	}
	shared.PostAPIKey = "rotated-post"
	shared.OAuthToken = "rotated-oauth"
	_, err = appmetrica.Configure(x.app, shared)
	must(t, err)
	c, err = partnerlinks.Load(x.app)
	must(t, err)
	cfg, err := Load(x.app)
	must(t, err)
	if c.PostAPIKey != "rotated-post" || cfg.OAuthToken != "rotated-oauth" {
		t.Fatal("consumers use stale credentials")
	}
	cfg.OAuthToken = "bypass"
	if _, err = Configure(x.app, cfg); err == nil {
		t.Fatal("consumer changed shared OAuth")
	}
	shared, err = appmetrica.Load(x.app)
	must(t, err)
	version := shared.Version
	shared.ApplicationID = 456
	if _, err = appmetrica.Configure(x.app, shared); err == nil {
		t.Fatal("rebound existing devices")
	}
	shared, err = appmetrica.Load(x.app)
	must(t, err)
	if shared.ApplicationID != 123 || shared.Version != version {
		t.Fatal("partial identity change")
	}
}
func TestCampaignRateOverrideSnapshot(t *testing.T) {
	x := setup(t, false)
	campaign := x.campaign(t)
	for _, rate := range []int{-1, 1, 99, 5001} {
		in := campaign
		in.SendRate = rate
		if _, err := x.p.SaveCampaign(x.app, in); err == nil {
			t.Fatal("invalid rate accepted")
		}
	}
	for _, rate := range []int{0, 750} {
		campaign.SendRate = rate
		var err error
		campaign, err = x.p.SaveCampaign(x.app, campaign)
		must(t, err)
		run, err := x.p.Launch(x.app, Launch{CampaignID: campaign.ID, Version: campaign.Version, IdempotencyKey: secret()})
		must(t, err)
		r, err := x.app.FindRecordById(RunsCollection, run["id"].(string))
		must(t, err)
		var d runDefinition
		must(t, decodeRecord(r, &d))
		want := rate
		if want == 0 {
			want = 1000
		}
		if d.SendRate != want {
			t.Fatal("run did not snapshot effective rate")
		}
	}
}

func TestFirstSharedMigrationRejectsChangedLegacyManagedID(t *testing.T) {
	x := setup(t, false)
	before, err := loadStored(x.app)
	must(t, err)
	next := before.ApplicationID + 1
	setManaged(x.app, &ManagedConfig{ApplicationID: &next})
	appmetrica.Register(x.app, appmetrica.Options{})
	router, err := apis.NewRouter(x.app)
	must(t, err)
	err = x.app.OnServe().Trigger(&core.ServeEvent{App: x.app, Router: router}, func(*core.ServeEvent) error { return nil })
	if err == nil {
		t.Fatal("first shared migration reassigned existing devices")
	}
	if _, err := x.app.FindCollectionByNameOrId(appmetrica.Collection); err == nil {
		t.Fatal("failed migration persisted shared configuration")
	}
	after, err := loadStored(x.app)
	must(t, err)
	if after.ApplicationID != before.ApplicationID || after.Version != before.Version {
		t.Fatal("failed migration changed legacy configuration")
	}
}
