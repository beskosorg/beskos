// Package secretbox encrypts small secrets at rest (e.g. the TOTP secret) with AES-256-GCM.
// The key comes from an environment string, derived via SHA-256. An encrypted value carries the
// "enc:" prefix; values without the prefix are treated as plaintext (compat. with legacy data and
// with environments that have no key configured).
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const prefix = "enc:"

// Box encrypts/decrypts with a fixed key. nil = no encryption (no-op): see New.
type Box struct{ key [32]byte }

// New returns a Box from the key string. Empty → nil (encryption off; stores in plaintext).
func New(secret string) *Box {
	if secret == "" {
		return nil
	}
	return &Box{key: sha256.Sum256([]byte(secret))}
}

// Encrypt returns "enc:" + base64(nonce||ciphertext). With a nil Box, returns the plaintext.
func (b *Box) Encrypt(plain string) (string, error) {
	if b == nil {
		return plain, nil
	}
	gcm, err := b.gcm()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.RawURLEncoding.EncodeToString(ct), nil
}

// Decrypt reverses Encrypt. A value without the "enc:" prefix is returned as-is (legacy/plaintext).
func (b *Box) Decrypt(stored string) (string, error) {
	if !strings.HasPrefix(stored, prefix) {
		return stored, nil
	}
	if b == nil {
		return "", errors.New("secretbox: encrypted value with no key configured")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil {
		return "", err
	}
	gcm, err := b.gcm()
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("secretbox: ciphertext muito curto")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

func (b *Box) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(b.key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
