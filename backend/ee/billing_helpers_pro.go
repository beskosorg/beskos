//go:build pro

package ee

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// stripeSubID returns the tenant's Stripe subscription id ("" if there isn't one).
func stripeSubID(r *http.Request, tid uuid.UUID, deps Deps) string {
	if deps.Billing.Name() != "stripe" {
		return ""
	}
	sub, err := deps.Queries.GetSubscription(r.Context(), tid)
	if err != nil || sub.StripeID == nil {
		return ""
	}
	return *sub.StripeID
}

// billableSeats = max(current users, paid minimum). Price per user × seats.
func billableSeats(r *http.Request, tid uuid.UUID, deps Deps) int {
	n, _ := deps.Queries.CountMailboxesByTenant(r.Context(), tid)
	seats := int(n)
	if seats < deps.BillingCfg.MinPaidUsers {
		seats = deps.BillingCfg.MinPaidUsers
	}
	if seats < 1 {
		seats = 1
	}
	return seats
}

// checkoutCustomer ensures the customer exists on the provider (legal name, address, tax ID, billing
// contact email) and returns the id + the invoice email. With no profile filled in it returns empty
// values — checkout proceeds and the provider asks for whatever it needs.
func checkoutCustomer(r *http.Request, tid uuid.UUID, deps Deps) (customerID, invoiceEmail string) {
	p, err := deps.Queries.GetBillingProfile(r.Context(), tid)
	if err != nil || p.LegalName == "" {
		return "", ""
	}
	id, err := deps.Billing.EnsureCustomer(r.Context(), p.StripeCustomer, billingCustomer(uuid.UUID(tid).String(), p))
	if err != nil {
		log.Printf("billing: ensure customer for tenant %s: %v", tid, err)
		return "", p.InvoiceEmail // at least the invoice goes to the billing contact
	}
	if id != "" && id != p.StripeCustomer {
		_ = deps.Queries.SetBillingStripeCustomer(r.Context(), db.SetBillingStripeCustomerParams{TenantID: tid, StripeCustomer: id})
	}
	return id, p.InvoiceEmail
}

// applyPlan validates against the database catalog; an unknown plan falls back to the default
// (fail-safe), then writes the plan and mirrors it onto the subscription record.
func applyPlan(r *http.Request, tid uuid.UUID, plan, stripeID, status string, deps Deps) {
	p, ok := deps.CatalogPlanByKey(r.Context(), plan)
	if !ok {
		p = deps.PlanByKey(r.Context(), billing.DefaultPlanID)
		plan = p.ID
	}
	_ = deps.Queries.UpdateTenantPlan(r.Context(), db.UpdateTenantPlanParams{ID: tid, Plan: plan})
	_ = deps.Queries.UpsertSubscription(r.Context(), db.UpsertSubscriptionParams{
		TenantID: tid, StripeID: &stripeID, MailboxRange: p.MailboxRange, Status: status,
	})
}

// money formats a cents value for the email alerts ("12.00 EUR").
func money(cents int, currency string) string {
	if currency == "" {
		currency = "EUR"
	}
	return fmt.Sprintf("%.2f %s", float64(cents)/100, strings.ToUpper(currency))
}

func cycleLabel(c billing.Cycle) string {
	if c == billing.CycleYear {
		return "anual"
	}
	return "mensal"
}

// perLabel is product copy composed into the alert e-mail text (per-user price suffix). lang:ok
func perLabel(c billing.Cycle) string {
	if c == billing.CycleYear {
		return "/ano por utilizador" // lang:ok
	}
	return "/mês por utilizador" // lang:ok
}

// planLabel returns the plan name from its key (for the alert text).
func planLabel(ctx context.Context, key string, deps Deps) string {
	if key == "" {
		return "Beskos"
	}
	if p, ok := deps.CatalogPlanByKey(ctx, key); ok {
		return p.Name
	}
	return key
}

// tenantCurrency returns the tenant's currency ("" → falls back to the global BILLING_CURRENCY).
func tenantCurrency(ctx context.Context, tid uuid.UUID, deps Deps) string {
	if t, err := deps.Queries.GetTenant(ctx, tid); err == nil && t.Currency != "" {
		return t.Currency
	}
	return deps.BillingCfg.Currency
}

// localPlan returns the plan with the PRICE in the tenant's currency (PriceFor; falls back to the
// base without inventing an exchange rate) and the effective currency annotated — the frontend
// still sees a single price.
func localPlan(p billing.Plan, currency string, deps Deps) billing.Plan {
	cents, cur := p.PriceFor(currency, deps.BillingCfg.Currency)
	p.PriceCentsPerUser = cents
	p.Currency = cur
	p.Prices = nil
	return p
}
