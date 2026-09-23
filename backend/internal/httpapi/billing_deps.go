package httpapi

// The bridge between the Server and the Enterprise billing logic in ee/ (see the engineering standards on
// Community/Enterprise separation). Always compiled: a Community build still needs accountState
// and syncSeats to exist (ee.AccountState/ee.SyncSeats have a !pro no-op twin), even though the
// routes calling ee's checkout/signup handlers only mount under -tags pro (billing_routes_pro.go).

import (
	"context"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/ee"
)

// billingDeps assembles what ee's billing handlers need from this Server instance.
func (s *Server) billingDeps() ee.Deps {
	return ee.Deps{
		Queries:          s.q,
		Billing:          s.billing,
		BillingCfg:       s.billingCfg,
		Stalwart:         s.sw,
		PublicURL:        s.publicURL,
		SignupsOpen:      s.signupsOpen,
		ActivePlans:      s.activePlans,
		CatalogPlanByKey: s.catalogPlanByKey,
		PlanByKey:        s.planByKey,

		AlertAccountCreated: s.alertAccountCreated,
		AlertTrialStarted:   s.alertTrialStarted,
		AlertCharge:         s.alertCharge,
		AlertSubscription:   s.alertSubscription,

		IssueSession:   s.issueSession,
		EncTOTP:        s.encTOTP,
		AllowLoginRate: s.loginIP.allow,
	}
}

// accountState resolves the state of the organisation this user belongs to. See ee.AccountState's
// doc comment (billing_account_pro.go) for the fail-open rationale.
func (s *Server) accountState(ctx context.Context, tenant uuid.UUID) ee.Account {
	return ee.AccountState(ctx, tenant, s.billingDeps())
}

// syncSeats mirrors the user count into the Stripe subscription quantity. See ee.SyncSeats's doc
// comment (billing_pro.go) for what it actually does on a Pro build; a no-op on Community.
func (s *Server) syncSeats(tid uuid.UUID) {
	ee.SyncSeats(tid, s.billingDeps())
}
