package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Passwords use PBKDF2-HMAC-SHA256 with OWASP's recommended iteration count,
// from the Go standard library. Format: pbkdf2-sha256$<iterations>$<salt>$<hash>.
const (
	pbkdf2Iterations = 600_000
	pbkdf2KeyLen     = 32
	// MinPasswordLength applies to every password set through the API.
	MinPasswordLength = 10
)

// HashPassword returns an encoded PBKDF2 hash of password with a random salt.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// dummyHash is verified against when a user does not exist, so response time
// does not reveal which usernames are valid.
var dummyHash, _ = HashPassword("langdeck-timing-equalizer")

// VerifyPassword reports whether password matches the encoded hash.
// An empty hash (no password set) never matches.
func VerifyPassword(encoded, password string) bool {
	if encoded == "" {
		_ = verify(dummyHash, password)
		return false
	}
	return verify(encoded, password)
}

func verify(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// ValidatePassword enforces the minimum password policy.
func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	if len(pw) > 256 {
		return errors.New("password is too long")
	}
	return nil
}

// LocalSubject is the identity subject for username/password accounts.
func LocalSubject(username string) string { return "local|" + strings.ToLower(username) }
