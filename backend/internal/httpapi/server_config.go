package httpapi

// Server configuration and optional wiring: what main switches on before serving, plus the
// health probes. Kept apart from the route table so that table reads as a table.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/chat"
	"github.com/beskosorg/beskos/backend/internal/notify"
	"github.com/beskosorg/beskos/backend/internal/secretbox"
)

// SetReadiness wires the readiness probe (/readyz): the function should fail if a critical
// dependency (e.g. Postgres) is unavailable. Without it, /readyz always answers "ready".
func (s *Server) SetReadiness(ping func(context.Context) error) { s.readiness = ping }

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.readiness != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := s.readiness(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unready"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// SetSalesEmail sets where the website's contact form delivers. Empty disables the form.
func (s *Server) SetSalesEmail(addr string) { s.salesEmail = addr }

// EnableTOTPEncryption enables encryption of the TOTP secret at rest (MFA_ENC_KEY). Empty = off.
func (s *Server) EnableTOTPEncryption(key string) { s.totpBox = secretbox.New(key) }

// encTOTP/decTOTP encrypt/decrypt the TOTP secret; with a nil box, they are no-ops (plaintext).
func (s *Server) encTOTP(plain string) string {
	if v, err := s.totpBox.Encrypt(plain); err == nil {
		return v
	}
	return plain
}

func (s *Server) decTOTP(stored string) string {
	if v, err := s.totpBox.Decrypt(stored); err == nil {
		return v
	}
	return stored
}

// decryptCred decrypts the stored JMAP credential (H11 brokering). Fail-closed: an empty string or
// a decryption error → ("", false), which the proxy treats as "no mailbox" (401 → re-login).
func (s *Server) decryptCred(stored string) (string, bool) {
	if stored == "" {
		return "", false
	}
	v, err := s.totpBox.Decrypt(stored)
	if err != nil || v == "" {
		return "", false
	}
	return v, true
}

// SetFileUploadLimit sets the maximum size per file in Files (configurable via env).
func (s *Server) SetFileUploadLimit(n int64) { s.maxUpload = n }

// SetAppOrigins wires CORS for the given origins (e.g. the Tauri app) and sets whether the
// session cookie is Secure/SameSite=None (required for cross-site use over HTTPS).
func (s *Server) SetAppOrigins(origins []string, cookieSecure bool) {
	s.corsOrigins = origins
	s.cookieSecure = cookieSecure
}

// SetPublicURL sets the app's public URL, used to build absolute links that go out externally
// (e.g. a meeting room link in an e-mail). Without it, those links would stay relative and
// useless outside the app.
func (s *Server) SetPublicURL(u string) { s.publicURL = strings.TrimRight(u, "/") }

// SetBrandURL sets the fallback address for links that must stay absolute (e.g. inside an
// e-mail) when PublicURL is unset -- see appURL in mail_alerts.go.
func (s *Server) SetBrandURL(u string) { s.brandURL = strings.TrimRight(u, "/") }

// originHostPatterns extracts the hosts from the configured origins for the WebSocket's
// OriginPatterns. It authorizes the native app (cross-origin) without opening the WS to any
// site — the default (empty list) already accepts same-origin only, closing off CSWSH.
func originHostPatterns(origins []string) []string {
	out := make([]string, 0, len(origins))
	for _, o := range origins {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			out = append(out, u.Host)
		}
	}
	return out
}

func (s *Server) originAllowed(origin string) bool {
	for _, o := range s.corsOrigins {
		if o == origin {
			return true
		}
	}
	return false
}

// SetSignupsOpen opens/closes self-service signup (SIGNUPS_OPEN). When closed, the page shows
// "coming soon" and the API refuses — the rest of the product stays the same for existing customers.
func (s *Server) SetSignupsOpen(open bool) { s.signupsOpen = open }

// SetDevSignupEnabled mounts (or leaves unmounted) the legacy /auth/signup route the e2e scripts
// use for test convenience (DEV_SIGNUP_ENABLED). It bypasses signupsOpen, rate limiting, and the
// billing signup flow entirely, so it stays off unless a caller explicitly wants it -- never in
// production, which config.validateProduction refuses outright regardless of this call.
func (s *Server) SetDevSignupEnabled(enabled bool) { s.devSignupEnabled = enabled }

// EnableEmail wires e-mail sending for notifications (optional).
func (s *Server) EnableEmail(e *notify.Email) { s.email = e }

// EnableChat wires the chat's real-time transport (WebSocket + Redis pub/sub).
func (s *Server) EnableChat(h *chat.Hub) { s.hub = h }
