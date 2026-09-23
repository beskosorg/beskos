package httpapi

import (
	"net/http"
	"strings"
)

// The gate Caddy asks before issuing a certificate for a hostname it has never seen.
//
// WHY THIS EXISTS: MTA-STS requires the policy at https://mta-sts.<customer-domain>/ with a
// certificate valid for THAT name. A certificate per customer domain has to be issued on demand,
// when the first sender connects. Caddy will do that, but only for hostnames something vouches for
// -- otherwise anybody could point a DNS record at us and have us mint certificates on their behalf
// until the rate limits lock us out.
//
// PUBLIC ON PURPOSE, and it gives nothing away: it answers yes or no about whether a domain is
// hosted here, which the domain's own MX record already announces to the whole internet. Putting a
// shared secret in front would be protecting a fact that is published in DNS.
//
// It is deliberately not a lookup people can enumerate: no list, no detail, no timing difference
// worth the name -- one question, one bit of answer.
func (s *Server) handleMTASTSAllowed(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	host = strings.TrimSuffix(host, ".")

	// Caddy asks about the hostname in the TLS handshake, which is always "mta-sts.<domain>". Any
	// other name is not part of this feature: refusing it keeps the certificate surface to exactly
	// the policy hosts, so a stray DNS record pointed at us cannot turn into a certificate for
	// something else.
	const prefix = "mta-sts."
	if !strings.HasPrefix(host, prefix) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	domain := strings.TrimPrefix(host, prefix)
	if domain == "" || !auditDomainRe.MatchString(domain) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// The brand's own policy host, which has no row in the domains table of any tenant.
	if s.mailHost != "" {
		if i := strings.Index(s.mailHost, "."); i > 0 && domain == s.mailHost[i+1:] {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	if _, err := s.q.GetDomainByName(r.Context(), domain); err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusOK)
}
