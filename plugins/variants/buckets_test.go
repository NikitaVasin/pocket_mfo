package variants

import (
	"fmt"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestIndependentExperimentsUseNativeRulesAndRemainStable(t *testing.T) {
	x := newFixture(t)
	configs := []*Config{}
	for i := range 4 {
		c := core.NewBaseCollection(fmt.Sprintf("independent%d", i))
		c.ListRule, c.ViewRule = types.Pointer(""), types.Pointer("")
		must(t, x.app.Save(c))
		cfg, err := Publish(x.app, Config{Collection: c.Id, AuthCollection: x.users.Id, Variables: true, Experiments: true, Default: Variant{Key: "default", Experiments: []Experiment{{Key: "test", Active: true, Groups: []Group{{Key: "a", From: 1, To: 5000}, {Key: "b", From: 5001, To: 10000}}}}}})
		must(t, err)
		if cfg.Default.Experiments[0].Distribution != "independent" {
			t.Fatal("new experiment isn't independent")
		}
		configs = append(configs, cfg)
		c, err = x.app.FindCollectionByNameOrId(c.Id)
		must(t, err)
		for _, group := range []string{"a", "b"} {
			r := core.NewRecord(c)
			r.Set(SetField, setID(c.Id, "default", "test", group))
			must(t, x.app.Save(r))
		}
	}
	combinations := map[string]bool{}
	// No password hashing or HTTP authentication is needed for pure SQL selection.
	for i := range 256 {
		id := fmt.Sprintf("test%011d", i)
		_, err := x.app.DB().NewQuery("INSERT INTO members (id, tokenKey, content_bucket) VALUES ({:id}, {:id}, {:bucket})").Bind(map[string]any{"id": id, "bucket": Bucket(x.users.Id, id)}).Execute()
		must(t, err)
		u, err := x.app.FindRecordById(x.users.Id, id)
		must(t, err)
		key := ""
		for _, cfg := range configs {
			d, err := Resolve(x.app, cfg, u)
			must(t, err)
			if d.ExperimentBucket != ExperimentBucket(x.users.Id, id, cfg.Collection, "default", "test") {
				t.Fatal("SQL and Go buckets differ")
			}
			key += d.Group
			c, err := x.app.FindCollectionByNameOrId(cfg.Collection)
			must(t, err)
			rows, err := x.app.FindAllRecords(c)
			must(t, err)
			count := 0
			for _, r := range rows {
				ok, err := x.app.CanAccessRecord(r, &core.RequestInfo{Auth: u}, c.ListRule)
				must(t, err)
				if ok {
					count++
					if r.GetString(SetField) != d.Set {
						t.Fatal("native selection differs")
					}
				}
			}
			if count != 1 {
				t.Fatal("native rules didn't choose exactly one group")
			}
		}
		combinations[key] = true
	}
	if len(combinations) != 16 {
		t.Fatalf("only %d combinations", len(combinations))
	}
	cfg := configs[0]
	before, err := Resolve(x.app, cfg, x.user)
	must(t, err)
	cfg.Default.Experiments[0].Name = "Renamed"
	next, err := Publish(x.app, *cfg)
	must(t, err)
	after, err := Resolve(x.app, next, x.user)
	must(t, err)
	if before.ExperimentBucket != after.ExperimentBucket || before.Set != after.Set {
		t.Fatal("rename reshuffled users")
	}
	query, err := UserSelection(x.app, next.Collection, x.users.Id, "default", "test", after.Group)
	must(t, err)
	var count int
	must(t, x.app.DB().NewQuery("SELECT count(*) FROM ("+query+") WHERE id={:id}").Bind(map[string]any{"id": x.user.Id}).Row(&count))
	// x.user predates Variants and has no persisted bucket yet.
	if count != 0 {
		t.Fatal("push selection included an unassigned legacy user")
	}
	must(t, ensureUserBucket(x.app, x.user))
	must(t, x.app.DB().NewQuery("SELECT count(*) FROM ("+query+") WHERE id={:id}").Bind(map[string]any{"id": x.user.Id}).Row(&count))
	if count != 1 {
		t.Fatal("push selection disagrees with Resolve")
	}
}

func TestLegacyDistributionSurvivesRepublish(t *testing.T) {
	x := newFixture(t)
	cfg := *x.cfg
	cfg.Variants[0].Experiments[0].Distribution = ""
	r, err := x.app.FindRecordById(configs, digest(cfg.Collection)[:15])
	must(t, err)
	r.Set("definition", cfg)
	must(t, save(x.app, r))
	next, err := Publish(x.app, cfg)
	must(t, err)
	if next.Variants[0].Experiments[0].Distribution != "shared" {
		t.Fatal("legacy experiment reshuffled")
	}
	next.Variants[0].Experiments[0].Distribution = "invalid"
	if _, err := Publish(x.app, *next); err == nil {
		t.Fatal("invalid distribution accepted")
	}
	actual, err := Load(x.app, cfg.Collection)
	must(t, err)
	if actual.Version != next.Version || actual.Variants[0].Experiments[0].Distribution != "shared" {
		t.Fatal("failed publication changed state")
	}
}
