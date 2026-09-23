package totp

import (
	"testing"
	"time"
)

func TestGenerateAndValidate(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	c, err := code(secret, uint64(now.Unix())/period)
	if err != nil {
		t.Fatal(err)
	}
	if !Validate(secret, c, now) {
		t.Errorf("valid code rejected")
	}
	if Validate(secret, "000000", now) && c != "000000" {
		t.Errorf("wrong code accepted")
	}
}

func TestWindowTolerance(t *testing.T) {
	secret, _ := GenerateSecret()
	now := time.Unix(1_700_000_000, 0)
	prev, _ := code(secret, uint64(now.Unix())/period-1) // previous step
	if !Validate(secret, prev, now) {
		t.Errorf("previous step (±1) should be accepted")
	}
	old, _ := code(secret, uint64(now.Unix())/period-5) // outside the window
	if Validate(secret, old, now) {
		t.Errorf("distant step should NOT be accepted")
	}
}

func TestRFC6238Vector(t *testing.T) {
	// RFC 6238 test vector (SHA1): ASCII secret "12345678901234567890".
	// Base32 of the ASCII above:
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	// At T=59s (counter 1) the 8-digit code is 94287082; 6 digits = 287082.
	got, err := code(secret, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "287082" {
		t.Errorf("RFC 6238 vector: got %s, want 287082", got)
	}
}

func TestBadInputs(t *testing.T) {
	secret, _ := GenerateSecret()
	now := time.Now()
	for _, in := range []string{"", "12345", "1234567", "abcdef"} {
		if Validate(secret, in, now) {
			t.Errorf("invalid input %q accepted", in)
		}
	}
}

func TestProvisioningURI(t *testing.T) {
	uri := ProvisioningURI("ABC", "user@x.com")
	if !contains(uri, "otpauth://totp/") || !contains(uri, "secret=ABC") || !contains(uri, "issuer=Beskos") {
		t.Errorf("unexpected URI: %s", uri)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
