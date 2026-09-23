package assistant

import (
	"testing"
	"time"
)

func TestQuotaRemainingAndRenewal(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	oldest := now.Add(-2 * time.Hour) // the oldest usage was 2h ago

	q := QuotaOf(200_000, 150_000, oldest, now)
	if q.Remaining != 50_000 {
		t.Errorf("remaining = %d, want 50000", q.Remaining)
	}
	// The window slides: the oldest usage falls out of it 6h after it happened, i.e. in 4h.
	want := oldest.Add(QuotaWindow)
	if !q.RenewsAt.Equal(want) {
		t.Errorf("renews at %v, want %v", q.RenewsAt, want)
	}
}

// Exceeded is >=, not >: reaching the ceiling is hitting the ceiling.
func TestQuotaExceededIsInclusive(t *testing.T) {
	now := time.Now()
	if !QuotaOf(100, 100, now, now).Exceeded() {
		t.Error("spending exactly the ceiling should count as exceeded")
	}
	if QuotaOf(100, 99, now, now).Exceeded() {
		t.Error("below the ceiling is not exceeded")
	}
	// Never goes negative — a negative remaining would confuse the screen.
	if r := QuotaOf(100, 150, now, now).Remaining; r != 0 {
		t.Errorf("remaining = %d, want 0 (not negative)", r)
	}
}

// With no usage in the window, there's nothing to renew — and the field stays empty, not with a
// made-up date.
func TestQuotaWithNoUsageHasNoRenewal(t *testing.T) {
	now := time.Now()
	q := QuotaOf(200_000, 0, time.Time{}, now)
	if !q.RenewsAt.IsZero() {
		t.Errorf("with no usage there should be no renewal, got %v", q.RenewsAt)
	}
	if q.Remaining != 200_000 {
		t.Errorf("remaining = %d, want the full ceiling", q.Remaining)
	}
}

// The per-run step ceiling (§9.1) and the window quota (§9.2) are distinct things, and neither
// replaces the other. This test fixes the boundary: the quota does NOT change the step ceiling.
func TestQuotaIsSeparateFromStepCeiling(t *testing.T) {
	// The step ceiling is an engine constant, independent of any quota.
	if DefaultMaxSteps != 12 {
		t.Errorf("the step ceiling changed (%d) — it's a safety limit and shouldn't depend on the quota", DefaultMaxSteps)
	}
	// The window is commercial and measured in hours; the step ceiling is measured in steps. There's
	// no conversion between them, and that's what makes them two limits instead of one.
	if QuotaWindow != 6*time.Hour {
		t.Errorf("the commercial window changed: %v", QuotaWindow)
	}
}
