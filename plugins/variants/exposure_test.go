package variants

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

func TestExposureFollowsReturnedContentAndRejectsTampering(t *testing.T) {
	x := newFixture(t)
	page := call(t, x, "GET", "/api/collections/offers/records", x.token, nil, 200)
	r := items(page)[0].(map[string]any)
	var exposure Exposure
	b, err := json.Marshal(r[ExposureField])
	must(t, err)
	must(t, json.Unmarshal(b, &exposure))
	if exposure.Token == "" || exposure.ID == "" || exposure.Decision.Set != r[SetField] {
		t.Fatal("no content context")
	}
	verified, err := VerifyExposures(x.user, []string{exposure.Token})
	must(t, err)
	if len(verified) != 1 || verified[0].Token != "" || verified[0].Revision == "" {
		t.Fatal("bad verified context")
	}
	x.cfg.Experiments = false
	_, err = Publish(x.app, *x.cfg)
	must(t, err)
	verified, err = VerifyExposures(x.user, []string{exposure.Token})
	must(t, err)
	if verified[0].Decision.Experiment != "trial" {
		t.Fatal("old material attribution recomputed")
	}
	parts := strings.Split(exposure.Token, ".")
	parts[1] = "e30"
	if _, err := VerifyExposures(x.user, []string{strings.Join(parts, ".")}); err == nil {
		t.Fatal("forged token accepted")
	}
	other := core.NewRecord(x.users)
	other.Id = "differentuser01"
	other.SetTokenKey("different-secret")
	if _, err := VerifyExposures(other, []string{exposure.Token}); err == nil {
		t.Fatal("foreign token accepted")
	}
	expired, err := security.NewJWT(jwt.MapClaims{"type": "variant_exposure", "user": x.user.Id, "auth": x.users.Id, "exposure": exposure}, exposureKey(x.user), -time.Hour)
	must(t, err)
	if _, err := VerifyExposures(x.user, []string{expired}); err == nil {
		t.Fatal("expired context accepted")
	}
	row, err := x.app.FindRecordById(x.offers.Id, r["id"].(string))
	must(t, err)
	if row.GetString(ExposureField) != "" {
		t.Fatal("context persisted in content")
	}
}
