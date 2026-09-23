//go:build pro

package ee

// The decision that can lock a paying customer out of their own mail. Worth pinning down.

import (
	"testing"
	"time"
)

var epoch = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return epoch.Add(d) }

func TestTrialRunsFromSignupAndWarnsBeforeItEnds(t *testing.T) {
	in := func(now time.Time) Input {
		return Input{Now: now, CreatedAt: epoch, TrialDays: 14}
	}
	for _, c := range []struct {
		when time.Duration
		want State
	}{
		{0, StateOK},                                 // day 0
		{10 * 24 * time.Hour, StateOK},               // day 10: still quiet
		{11*24*time.Hour + time.Minute, StateEnding}, // day 11: three days left, start saying so
		{14*24*time.Hour + time.Minute, StateSendBlocked},
		{20 * 24 * time.Hour, StateSendBlocked},      // still inside the grace week
		{21*24*time.Hour + time.Minute, StateLocked}, // 14 + 7
	} {
		if got := Evaluate(in(at(c.when))).State; got != c.want {
			t.Errorf("at %v: got %q, want %q", c.when, got, c.want)
		}
	}
}

// A failed card must not cost anybody their mail on the same day: Stripe retries for days, and a
// bank that declined once is not a customer who left.
func TestAFailedPaymentDoesNotBlockImmediately(t *testing.T) {
	in := Input{Now: epoch, CreatedAt: epoch.Add(-90 * 24 * time.Hour), Status: "past_due",
		PeriodEnd: at(2 * 24 * time.Hour)}
	if got := Evaluate(in); got.State != StatePastDue {
		t.Fatalf("still paid up: got %q, want past_due", got.State)
	}
	in.Now = at(3 * 24 * time.Hour) // a day past the paid period
	if got := Evaluate(in); got.State != StateSendBlocked {
		t.Fatalf("past the paid period: got %q, want send_blocked", got.State)
	}
	in.Now = at(10 * 24 * time.Hour) // period end + 8 days
	if got := Evaluate(in); got.State != StateLocked {
		t.Fatalf("past the grace week: got %q, want locked", got.State)
	}
}

// Cancelling is not a punishment: what was paid for stays usable until it runs out.
func TestCancelledKeepsWhatWasPaidFor(t *testing.T) {
	in := Input{Now: epoch, CreatedAt: epoch.Add(-90 * 24 * time.Hour), Status: "canceled",
		PeriodEnd: at(20 * 24 * time.Hour)}
	if got := Evaluate(in); got.State != StateOK {
		t.Fatalf("inside the paid period: got %q, want ok", got.State)
	}
}

func TestActiveIsNeverBlocked(t *testing.T) {
	got := Evaluate(Input{Now: epoch, CreatedAt: epoch.Add(-900 * 24 * time.Hour), Status: "active"})
	if got.Blocked() || got.State != StateOK {
		t.Fatalf("an active subscription must be ok, got %q", got.State)
	}
}

// Reading and exporting survive the block; only the grace period ending takes those away. This is
// what makes "delete or return" still possible for somebody who stopped paying.
func TestOnlyLockedTakesAwayAccess(t *testing.T) {
	if !(Account{State: StateSendBlocked}).Blocked() || !(Account{State: StateLocked}).Blocked() {
		t.Error("both blocked states must block sending")
	}
	for _, s := range []State{StateOK, StateEnding, StatePastDue} {
		if (Account{State: s}).Blocked() {
			t.Errorf("%q must not block sending", s)
		}
	}
}
