package httpapi

// Getting back in after forgetting the password.
//
// A MAIL PROVIDER CANNOT DO THE USUAL THING. Everywhere else "forgot your password" sends a link to
// your e-mail; here the e-mail is what you are locked out of. So the way back runs outside the mail:
// the request goes to the organisation's administrators, who already have the button, and — only for
// somebody who set an address outside their own domain — a link goes there too.
//
// THE ANSWER IS ALWAYS THE SAME. Whether the address exists, is suspended, or was never a customer,
// the response is identical: "if that account exists, its administrators have been told". Anything
// else turns this form into a way to find out who works at a company.
//
// AND IT NEVER BYPASSES THE SECOND FACTOR. If the account has MFA, the reset still asks for it.
// Otherwise the recovery inbox becomes the account, and every organisation that turned MFA on to
// protect itself would have handed out a way around it.

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/notify"
)

// resetWindow is how long a recovery link lives. Long enough to walk to another computer, short
// enough that a link left in an inbox is not a spare key.
const resetWindow = time.Hour

type forgotReq struct {
	Email string `json:"email"`
}

// handleForgotPassword takes the request and tells the people who can act on it.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotReq
	if !decode(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))

	// Rate-limited by address as well as by IP: without this the form is a way to have us send mail
	// to anybody, repeatedly, on somebody else's behalf.
	if email == "" || !s.loginUser.allow("forgot:"+email, time.Now()) {
		writeJSON(w, http.StatusOK, forgotAnswer)
		return
	}

	// Everything below is best-effort and silent. The caller gets the same answer either way, so a
	// failure here must not change the shape of the response — including how long it takes.
	go s.startRecovery(context.WithoutCancel(r.Context()), email)

	writeJSON(w, http.StatusOK, forgotAnswer)
}

// forgotAnswer is the only thing this endpoint ever says.
var forgotAnswer = map[string]string{"status": "requested"}

func (s *Server) startRecovery(ctx context.Context, email string) {
	u, err := s.q.GetMailUserByEmail(ctx, email)
	if err != nil {
		return // no such account; the caller was already told the same thing
	}
	if u.Status != "active" {
		// Suspended or deleted. An administrator withdrew this person's access on purpose, and a
		// recovery flow must not be the way around that decision.
		return
	}

	// The administrators, always. This is the path that needs no new secrets and works for the
	// employee who forgot their password on a Monday.
	admins, err := s.q.ListTenantAdmins(ctx, u.TenantID)
	if err != nil {
		log.Printf("password recovery for %s: cannot find the administrators: %v", email, err)
	}
	for _, a := range admins {
		if a.ID == u.ID {
			continue // asking somebody to reset their own password is not help
		}
		if _, err := s.q.CreateNotification(ctx, db.CreateNotificationParams{
			TenantID: u.TenantID, UserID: a.ID, Kind: "password_reset_requested",
			Title: "Password reset requested", Body: u.Email + " cannot sign in and asked for a new password",
			Link: "/mail/admin/users", MeetingID: pgtype.UUID{},
		}); err != nil {
			log.Printf("password recovery for %s: cannot notify %s: %v", email, a.Email, err)
		}
		s.sendMsg(a.Email, notify.PasswordResetRequested(a.Language, u.Email, u.Name, s.appURL("/mail/admin/users")))
	}

	// And the self-service path, only for somebody who set an address outside their own domain.
	// Usually the owner of a small organisation, who has no administrator above them.
	if u.RecoveryEmail == "" {
		return
	}
	token, err := newLinkToken()
	if err != nil {
		return
	}
	if _, err := s.q.CreatePasswordReset(ctx, db.CreatePasswordResetParams{
		Token: token, UserID: u.ID, TenantID: u.TenantID,
		ExpiresAt: time.Now().Add(resetWindow),
	}); err != nil {
		log.Printf("password recovery for %s: cannot create the link: %v", email, err)
		return
	}
	s.sendMsg(u.RecoveryEmail, notify.PasswordResetLink(
		u.Language, u.Email, s.appURL("/reset?token="+token), int(resetWindow.Minutes())))
}

type resetReq struct {
	Token    string `json:"token"`
	Password string `json:"password"`
	// The second factor, when the account has one. A reset that skipped it would hand the account to
	// whoever controls the recovery inbox, which is exactly what the second factor exists to prevent.
	Code string `json:"code"`
}

// handleResetPassword sets the new password against a live link.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetReq
	if !decode(w, r, &req) {
		return
	}
	if !s.loginIP.allow("reset:"+clientIP(r), time.Now()) {
		writeErr(w, http.StatusTooManyRequests, "auth.rate_limited")
		return
	}
	row, err := s.q.GetPasswordReset(r.Context(), strings.TrimSpace(req.Token))
	if err != nil {
		// Spent, expired or never existed — the same answer for all three. Saying which would tell
		// somebody holding a stolen link that it was once real.
		writeErr(w, http.StatusForbidden, "auth.reset_invalid")
		return
	}
	u := row.MailUser
	if u.Status != "active" {
		writeErr(w, http.StatusForbidden, "auth.reset_invalid")
		return
	}
	if len(req.Password) < 10 {
		writeErr(w, http.StatusBadRequest, "auth.password_weak")
		return
	}

	// The second factor still applies. This is the whole reason the flow is safe to offer.
	if u.MfaTotpEnabled || u.MfaSmsEnabled {
		// An empty challenge on purpose: this is not a login, so there is no SMS code in flight.
		// checkSecondFactor then accepts an authenticator code or a recovery code — and everybody
		// who set up MFA has recovery codes, including somebody whose only factor is SMS.
		if !s.checkSecondFactor(r.Context(), u, db.MfaChallenge{}, strings.TrimSpace(req.Code)) {
			writeErr(w, http.StatusUnauthorized, "auth.mfa_required")
			return
		}
	}

	if u.StalwartAccountID != nil {
		if err := s.sw.SetAccountPassword(r.Context(), *u.StalwartAccountID, req.Password); err != nil {
			if strings.Contains(err.Error(), "weak") {
				writeErr(w, http.StatusBadRequest, "auth.password_weak")
				return
			}
			writeErr(w, http.StatusBadGateway, "auth.reset_failed")
			return
		}
	}
	_ = s.q.SpendPasswordReset(r.Context(), row.Token)

	// Every session ends. Somebody resetting a password has often just been compromised, and leaving
	// the attacker's session alive is the one outcome that makes the reset pointless.
	_ = s.q.DeleteWebmailSessionsForUser(r.Context(), u.ID)

	log.Printf("password reset completed for %s", u.Email)
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}
