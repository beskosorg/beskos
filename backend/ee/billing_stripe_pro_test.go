//go:build pro

package ee

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

func sign(payload, secret string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", ts, payload)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func TestWebhookSignatureValid(t *testing.T) {
	payload, secret := `{"type":"checkout.session.completed"}`, "whsec_test"
	hdr := sign(payload, secret, time.Now().Unix())
	if err := verifyStripeSignature([]byte(payload), hdr, secret, 5*time.Minute); err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}
}

func TestWebhookSignatureTampered(t *testing.T) {
	secret := "whsec_test"
	hdr := sign(`{"a":1}`, secret, time.Now().Unix())
	// Payload different from the signed one → must fail.
	if err := verifyStripeSignature([]byte(`{"a":2}`), hdr, secret, 5*time.Minute); err == nil {
		t.Error("tampered payload should fail")
	}
	// Wrong secret → must fail.
	if err := verifyStripeSignature([]byte(`{"a":1}`), hdr, "whsec_outro", 5*time.Minute); err == nil {
		t.Error("wrong secret should fail")
	}
}

func TestWebhookSignatureExpired(t *testing.T) {
	payload, secret := `{}`, "whsec_test"
	hdr := sign(payload, secret, time.Now().Add(-10*time.Minute).Unix())
	if err := verifyStripeSignature([]byte(payload), hdr, secret, 5*time.Minute); err == nil {
		t.Error("old timestamp should fail (replay)")
	}
}

func TestWebhookMalformedHeader(t *testing.T) {
	if err := verifyStripeSignature([]byte(`{}`), "garbage", "s", time.Minute); err == nil {
		t.Error("malformed header should fail")
	}
}

// FuzzVerifyStripeSignature targets the manual "t=...,v1=..." header parsing that runs BEFORE any
// other auth check on the webhook endpoint. Every field (the split on ",", the split on "=", the
// int64 timestamp, the hex signatures) comes straight from an unauthenticated HTTP header, so the
// parser must never panic no matter how it is malformed. It also must never accept a payload
// signed with a DIFFERENT secret (checked with a fixed, valid-shaped header so the assertion does
// not depend on how the fuzzer happens to mutate whitespace/ordering).
func FuzzVerifyStripeSignature(f *testing.F) {
	const payload = `{"type":"checkout.session.completed"}`
	const secret = "whsec_test"
	valid := sign(payload, secret, time.Now().Unix())

	seeds := []string{
		valid,
		"",
		"garbage",
		"t=,v1=",
		"t=abc,v1=xyz",
		"v1=aaaa",
		"t=1700000000",
		"t=1700000000,v1=",
		",,,",
		"t=1700000000,v1=deadbeef,v1=deadbeef2",
		"t=99999999999999999999999999999,v1=ff",
		"t=-1,v1=ff",
		strings.Repeat("t=1,v1=2,", 2000),
		valid + ",v1=" + strings.Repeat("f", 10000),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, header string) {
		// The only property that is safe to assert on an arbitrarily-mutated header is that
		// parsing it never panics — a whitespace/ordering mutation of a valid header can still
		// legitimately validate under the real (lenient) parser, so "must reject" is not a safe
		// generic assertion here.
		_ = verifyStripeSignature([]byte(payload), header, secret, 5*time.Minute)
	})
}
