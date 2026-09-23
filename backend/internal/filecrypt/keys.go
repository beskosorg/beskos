package filecrypt

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Key hierarchy:  KEK (environment) → tenant key → DEK (one per blob) → object.
//
// The KEK never encrypts an object: it encrypts keys. That's what makes rotation cheap — swapping
// the KEK re-wraps a small table and does not rewrite a single byte of storage.

// KEK is the set of known master keys, by version. More than one is kept so that a rotation can
// run in the background: old material keeps opening as long as it hasn't been re-wrapped yet.
type KEK struct {
	byVersion map[int][KeySize]byte
	current   int
}

// ParseKEK reads the configuration from the environment. Format: "1:<64-char hex>,2:<64-char hex>".
// The highest version is the current one — the one that wraps new material.
//
// It also accepts a key with no version (just the hex), treated as version 1, so as not to force
// touching the format when only one exists.
func ParseKEK(s string) (*KEK, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil // encryption disabled: see Service.
	}
	k := &KEK{byVersion: map[int][KeySize]byte{}}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		version, encoded := 1, part
		if i := strings.Index(part, ":"); i > 0 {
			n, err := strconv.Atoi(part[:i])
			if err != nil {
				return nil, fmt.Errorf("filecrypt: invalid KEK version in %q", part)
			}
			version, encoded = n, part[i+1:]
		}
		raw, err := hex.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return nil, fmt.Errorf("filecrypt: KEK v%d is not hexadecimal: %w", version, err)
		}
		if len(raw) != KeySize {
			return nil, fmt.Errorf("filecrypt: KEK v%d has %d bytes, needs %d", version, len(raw), KeySize)
		}
		var key [KeySize]byte
		copy(key[:], raw)
		k.byVersion[version] = key
		if version > k.current {
			k.current = version
		}
	}
	if len(k.byVersion) == 0 {
		return nil, fmt.Errorf("filecrypt: FILE_ENC_KEYS is set but holds no valid key")
	}
	return k, nil
}

// CurrentVersion is the version that wraps new material.
func (k *KEK) CurrentVersion() int { return k.current }

// NewKey generates random key material (tenant key or DEK).
func NewKey() ([]byte, error) {
	b := make([]byte, KeySize)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Wrap wraps `key` with the current version's KEK.
func (k *KEK) Wrap(key []byte) ([]byte, int, error) {
	master, ok := k.byVersion[k.current]
	if !ok {
		return nil, 0, fmt.Errorf("filecrypt: unknown KEK v%d", k.current)
	}
	out, err := wrapWith(master[:], key)
	return out, k.current, err
}

// WrapWithVersion wraps with a specific version. Used by rotation, which needs to re-wrap
// into the new version knowing what the old one was.
func (k *KEK) WrapWithVersion(key []byte, version int) ([]byte, error) {
	master, ok := k.byVersion[version]
	if !ok {
		return nil, fmt.Errorf("filecrypt: unknown KEK v%d", version)
	}
	return wrapWith(master[:], key)
}

// Unwrap opens material wrapped by a given KEK version.
func (k *KEK) Unwrap(wrapped []byte, version int) ([]byte, error) {
	master, ok := k.byVersion[version]
	if !ok {
		// Happens if the old KEK is removed from the environment before the rotation finishes.
		// It's an operational error, not a data one — and it's better to fail loud than serve garbage.
		return nil, fmt.Errorf("filecrypt: KEK v%d is not configured; older material stays unreadable without it", version)
	}
	return unwrapWith(master[:], wrapped)
}

// WrapDEK wraps a DEK with the tenant's key (not with the KEK): it's the middle step that allows
// making a tenant's data unreadable by deleting a single row.
func WrapDEK(tenantKey, dek []byte) ([]byte, error) { return wrapWith(tenantKey, dek) }

// UnwrapDEK opens a DEK with the tenant's key.
func UnwrapDEK(tenantKey, wrapped []byte) ([]byte, error) { return unwrapWith(tenantKey, wrapped) }

// wrapWith encrypts key material with AES-256-GCM: nonce ‖ ciphertext ‖ tag.
func wrapWith(key, material []byte) ([]byte, error) {
	gcm, err := newGCM(normalize(key))
	if err != nil {
		return nil, err
	}
	n := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return nil, err
	}
	return gcm.Seal(n, n, material, nil), nil
}

func unwrapWith(key, wrapped []byte) ([]byte, error) {
	gcm, err := newGCM(normalize(key))
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(wrapped) < ns {
		return nil, ErrCorrupt
	}
	out, err := gcm.Open(nil, wrapped[:ns], wrapped[ns:], nil)
	if err != nil {
		return nil, ErrCorrupt
	}
	return out, nil
}

// normalize accepts keys that are not exactly 32 bytes, deriving them via SHA-256 — the same
// thing secretbox does for TOTP. Keys we generate already come at 32 and pass through unchanged.
func normalize(key []byte) []byte {
	if len(key) == KeySize {
		return key
	}
	sum := sha256.Sum256(key)
	return sum[:]
}
