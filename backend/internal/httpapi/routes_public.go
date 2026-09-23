package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Unauthenticated routes: the public face of the product -- a shared file, a meeting link, the
// mail audit on the landing page. Every one of them is rate limited, because anything reachable
// without an account is reachable by everybody.
func (s *Server) mountPublic(r chi.Router) {
	// Public Meet (external guests, NO login) — joins via link. Rate-limited per IP.
	// Mail configuration audit: the landing's lead tool. Public DNS only, rate limited so it
	// cannot be turned into a scanner.
	// The landing calls this from beskos.com, a different origin from the API, so it needs its own
	// CORS -- and the preflight has to be REGISTERED: chi answers 405 for an unregistered method
	// before any middleware runs, which is exactly how this shipped broken.
	r.With(publicCORS, s.publicRateLimit).Get("/public/mail-audit", s.handleMailAudit)
	r.With(publicCORS).Options("/public/mail-audit", func(http.ResponseWriter, *http.Request) {})
	s.mountHostedPublic(r)
	// Asked by the policy server before it issues a certificate for a customer's mta-sts hostname.
	r.With(s.publicRateLimit).Get("/public/mta-sts-allowed", s.handleMTASTSAllowed)
	r.With(s.publicRateLimit).Get("/public/meet/{room}", s.handlePublicMeetInfo)
	r.With(s.publicRateLimit).Post("/public/meet/{room}/join", s.handlePublicMeetJoin)
	r.With(s.publicRateLimit).Get("/public/meet/lobby/{id}", s.handlePublicLobbyPoll)
	// Presentation PDF (members AND guests) — under /api so Vite/Caddy forward it.
	// Authorized by the HMAC token (the token is the authorization; no session).
	r.With(s.presentationRateLimit).Get("/public/meet/presentation/{id}/pdf", s.handlePresentationPDF)

	// Public download of a large attachment by token (NO login) — the token is the authorization.
	r.With(s.publicRateLimit).Post("/public/chat/hook/{token}", s.handleInboundWebhook) // inbound webhook (bot)
	// Forgot your password. Public out of necessity: somebody locked out has no session. The answer
	// is always the same, whether or not the account exists -- see password_recovery.go.
	r.With(s.publicRateLimit).Post("/public/password/forgot", s.handleForgotPassword)
	r.With(s.publicRateLimit).Post("/public/password/reset", s.handleResetPassword)
	r.With(s.publicRateLimit).Get("/public/booking/{org}/{user}", s.handlePublicBookingInfo)
	r.With(s.publicRateLimit).Get("/public/booking/{org}/{user}/slots", s.handlePublicBookingSlots)
	r.With(s.publicRateLimit).Post("/public/booking/{org}/{user}/book", s.handlePublicBookingCreate)
	r.With(s.publicRateLimit).Get("/public/wiki/{slug}", s.handlePublicWikiSpace)
	r.With(s.publicRateLimit).Get("/public/wiki/{slug}/{pageSlug}", s.handlePublicWikiPage)
	r.With(s.publicRateLimit).Get("/public/files/{token}/download", s.handlePublicFileDownload)
	r.With(s.publicRateLimit).Get("/public/files/{token}/session", s.handlePublicDocSession) // opens in OnlyOffice (anonymous)

	// Branding by email (login screen, NO session): loads the organization's logo/color.
	r.Get("/public/branding", s.handleResolveBranding)
}
