package variants

import (
	"reflect"
	"testing"
)

func TestAnalyticsExperimentDimensions(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		decision Decision
		want     map[string]string
	}{
		{Decision{Collection: f.offers.Id, Variant: "default"}, map[string]string{"offers": "default"}},
		{Decision{Collection: f.offers.Id, Variant: "premium", Experiment: "checkout_test", Group: "B"}, map[string]string{"offers": "premium/checkout_test/B"}},
		{Decision{Collection: f.offers.Id, Variant: "default", Reason: "variables_disabled"}, map[string]string{}},
	} {
		got, err := AnalyticsExperiments(f.app, []Decision{tc.decision})
		must(t, err)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("got %v want %v", got, tc.want)
		}
	}
}
