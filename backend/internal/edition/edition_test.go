package edition

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func sign(t *testing.T, priv ed25519.PrivateKey, p payload) string {
	t.Helper()
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	sig := ed25519.Sign(priv, body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestCommunityCapabilities(t *testing.T) {
	c := CommunityCapabilities()
	if c.Edition != Community || c.Recording || c.Branding || c.Seats != 0 {
		t.Fatalf("community capabilities not empty: %+v", c)
	}
}

func TestVerifyValidKey(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	key := sign(t, priv, payload{Org: "acme", Seats: 25, Recording: true, Branding: true})

	caps, err := Verify(key, pub)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if caps.Edition != Pro || caps.Org != "acme" || caps.Seats != 25 || !caps.Recording || !caps.Branding {
		t.Fatalf("unexpected capabilities: %+v", caps)
	}
}

func TestVerifyTamperedPayload(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	key := sign(t, priv, payload{Org: "acme", Seats: 5})

	body, sig := splitTwo(key)
	tampered := swapOrg(t, body) + "." + sig

	if _, err := Verify(tampered, pub); err == nil {
		t.Fatal("Verify accepted a tampered payload")
	}
}

func TestVerifyWrongKey(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	key := sign(t, otherPriv, payload{Org: "acme"})

	if _, err := Verify(key, pub); err == nil {
		t.Fatal("Verify accepted a signature from a different key")
	}
}

func TestVerifyExpiredOutsideGrace(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	key := sign(t, priv, payload{Org: "acme", Expires: time.Now().Add(-gracePeriod - 24*time.Hour)})

	if _, err := Verify(key, pub); err == nil {
		t.Fatal("Verify accepted a license expired past the grace period")
	}
}

func TestVerifyExpiredWithinGrace(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	key := sign(t, priv, payload{Org: "acme", Expires: time.Now().Add(-24 * time.Hour), Recording: true})

	caps, err := Verify(key, pub)
	if err != nil {
		t.Fatalf("Verify rejected a license inside its grace period: %v", err)
	}
	if caps.Edition != Pro || !caps.Recording {
		t.Fatalf("grace-period capabilities missing: %+v", caps)
	}
}

func TestVerifyMalformedKey(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	if _, err := Verify("not-a-license-key", pub); err == nil {
		t.Fatal("Verify accepted a key with no signature segment")
	}
}

func TestLoadWithoutEnvIsCommunity(t *testing.T) {
	t.Setenv("EDITION_LICENSE_KEY", "")
	t.Setenv("EDITION_PUBLIC_KEY", "")

	caps, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if caps.Edition != Community {
		t.Fatalf("expected community edition, got %+v", caps)
	}
}

func TestLoadWithValidKey(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	key := sign(t, priv, payload{Org: "acme", Seats: 10, Agents: true})

	t.Setenv("EDITION_PUBLIC_KEY", base64.StdEncoding.EncodeToString(pub))
	t.Setenv("EDITION_LICENSE_KEY", key)

	caps, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if caps.Edition != Pro || caps.Org != "acme" || !caps.Agents {
		t.Fatalf("unexpected capabilities: %+v", caps)
	}
}

func TestLoadRejectsInvalidKeyRatherThanDowngrading(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	t.Setenv("EDITION_PUBLIC_KEY", base64.StdEncoding.EncodeToString(pub))
	t.Setenv("EDITION_LICENSE_KEY", "garbage.garbage")

	if _, err := Load(); err == nil {
		t.Fatal("Load silently downgraded an invalid license to Community")
	}
}

func TestLoadRejectsMalformedPublicKey(t *testing.T) {
	t.Setenv("EDITION_PUBLIC_KEY", "not-base64!!")
	t.Setenv("EDITION_LICENSE_KEY", "anything.anything")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a malformed EDITION_PUBLIC_KEY")
	}
}

// splitTwo and swapOrg help TestVerifyTamperedPayload construct a payload
// that decodes but no longer matches its signature.
func splitTwo(key string) (body, sig string) {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '.' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

func swapOrg(t *testing.T, body string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	p.Org = "attacker"
	p.Seats = 999999
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal tampered body: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(out)
}
