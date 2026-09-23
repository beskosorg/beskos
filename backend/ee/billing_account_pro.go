//go:build pro

package ee

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Whether an organisation may still use the product, and what to say about it.
//
// THE ORDER OF PUNISHMENTS IS DELIBERATE. Sending stops first, and access to what is already yours
// stops last -- a week later. Cutting somebody out of their own mail because a card expired is the
// classic mistake: the mail keeps arriving, nobody reads it, and a customer who was going to pay
// leaves instead. Blocking outbound hurts on the same day and costs nobody their correspondence.
//
// It is also the only order that is defensible about data. Under Article 28(3)(g) the customer may
// ask for their data back at the end of the service; a product that locks them out the moment
// payment lapses has made that impossible to honour by accident. So during the grace period the
// mailbox stays readable and exportable, and only then does the door close.

// State is what the whole product asks about an organisation.
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

// GracePeriod is how long a blocked organisation can still read and export before the door closes.
const GracePeriod = 7 * 24 * time.Hour

// EndingSoon is how far ahead of the trial's end the warning starts. Long enough to talk to whoever
// signs the invoices, short enough that the banner is not wallpaper by the time it matters.
const EndingSoon = 3 * 24 * time.Hour

// Account is what the API hands the frontend and the gates ask.
type Account struct {
	State State     `json:"state"`
	Until time.Time `json:"until"` // when the CURRENT state ends and the next one begins
}

// Blocked reports whether the organisation may still send.
func (a Account) Blocked() bool { return a.State == StateSendBlocked || a.State == StateLocked }

// Input is the little the decision actually needs, so it can be decided without a database.
type Input struct {
	Now       time.Time
	CreatedAt time.Time // when the organisation signed up -- the trial runs from here
	TrialDays int
	// Status as the payment provider last reported it: "" (never subscribed), trialing, active,
	// past_due, unpaid, canceled.
	Status string
	// PeriodEnd is what a paid subscription has already been paid up to. Zero when there is none.
	PeriodEnd time.Time
}

// Evaluate decides the state of an organisation.
func Evaluate(in Input) Account {
	switch in.Status {
	case "active":
		return Account{State: StateOK}

	case "past_due", "unpaid":
		// Paid up to PeriodEnd, then a week to fix the card, then the door.
		blockAt := in.PeriodEnd
		if blockAt.IsZero() {
			blockAt = in.Now
		}
		return afterDeadline(in.Now, blockAt, StatePastDue)

	case "canceled":
		// They asked to leave. What they paid for is theirs until it runs out.
		return afterDeadline(in.Now, in.PeriodEnd, StateOK)
	}

	// No subscription at all: the trial, counted from the day they signed up.
	days := in.TrialDays
	if days <= 0 {
		days = 14
	}
	end := in.CreatedAt.Add(time.Duration(days) * 24 * time.Hour)
	if in.Now.Before(end.Add(-EndingSoon)) {
		return Account{State: StateOK, Until: end}
	}
	if in.Now.Before(end) {
		return Account{State: StateEnding, Until: end}
	}
	return afterDeadline(in.Now, end, StateOK)
}

// afterDeadline turns "the money runs out at this moment" into the three states that follow it.
// before is what to report while the deadline is still in the future.
func afterDeadline(now, deadline time.Time, before State) Account {
	if now.Before(deadline) {
		return Account{State: before, Until: deadline}
	}
	locked := deadline.Add(GracePeriod)
	if now.Before(locked) {
		return Account{State: StateSendBlocked, Until: locked}
	}
	return Account{State: StateLocked, Until: locked}
}

// AccountState resolves the state of the organisation this tenant belongs to.
//
// Fails OPEN. If the subscription or the organisation cannot be read, the answer is "ok": a
// database hiccup must never be the reason a paying customer cannot send mail. The failure mode of
// the opposite choice is silent and total, and it arrives at the worst possible moment.
func AccountState(ctx context.Context, tenant uuid.UUID, deps Deps) Account {
	// No payment provider means nothing to pay for: a self-hosted installation has no trial to
	// run out and no subscription to lapse, so the trial clock below must not apply to it.
	if deps.Billing == nil || deps.Billing.Name() != "stripe" {
		return Account{State: StateOK}
	}
	t, err := deps.Queries.GetTenant(ctx, tenant)
	if err != nil {
		return Account{State: StateOK}
	}
	in := Input{
		Now:       time.Now(),
		CreatedAt: t.CreatedAt,
		TrialDays: trialDays(),
	}
	if sub, err := deps.Queries.GetSubscription(ctx, tenant); err == nil {
		in.Status = sub.Status
		if sub.CurrentPeriodEnd.Valid {
			in.PeriodEnd = sub.CurrentPeriodEnd.Time
		}
	}
	return Evaluate(in)
}
