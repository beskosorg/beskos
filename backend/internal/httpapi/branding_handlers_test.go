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

// Without the Branding capability, setting branding must be refused before ever touching the
// database or the authenticated user in context -- a Community server has neither wired up here.
func TestUpdateBrandingGateRefusesWithoutCapability(t *testing.T) {
	srv := NewServer(nil, stalwart.New("http://stalwart.invalid", "", ""), nil, nil, nil,
		billing.ManualProvider{}, billing.Settings{}, edition.CommunityCapabilities())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPatch, "/api/branding", nil)

	srv.handleUpdateBrandingGate(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if got := w.Body.String(); !strings.Contains(got, "branding.edition_required") {
		t.Errorf("body = %q, want it to contain branding.edition_required", got)
	}
}
