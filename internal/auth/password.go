package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// NOTE: Phase 1 uses HMAC-SHA256 with random salt (stdlib only) to avoid extra
// deps. Swap hashPassword/checkPassword for bcrypt before production if desired;
// the function signatures stay identical.

// HashPassword returns "saltHex:hmacHex".
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", fmt.Errorf("password must be at least 8 characters")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	saltHex := hex.EncodeToString(salt)
	return saltHex + ":" + hmacHex(saltHex, password), nil
}

// CheckPassword verifies a password against a stored hash.
func CheckPassword(password, stored string) bool {
	parts := strings.SplitN(stored, ":", 2)
	if len(parts) != 2 {
		return false
	}
	want := hmacHex(parts[0], password)
	return hmac.Equal([]byte(want), []byte(parts[1]))
}

func hmacHex(saltHex, password string) string {
	m := hmac.New(sha256.New, []byte(saltHex))
	m.Write([]byte(password))
	return hex.EncodeToString(m.Sum(nil))
}

// SessionLifetime and verification/reset TTLs (Phase 1 policy).
const (
	SessionLifetime  = 24 * time.Hour
	VerificationTTL  = 24 * time.Hour
	PasswordResetTTL = 1 * time.Hour
)

// NewToken returns a random 32-byte hex token for sessions/verification/reset.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
