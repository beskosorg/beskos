// Package billing defines the plans (mailbox tiers) and their quota limits, and abstracts the
// payment provider (Stripe in production; Manual in dev/tests) — swappable via config.
package billing

import "fmt"

// Plan is a subscription tier. Prices are per seat per month; the plans table
// is the source of truth and this catalog is the fallback when it is empty.
type Plan struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	MailboxRange        string `json:"mailbox_range"`
	MaxMailboxes        int    `json:"max_mailboxes"`
	MaxStorageBytes     int64  `json:"max_storage_bytes"`
	MaxDailySend        int    `json:"max_daily_send"`
	MaxMeetParticipants int    `json:"max_meet_participants"` // participant cap per video room
	PriceCentsPerUser   int    `json:"price_cents_per_user"`  // per user, per month (in the configured currency)

	// Operator-configurable knobs. Zero or empty means no restriction / product default.
	MinUsers          int      `json:"min_users"`
	AppsEnabled       []string `json:"apps_enabled"`         // empty = every app in the rail
	AITokensPerWindow int      `json:"ai_tokens_per_window"` // AI quota for the 6h window (0 = default)
	MeetMaxMinutes    int      `json:"meet_max_minutes"`
	Recording         string   `json:"recording"` // none | offline | offline_priority
	RecordingDays     int      `json:"recording_retention_days"`
	OnlyOfficeEdit    bool     `json:"onlyoffice_edit"`
	WikiPublicLinks   bool     `json:"wiki_public_links"`
	IsActive          bool     `json:"active"` // inactive plans do not accept new tenants

	// StripePriceID mirrors this plan in Stripe: when set, checkout and plan changes use this
	// price instead of inline price_data, so invoicing, reporting, and the billing portal stay
	// in Stripe.
	StripePriceID string `json:"stripe_price_id,omitempty"`
	// Prices holds the price per currency ({"USD":1900,...}, in cents). PriceCentsPerUser is the
	// price in the BASE currency.
	Prices map[string]int `json:"prices,omitempty"`
	// Currency is the EFFECTIVE currency of the displayed price (via PriceFor). It travels in the
	// JSON so the UI never formats a price using another plan's currency.
	Currency string `json:"currency,omitempty"`
}

// PriceFor returns the price in the requested currency and the effective currency: it falls back
// to the base currency when the plan has no specific price for it (never makes up an exchange rate).
func (p Plan) PriceFor(currency, baseCurrency string) (int, string) {
	if v, ok := p.Prices[currency]; ok && currency != "" {
		return v, currency
	}
	return p.PriceCentsPerUser, baseCurrency
}

// MailboxesExceeded reports whether creating one more mailbox would exceed the plan. A
// MaxMailboxes of 0 or less means no cap (e.g. the self-hosted plan).
func (p Plan) MailboxesExceeded(current int) bool {
	return p.MaxMailboxes > 0 && current >= p.MaxMailboxes
}

// StorageExceeded reports whether adding addBytes to usedBytes would exceed the plan. A
// MaxStorageBytes of 0 or less means no cap (e.g. the self-hosted plan).
func (p Plan) StorageExceeded(usedBytes, addBytes int64) bool {
	return p.MaxStorageBytes > 0 && usedBytes+addBytes > p.MaxStorageBytes
}

// MeetParticipantsExceeded reports whether one more participant would exceed the plan. A
// MaxMeetParticipants of 0 or less means no cap (e.g. the self-hosted plan).
func (p Plan) MeetParticipantsExceeded(current int) bool {
	return p.MaxMeetParticipants > 0 && current >= p.MaxMeetParticipants
}

// RangeLabel builds the range label from min/max ("up to 3", "1–10", "51+").
func RangeLabel(min, max int) string {
	switch {
	case max <= 0 && min <= 0:
		return ""
	case max <= 0:
		return fmt.Sprintf("%d+", min)
	case min <= 0:
		return fmt.Sprintf("up to %d", max)
	default:
		return fmt.Sprintf("%d–%d", min, max)
	}
}

const GB = 1 << 30

// Global billing settings (per deploy, via .env).
type Settings struct {
	Currency     string `json:"currency"`       // BRL | EUR | USD (Intl on the frontend)
	MinPaidUsers int    `json:"min_paid_users"` // minimum billed users on paid plans
}

// Plans returns the catalog (for the billing screen).
func Plans() []Plan { return append([]Plan(nil), plans...) }

// PlanByID looks up a plan; ok=false if unknown.
func PlanByID(id string) (Plan, bool) {
	for _, p := range plans {
		if p.ID == id {
			return p, true
		}
	}
	return Plan{}, false
}

// PlanOrDefault returns the plan for the id, or the trial (fail-safe for tenants with an
// unexpected plan).
func PlanOrDefault(id string) Plan {
	if p, ok := PlanByID(id); ok {
		return p
	}
	d, _ := PlanByID(DefaultPlanID)
	return d
}

// IsValidPlan says whether the id is a known plan (to validate checkout).
func IsValidPlan(id string) bool {
	_, ok := PlanByID(id)
	return ok
}

// UnitCents returns the price per user for the requested cycle. The annual cycle bills
// AnnualMonthsCharged months — the other two are the discount for paying the year upfront.
func (p Plan) UnitCents(cycle Cycle) int {
	if cycle.Normalize() == CycleYear {
		return p.PriceCentsPerUser * AnnualMonthsCharged
	}
	return p.PriceCentsPerUser
}
