package httpapi

import (
	"strings"
	"testing"
	"time"
)

// FuzzParsePresToken targets the HMAC token that authorizes two PUBLIC, unauthenticated endpoints
// (GET /meet/presentation/{id}/pdf and GET /meet/presentation-src/{token}): the token is the sole
// authorization, so its parsing (base64 split, HMAC check, "|"-delimited payload, int64 exp) runs
// on fully untrusted input before anything else. It must never panic no matter how it is
// malformed, and a token that was not minted with the real secret (or was minted for a different
// kind/expired) must never be accepted.
func FuzzParsePresToken(f *testing.F) {
	o := &onlyoffice{tokenSecret: []byte("test-secret")}
	validPDF := o.mintPresToken("pdf", "123e4567-e89b-12d3-a456-426614174000", time.Hour)
	validSrc := o.mintPresToken("src", "tenant/meet-src/abc.pptx", 10*time.Minute)
	expired := o.mintPresToken("pdf", "123e4567-e89b-12d3-a456-426614174000", -time.Hour)

	seeds := []string{
		validPDF,
		validSrc,
		expired,
		"",
		".",
		"..",
		"a.b",
		"a.b.c",
		validPDF + "x",
		strings.Replace(validPDF, ".", "", 1),
		strings.Repeat("A", 10000),
		strings.Repeat("A", 10000) + "." + strings.Repeat("B", 10000),
		"not-base64!!.also-not-base64!!",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, tok string) {
		// The payload is HMAC-authenticated, so any accepted token must decode to exactly the
		// plaintext value we signed — forging a different value would mean HMAC-SHA256 (or its
		// constant-time comparison) is broken. Note: this does NOT require tok to be byte-identical
		// to the minted token string — encoding/base64 documents that '\r'/'\n' are skipped during
		// decoding, so cosmetic mutations that only insert those bytes decode to the same payload
		// and legitimately re-validate; that is expected stdlib behavior, not a forgery.
		if val, ok := o.parsePresToken(tok, "pdf"); ok {
			if val != "123e4567-e89b-12d3-a456-426614174000" {
				t.Fatalf("parsePresToken accepted a forged pdf token: %q -> %q", tok, val)
			}
		}
		if val, ok := o.parsePresToken(tok, "src"); ok {
			if val != "tenant/meet-src/abc.pptx" {
				t.Fatalf("parsePresToken accepted a forged src token: %q -> %q", tok, val)
			}
		}
	})
}
