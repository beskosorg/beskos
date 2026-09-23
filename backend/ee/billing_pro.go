//go:build pro

package ee

import (
	"context"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/httpx"
	"github.com/beskosorg/beskos/backend/internal/reqauth"
)

// GetBilling returns the tenant's billing state for the panel: current plan (resolved by the
// caller, which owns the tenant-plan lookup shared with quota enforcement -- see plan), catalog
// for upgrade/downgrade, and, when on Stripe, the live subscription snapshot to manage it.
func GetBilling(w http.ResponseWriter, r *http.Request, plan billing.Plan, deps Deps) {
	u := reqauth.MailUserFrom(r.Context())
	var subStatus string
	sub, subErr := deps.Queries.GetSubscription(r.Context(), u.TenantID)
	if subErr == nil {
		subStatus = sub.Status
	}
	seats, _ := deps.Queries.CountMailboxesByTenant(r.Context(), u.TenantID)
	cur := tenantCurrency(r.Context(), u.TenantID, deps)
	localPlans := make([]billing.Plan, 0)
	for _, p := range deps.ActivePlans(r.Context()) {
		localPlans = append(localPlans, localPlan(p, cur, deps))
	}
	lp := localPlan(plan, cur, deps)
	out := map[string]any{
		"provider":       deps.Billing.Name(),
		"current_plan":   lp,
		"status":         subStatus,
		"plans":          localPlans,
		"currency":       lp.Currency,
		"min_paid_users": deps.BillingCfg.MinPaidUsers,
		"seats":          seats, // current users (basis for the per-user calculation)
	}
	// Subscription on the provider (Stripe): real state for the panel to manage (cancel/resume/portal).
	if subErr == nil && sub.StripeID != nil && *sub.StripeID != "" && deps.Billing.Name() == "stripe" {
		if info, err := deps.Billing.Subscription(r.Context(), *sub.StripeID); err == nil {
			out["subscription"] = map[string]any{
				"manageable":           true,
				"status":               info.Status,
				"cancel_at_period_end": info.CancelAtPeriodEnd,
				"current_period_end":   info.CurrentPeriodEnd,
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type checkoutReq struct {
	Plan  string `json:"plan"`
	Cycle string `json:"cycle"` // month (default) | year
}

// Checkout starts the plan change. On the Manual provider it applies immediately; on Stripe
// it returns the checkout URL (the actual activation comes through the webhook).
func Checkout(w http.ResponseWriter, r *http.Request, deps Deps) {
	u := reqauth.MailUserFrom(r.Context())
	var req checkoutReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	// Validates against the DATABASE CATALOG: the plan must exist and be active; the organization
	// must fit within the plan's ceiling; the minimum billed seats is the plan's floor (tier).
	plan, ok := deps.CatalogPlanByKey(r.Context(), req.Plan)
	if !ok || !plan.IsActive {
		httpx.WriteErr(w, http.StatusBadRequest, "billing.invalid_plan")
		return
	}
	seats := billableSeats(r, u.TenantID, deps)
	if plan.MaxMailboxes > 0 && seats > plan.MaxMailboxes {
		httpx.WriteErr(w, http.StatusBadRequest, "billing.plan_too_small")
		return
	}
	if seats < plan.MinUsers {
		seats = plan.MinUsers
	}
	cycle := billing.Cycle(req.Cycle).Normalize()
	// Currency by country: the price/checkout uses the tenant's currency (fallback: base).
	plan = localPlan(plan, tenantCurrency(r.Context(), u.TenantID, deps), deps)
	// Stripe subscription already active → UPGRADE/DOWNGRADE on the existing subscription (with
	// proration), without a new checkout (which would create a second subscription).
	if subID := stripeSubID(r, u.TenantID, deps); subID != "" {
		if err := deps.Billing.UpdatePlan(r.Context(), subID, plan, seats, cycle); err != nil {
			httpx.WriteErr(w, http.StatusBadGateway, "billing.checkout_failed")
			return
		}
		applyPlan(r, u.TenantID, req.Plan, subID, "active", deps)
		_ = deps.Queries.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "billing.plan_change", Target: req.Plan})
		httpx.WriteJSON(w, http.StatusOK, billing.CheckoutResult{Applied: true})
		return
	}
	// Customer on the provider with the billing data: checkout only needs the card, and the
	// invoice comes out with legal name, address, and tax ID.
	cus, invEmail := checkoutCustomer(r, u.TenantID, deps)
	res, err := deps.Billing.Checkout(r.Context(), uuid.UUID(u.TenantID).String(), plan, seats,
		billing.CheckoutOpts{Cycle: cycle, CustomerID: cus, InvoiceEmail: invEmail})
	if err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, "billing.checkout_failed")
		return
	}
	if res.Applied { // Manual: applies immediately
		applyPlan(r, u.TenantID, req.Plan, "", "active", deps)
		_ = deps.Queries.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "billing.plan_change", Target: req.Plan})
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

// BillingCancel schedules the cancellation for the END of the paid period (standard SaaS
// behavior). On the manual provider, it reverts to the default plan immediately.
func BillingCancel(w http.ResponseWriter, r *http.Request, deps Deps) {
	u := reqauth.MailUserFrom(r.Context())
	if subID := stripeSubID(r, u.TenantID, deps); subID != "" {
		if err := deps.Billing.SetCancelAtPeriodEnd(r.Context(), subID, true); err != nil {
			httpx.WriteErr(w, http.StatusBadGateway, "billing.cancel_failed")
			return
		}
	} else {
		// Manual: no external provider — applies the default plan immediately.
		applyPlan(r, u.TenantID, billing.DefaultPlanID, "", "canceled", deps)
	}
	_ = deps.Queries.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "billing.cancel", Target: ""})
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// BillingResume undoes a scheduled cancellation (before the period ends).
func BillingResume(w http.ResponseWriter, r *http.Request, deps Deps) {
	u := reqauth.MailUserFrom(r.Context())
	subID := stripeSubID(r, u.TenantID, deps)
	if subID == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "billing.no_subscription")
		return
	}
	if err := deps.Billing.SetCancelAtPeriodEnd(r.Context(), subID, false); err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, "billing.resume_failed")
		return
	}
	_ = deps.Queries.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "billing.resume", Target: ""})
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// BillingPortal opens Stripe's Billing Portal (change payment method, invoices).
func BillingPortal(w http.ResponseWriter, r *http.Request, deps Deps) {
	u := reqauth.MailUserFrom(r.Context())
	subID := stripeSubID(r, u.TenantID, deps)
	if subID == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "billing.no_subscription")
		return
	}
	info, err := deps.Billing.Subscription(r.Context(), subID)
	if err != nil || info.Customer == "" {
		httpx.WriteErr(w, http.StatusBadGateway, "billing.portal_failed")
		return
	}
	ret := deps.PublicURL + "/mail/admin/billing"
	link, err := deps.Billing.PortalURL(r.Context(), info.Customer, ret)
	if err != nil || link == "" {
		httpx.WriteErr(w, http.StatusBadGateway, "billing.portal_failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"url": link})
}

// BillingWebhook applies events from the provider. Idempotent by event_id (brief §10).
func BillingWebhook(w http.ResponseWriter, r *http.Request, deps Deps) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "billing.bad_body")
		return
	}
	ev, err := deps.Billing.ParseWebhook(body, r.Header.Get("Stripe-Signature"))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "billing.bad_signature")
		return
	}
	if ev == nil { // event with no effect
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	// Idempotency: if the event was already processed, don't reapply it.
	if ev.EventID != "" {
		if _, err := deps.Queries.MarkWebhookProcessed(r.Context(), ev.EventID); err != nil {
			httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
			return
		}
	}
	if tid, err := uuid.Parse(ev.TenantID); err == nil {
		// What they have paid up to, whenever the provider mentions it. Written before the switch
		// because every branch below wants it to already be true, and none of them owns it.
		if ev.PeriodEnd > 0 {
			_ = deps.Queries.SetSubscriptionPeriodEnd(r.Context(), db.SetSubscriptionPeriodEndParams{
				TenantID:         tid,
				CurrentPeriodEnd: pgtype.Timestamptz{Time: time.Unix(ev.PeriodEnd, 0).UTC(), Valid: true},
			})
		}
		switch {
		case ev.Type == "charged":
			// Money charged: notify whoever pays, with the amount and the invoice link.
			deps.AlertCharge(r.Context(), tid, planLabel(r.Context(), ev.PlanKey, deps), money(ev.AmountCents, ev.Currency), ev.InvoiceURL)
		case ev.Plan == "":
			// STATUS-only event (e.g. customer.subscription.updated → past_due): doesn't touch the plan.
			_ = deps.Queries.UpdateSubscriptionStatus(r.Context(), db.UpdateSubscriptionStatusParams{TenantID: tid, Status: ev.Status})
		default:
			applyPlan(r, tid, ev.Plan, ev.SubscriptionID, ev.Status, deps)
			if ev.Type == "activated" {
				plan := deps.PlanByKey(r.Context(), ev.Plan)
				lp := localPlan(plan, tenantCurrency(r.Context(), tid, deps), deps)
				cycle := billing.Cycle(ev.Cycle).Normalize()
				deps.AlertSubscription(r.Context(), tid, plan.Name, cycleLabel(cycle),
					money(lp.UnitCents(cycle), lp.Currency)+perLabel(cycle))
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}

// SyncSeats mirrors the user count into the Stripe subscription QUANTITY, with proration: a user
// added mid-period is prorated onto the next invoice; a removed one generates credit. Called
// after creating/removing a user or mailbox. Best-effort, in a goroutine — never blocks or fails
// the originating operation.
func SyncSeats(tid uuid.UUID, deps Deps) {
	if deps.Billing.Name() != "stripe" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		sub, err := deps.Queries.GetSubscription(ctx, tid)
		if err != nil || sub.StripeID == nil || *sub.StripeID == "" {
			return
		}
		// Same fallback as tenantPlanByID (httpapi/usage_handlers.go): an unreadable tenant falls
		// back to the default plan rather than leaving the seat count unbounded.
		plan := deps.PlanByKey(ctx, billing.DefaultPlanID)
		if t, err := deps.Queries.GetTenant(ctx, tid); err == nil {
			plan = deps.PlanByKey(ctx, t.Plan)
		}
		n, _ := deps.Queries.CountMailboxesByTenant(ctx, tid)
		seats := int(n)
		if seats < deps.BillingCfg.MinPaidUsers {
			seats = deps.BillingCfg.MinPaidUsers
		}
		if seats < plan.MinUsers {
			seats = plan.MinUsers
		}
		if seats < 1 {
			seats = 1
		}
		if err := deps.Billing.UpdateQuantity(ctx, *sub.StripeID, seats); err != nil {
			log.Printf("billing: seat sync for tenant %s: %v", tid, err)
		}
	}()
}
