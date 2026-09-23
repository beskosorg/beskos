package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

// Without the OrgPolicies capability, setting the organization's security policy must be refused
// before ever touching the database or the authenticated user in context.
func TestSetSecurityPolicyGateRefusesWithoutCapability(t *testing.T) {
	srv := NewServer(nil, stalwart.New("http://stalwart.invalid", "", ""), nil, nil, nil,
		billing.ManualProvider{}, billing.Settings{}, edition.CommunityCapabilities())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/org/security", nil)

	srv.handleSetSecurityPolicyGate(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if got := w.Body.String(); !strings.Contains(got, "security.edition_required") {
		t.Errorf("body = %q, want it to contain security.edition_required", got)
	}
}
