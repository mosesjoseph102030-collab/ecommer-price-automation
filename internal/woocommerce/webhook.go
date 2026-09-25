package woocommerce

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

func WebhookSignature(secret, body []byte) string {
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write(body)
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func VerifyWebhookSignature(secret, body []byte, signature string) bool {
	expected := WebhookSignature(secret, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}
