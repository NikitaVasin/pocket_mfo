package migrations

import (
	"github.com/NikitaVasin/pocket_mfo/plugins/partnerlinks"
	"testing"

	"github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
	"github.com/NikitaVasin/pocket_mfo/plugins/singleton"
	"github.com/NikitaVasin/pocket_mfo/plugins/typedconfig"
	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

func TestDemoVariants(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	polymorphicrelation.Register(app)
	variants.Register(app)
	typedconfig.Register(app)
	singleton.Register(app)
	partnerlinks.Register(app, partnerlinks.Options{})
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(app.Bootstrap())
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	check(app.RunAppMigrations())
	cfg, err := variants.Load(app, demoOffersID)
	check(err)
	offers, err := app.FindCollectionByNameOrId(demoOffersID)
	check(err)
	rows, err := app.FindAllRecords(offers)
	check(err)
	if len(rows) != 12 {
		t.Fatalf("want 12 offers, got %d", len(rows))
	}
	for collection, want := range map[string]int{demoMembersID: 6, demoSubscriptionsID: 3, "pv_sets": 6} {
		records, err := app.FindAllRecords(collection)
		if collection == "pv_sets" {
			records, err = app.FindAllRecords(collection, dbx.HashExp{"collection": demoOffersID})
		}
		check(err)
		if len(records) != want {
			t.Fatalf("%s: want %d records, got %d", collection, want, len(records))
		}
	}
	assertVisible := func(user *core.Record, set string) {
		t.Helper()
		count := 0
		for _, row := range rows {
			visible, err := app.CanAccessRecord(row, &core.RequestInfo{Auth: user}, offers.ListRule)
			check(err)
			if visible {
				count++
				if row.GetString(variants.SetField) != set {
					t.Fatal("native rule exposed another content set")
				}
			}
		}
		if count != 2 {
			t.Fatalf("want 2 visible offers, got %d", count)
		}
	}
	for _, member := range demoMembers {
		t.Run(member.email, func(t *testing.T) {
			user, err := app.FindAuthRecordByEmail(demoMembersID, member.email)
			check(err)
			if !user.ValidatePassword(demoVariantsPassword) {
				t.Fatal("demo password must authenticate")
			}
			decision, err := variants.Resolve(app, cfg, user)
			check(err)
			if decision.Variant != member.variant || decision.Group != member.group {
				t.Fatalf("unexpected decision: %+v", decision)
			}
			if user.GetInt(variants.BucketField) != variants.Bucket(demoMembersID, user.Id) {
				t.Fatal("stored bucket must match the stable hash")
			}
			assertVisible(user, decision.Set)
		})
	}
	guest, err := variants.Resolve(app, cfg, nil)
	check(err)
	assertVisible(nil, guest.Set)

	// Seed is safe to repeat after editing content, credentials and configuration.
	rows[0].Set("title", "Edited demo offer")
	check(app.Save(rows[0]))
	user, err := app.FindAuthRecordByEmail(demoMembersID, demoMembers[0].email)
	check(err)
	user.SetPassword("changed-demo-password")
	check(app.Save(user))
	cfg.Experiments = false
	cfg, err = variants.Publish(app, *cfg)
	check(err)
	check(ensureDemoVariants(app))
	preserved, err := app.FindRecordById(offers, rows[0].Id)
	check(err)
	if preserved.GetString("title") != "Edited demo offer" {
		t.Fatal("seed overwrote edited content")
	}
	user, err = app.FindRecordById(demoMembersID, user.Id)
	check(err)
	if !user.ValidatePassword("changed-demo-password") {
		t.Fatal("seed overwrote changed credentials")
	}
	after, err := variants.Load(app, demoOffersID)
	check(err)
	if after.Version != cfg.Version || after.Experiments {
		t.Fatal("seed republished configuration")
	}
	rows, err = app.FindAllRecords(offers)
	check(err)
	if len(rows) != 12 {
		t.Fatalf("repeated seed changed offer count: %d", len(rows))
	}
}
