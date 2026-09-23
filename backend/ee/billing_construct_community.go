//go:build !pro

package ee

import (
	"fmt"

	"github.com/beskosorg/beskos/backend/internal/billing"
)

// NewBillingProvider is the Community build's provider construction. Manual works exactly as it
// does under Pro (no external gateway, the plan applies directly); Stripe fails loud instead of
// silently downgrading to Manual, matching DevSignupEnabled's and ONLYOFFICE_SOURCE_URL's
// philosophy elsewhere in this codebase — a Community deploy misconfigured with
// BILLING_PROVIDER=stripe should refuse to start, not quietly take no payments.
func NewBillingProvider(cfg billing.Config) (billing.Provider, error) {
	switch cfg.Provider {
	case "manual", "":
		return billing.ManualProvider{}, nil
	case "stripe":
		return nil, fmt.Errorf("billing: stripe billing requires a Pro build")
	default:
		return nil, fmt.Errorf("billing: unknown provider %q", cfg.Provider)
	}
}
