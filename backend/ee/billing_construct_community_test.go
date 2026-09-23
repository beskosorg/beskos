//go:build !pro

package ee

import (
	"testing"

	"github.com/beskosorg/beskos/backend/internal/billing"
)

func TestNewBillingProviderManualOnCommunity(t *testing.T) {
	p, err := NewBillingProvider(billing.Config{Provider: "manual"})
	if err != nil || p.Name() != "manual" {
		t.Fatalf("provider=%v error=%v", p, err)
	}
	p, err = NewBillingProvider(billing.Config{})
	if err != nil || p.Name() != "manual" {
		t.Fatalf("empty provider=%v error=%v", p, err)
	}
}

func TestNewBillingProviderRefusesStripeOnCommunity(t *testing.T) {
	p, err := NewBillingProvider(billing.Config{Provider: "stripe", SecretKey: "test-only"})
	if err == nil {
		t.Fatalf("expected an error, got provider=%v", p)
	}
}
