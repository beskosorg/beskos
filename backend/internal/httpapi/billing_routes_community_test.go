//go:build !pro

package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/stalwart"

	"github.com/beskosorg/beskos/backend/ee"
)

func TestSelfHostedRoutesExcludeHostedBilling(t *testing.T) {
	srv := NewServer(nil, stalwart.New("http://stalwart.invalid", "", ""), nil, nil, nil,
		billing.ManualProvider{}, billing.Settings{}, edition.CommunityCapabilities())
	usage := false
	err := chi.Walk(srv.Router().(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// Only the routes this build tag actually gates (mountBilling/mountPublicBilling in
		// billing_routes_pro.go vs. the no-ops in billing_routes_community.go). Waitlist, contact,
		// and the SES webhook are a SEPARATE hosted-only feature set, excluded from the public
		// export at file level (scripts/export_public.py), not by this build tag -- they stay
		// mounted in a plain `go build` of this repo either way, so asserting their absence here
		// would test a mechanism this file has nothing to do with.
		if strings.HasPrefix(route, "/api/billing") || route == "/api/public/signup" || route == "/api/public/plans" || route == "/api/public/quote" {
			t.Errorf("hosted route exported: %s %s", method, route)
		}
		if method == "GET" && route == "/api/usage" {
			usage = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !usage {
		t.Fatal("usage route was removed with billing")
	}
}

func TestSelfHostedAccessDoesNotRequireSubscription(t *testing.T) {
	// A nil database proves this decision does not read hosted trial or subscription state.
	srv := &Server{}
	account := srv.accountState(context.Background(), uuid.New())
	if account.State != ee.StateOK || account.Blocked() || !account.Until.IsZero() {
		t.Fatalf("self-hosted account is restricted: %+v", account)
	}
}
