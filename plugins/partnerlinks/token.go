package partnerlinks

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"
)

var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

const maxTokenBytes = 32768

// Only the digest is stored. Tokens contain no user or experiment data.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func tokenDigest(token string) (string, error) {
	if len(token) != 43 {
		return "", fmt.Errorf("partnerlinks: invalid clickData")
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(b) != 32 {
		return "", fmt.Errorf("partnerlinks: invalid clickData")
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(token))), nil
}
