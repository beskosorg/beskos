package httpapi

import (
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/provisioning"
)

// Connection data for external e-mail clients (Outlook, Thunderbird, mobile) and for anyone who
// wants to send from their own site via SMTP. This is information, not a secret: the ports and
// host are public; the password is the mailbox's own and never passes through here.
//
// The host comes from STALWART_HOSTNAME. Without it configured, what shows up in the DNS zone is
// the container's internal hostname — useless to anyone outside.

// SetMailHost sets the mail server's public host (IMAP/SMTP/JMAP).
func (s *Server) SetMailHost(h string) { s.mailHost = h }

// SetMTASTSHost sets the public name that serves MTA-STS policies, which every customer's
// mta-sts.<domain> is pointed at. Empty leaves those records as Stalwart wrote them.
func (s *Server) SetMTASTSHost(h string) { s.mtaStsHost = h }

// zoneOf returns the customer-facing DNS zone: what Stalwart produced, with its own container
// hostname replaced by names a customer can actually publish.
//
// WHY AT READ TIME AND NOT ONLY WHEN PROVISIONING: the zone is generated once, on the day the
// domain is created, and stored as text. Stalwart writes its container hostname into it, so a
// domain provisioned before that substitution existed -- or before these public names changed --
// would show a target that resolves nowhere, for as long as the row lives. Correcting on the way
// out costs one pass over a handful of lines and makes the stored value stop mattering.
func (s *Server) zoneOf(d db.Domain) string {
	if d.DnsZone == nil {
		return ""
	}
	return provisioning.PublicHosts(*d.DnsZone, s.mailHost, s.mtaStsHost)
}

func (s *Server) handleMailConnection(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	host := s.mailHost
	writeJSON(w, http.StatusOK, map[string]any{
		"host":     host,
		"username": mu.Email, // always the full address — that's what Stalwart expects
		"imap":     map[string]any{"host": host, "port": 993, "security": "ssl"},
		"smtp": []map[string]any{
			{"host": host, "port": 465, "security": "ssl"},      // implicit, the simplest one
			{"host": host, "port": 587, "security": "starttls"}, // alternative for clients that only speak 587
		},
		"jmap":   map[string]any{"url": "https://" + host + "/.well-known/jmap"},
		"caldav": map[string]any{"url": "https://" + host + "/dav/"},
	})
}
