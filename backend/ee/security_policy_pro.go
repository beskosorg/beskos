//go:build pro

package ee

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/httpx"
	"github.com/beskosorg/beskos/backend/internal/reqauth"
)

// Setting the organization-wide security policy (MFA required for everyone, with the method
// chosen by the admin) is Enterprise; reading the current policy stays Community
// (httpapi's handleGetSecurityPolicy) -- it is informational, not a control, and nothing else in
// the product needs it: the login gate itself reads the tenant's stored MFA fields directly, not
// through this handler. Only the ability to CHANGE the policy is the paid capability -- personal
// MFA enrollment (mfa_handlers.go) is untouched and stays free either way (project law §3.2).

// SetSecurityPolicy turns the organization's MFA requirement on or off. Turning it ON drops every
// other session in the tenant, so everyone re-enters and hits the enrollment gate.
func SetSecurityPolicy(w http.ResponseWriter, r *http.Request, deps Deps) {
	u := reqauth.MailUserFrom(r.Context())
	var req struct {
		MFARequired bool   `json:"mfa_required"`
		MFAMethod   string `json:"mfa_method"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.MFAMethod == "" {
		req.MFAMethod = "any"
	}
	if req.MFAMethod != "any" && req.MFAMethod != "totp" && req.MFAMethod != "sms" {
		httpx.WriteErr(w, http.StatusBadRequest, "security.invalid_method")
		return
	}
	if err := deps.Queries.SetTenantMFAPolicy(r.Context(), db.SetTenantMFAPolicyParams{
		ID: u.TenantID, MfaRequired: req.MFARequired, MfaMethod: req.MFAMethod,
	}); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "security.failed")
		return
	}
	action := "security.mfa_required_off"
	if req.MFARequired {
		action = "security.mfa_required_on"
		// Drops every session in the tenant (except the current one): next access = login + gate.
		_ = deps.Queries.DeleteTenantWebmailSessions(r.Context(), db.DeleteTenantWebmailSessionsParams{
			TenantID: u.TenantID, KeepToken: sessionToken(r),
		})
	}
	_ = deps.Queries.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email,
		Action: action, Target: req.MFAMethod,
	})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"mfa_required": req.MFARequired, "mfa_method": req.MFAMethod})
}

// sessionToken mirrors httpapi's own (webmail_auth.go): which session to keep when the others are
// dropped. Small and pure enough that duplicating it here beats a shared package for one caller.
func sessionToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("wm_session"); err == nil {
		return c.Value
	}
	return r.URL.Query().Get("access_token")
}
