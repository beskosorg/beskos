//go:build pro

package ee

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/httpx"
	"github.com/beskosorg/beskos/backend/internal/provisioning"
	"github.com/beskosorg/beskos/backend/internal/rbac"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

// Self-service signup (/signup): in a single pass, creates the organization, the domain, the
// owner's MAILBOX, and the billing data, and returns the checkout URL with a trial period.
//
// The mailbox is created with the domain still in `domain_created` (DNS pending publication),
// not in `active` as the panel requires: whoever just signed up has to be able to log in and use
// chat, drive, calendar, and meetings the next minute. External e-mail only starts arriving once
// DNS is published — and the app says so at the top until then.
//
// If something fails midway, whatever was already created gets undone: half an organization (no
// mailbox, no domain) would lock the e-mail and the domain forever, with no one inside to free them.

var (
	domainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	loginRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)
)

// trialDays is the checkout's free trial. The card is requested upfront (a product decision), but
// is only charged at the end of the period.
func trialDays() int {
	if v := atoiOr(os.Getenv("BILLING_TRIAL_DAYS"), 14); v > 0 {
		return v
	}
	return 14
}

func atoiOr(s string, def int) int {
	n := 0
	if s == "" {
		return def
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// clientIP returns the client's IP, the same way httpapi's own clientIP (internal/httpapi/
// ratelimit.go) does: middleware.ClientIPFromXFFTrustedProxies(1), installed in httpapi's
// Router(), already resolved it from X-Forwarded-For when behind our proxy (Caddy). Duplicated
// here (rather than added to Deps) because it's a pure function of the request, not server state.
func clientIP(r *http.Request) string {
	if ip := middleware.GetClientIP(r.Context()); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

type publicSignupReq struct {
	// Account
	OrgName  string `json:"org_name"`
	Domain   string `json:"domain"`   // acme.com
	Login    string `json:"login"`    // part before the @
	Name     string `json:"name"`     // name of whoever is signing up
	Password string `json:"password"` // mailbox password (Stalwart + panel)
	// Plan
	Plan  string `json:"plan"`
	Cycle string `json:"cycle"`
	// Billing (same fields as the profile; checkout uses them for the invoice)
	Billing billingProfileReq `json:"billing"`
}

// Signup creates the tenant, domain, and owner mailbox, then starts the trial checkout.
func Signup(w http.ResponseWriter, r *http.Request, deps Deps) {
	// Signup creates accounts and talks to Stalwart: without a limit, a script could create
	// organizations at network speed.
	if !deps.AllowLoginRate("signup:"+clientIP(r), time.Now()) {
		httpx.WriteErr(w, http.StatusTooManyRequests, "auth.rate_limited")
		return
	}
	if !deps.SignupsOpen {
		httpx.WriteErr(w, http.StatusForbidden, "signup.closed")
		return
	}
	var req publicSignupReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	req.OrgName = strings.TrimSpace(req.OrgName)
	req.Domain = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(req.Domain, "https://"), "http://")))
	req.Domain = strings.TrimSuffix(strings.TrimPrefix(req.Domain, "www."), "/")
	req.Login = strings.ToLower(strings.TrimSpace(req.Login))
	req.Name = strings.TrimSpace(req.Name)

	switch {
	case req.OrgName == "":
		httpx.WriteErr(w, http.StatusBadRequest, "signup.org_required")
		return
	case !domainRe.MatchString(req.Domain):
		httpx.WriteErr(w, http.StatusBadRequest, "signup.domain_invalid")
		return
	case !loginRe.MatchString(req.Login):
		httpx.WriteErr(w, http.StatusBadRequest, "signup.login_invalid")
		return
	case req.Password != "" && len(req.Password) < 10:
		httpx.WriteErr(w, http.StatusBadRequest, "signup.password_weak")
		return
	}
	// No password on signup (the normal path): we generate a random one and the user sets their
	// own on first login. It's one fewer field before the card, and the password never travels
	// unnecessarily through the form.
	if req.Password == "" {
		pw, err := auth.NewSessionToken() // 32 random bytes in base64url
		if err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "auth.password_hash_failed")
			return
		}
		req.Password = pw
	}
	if code := validateProfile(&req.Billing); code != "" {
		httpx.WriteErr(w, http.StatusBadRequest, code)
		return
	}
	plan, ok := deps.CatalogPlanByKey(r.Context(), req.Plan)
	if !ok || !plan.IsActive {
		httpx.WriteErr(w, http.StatusBadRequest, "billing.invalid_plan")
		return
	}
	email := req.Login + "@" + req.Domain
	// Domain already registered (by another organization or this one) → never stolen, never duplicated.
	if _, err := deps.Queries.GetDomainByName(r.Context(), req.Domain); err == nil {
		httpx.WriteErr(w, http.StatusConflict, "signup.domain_taken")
		return
	}
	if _, err := deps.Queries.GetMailUserByEmail(r.Context(), email); err == nil {
		httpx.WriteErr(w, http.StatusConflict, "auth.email_taken")
		return
	}

	tenant, err := deps.Queries.CreateTenant(r.Context(), db.CreateTenantParams{Name: req.OrgName, Plan: billing.DefaultPlanID})
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "tenant.create_failed")
		return
	}
	// From here on, any failure undoes everything (the tenant cascades away with domain and mailbox).
	rollback := func(reason string, err error) {
		log.Printf("signup: %s: %v (rolling back tenant %s)", reason, err, tenant.ID)
		_ = deps.Queries.DeleteTenant(context.WithoutCancel(r.Context()), tenant.ID)
	}

	dom, err := deps.Queries.CreateDomain(r.Context(), db.CreateDomainParams{TenantID: tenant.ID, Domain: req.Domain})
	if err != nil {
		rollback("create domain", err)
		httpx.WriteErr(w, http.StatusConflict, "signup.domain_taken")
		return
	}
	// Create the domain in Stalwart right away (it's the 1st step of the provisioning machine):
	// without it there's nowhere to hang the mailbox. The worker picks up the rest — DKIM, DNS
	// zone, and activation.
	// autoDKIM follows the same rule as the worker: with an SES relay, SES does the signing (BYODKIM).
	autoDKIM := os.Getenv("SES_REGION") == "" || os.Getenv("AWS_ACCESS_KEY_ID") == ""
	swDomainID, err := deps.Stalwart.CreateDomain(r.Context(), req.Domain, "tenant_id="+tenant.ID.String(), autoDKIM)
	if err != nil {
		rollback("create domain in Stalwart", err)
		httpx.WriteErr(w, http.StatusBadGateway, "signup.provision_failed")
		return
	}
	if _, err := deps.Queries.UpdateDomainProvision(r.Context(), db.UpdateDomainProvisionParams{
		ID: dom.ID, ProvisionState: provisioning.StateDomainCreated, StalwartID: &swDomainID,
		DkimStatus: dom.DkimStatus, DnsStatus: dom.DnsStatus, DnsZone: dom.DnsZone,
	}); err != nil {
		rollback("save domain state", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "signup.provision_failed")
		return
	}

	// Owner's mailbox in Stalwart + user (unified identity: mailbox, password, and role in one).
	acctID, err := deps.Stalwart.CreateAccount(r.Context(), stalwart.AccountInput{
		Login: req.Login, DomainID: swDomainID, DisplayName: req.Name, Password: req.Password, QuotaBytes: 5 << 30,
	})
	if err != nil {
		rollback("create mailbox in Stalwart", err)
		if errors.Is(err, stalwart.ErrWeakPassword) {
			httpx.WriteErr(w, http.StatusBadRequest, "mailbox.password_weak")
			return
		}
		httpx.WriteErr(w, http.StatusBadGateway, "signup.provision_failed")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		rollback("hash password", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "auth.password_hash_failed")
		return
	}
	owner, err := deps.Queries.CreateMailUser(r.Context(), db.CreateMailUserParams{
		TenantID: tenant.ID, Email: email, Name: req.Name, Role: string(rbac.RoleOwner),
		StalwartAccountID: &acctID, QuotaBytes: 5 << 30, PasswordHash: hash,
	})
	if err != nil {
		rollback("create user", err)
		httpx.WriteErr(w, http.StatusConflict, "auth.email_taken")
		return
	}

	// Billing data: saved before checkout, so the invoice comes out complete from the first one.
	b := req.Billing
	if _, err := deps.Queries.UpsertBillingProfile(r.Context(), db.UpsertBillingProfileParams{
		TenantID: tenant.ID, Kind: b.Kind, LegalName: b.LegalName, TaxID: b.TaxID, Country: b.Country,
		City: b.City, PostalCode: b.PostalCode, AddressLine1: b.AddressLine1, AddressLine2: b.AddressLine2,
		InvoiceEmail: b.InvoiceEmail, InvoiceName: b.InvoiceName, Phone: b.Phone,
	}); err != nil {
		log.Printf("signup: save billing for tenant %s: %v", tenant.ID, err) // doesn't block signup
	}
	_ = deps.Queries.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: tenant.ID, Valid: true}, Actor: email, Action: "signup", Target: req.Domain,
	})

	// Session: whoever signs up enters the app right after payment, without re-entering the password.
	token, ok := deps.IssueSession(w, r.Context(), owner)
	if !ok {
		return
	}
	// JMAP credential (mailbox's Basic auth) encrypted server-side, same as login: it's what the
	// proxy injects to talk to Stalwart. Without this the webmail would answer 401 right after signup.
	basic := base64.StdEncoding.EncodeToString([]byte(email + ":" + req.Password))
	_ = deps.Queries.SetJmapCred(r.Context(), db.SetJmapCredParams{ID: owner.ID, JmapCred: deps.EncTOTP(basic)})

	// Alerts: the account exists (with the address and what's still missing in DNS) and the trial
	// is running. They go to the address just created; the billing address gets the money alerts.
	deps.AlertAccountCreated(owner.Language, email, req.Domain)
	deps.AlertTrialStarted(owner.Language, email, plan.Name, trialDays(), planAmount(localPlan(plan, tenantCurrency(r.Context(), tenant.ID, deps), deps), req.Cycle))

	out := map[string]any{"tenant_id": tenant.ID, "email": email, "token": token, "trial_days": trialDays()}
	if url := signupCheckoutURL(r, tenant.ID, plan, req.Cycle, deps); url != "" {
		out["checkout_url"] = url
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

// signupCheckoutURL creates the trial checkout (card requested upfront, charged only at the end).
// Failing here doesn't undo the signup: the account exists and the subscription can be done
// later in Billing.
func signupCheckoutURL(r *http.Request, tid uuid.UUID, plan billing.Plan, cycle string, deps Deps) string {
	plan = localPlan(plan, tenantCurrency(r.Context(), tid, deps), deps)
	seats := plan.MinUsers
	if seats < 1 {
		seats = 1
	}
	cus, invEmail := checkoutCustomer(r, tid, deps)
	res, err := deps.Billing.Checkout(r.Context(), tid.String(), plan, seats, billing.CheckoutOpts{
		Cycle: billing.Cycle(cycle).Normalize(), CustomerID: cus, InvoiceEmail: invEmail, TrialDays: trialDays(),
	})
	if err != nil {
		log.Printf("signup: checkout for tenant %s: %v", tid, err)
		return ""
	}
	return res.URL
}

// --- Public catalog and simulation endpoints (the signup page has no session yet) ---

// PublicPlans returns the active plans with the price in the base currency. It doesn't
// expose anything that isn't already on the site's pricing page.
func PublicPlans(w http.ResponseWriter, r *http.Request, deps Deps) {
	cur := deps.BillingCfg.Currency
	plans := make([]billing.Plan, 0)
	for _, p := range deps.ActivePlans(r.Context()) {
		if p.PriceCentsPerUser <= 0 {
			continue // free/enterprise plans aren't subscribed to through here
		}
		plans = append(plans, localPlan(p, cur, deps))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"plans": plans, "currency": cur, "trial_days": trialDays(), "signups_open": deps.SignupsOpen,
		"months_charged": billing.AnnualMonthsCharged, "seller_country": SellerCountry,
	})
}

type publicQuoteReq struct {
	Plan    string `json:"plan"`
	Cycle   string `json:"cycle"`
	Kind    string `json:"kind"`
	Country string `json:"country"`
	TaxID   string `json:"tax_id"`
	Seats   int    `json:"seats"`
}

// PublicQuote is the same computation as /api/billing/quote for someone who doesn't have an
// account yet: the signup page has to show the total with VAT before the organization exists.
func PublicQuote(w http.ResponseWriter, r *http.Request, deps Deps) {
	var req publicQuoteReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	plan, ok := deps.CatalogPlanByKey(r.Context(), req.Plan)
	if !ok || !plan.IsActive {
		httpx.WriteErr(w, http.StatusBadRequest, "billing.invalid_plan")
		return
	}
	plan = localPlan(plan, deps.BillingCfg.Currency, deps)
	cycle := billing.Cycle(req.Cycle).Normalize()
	seats := req.Seats
	if seats < plan.MinUsers {
		seats = plan.MinUsers
	}
	if seats < 1 {
		seats = 1
	}
	q := QuoteFor(plan.Currency, plan.UnitCents(cycle), seats, req.Kind, req.Country, req.TaxID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"quote": q, "cycle": string(cycle), "plan": plan.ID, "plan_name": plan.Name,
		"monthly_cents": plan.PriceCentsPerUser, "months_charged": billing.AnnualMonthsCharged,
		"seller_country": SellerCountry, "vat_rate_percent": float64(q.VATRateBps) / 100,
		"trial_days": trialDays(),
	})
}

// planAmount formats a price with its billing-cycle suffix (e.g. "12.00 EUR" plus per-month or
// per-year) for the alerts — the per-user amount, as in checkout.
func planAmount(plan billing.Plan, cycle string) string {
	c := billing.Cycle(cycle).Normalize()
	unit := float64(plan.UnitCents(c)) / 100
	per := "/mês" // lang:ok
	if c == billing.CycleYear {
		per = "/ano" // lang:ok
	}
	return fmt.Sprintf("%.2f %s%s", unit, plan.Currency, per)
}
