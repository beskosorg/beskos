//go:build !pro

package ee

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// State and Account are the Community build's own account-state shape: this build links none of
// the real subscription evaluation (see billing_account_pro.go's Evaluate for what a Pro build
// actually computes from a subscription's status and period end). A Community install has no
// hosted subscription to evaluate in the first place, so AccountState only ever returns StateOK
// below -- but the full five-state enum still has to exist as a symbol here, because
// webmail_auth.go (an ordinary, always-compiled httpapi file) compares against ee.StateLocked
// unconditionally, regardless of which build tag is active.
type State string

const (
	// StateOK: nothing to say.
	StateOK State = "ok"
	// StateEnding: still fine, but the trial runs out soon enough to be worth a banner.
	StateEnding State = "ending"
	// StatePastDue: a payment failed. Nothing is blocked yet -- Stripe retries a failed card for
	// days, and blocking on the first failure punishes people whose bank simply declined once.
	StatePastDue State = "past_due"
	// StateSendBlocked: outbound mail and anything that creates work for us is refused. Reading,
	// searching and exporting still work.
	StateSendBlocked State = "send_blocked"
	// StateLocked: the grace period is over. Sessions end and sign-in is refused.
	StateLocked State = "locked"
)

// Account is what AccountState hands back.
type Account struct {
	State State     `json:"state"`
	Until time.Time `json:"until"`
}

// Blocked reports whether the organisation may still send. Always false: self-hosted access
// never depends on a hosted subscription or trial deadline.
func (a Account) Blocked() bool { return false }

// AccountState is the Community build's answer, matching today's
// scripts/public/billing_routes.go stub: an account state that's always OK.
func AccountState(context.Context, uuid.UUID, Deps) Account { return Account{State: StateOK} }

// SyncSeats is a no-op: there is no hosted subscription to keep a seat count in sync with.
func SyncSeats(uuid.UUID, Deps) {}
