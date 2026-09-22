package variants

import "github.com/pocketbase/pocketbase/core"

// AnalyticsExperiments converts resolved assignments into the shared analytics
// dimensions. Callers storing a conversion must persist this map at issuance.
func AnalyticsExperiments(app core.App, decisions []Decision) (map[string]string, error) {
	result := map[string]string{}
	for _, d := range decisions {
		if d.Reason == "variables_disabled" {
			continue
		}
		c, err := app.FindCollectionByNameOrId(d.Collection)
		if err != nil {
			return nil, err
		}
		value := d.Variant
		if d.Experiment != "" {
			value += "/" + d.Experiment + "/" + d.Group
		}
		result[c.Name] = value
	}
	return result, nil
}
