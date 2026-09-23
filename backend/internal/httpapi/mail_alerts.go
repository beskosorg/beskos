package httpapi

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/notify"
)

// Email alerts at the moments that matter to the customer: account created, trial started,
// subscription active, billing, and new login. All best-effort and off the critical path
// (goroutine): a slow email server must not delay a login or fail a signup.

// sendMsg delivers an already-built alert. It's a silent no-op without SMTP configured.
func (s *Server) sendMsg(to string, m notify.Msg) {
	if s.email == nil || to == "" {
		return
	}
	go func() {
		if err := s.email.SendHTML(to, m.Subject, m.HTML, m.Text); err != nil {
			log.Printf("notify: %q to %s: %v", m.Subject, to, err)
		}
	}()
}

// appURL is the address used for the buttons in alert e-mails. A relative link would be useless
// there, so an unset PUBLIC_URL falls back to BRAND_URL rather than leaving the button broken.
func (s *Server) appURL(path string) string {
	base := strings.TrimSuffix(s.publicURL, "/")
	if base == "" {
		base = s.brandURL
	}
	return base + path
}

// clientLocation returns the requester's country, when the network provides it. Behind Cloudflare
// it comes in CF-IPCountry; without it we're left with no location — better to say "unknown" than
// to guess.
func clientLocation(r *http.Request) string {
	for _, h := range []string{"CF-IPCountry", "X-Vercel-IP-Country", "X-Country-Code"} {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" && v != "XX" {
			if city := strings.TrimSpace(r.Header.Get("CF-IPCity")); city != "" {
				return city + ", " + v
			}
			return v
		}
	}
	return ""
}

// clientDevice summarizes the User-Agent into what a person recognizes: browser + system.
func clientDevice(r *http.Request) string {
	ua := r.Header.Get("User-Agent")
	if ua == "" {
		return "—"
	}
	browser := "—"
	switch {
	case strings.Contains(ua, "Beskos"): // desktop app (Tauri)
		browser = "Beskos"
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	os := ""
	switch {
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		os = "iOS"
	case strings.Contains(ua, "Mac OS X"), strings.Contains(ua, "Macintosh"):
		os = "macOS"
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	}
	if os == "" {
		return browser
	}
	return browser + " · " + os
}

// alertNewLogin notifies about a new login with enough detail to recognize it — or to notice it
// wasn't them. It's the alert that gives someone a chance to react to a stolen password.
func (s *Server) alertNewLogin(r *http.Request, u db.MailUser) {
	s.sendMsg(u.Email, notify.NewLogin(u.Language, u.Email, clientIP(r), clientLocation(r),
		clientDevice(r), time.Now().UTC(), s.appURL("/mail/profile/security")))
}

// alertAccountCreated welcomes the user with the created address and what's still missing (DNS).
func (s *Server) alertAccountCreated(lang, email, domain string) {
	s.sendMsg(email, notify.AccountCreated(lang, email, domain, s.appURL("/mail")))
}

// alertTrialStarted confirms the trial and says when (and how much) billing starts — the
// information that avoids the unpleasant surprise at the end of the month.
func (s *Server) alertTrialStarted(lang, email, plan string, days int, amount string) {
	s.sendMsg(email, notify.TrialStarted(lang, plan, days, time.Now().AddDate(0, 0, days), amount, s.appURL("/mail")))
}

// alertSubscription notifies whoever manages billing (and the owner) that the subscription is active.
func (s *Server) alertSubscription(ctx context.Context, tid uuid.UUID, plan, cycle, amount string) {
	to, lang := s.billingContact(ctx, tid)
	s.sendMsg(to, notify.SubscriptionActive(lang, plan, cycle, amount, s.appURL("/mail/admin/billing")))
}

// alertCharge notifies about the charge (end of trial or renewal), with the invoice link.
func (s *Server) alertCharge(ctx context.Context, tid uuid.UUID, plan, amount, invoiceURL string) {
	to, lang := s.billingContact(ctx, tid)
	s.sendMsg(to, notify.ChargeStarted(lang, plan, amount, invoiceURL, s.appURL("/mail/admin/billing")))
}

// billingContact returns where the money alerts go: the billing e-mail set on the record and,
// failing that, the organization's owner (who is always someone).
func (s *Server) billingContact(ctx context.Context, tid [16]byte) (email, lang string) {
	if p, err := s.q.GetBillingProfile(ctx, tid); err == nil && p.InvoiceEmail != "" {
		email = p.InvoiceEmail
	}
	if owner, err := s.q.GetTenantOwner(ctx, tid); err == nil {
		lang = owner.Language
		if email == "" {
			email = owner.Email
		}
	}
	return email, lang
}

// shareWithOutsider handles sharing with someone OUTSIDE the organization: it ensures a public
// link for the file and sends it by e-mail. Without this, sharing with a customer or supplier
// would do nothing — the person has no account to receive the notification, nor a "Shared with me"
// place to look.
//
// Returns the public link and whether the e-mail was actually sent. The LINK is created even
// without SMTP: it's what the person copies and sends by hand — without it, sharing with someone
// outside would produce nothing.
func (s *Server) shareWithOutsider(r *http.Request, f db.StorageFile, actor db.MailUser, email string) (link string, emailed bool) {
	token := s.ensureFileLink(r, f, actor)
	if token == "" {
		return "", false
	}
	link = s.appURL("/p/" + token)
	if s.email == nil {
		log.Printf("share of %q with %s: link created, but e-mail sending is off", f.Name, email)
		return link, false
	}
	who := actor.Name
	if who == "" {
		who = actor.Email
	}
	s.sendMsg(email, notify.FileShared(actor.Language, who, f.Name, link, ""))
	return link, true
}

// ensureFileLink returns the file's public link token, creating one if it doesn't exist yet. The
// link inherits the default validity (30 days) — whoever wants something else changes it in the app.
func (s *Server) ensureFileLink(r *http.Request, f db.StorageFile, actor db.MailUser) string {
	if existing, err := s.q.GetFileLinkByFile(r.Context(), f.ID); err == nil {
		return existing.Token
	}
	token, err := newLinkToken()
	if err != nil {
		return ""
	}
	link, err := s.q.CreateFileLink(r.Context(), db.CreateFileLinkParams{
		TenantID: f.TenantID, FileID: f.ID, Token: token, CreatedBy: actor.ID,
		ExpiresAt: linkExpiry(nil), PasswordHash: "", Permission: "view", AllowDownload: true,
	})
	if err != nil {
		log.Printf("share: create public link for %s: %v", f.ID, err)
		return ""
	}
	return link.Token
}
