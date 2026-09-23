package assistant

import "time"

// QuotaWindow is the sliding window for the commercial token quota. Six hours, self-renewing:
// spend older than six hours no longer counts.
//
// Not to be confused with the per-run step cap: that one is a safety limit, always applied, and
// built into the engine itself. This one is commercial, and is what will differentiate plans.
const QuotaWindow = 6 * time.Hour

// DefaultQuotaTokens is the token ceiling per user in the window, when nothing configures it. A
// generous default, the same for everyone (decision 6): without real usage, calibrating it by plan
// would mean making up a number (§12.2). It stays configurable per environment so it can be
// adjusted without recompiling.
const DefaultQuotaTokens = 200_000

// QuotaStatus is what the quota lets you know: how much was spent, how much remains, and when it
// renews.
type QuotaStatus struct {
	Limit     int `json:"limit"`
	Used      int `json:"used"`
	Remaining int `json:"remaining"`
	// RenewsAt is when the oldest usage in the window falls out of it — the moment there's room
	// again. Zero when there's no usage to expire (nothing spent in the window).
	RenewsAt time.Time `json:"renews_at,omitempty"`
}

// QuotaOf computes the quota state. used is the sum for the window; oldest is the instant of the
// oldest usage still inside it (to know when it renews); now is the present, injected so the
// computation is testable without depending on the clock.
func QuotaOf(limit, used int, oldest, now time.Time) QuotaStatus {
	st := QuotaStatus{Limit: limit, Used: used, Remaining: limit - used}
	if st.Remaining < 0 {
		st.Remaining = 0
	}
	if !oldest.IsZero() {
		// The window slides: the oldest usage expires one window after it happened.
		st.RenewsAt = oldest.Add(QuotaWindow)
	}
	return st
}

// Exceeded says whether the quota has been surpassed. It's >=, not >: reaching the ceiling is
// hitting the ceiling.
func (s QuotaStatus) Exceeded() bool { return s.Used >= s.Limit }
