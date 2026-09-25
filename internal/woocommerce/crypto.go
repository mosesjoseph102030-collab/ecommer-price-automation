package woocommerce

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const keyVersion = 1

// Crypter encrypts connector credentials with AES-256-GCM. Plaintext is never persisted.
type Crypter struct{ aead cipher.AEAD }

func NewCrypter(key []byte) (*Crypter, error) {
	if len(key) != 32 {
		return nil, errors.New("credential encryption key must decode to exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Crypter{aead: aead}, nil
}

type Ciphertext struct {
	Nonce      []byte
	Data       []byte
	KeyVersion int
}

func (c *Crypter) Encrypt(plaintext []byte) (Ciphertext, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Ciphertext{}, err
	}
	sealed := c.aead.Seal(nil, nonce, plaintext, []byte("woocommerce-credentials-v1"))
	return Ciphertext{Nonce: nonce, Data: sealed, KeyVersion: keyVersion}, nil
}

func (c *Crypter) Decrypt(enc Ciphertext) ([]byte, error) {
	if enc.KeyVersion != keyVersion {
		return nil, fmt.Errorf("unsupported credential key version %d", enc.KeyVersion)
	}
	plain, err := c.aead.Open(nil, enc.Nonce, enc.Data, []byte("woocommerce-credentials-v1"))
	if err != nil {
		return nil, errors.New("decrypt connector credentials")
	}
	return plain, nil
}

func DecodeKey(encoded string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("credential key must be base64: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("credential key must decode to 32 bytes, got %d", len(b))
	}
	return b, nil
}
