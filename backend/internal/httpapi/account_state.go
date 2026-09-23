package httpapi

// Asking, in one place, whether an organisation may still use the product.
//
// Every gate in the system calls this: the send path, the session, the panel banner. Keeping the
// decision in internal/billing and the lookup here means the rule is written once -- a second
// implementation of "is the trial over" would eventually disagree with the first, and the customer
// would meet whichever one was wrong.

import (
	"context"
	"time"

	"github.com/beskosorg/beskos/backend/ee"
	"github.com/beskosorg/beskos/backend/internal/provisioning"
)

// accountView is the shape the frontend renders the banner from.
func accountView(a ee.Account) map[string]any {
	out := map[string]any{"state": string(a.State), "blocked": a.Blocked()}
	if !a.Until.IsZero() {
		out["until"] = a.Until
		// Days rather than a timestamp for the copy: "3 days left" is what somebody acts on, and
		// computing it in two frontends would produce two different roundings.
		out["days_left"] = int(time.Until(a.Until).Hours() / 24)
	}
	return out
}

// domainPublished reports whether the user's own domain is live in DNS, and says which domain it is
// so the UI can name it.
//
// WHY THE PRODUCT CARES. Until the customer publishes the records, their MX still points at their
// old provider: no mail arrives here, and anything sent from here fails SPF and DKIM at the
// receiver -- which is the fastest way to teach Gmail that this domain is not to be trusted. An
// inbox that silently stays empty invites the conclusion that the product is broken; the truth is
// that one DNS record is missing, and saying so is worth more than any retry.
func (s *Server) domainPublished(ctx context.Context, email string) (string, bool) {
	name := domainOf(email)
	if name == "" {
		return "", true // nothing to check; do not invent a problem
	}
	d, err := s.q.GetDomainByName(ctx, name)
	if err != nil {
		// Unknown to us: an alias domain, or a lookup that failed. Not a reason to take the mail
		// away from somebody -- the same fail-open rule as the billing gate above.
		return name, true
	}
	return name, d.ProvisionState == provisioning.StateActive
}

// domainView is what the app renders the "publish your DNS" alert from.
func domainView(name string, published bool) map[string]any {
	return map[string]any{"domain": name, "published": published}
}
