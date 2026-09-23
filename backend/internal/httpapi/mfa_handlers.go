package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/notify"
	"github.com/beskosorg/beskos/backend/internal/totp"
)

const (
	mfaChallengeTTL = 5 * time.Minute
	mfaMaxAttempts  = 5
	recoveryCount   = 10
)

// ---- Two-step login ----

// startMFAChallenge issues a challenge after the correct password. If SMS is enabled, sends the code.
func (s *Server) startMFAChallenge(w http.ResponseWriter, r *http.Request, u db.MailUser) {
	token, err := auth.NewSessionToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "mfa.failed")
		return
	}
	smsHash := ""
	methods := []string{}
	if u.MfaTotpEnabled {
		methods = append(methods, "totp")
	}
	if u.MfaSmsEnabled {
		methods = append(methods, "sms")
		code := numericCode(6)
		if h, err := auth.HashPassword(code); err == nil {
			smsHash = h
			_ = s.sms.Send(r.Context(), u.Phone, notify.MFACode(u.Language, code))
		}
	}
	if err := s.q.CreateMFAChallenge(r.Context(), db.CreateMFAChallengeParams{
		Token: token, UserID: u.ID, SmsCodeHash: smsHash, ExpiresAt: time.Now().Add(mfaChallengeTTL),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "mfa.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mfa_required": true, "challenge": token, "methods": methods})
}

type mfaVerifyReq struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

// handleMFAVerify exchanges a challenge plus a second factor for a session. Accepts TOTP, an
// SMS code, or a recovery code (single use). Rate-limited per challenge (mfaMaxAttempts) against
// brute force.
func (s *Server) handleMFAVerify(w http.ResponseWriter, r *http.Request) {
	// Rate limit per IP in addition to the per-challenge limit: without it, an attacker who has
	// the password can request a new challenge every 5 guesses and brute-force the 6-digit code.
	if !s.mfaIP.allow("mfa:"+clientIP(r), time.Now()) {
		writeErr(w, http.StatusTooManyRequests, "mfa.too_many")
		return
	}
	var req mfaVerifyReq
	if !decode(w, r, &req) {
		return
	}
	ch, err := s.q.GetMFAChallenge(r.Context(), req.Challenge)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "mfa.invalid_challenge")
		return
	}
	attempts, _ := s.q.IncrMFAChallengeAttempts(r.Context(), req.Challenge)
	if attempts > mfaMaxAttempts {
		_ = s.q.DeleteMFAChallenge(r.Context(), req.Challenge)
		writeErr(w, http.StatusTooManyRequests, "mfa.too_many")
		return
	}
	u, err := s.q.GetUserByID(r.Context(), ch.UserID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "mfa.invalid_challenge")
		return
	}
	if !s.checkSecondFactor(r.Context(), u, ch, req.Code) {
		writeErr(w, http.StatusUnauthorized, "mfa.invalid_code")
		return
	}
	_ = s.q.DeleteMFAChallenge(r.Context(), req.Challenge)
	token, ok := s.issueSession(w, r.Context(), u)
	if !ok {
		return
	}
	// With a second factor, the session is only created after the code succeeds, so this is the
	// actual login moment.
	s.alertNewLogin(r, u)
	writeJSON(w, http.StatusOK, map[string]any{"user_id": u.ID, "token": token})
}

// checkSecondFactor tries TOTP, then SMS, then recovery codes (consuming the one used).
func (s *Server) checkSecondFactor(ctx context.Context, u db.MailUser, ch db.MfaChallenge, code string) bool {
	if u.MfaTotpEnabled && u.TotpSecret != "" {
		if step, ok := totp.NowStep(s.decTOTP(u.TotpSecret), code); ok {
			// Anti-replay: only accepts a step not yet used. SetTOTPLastStep only advances (guards
			// `$2 > last_totp_step`), so a repeated step doesn't rewrite it — but that alone doesn't
			// block it here if the clock ties; the strict check is below.
			if step <= u.LastTotpStep {
				return false // code for this step (or an earlier one) was already used
			}
			_ = s.q.SetTOTPLastStep(ctx, db.SetTOTPLastStepParams{ID: u.ID, LastTotpStep: step})
			return true
		}
	}
	if u.MfaSmsEnabled && ch.SmsCodeHash != "" && auth.CheckPassword(ch.SmsCodeHash, code) {
		return true
	}
	// Recovery codes (single use).
	codes, err := s.q.ListActiveRecoveryCodes(ctx, u.ID)
	if err == nil {
		for _, rc := range codes {
			if auth.CheckPassword(rc.CodeHash, code) {
				_ = s.q.UseRecoveryCode(ctx, rc.ID)
				return true
			}
		}
	}
	return false
}

// ---- TOTP enrollment ----

func (s *Server) handleTOTPStart(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	secret, err := totp.GenerateSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "mfa.failed")
		return
	}
	// Stores the secret ENCRYPTED at rest; returns the plaintext only now (for the QR/authenticator app).
	if err := s.q.SetTOTPSecret(r.Context(), db.SetTOTPSecretParams{ID: u.ID, TotpSecret: s.encTOTP(secret)}); err != nil {
		writeErr(w, http.StatusInternalServerError, "mfa.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauth_uri": totp.ProvisioningURI(secret, u.Email)})
}

type codeReq struct {
	Code string `json:"code"`
}

// handleTOTPEnable confirms TOTP (code from the app) and activates it; returns the recovery codes ONCE.
func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	fresh, err := s.q.GetUserByID(r.Context(), u.ID)
	if err != nil || fresh.TotpSecret == "" {
		writeErr(w, http.StatusBadRequest, "mfa.not_started")
		return
	}
	var req codeReq
	if !decode(w, r, &req) {
		return
	}
	if !totp.Now(s.decTOTP(fresh.TotpSecret), req.Code) {
		writeErr(w, http.StatusBadRequest, "mfa.invalid_code")
		return
	}
	if err := s.q.EnableTOTP(r.Context(), u.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "mfa.failed")
		return
	}
	codes := s.regenRecoveryCodes(w, r.Context(), u.ID)
	if codes == nil {
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "mfa.totp_enabled", Target: ""})
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "recovery_codes": codes})
}

// ---- SMS enrollment ----

type smsStartReq struct {
	Phone string `json:"phone"`
}

func (s *Server) handleSMSStart(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	// Rate-limited per IP and per account (audit): unlike the login-time SMS send, which is
	// only reachable after the correct password and is bounded by loginUser, this is an ordinary
	// authenticated endpoint that accepts an arbitrary phone number and fires a real SMS -- without
	// a limit here, any signed-up account can script it into an SMS bomb against any phone number.
	now := time.Now()
	if !s.smsIP.allow("sms:"+clientIP(r), now) || !s.smsUser.allow("sms:"+u.ID.String(), now) {
		writeErr(w, http.StatusTooManyRequests, "mfa.too_many")
		return
	}
	var req smsStartReq
	if !decode(w, r, &req) {
		return
	}
	if req.Phone == "" {
		writeErr(w, http.StatusBadRequest, "mfa.phone_required")
		return
	}
	_ = s.q.SetPhone(r.Context(), db.SetPhoneParams{ID: u.ID, Phone: req.Phone})
	token, _ := auth.NewSessionToken()
	code := numericCode(6)
	hash, _ := auth.HashPassword(code)
	if err := s.q.CreateMFAChallenge(r.Context(), db.CreateMFAChallengeParams{
		Token: token, UserID: u.ID, SmsCodeHash: hash, ExpiresAt: time.Now().Add(mfaChallengeTTL),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "mfa.failed")
		return
	}
	_ = s.sms.Send(r.Context(), req.Phone, notify.MFAEnrollCode(u.Language, code))
	writeJSON(w, http.StatusOK, map[string]string{"challenge": token})
}

type smsEnableReq struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

func (s *Server) handleSMSEnable(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req smsEnableReq
	if !decode(w, r, &req) {
		return
	}
	ch, err := s.q.GetMFAChallenge(r.Context(), req.Challenge)
	if err != nil || ch.UserID != u.ID {
		writeErr(w, http.StatusUnauthorized, "mfa.invalid_challenge")
		return
	}
	if ch.SmsCodeHash == "" || !auth.CheckPassword(ch.SmsCodeHash, req.Code) {
		writeErr(w, http.StatusBadRequest, "mfa.invalid_code")
		return
	}
	_ = s.q.EnableSMS(r.Context(), u.ID)
	_ = s.q.DeleteMFAChallenge(r.Context(), req.Challenge)
	codes := s.regenRecoveryCodes(w, r.Context(), u.ID)
	if codes == nil {
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "mfa.sms_enabled", Target: ""})
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "recovery_codes": codes})
}

type mfaDisableReq struct {
	Password string `json:"password"`
}

func (s *Server) handleMFADisable(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req mfaDisableReq
	if !decode(w, r, &req) {
		return
	}
	// Step-up auth: disabling the second factor (and deleting the recovery codes) requires
	// re-authenticating with the password. Without this, a hijacked session could silently
	// remove MFA. Organization policy (admin/security): when MFA is required, disabling it is
	// refused on the server too — hiding the button in the UI is not a barrier (defense in depth).
	if t, terr := s.q.GetTenant(r.Context(), u.TenantID); terr == nil && t.MfaRequired {
		writeErr(w, http.StatusForbidden, "mfa.org_required")
		return
	}
	fresh, err := s.q.GetUserByID(r.Context(), u.ID)
	if err != nil || !s.verifyUserPassword(r.Context(), fresh, req.Password) {
		writeErr(w, http.StatusForbidden, "auth.invalid_credentials")
		return
	}
	_ = s.q.DisableMFA(r.Context(), u.ID)
	_ = s.q.DeleteRecoveryCodes(r.Context(), u.ID)
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "mfa.disabled", Target: ""})
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

// ---- helpers ----

// regenRecoveryCodes replaces the recovery codes and returns the new ones IN PLAINTEXT (only here).
func (s *Server) regenRecoveryCodes(w http.ResponseWriter, ctx context.Context, userID uuid.UUID) []string {
	_ = s.q.DeleteRecoveryCodes(ctx, userID)
	out := make([]string, 0, recoveryCount)
	for i := 0; i < recoveryCount; i++ {
		c := recoveryCode()
		hash, err := auth.HashPassword(c)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "mfa.failed")
			return nil
		}
		if err := s.q.InsertRecoveryCode(ctx, db.InsertRecoveryCodeParams{UserID: userID, CodeHash: hash}); err != nil {
			writeErr(w, http.StatusInternalServerError, "mfa.failed")
			return nil
		}
		out = append(out, c)
	}
	return out
}

// numericCode generates an n-digit numeric code (crypto/rand) with no modulo bias: rejects
// bytes ≥ 250 (250 = 25×10), guaranteeing a uniform distribution across the 10 digits.
func numericCode(n int) string {
	out := make([]byte, n)
	buf := make([]byte, 1)
	for i := 0; i < n; {
		if _, err := rand.Read(buf); err != nil {
			continue
		}
		if buf[0] >= 250 {
			continue // discard to avoid bias (256 is not a multiple of 10)
		}
		out[i] = '0' + buf[0]%10
		i++
	}
	return string(out)
}

var b32nopad = base32.StdEncoding.WithPadding(base32.NoPadding)

// recoveryCode generates a human-readable recovery code (e.g. "K3M7-Q9AZ").
func recoveryCode() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	s := b32nopad.EncodeToString(b)
	return fmt.Sprintf("%s-%s", s[:4], s[4:8])
}
