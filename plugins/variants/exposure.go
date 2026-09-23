package variants

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const ExposureField = "variantContext"

// Exposure identifies content returned by the server. The client reports an
// actual impression only when it renders that content, using the same context
// for subsequent clicks. A context never grants access to the referenced record.
type Exposure struct {
	ID             string            `json:"id"`
	User           string            `json:"user"`
	AuthCollection string            `json:"authCollection"`
	Token          string            `json:"token,omitempty"`
	Collection     string            `json:"collection"`
	Record         string            `json:"record"`
	Revision       string            `json:"revision"`
	Decision       Decision          `json:"decision"`
	Experiments    map[string]string `json:"experiments"`
}

func exposureKey(user *core.Record) string {
	return digest("pocket_mfo.exposure.v1", user.TokenKey(), user.Collection().AuthToken.Secret)
}

func enrichExposure(app core.App, user, record *core.Record) error {
	if user == nil || user.IsSuperuser() {
		return nil
	}
	cfg, err := Load(app, record.Collection().Id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if cfg.AuthCollection != user.Collection().Id {
		return nil
	}
	// Read the set of the returned material, not a freshly resolved assignment.
	s, err := app.FindRecordById(sets, record.GetString(SetField))
	if err != nil {
		return err
	}
	d := Decision{Collection: cfg.Collection, Variant: s.GetString("variant"), Experiment: s.GetString("experiment"), Group: s.GetString("group"), Set: s.Id, Version: cfg.Version, Bucket: Bucket(cfg.AuthCollection, user.Id), Reason: "content_response"}
	if !cfg.Variables && d.Experiment == "" {
		d.Reason = "variables_disabled"
	}
	experiments, err := AnalyticsExperiments(app, []Decision{d})
	if err != nil {
		return err
	}
	revision, err := ContentRevision(record)
	if err != nil {
		return err
	}
	exposure := Exposure{ID: security.RandomString(20), User: user.Id, AuthCollection: user.Collection().Id, Collection: record.Collection().Name, Record: record.Id, Revision: revision, Decision: d, Experiments: experiments}
	token, err := security.NewJWT(jwt.MapClaims{"type": "variant_exposure", "user": user.Id, "auth": user.Collection().Id, "exposure": exposure}, exposureKey(user), 24*time.Hour)
	if err != nil {
		return err
	}
	exposure.Token = token
	record.WithCustomData(true).Set(ExposureField, exposure)
	return nil
}

// ContentRevision identifies public material independently of its exposure token.
// Expanded records have their own contexts and are excluded from this revision.
func ContentRevision(record *core.Record) (string, error) {
	value := record.PublicExport()
	delete(value, ExposureField)
	delete(value, "expand")
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return digest(string(payload)), nil
}

// VerifyExposures authenticates content contexts without recalculating their
// assignments. Reject conflicting versions of the same collection in one click.
func VerifyExposures(user *core.Record, tokens []string) ([]Exposure, error) {
	if user == nil || user.IsSuperuser() || len(tokens) > 30 {
		return nil, errInvalid("invalid exposure contexts")
	}
	items := []Exposure{}
	seen := map[string]Decision{}
	for _, token := range tokens {
		if len(token) > 16384 {
			return nil, errInvalid("exposure context is too large")
		}
		claims, err := security.ParseJWT(token, exposureKey(user))
		if err != nil || claims["type"] != "variant_exposure" || claims["user"] != user.Id || claims["auth"] != user.Collection().Id {
			return nil, errInvalid("invalid or expired exposure context")
		}
		data, err := json.Marshal(claims["exposure"])
		if err != nil {
			return nil, err
		}
		var item Exposure
		if err := json.Unmarshal(data, &item); err != nil || item.Decision.Collection == "" || item.Record == "" || item.Revision == "" {
			return nil, errInvalid("invalid exposure payload")
		}
		if previous, ok := seen[item.Decision.Collection]; ok && previous != item.Decision {
			return nil, errInvalid("conflicting exposure assignments")
		}
		seen[item.Decision.Collection] = item.Decision
		items = append(items, item) // No bearer tokens are persisted in clickData.
	}
	return items, nil
}
