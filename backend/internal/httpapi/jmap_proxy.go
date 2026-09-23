package httpapi

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// The JMAP proxy forwards /jmap/*, /auth/*, /.well-known/* to Stalwart. It BROKERS the
// credential: the browser authenticates with the SESSION (cookie/bearer/?access_token) and the
// proxy INJECTS the mailbox's Basic credential (stored encrypted in mail_users.jmap_cred) into
// the request on its way to Stalwart. This way the mailbox password never lives in the browser.
// It must ALWAYS sit in the JMAP path (web and native app) — the deploy routes /jmap to OUR API,
// never directly to Stalwart.
//
// SSE streaming (/jmap/eventsource) needs immediate flushing, so FlushInterval is -1.
func (s *Server) newJMAPProxy() http.Handler {
	target, err := url.Parse(s.sw.BaseURL())
	if err != nil {
		log.Fatalf("jmap proxy: invalid Stalwart URL %q: %v", s.sw.BaseURL(), err)
	}
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1 // real-time flush (required for the SSE eventsource)
	base := rp.Director
	rp.Director = func(r *http.Request) {
		base(r)
		r.Host = target.Host // Stalwart routes by Host; use the upstream one, not the public one
	}
	return rp
}

// mountJMAPProxy wires the JMAP proxy (with credential brokering) into the webmail routes. Always
// mounted — brokering requires the proxy to sit in the path for both the web and the native app.
func (s *Server) mountJMAPProxy(r interface {
	Handle(pattern string, h http.Handler)
}) {
	// broker (auth) → plan's sending quota (EmailSubmission/set) → proxy to Stalwart.
	p := s.jmapBrokerAuth(s.jmapMailQuota(s.newJMAPProxy()))
	r.Handle("/jmap/*", p)
	r.Handle("/auth/*", p)
	r.Handle("/.well-known/*", p)
}

// jmapBrokerAuth authenticates the user's session and injects the mailbox's Basic credential into
// the request before forwarding it to Stalwart. The browser sends only the session
// (cookie/bearer/?access_token); never the mailbox credential. No valid session or no stored
// credential -> 401 (forces login/re-login).
func (s *Server) jmapBrokerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := sessionToken(r)
		if tok == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		row, err := s.q.GetWebmailSession(r.Context(), tok)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		cred, ok := s.decryptCred(row.MailUser.JmapCred)
		if !ok {
			// Panel-only account (no mailbox) or a session predating brokering: no credential to inject.
			http.Error(w, "no mailbox credential", http.StatusUnauthorized)
			return
		}
		// Replaces any client Authorization with the mailbox's credential (server-to-server).
		r.Header.Set("Authorization", "Basic "+cred)
		// Injects the user into the context: the sending-quota middleware (mail_quota.go) needs
		// the tenant to apply the plan's daily limit.
		next.ServeHTTP(w, r.WithContext(withMailUser(r.Context(), row.MailUser)))
	})
}
