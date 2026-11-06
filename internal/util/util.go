// Package util holds small helpers with no domain knowledge.
package util

import (
	"crypto/rand"
	"encoding/base64"

	"github.com/google/uuid"
)

// NewID returns a time-ordered UUIDv7 string, which keeps B-tree indexes compact.
func NewID() string { return uuid.Must(uuid.NewV7()).String() }

// RandomToken returns n bytes of crypto randomness, base64url-encoded without padding.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error()) // the OS RNG never fails in practice
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
