package variants

import (
	"testing"

	"github.com/pocketbase/dbx"
)

func TestUserSelectionMatchesResolveWithoutHistory(t *testing.T) {
	x := newFixture(t)
	must(t, ensureUserBucket(x.app, x.user))
	for _, enabled := range []bool{true, false} {
		cfg, err := Load(x.app, x.offers.Id)
		must(t, err)
		cfg.Variables = enabled
		cfg.Experiments = enabled
		cfg, err = Publish(x.app, *cfg)
		must(t, err)
		decision, err := Resolve(x.app, cfg, x.user)
		must(t, err)
		query, err := UserSelection(x.app, x.offers.Id, x.users.Id, decision.Variant, decision.Experiment, decision.Group)
		must(t, err)
		var count int
		must(t, x.app.DB().NewQuery("SELECT count(*) FROM ("+query+") WHERE id={:id}").Bind(dbx.Params{"id": x.user.Id}).Row(&count))
		if count != 1 {
			t.Fatalf("selection disagrees with Resolve, variables=%v", enabled)
		}
	}
	if _, err := UserSelection(x.app, x.offers.Id, "other", "default", "", ""); err == nil {
		t.Fatal("auth collection mismatch accepted")
	}
	rows, err := x.app.FindAllRecords(history)
	must(t, err)
	if len(rows) != 0 {
		t.Fatal("selection recorded history")
	}
}
