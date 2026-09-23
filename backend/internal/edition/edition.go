// Package edition determines which product capabilities a running instance
// unlocks. The Community edition unlocks none; a signed Pro license key
// unlocks the set it was issued for. Verification is offline: the running
// process never calls out to a licensing server to decide what it can do.
package edition

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Name identifies which edition a set of capabilities came from.
type Name string

const (
	Community Name = "community"
	Pro       Name = "pro"
)

// gracePeriod keeps Pro capabilities active for a while after a license
// expires, so a delayed renewal never cuts a customer off abruptly.
const gracePeriod = 14 * 24 * time.Hour

// Capabilities is what a license key grants. The zero value is the
// Community edition: every feature flag off, seats unlimited.
type Capabilities struct {
	Edition Name      `json:"edition"`
	Org     string    `json:"org,omitempty"`
	Seats   int       `json:"seats,omitempty"` // 0 = unlimited
	Expires time.Time `json:"expires,omitempty"`

	Recording   bool `json:"recording"`
	Branding    bool `json:"branding"`
	AuditLog    bool `json:"audit_log"`
	OrgPolicies bool `json:"org_policies"`
	Agents      bool `json:"agents"`
}

// CommunityCapabilities is the fallback when no license key is configured or
// verification fails: every Pro feature off, no seat limit.
func CommunityCapabilities() Capabilities {
	return Capabilities{Edition: Community}
}

// payload is the JSON body signed by the license issuer (backend/cmd/... in
// the private beskos-cloud repository holds the signing key; this package
// only ever holds the public key).
type payload struct {
	Org     string    `json:"org"`
	Seats   int       `json:"seats"`
	Expires time.Time `json:"expires"`

	Recording   bool `json:"recording"`
	Branding    bool `json:"branding"`
	AuditLog    bool `json:"audit_log"`
	OrgPolicies bool `json:"org_policies"`
	Agents      bool `json:"agents"`
}

// Verify checks a license key of the form "<base64url payload>.<base64url
// Ed25519 signature>" against publicKey and returns the capabilities it
// grants. An expired key still verifies within gracePeriod, so a renewal
// delay degrades gracefully instead of an abrupt cutoff.
func Verify(licenseKey string, publicKey ed25519.PublicKey) (Capabilities, error) {
	body, sig, ok := strings.Cut(licenseKey, ".")
	if !ok {
		return Capabilities{}, fmt.Errorf("edition: malformed license key")
	}
	rawBody, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Capabilities{}, fmt.Errorf("edition: decode payload: %w", err)
	}
	rawSig, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return Capabilities{}, fmt.Errorf("edition: decode signature: %w", err)
	}
	if !ed25519.Verify(publicKey, rawBody, rawSig) {
		return Capabilities{}, fmt.Errorf("edition: invalid signature")
	}
	var p payload
	if err := json.Unmarshal(rawBody, &p); err != nil {
		return Capabilities{}, fmt.Errorf("edition: decode license: %w", err)
	}
	if !p.Expires.IsZero() && time.Now().After(p.Expires.Add(gracePeriod)) {
		return Capabilities{}, fmt.Errorf("edition: license expired on %s", p.Expires.Format(time.DateOnly))
	}
	return Capabilities{
		Edition: Pro, Org: p.Org, Seats: p.Seats, Expires: p.Expires,
		Recording: p.Recording, Branding: p.Branding, AuditLog: p.AuditLog,
		OrgPolicies: p.OrgPolicies, Agents: p.Agents,
	}, nil
}

// Load reads EDITION_LICENSE_KEY and EDITION_PUBLIC_KEY from the environment
// and returns the capabilities they grant, falling back to Community when
// either is empty. A configured but invalid key is an error: silently
// downgrading a paying customer to Community would hide a misconfiguration
// (law §2.9 -- "not yet" must stay distinguishable from "never").
func Load() (Capabilities, error) {
	key := os.Getenv("EDITION_LICENSE_KEY")
	pub := os.Getenv("EDITION_PUBLIC_KEY")
	if key == "" || pub == "" {
		return CommunityCapabilities(), nil
	}
	rawPub, err := base64.StdEncoding.DecodeString(pub)
	if err != nil {
		return Capabilities{}, fmt.Errorf("edition: decode EDITION_PUBLIC_KEY: %w", err)
	}
	if len(rawPub) != ed25519.PublicKeySize {
		return Capabilities{}, fmt.Errorf("edition: EDITION_PUBLIC_KEY has %d bytes, want %d", len(rawPub), ed25519.PublicKeySize)
	}
	caps, err := Verify(key, ed25519.PublicKey(rawPub))
	if err != nil {
		return Capabilities{}, fmt.Errorf("edition: EDITION_LICENSE_KEY: %w", err)
	}
	return caps, nil
}
