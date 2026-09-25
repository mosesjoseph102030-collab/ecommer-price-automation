package woocommerce

import "testing"

func TestWebhookSignature(t *testing.T) {
	body := []byte(`{"id":42}`)
	secret := []byte("webhook-secret")
	sig := WebhookSignature(secret, body)
	if !VerifyWebhookSignature(secret, body, sig) {
		t.Fatal("valid webhook rejected")
	}
	if VerifyWebhookSignature(secret, []byte(`{"id":43}`), sig) {
		t.Fatal("modified body accepted")
	}
	if VerifyWebhookSignature([]byte("wrong"), body, sig) {
		t.Fatal("wrong secret accepted")
	}
}
