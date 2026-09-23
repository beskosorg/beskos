//go:build pro

package ee

import "github.com/beskosorg/beskos/backend/internal/billing"

// NewBillingProvider builds the Provider cfg.Provider selects: Manual for dev/self-serve, Stripe
// for a hosted production deploy (BILLING_PROVIDER=stripe + keys). The only place in a Pro build
// that knows concrete providers.
func NewBillingProvider(cfg billing.Config) (billing.Provider, error) {
	switch cfg.Provider {
	case "manual", "":
		return billing.ManualProvider{}, nil
	case "stripe":
		return NewStripe(cfg), nil
	default:
		return nil, unknownProvider(cfg.Provider)
	}
}
