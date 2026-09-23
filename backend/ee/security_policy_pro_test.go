//go:build pro

package ee

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/reqauth"
)

func TestSetSecurityPolicyRejectsInvalidMethod(t *testing.T) {
	r := httptest.NewRequest(http.MethodPut, "/api/org/security", bytes.NewReader([]byte(`{"mfa_required":true,"mfa_method":"carrier-pigeon"}`)))
	r = r.WithContext(reqauth.WithMailUser(r.Context(), db.MailUser{}))
	w := httptest.NewRecorder()

	SetSecurityPolicy(w, r, Deps{})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestSessionTokenPrefersBearer(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer abc123")
	r.AddCookie(&http.Cookie{Name: "wm_session", Value: "cookie-token"})
	if got := sessionToken(r); got != "abc123" {
		t.Errorf("sessionToken = %q, want abc123", got)
	}
}

func TestSessionTokenFallsBackToCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "wm_session", Value: "cookie-token"})
	if got := sessionToken(r); got != "cookie-token" {
		t.Errorf("sessionToken = %q, want cookie-token", got)
	}
}
