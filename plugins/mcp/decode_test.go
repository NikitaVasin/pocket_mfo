package mcp

import (
	"strings"
	"testing"
)

func TestDecodeBodyLimit(t *testing.T) {
	const limit = 1 << 20
	for _, tc := range []struct {
		name, body string
		wantError  bool
	}{
		{"object", `{}`, false},
		{"at limit", `{}` + strings.Repeat(" ", limit-2), false},
		{"over limit", `{}` + strings.Repeat(" ", limit-1), true},
		{"trailing object beyond limit", `{}` + strings.Repeat(" ", limit) + `{}`, true},
		{"trailing object", `{} {}`, true},
		{"unknown field", `{"unexpected":true}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var value struct{}
			err := Decode(strings.NewReader(tc.body), &value)
			if (err != nil) != tc.wantError {
				t.Fatalf("Decode error = %v, want error = %v", err, tc.wantError)
			}
		})
	}
}
