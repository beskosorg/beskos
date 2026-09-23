// Package totp implements TOTP (RFC 6238, HMAC-SHA1, 6 digits, 30s period) in pure Go —
// compatible with Google/Microsoft Authenticator. No external dependencies.
//
// Security: constant-time code comparison; ±1 step window (clock tolerance).
// The secret is sensitive — see the note in db/migrations about encrypting at rest in the future.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	period = 30
	digits = 6
	issuer = "Beskos"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret creates a 160-bit (20-byte) base32 secret, as required by RFC 4226.
func GenerateSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return b32.EncodeToString(buf), nil
}

// ProvisioningURI returns the otpauth:// URI for the authenticator app's QR code.
func ProvisioningURI(secret, account string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(digits))
	q.Set("period", fmt.Sprint(period))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// code computes the TOTP for a counter (time step).
func code(secret string, counter uint64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	h := hmac.New(sha1.New, key)
	h.Write(msg[:])
	sum := h.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[offset]&0x7f) << 24) | (uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) | uint32(sum[offset+3])
	return fmt.Sprintf("%0*d", digits, bin%1_000_000), nil
}

// Validate checks a code at instant t, accepting ±1 step (90s window). Constant time.
func Validate(secret, input string, t time.Time) bool {
	input = strings.TrimSpace(input)
	if len(input) != digits {
		return false
	}
	counter := uint64(t.Unix()) / period
	for _, delta := range []int64{0, -1, 1} {
		want, err := code(secret, uint64(int64(counter)+delta))
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(input)) == 1 {
			return true
		}
	}
	return false
}

// Now validates using the current clock (convenience).
func Now(secret, input string) bool { return Validate(secret, input, time.Now()) }

// ValidateStep checks the code and returns the accepted time step, for the caller to prevent
// replay (rejecting a step already used before). ok=false if no step in the ±1 window matches.
// Constant time.
func ValidateStep(secret, input string, t time.Time) (step int64, ok bool) {
	input = strings.TrimSpace(input)
	if len(input) != digits {
		return 0, false
	}
	counter := int64(uint64(t.Unix()) / period)
	for _, delta := range []int64{0, -1, 1} {
		s := counter + delta
		want, err := code(secret, uint64(s))
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(input)) == 1 {
			return s, true
		}
	}
	return 0, false
}

// NowStep validates with the current clock and returns the accepted step (for anti-replay).
func NowStep(secret, input string) (int64, bool) { return ValidateStep(secret, input, time.Now()) }
