// Package ee holds the Beskos Enterprise features: the capabilities a Pro license key unlocks
// (see internal/edition and docs/editions.md). It only compiles into the binary when
// built with `-tags pro`; the default build links build_community.go instead, which registers
// nothing. Its license is ../../ee/LICENSE, separate from the repository's.
package ee

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

// Deps is what an Enterprise route needs from the rest of the server. It grows one field at a
// time, as a feature actually moves into this package -- never speculatively.
type Deps struct {
	Queries *db.Queries

	// The plan-catalog cache and the e-mail/session machinery below are single-instance Server
	// state (a cache, a rate limiter, an encryption box) that must not be duplicated here: a
	// second cache or limiter living in ee would drift from the one httpapi actually serves
	// requests through. They come in as closures bound to the real Server instead of being
	// reconstructed in this package.
	Billing          billing.Provider
	BillingCfg       billing.Settings
	Stalwart         *stalwart.Client
	PublicURL        string
	SignupsOpen      bool
	ActivePlans      func(ctx context.Context) []billing.Plan
	CatalogPlanByKey func(ctx context.Context, key string) (billing.Plan, bool)
	PlanByKey        func(ctx context.Context, key string) billing.Plan

	AlertAccountCreated func(lang, email, domain string)
	AlertTrialStarted   func(lang, email, plan string, days int, amount string)
	AlertCharge         func(ctx context.Context, tid uuid.UUID, plan, amount, invoiceURL string)
	AlertSubscription   func(ctx context.Context, tid uuid.UUID, plan, cycle, amount string)

	IssueSession   func(w http.ResponseWriter, ctx context.Context, u db.MailUser) (string, bool)
	EncTOTP        func(plain string) string
	AllowLoginRate func(key string, now time.Time) bool
}

// Register wires the Enterprise routes for whatever caps grants. It is defined twice, once per
// build tag (build_pro.go, build_community.go); this file only holds the shared types.
