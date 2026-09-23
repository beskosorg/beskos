//go:build pro

package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/ee"
	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

var hostedBillingRoutes = []string{
	"POST /api/public/signup", "GET /api/public/plans", "POST /api/public/quote",
	"GET /api/billing", "GET /api/billing/profile", "PUT /api/billing/profile",
	"POST /api/billing/quote", "POST /api/billing/checkout", "POST /api/billing/webhook",
	"POST /api/billing/cancel", "POST /api/billing/resume", "POST /api/billing/portal",
}

func routeSet(t *testing.T, provider billing.Provider) map[string]bool {
	t.Helper()
	srv := NewServer(nil, stalwart.New("http://stalwart.invalid", "", ""), nil, nil, nil,
		provider, billing.Settings{}, edition.CommunityCapabilities())
	got := map[string]bool{}
	err := chi.Walk(srv.Router().(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+strings.TrimSuffix(route, "/")] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestHostedBillingRoutesRegisteredWithPaymentProvider(t *testing.T) {
	got := routeSet(t, ee.NewStripe(billing.Config{Provider: "stripe", SecretKey: "sk_test_only"}))
	for _, route := range hostedBillingRoutes {
		if !got[route] {
			t.Errorf("missing hosted billing route: %s", route)
		}
	}
}

// A Pro build is what the release binaries are. Without a payment provider it is a self-hosted
// installation, which must expose neither checkout nor anonymous organization signup.
func TestSelfHostedProBuildMountsNoBillingRoutes(t *testing.T) {
	got := routeSet(t, billing.ManualProvider{})
	for _, route := range hostedBillingRoutes {
		if got[route] {
			t.Errorf("billing route mounted without a payment provider: %s", route)
		}
	}
	if !got["GET /api/usage"] {
		t.Fatal("usage route was removed with billing")
	}
}

func TestSelfHostedProAccessDoesNotRequireSubscription(t *testing.T) {
	// A nil database proves the trial clock is never consulted without a payment provider.
	srv := &Server{billing: billing.ManualProvider{}}
	account := srv.accountState(context.Background(), uuid.New())
	if account.State != ee.StateOK || account.Blocked() || !account.Until.IsZero() {
		t.Fatalf("self-hosted account is restricted: %+v", account)
	}
}
