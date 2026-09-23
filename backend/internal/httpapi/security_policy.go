package httpapi

import (
	"net/http"

	"github.com/beskosorg/beskos/backend/ee"
)

// ORGANIZATION-level security policy (admin/security): MFA required for the whole tenant, with
// the METHOD chosen by the admin ('any' = the user chooses; 'totp'/'sms' = only that one counts).
// Turning the policy ON kills every session in the tenant (except the admin's who enabled it):
// everyone re-enters and hits the webmail enrollment gate.
//
// Reading the policy is Community; setting it is Pro -- see handleSetSecurityPolicyGate and
// ee/security_policy_pro.go.

func (s *Server) handleGetSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	t, err := s.q.GetTenant(r.Context(), u.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "security.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mfa_required": t.MfaRequired, "mfa_method": t.MfaMethod})
}

// handleSetSecurityPolicyGate enforces the Pro OrgPolicies capability before handing off to
// ee.SetSecurityPolicy for the actual logic.
func (s *Server) handleSetSecurityPolicyGate(w http.ResponseWriter, r *http.Request) {
	if !s.edition.OrgPolicies {
		writeErr(w, http.StatusForbidden, "security.edition_required")
		return
	}
	ee.SetSecurityPolicy(w, r, ee.Deps{Queries: s.q})
}

// mfaSatisfied says whether the user meets the policy (method required by the tenant).
func mfaSatisfied(method string, totpOn, smsOn bool) bool {
	switch method {
	case "totp":
		return totpOn
	case "sms":
		return smsOn
	default: // any
		return totpOn || smsOn
	}
}
