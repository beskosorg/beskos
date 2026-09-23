package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// The AI token is the "API key" the OnlyOffice plugin uses to reach the proxy (cross-origin, no
// cookie). It must round-trip with user+tenant, reject a tampered signature, and reject expired
// ones — otherwise it would be a hole to get in without an identity.
func TestAIToken(t *testing.T) {
	o := &onlyoffice{tokenSecret: []byte("test-secret-32-bytes-long-enough!")}
	user, tenant := uuid.New(), uuid.New()

	tok := o.mintAIToken(user, tenant, time.Now().Add(time.Hour))
	gotUser, gotTenant, ok := o.parseAIToken(tok)
	if !ok || gotUser != user || gotTenant != tenant {
		t.Fatalf("round trip failed: ok=%v user=%v tenant=%v", ok, gotUser, gotTenant)
	}

	// Tampered signature -> rejected.
	if _, _, ok := o.parseAIToken(tok + "x"); ok {
		t.Error("accepted tampered token")
	}

	// A different secret doesn't validate this server's token.
	other := &onlyoffice{tokenSecret: []byte("another-secret-different-key-here")}
	if _, _, ok := other.parseAIToken(tok); ok {
		t.Error("a different secret validated the token")
	}

	// Expired -> rejected.
	expired := o.mintAIToken(user, tenant, time.Now().Add(-time.Minute))
	if _, _, ok := o.parseAIToken(expired); ok {
		t.Error("accepted expired token")
	}
}
