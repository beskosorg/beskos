package httpapi

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/mailaudit"
)

// Public mail configuration audit. Unauthenticated on purpose: it is the front door, and asking for
// an account before answering would defeat what it is for.
//
// IT ANSWERS FIRST AND ASKS NOTHING. Every tool of this kind that hides the result behind a form
// gets used once and remembered as a trick. The report is complete on screen; the e-mail is for the
// re-check, which is a reason to come back rather than a toll to get in.
//
// PUBLIC DNS ONLY. Nothing here touches the queried domain: no connection to its servers, no probe
// of its mail. Somebody can audit a domain that is not theirs, and that is fine -- every record read
// is already published to the world. But it also means the endpoint must not become a scanner, hence
// the rate limit.

// A hostname, not a URL and not an address. Anything else is rejected before a resolver sees it.
var auditDomainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

func (s *Server) handleMailAudit(w http.ResponseWriter, r *http.Request) {
	raw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	// People paste what they have: a full address, a URL, a trailing slash. Taking the domain out of
	// it is one line here and one less reason for somebody to give up on the tool.
	if i := strings.LastIndex(raw, "@"); i >= 0 {
		raw = raw[i+1:]
	}
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	raw = strings.TrimSuffix(strings.SplitN(raw, "/", 2)[0], ".")

	// An IPv4 address satisfies the hostname shape ("127.0.0.1" is labels and dots), so the regex
	// alone lets one through. A real TLD is never all digits, and the tool has no business resolving
	// addresses -- it audits domains.
	labels := strings.Split(raw, ".")
	numericTLD := len(labels) > 0 && strings.Trim(labels[len(labels)-1], "0123456789") == ""
	if !auditDomainRe.MatchString(raw) || len(raw) > 253 || numericTLD {
		writeErr(w, http.StatusBadRequest, "audit.invalid_domain")
		return
	}
	report := mailaudit.Audit(r.Context(), raw)
	writeJSON(w, http.StatusOK, report)
}
