package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Claims is the minimal JWT payload for Phase 1 (HMAC-SHA256, stdlib only).
type Claims struct {
	UserID string `json:"uid"`
	Email  string `json:"em"`
	Exp    int64  `json:"exp"`
}

// IssueToken signs claims with secret. Secret must be >= 32 chars in production.
func IssueToken(secret []byte, userID, email string) (string, error) {
	c := Claims{UserID: userID, Email: email, Exp: time.Now().Add(SessionLifetime).Unix()}
	raw, _ := json.Marshal(c)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	sig := sign(secret, payload)
	return payload + "." + sig, nil
}

// ValidateToken verifies signature + expiry.
func ValidateToken(secret []byte, token string) (Claims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return Claims{}, fmt.Errorf("invalid token format")
	}
	want := sign(secret, parts[0])
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return Claims{}, fmt.Errorf("invalid token signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("invalid token payload")
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return Claims{}, fmt.Errorf("invalid token claims")
	}
	if time.Now().Unix() > c.Exp {
		return Claims{}, fmt.Errorf("token expired")
	}
	if c.UserID == "" {
		return Claims{}, fmt.Errorf("token missing user")
	}
	return c, nil
}

func sign(secret []byte, payload string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
