package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/rbac"
)

// dummyBcryptHash is a fixed hash used to burn bcrypt time on a login with a nonexistent email
// (timing-based anti-enumeration). Computed once at startup.
var dummyBcryptHash, _ = auth.HashPassword("mailin-login-timing-equalizer")

// verifyUserPassword checks the password against the right source: Stalwart when there's a
// mailbox, otherwise the local hash. Used at login and as a step-up (re-authentication) for
// sensitive actions (e.g. disabling MFA).
func (s *Server) verifyUserPassword(ctx context.Context, u db.MailUser, password string) bool {
	if password == "" {
		return false
	}
	if u.StalwartAccountID != nil {
		ok, err := s.sw.VerifyUser(ctx, u.Email, password)
		return err == nil && ok
	}
	return auth.CheckPassword(u.PasswordHash, password)
}

type signupReq struct {
	OrgName  string `json:"org_name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleSignup creates the tenant, the Stripe customer, and the tenant's first admin, and
// responds immediately. Domain/mailbox provisioning is asynchronous, driven by a state machine.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req signupReq
	if !decode(w, r, &req) {
		return
	}
	if req.Email == "" || req.Password == "" || req.OrgName == "" {
		writeErr(w, http.StatusBadRequest, "auth.missing_fields")
		return
	}

	tenant, err := s.q.CreateTenant(r.Context(), db.CreateTenantParams{Name: req.OrgName, Plan: "trial"})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "tenant.create_failed")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "auth.password_hash_failed")
		return
	}
	u, err := s.q.CreateAccount(r.Context(), db.CreateAccountParams{
		TenantID:     tenant.ID,
		Email:        req.Email,
		PasswordHash: hash,
		Role:         string(rbac.RoleOwner), // first user = organization owner
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "auth.email_taken")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: tenant.ID, Valid: true}, Actor: req.Email, Action: "signup", Target: tenant.ID.String(),
	})
	token, ok := s.issueSession(w, r.Context(), u)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"tenant_id": tenant.ID, "user_id": u.ID, "token": token})
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	// Anti brute-force / credential stuffing (audit finding H5): rate-limits by IP and by account
	// before spending bcrypt time or a round trip to Stalwart. The password is checked here, so
	// without this the login stays open to online guessing even with MFA (MFA comes after the password).
	if !s.loginIP.allow("login:"+clientIP(r), now) {
		writeErr(w, http.StatusTooManyRequests, "auth.rate_limited")
		return
	}
	var req loginReq
	if !decode(w, r, &req) {
		return
	}
	if req.Email != "" && !s.loginUser.allow("login:"+strings.ToLower(req.Email), now) {
		writeErr(w, http.StatusTooManyRequests, "auth.rate_limited")
		return
	}
	u, err := s.q.GetMailUserByEmail(r.Context(), req.Email)
	if err != nil {
		// Anti-enumeration: burns a bcrypt cycle "for nothing" to match the timing of the path
		// where the account exists (otherwise a nonexistent email would respond faster and leak
		// which ones exist).
		auth.CheckPassword(dummyBcryptHash, req.Password)
		writeErr(w, http.StatusUnauthorized, "auth.invalid_credentials")
		return
	}
	// A suspended or deleted mailbox has no credentials in Stalwart, so the password check below
	// would fail anyway -- but it would fail as "wrong password", which sends somebody to reset a
	// password that will not help. Say the true thing instead, and say it before touching Stalwart.
	// It also covers accounts with no mailbox, where nothing else would stop a suspended person.
	if u.Status != "active" {
		writeErr(w, http.StatusForbidden, "auth.account_suspended")
		return
	}
	if u.StalwartAccountID != nil {
		// Has a mailbox: the credential is Stalwart's (the mailbox is the source of truth for the password).
		ok, verr := s.sw.VerifyUser(r.Context(), req.Email, req.Password)
		if verr != nil {
			writeErr(w, http.StatusBadGateway, "webmail.stalwart_unreachable")
			return
		}
		if !ok {
			writeErr(w, http.StatusUnauthorized, "auth.invalid_credentials")
			return
		}
		// JMAP credential brokering: stores the Basic credential ENCRYPTED server-side for the proxy
		// to inject. The browser no longer stores the mailbox password; it authenticates with the
		// session only. Done here, while the password is known, before the second factor — the
		// session is only issued afterward, so the credential is ready by then.
		basic := base64.StdEncoding.EncodeToString([]byte(req.Email + ":" + req.Password))
		_ = s.q.SetJmapCred(r.Context(), db.SetJmapCredParams{ID: u.ID, JmapCred: s.encTOTP(basic)})
	} else if !auth.CheckPassword(u.PasswordHash, req.Password) {
		// No mailbox (newly created organization, no domain yet): password from our own signup.
		// Constant cost: bcrypt was already computed; nothing to do beyond a generic response.
		writeErr(w, http.StatusUnauthorized, "auth.invalid_credentials")
		return
	}
	// If MFA is enabled, do NOT issue a session yet: return a challenge for the second factor
	// (two-step login).
	if u.MfaTotpEnabled || u.MfaSmsEnabled {
		s.startMFAChallenge(w, r, u)
		return
	}
	token, ok := s.issueSession(w, r.Context(), u)
	if !ok {
		return
	}
	// New-login alert (when, from where, which device): lets someone notice their password was
	// stolen. Best-effort, off the login critical path.
	s.alertNewLogin(r, u)
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role,
		"has_mailbox": u.StalwartAccountID != nil, "token": token,
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	disabled := s.disabledAppsEffective(r.Context(), u.TenantID) // org admin + plan exclusions
	// Assistant enabled: the AI must be configured AND the admin must not have turned it off. False
	// in either case → the frontend hides the assistant bubble (no point offering what it will refuse).
	assistantEnabled := s.ai != nil && s.agent != nil && !s.assistantDisabled(r.Context(), u.TenantID)
	// Organization policy: MFA required and the user still has no second factor → the webmail
	// blocks with an enrollment gate until it's configured (tenant admin/security setting).
	mfaSetupRequired := false
	orgMFARequired := false
	mfaMethod := "any"
	if t, terr := s.q.GetTenant(r.Context(), u.TenantID); terr == nil {
		mfaMethod = t.MfaMethod
		orgMFARequired = t.MfaRequired
		mfaSetupRequired = t.MfaRequired && !mfaSatisfied(t.MfaMethod, u.MfaTotpEnabled, u.MfaSmsEnabled)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": u.ID, "email": u.Email, "role": u.Role, "tenant_id": u.TenantID, "name": u.Name, "avatar": u.Avatar,
		"mfa_setup_required":  mfaSetupRequired,
		"mfa_required_method": mfaMethod,
		"org_mfa_required":    orgMFARequired,
		"language":            u.Language,                                              // "" = automatic (browser); otherwise the saved preference
		"permissions":         s.effectivePerms(r.Context(), u.TenantID, u.ID, u.Role), // role ± exceptions; the UI gates buttons with this
		"mfa":                 map[string]bool{"totp": u.MfaTotpEnabled, "sms": u.MfaSmsEnabled},
		"presence":            u.Presence,
		"disabled_apps":       orEmpty(disabled), // the rail disappears for these; the AI loses their tools
		"assistant_enabled":   assistantEnabled,
		// The organisation's billing state. Here rather than in a call of its own: the warning banner
		// has to be on screen at the first render, not a moment later.
		"account": accountView(s.accountState(r.Context(), u.TenantID)),
		// Whether their own domain is live in DNS. Until it is, no mail arrives and nothing sent from
		// here would pass SPF at the far end -- so the app says so instead of showing an empty inbox.
		"domain": domainView(s.domainPublished(r.Context(), u.Email)),
	})
}

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	domains, err := s.q.ListDomainsByTenant(r.Context(), u.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "domains.list_failed")
		return
	}
	writeJSON(w, http.StatusOK, domains)
}

// --- session / decode helpers ---

// issueSession creates the session, sets the cookie (web same-origin), and RETURNS the token in
// the response body — the native app (Tauri) stores it and sends it as `Authorization: Bearer`
// (without depending on cross-site cookies). Returns ("", false) on failure (the error was
// already written).
func (s *Server) issueSession(w http.ResponseWriter, ctx context.Context, u db.MailUser) (string, bool) {
	token, err := auth.NewSessionToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "auth.session_create_failed")
		return "", false
	}
	if _, err = s.q.CreateWebmailSession(ctx, db.CreateWebmailSessionParams{
		Token: token, MailUserID: u.ID, ExpiresAt: time.Now().Add(sessionTTL),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "auth.session_persist_failed")
		return "", false
	}
	// Cookie: SameSite=None+Secure when served over HTTPS (allows cross-site webview); otherwise
	// Lax (dev over http). The native app doesn't depend on this — it uses the token from the body.
	same, secure := http.SameSiteLaxMode, false
	if s.cookieSecure {
		same, secure = http.SameSiteNoneMode, true
	}
	http.SetCookie(w, &http.Cookie{
		Name: "wm_session", Value: token, Path: "/", HttpOnly: true,
		SameSite: same, Secure: secure, MaxAge: int(sessionTTL.Seconds()),
	})
	return token, true
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "request.invalid_json")
		return false
	}
	return true
}
