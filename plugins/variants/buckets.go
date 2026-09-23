package variants

import (
	"crypto/sha256"
	"database/sql/driver"
	"encoding/binary"
	"slices"
	"strings"

	"modernc.org/sqlite"
)

const experimentBucketSQL = "pocket_mfo_experiment_bucket_v1"

// Register before any database connection opens. This pure function has no
// application state and is shared by native API rules, Resolve and Push SQL.
func init() {
	sqlite.MustRegisterDeterministicScalarFunction(experimentBucketSQL, 5, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		parts := make([]string, len(args))
		for i, arg := range args {
			value, ok := arg.(string)
			if !ok {
				return nil, errInvalid("invalid experiment bucket argument")
			}
			parts[i] = value
		}
		return int64(ExperimentBucket(parts[0], parts[1], parts[2], parts[3], parts[4])), nil
	})
}

// ExperimentBucket is stable for this experiment's identity, independent of
// display names, group boundaries and other experiments. It doesn't modify the
// user's legacy content_bucket. Keys must change to start a fresh allocation.
func ExperimentBucket(authCollection, user, collection, variant, experiment string) int {
	s := sha256.Sum256([]byte(strings.Join([]string{"pocket_mfo.experiment.v1", authCollection, user, collection, variant, experiment}, "\x00")))
	return int(binary.BigEndian.Uint64(s[:8])%10000) + 1
}

func normalizeDistributions(c *Config, old *Config) {
	c.Default.Experiments = slices.Clone(c.Default.Experiments)
	c.Variants = slices.Clone(c.Variants)
	for i := range c.Variants {
		c.Variants[i].Experiments = slices.Clone(c.Variants[i].Experiments)
	}
	previous := map[string]string{}
	if old != nil {
		for _, v := range append([]Variant{old.Default}, old.Variants...) {
			for _, ex := range v.Experiments {
				value := ex.Distribution
				if value == "" {
					value = "shared"
				}
				previous[v.Key+"/"+ex.Key] = value
			}
		}
	}
	all := append([]*Variant{&c.Default}, variantPointers(c.Variants)...)
	for _, v := range all {
		for i := range v.Experiments {
			ex := &v.Experiments[i]
			if ex.Distribution != "" {
				continue
			}
			ex.Distribution = previous[v.Key+"/"+ex.Key]
			if ex.Distribution == "" {
				ex.Distribution = "independent"
			}
		}
	}
}

func variantPointers(items []Variant) []*Variant {
	result := make([]*Variant, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
}
