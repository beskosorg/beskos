package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/beskosorg/beskos/backend/ee"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/reqauth"
)

// Single product session (wm_session cookie). Login lives in auth_handlers.go: with a mailbox,
// the credential is validated against Stalwart; without one, against our own signup records.
// Email itself talks JMAP directly to Stalwart.

// withMailUser and mailUserFrom wrap internal/reqauth: every call site in this package keeps its
// existing shape, but the context key is now shared with ee (see reqauth's doc comment for why).
func withMailUser(ctx context.Context, mu db.MailUser) context.Context {
	return reqauth.WithMailUser(ctx, mu)
}

func mailUserFrom(ctx context.Context) db.MailUser { return reqauth.MailUserFrom(ctx) }

// sessionToken extracts the webmail session token: Bearer header (native app) > wm_session cookie
// (web same-origin) > ?access_token= query param (WebSocket / media, which don't send Authorization).
func sessionToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("wm_session"); err == nil {
		return c.Value
	}
	return r.URL.Query().Get("access_token")
}

// requireWebmailSession validates the webmail session cookie and injects the mail_user into the context.
func (s *Server) requireWebmailSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := sessionToken(r)
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "auth.no_session")
			return
		}
		row, err := s.q.GetWebmailSession(r.Context(), token)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "auth.invalid_session")
			return
		}
		// Suspending somebody has to take effect NOW, not when their cookie happens to expire.
		// Blocking the login was never enough: an admin suspends a person who is already signed in,
		// the panel says "suspended", and that person carries on reading their mail for a week --
		// which is the whole reason suspension exists, defeated silently.
		if row.MailUser.Status != "active" {
			s.endSession(w, token)
			writeErr(w, http.StatusForbidden, "auth.account_suspended")
			return
		}
		// A locked organisation loses access, not just the ability to send -- but only after the
		// grace week, during which everything above still works and the data can be taken out.
		if s.accountState(r.Context(), row.MailUser.TenantID).State == ee.StateLocked {
			s.endSession(w, token)
			writeErr(w, http.StatusPaymentRequired, "account.locked")
			return
		}
		ctx := withMailUser(r.Context(), row.MailUser)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleWebmailLogout(w http.ResponseWriter, r *http.Request) {
	// sessionToken, not the raw cookie: the native app authenticates over Authorization: Bearer,
	// never the cookie, so reading only the cookie left its session alive in the DB after "log
	// out" -- a stolen Bearer token from before this fix would have outlived the user's own logout.
	s.endSession(w, sessionToken(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// endSession destroys the session and clears the cookie. Used when access is withdrawn mid-session,
// so the next request is not merely refused but arrives with nothing to refuse.
func (s *Server) endSession(w http.ResponseWriter, token string) {
	if token != "" {
		_ = s.q.DeleteWebmailSession(context.Background(), token)
	}
	http.SetCookie(w, &http.Cookie{Name: "wm_session", Value: "", Path: "/", MaxAge: -1})
}
