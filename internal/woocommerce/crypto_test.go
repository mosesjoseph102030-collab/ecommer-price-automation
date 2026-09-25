package woocommerce

import (
	"bytes"
	"testing"
)

func TestCredentialEncryptionRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	crypter, err := NewCrypter(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"consumer_key":"ck_test","consumer_secret":"cs_test","webhook_secret":"wh_test"}`)
	enc, err := crypter.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc.Data, []byte("cs_test")) {
		t.Fatal("ciphertext leaks secret")
	}
	got, err := crypter.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round trip mismatch")
	}
}

func TestCredentialTamperingRejected(t *testing.T) {
	crypter, _ := NewCrypter(bytes.Repeat([]byte{1}, 32))
	enc, _ := crypter.Encrypt([]byte("secret"))
	enc.Data[0] ^= 1
	if _, err := crypter.Decrypt(enc); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}
