package billing

import (
	"context"
)

// CheckoutResult: Manual applies the plan immediately (Applied=true, no redirect); Stripe returns a URL.
type CheckoutResult struct {
	URL     string `json:"url,omitempty"`
	Applied bool   `json:"applied"`
}

// Cycle is the subscription's billing period. Monthly is the default; annual bills
// AnnualMonthsCharged months at once (the rest is the discount for paying upfront).
type Cycle string

const (
	CycleMonth Cycle = "month"
	CycleYear  Cycle = "year"
	// AnnualMonthsCharged: 10 months for the year = 2 months free (~17% discount).
	AnnualMonthsCharged = 10
)

// Normalize returns a valid cycle (anything other than "year" is monthly).
func (c Cycle) Normalize() Cycle {
	if c == CycleYear {
		return CycleYear
	}
	return CycleMonth
}

// CheckoutOpts are the checkout extras: billing period and the customer already created at the
// provider (with billing address/tax ID/email), so the invoice comes out complete.
type CheckoutOpts struct {
	Cycle        Cycle
	CustomerID   string // cus_… (Stripe). Empty = the provider creates one at checkout.
	InvoiceEmail string // used only when there is no CustomerID
	// TrialDays > 0 asks for the card but only charges at the end of the trial (self-service signup).
	TrialDays int
}

// WebhookEvent is the normalized event (provider-independent) that the control plane applies.
type WebhookEvent struct {
	EventID        string // provider id — used for idempotency (brief §10)
	Type           string // "activated" | "canceled" | "status" | "charged"
	TenantID       string
	Plan           string // plan to apply ("" = do not touch the plan)
	Status         string
	SubscriptionID string
	// Charge (Type="charged"): what the invoice took, for the customer notice.
	Cycle       string
	AmountCents int
	Currency    string
	InvoiceURL  string
	PlanKey     string // plan on the invoice, only for the notice text
	// PeriodEnd is what the subscription is paid up to (unix, 0 = unknown). It decides when a
	// lapsed account stops being able to send, so it is stored rather than asked for later: putting
	// Stripe's availability in front of our customers' mail is not a trade we want to make.
	PeriodEnd int64
}

// SubscriptionInfo is the snapshot of the subscription at the provider — the panel manages it
// without leaving the app.
type SubscriptionInfo struct {
	Customer          string
	ItemID            string
	Status            string
	CancelAtPeriodEnd bool
	CurrentPeriodEnd  int64 // unix
}

// Provider abstracts payment. Swapping Stripe/Manual/other is config-only (like ai/sms).
type Provider interface {
	Name() string
	// Checkout starts or updates the tenant's subscription. seats is the number of billed users.
	// The caller resolves Plan against the database catalog before calling — the provider never
	// reads the in-code catalog, or its price could drift from what is edited in the operator panel.
	Checkout(ctx context.Context, tenantID string, plan Plan, seats int, opts CheckoutOpts) (CheckoutResult, error)
	// ParseWebhook validates the signature and normalizes the event. (nil, nil) = ignore/no effect.
	ParseWebhook(payload []byte, sigHeader string) (*WebhookEvent, error)
	// Subscription reads the subscription at the provider (status, period end, item to swap the price).
	Subscription(ctx context.Context, subID string) (SubscriptionInfo, error)
	// UpdatePlan swaps the price/quantity of the ACTIVE subscription (upgrade/downgrade with proration).
	UpdatePlan(ctx context.Context, subID string, plan Plan, seats int, cycle Cycle) error
	// SetCancelAtPeriodEnd cancels at the end of the period (true) or resumes it (false).
	SetCancelAtPeriodEnd(ctx context.Context, subID string, cancel bool) error
	// PortalURL opens the customer portal (change payment method, invoices). "" = unsupported.
	PortalURL(ctx context.Context, customerID, returnURL string) (string, error)
	// UpdateQuantity adjusts the active subscription's seat count, with proration — a new user
	// mid-period is billed the proportional amount.
	UpdateQuantity(ctx context.Context, subID string, seats int) error
	// SyncPlan mirrors the plan at the provider (Product + Price). Price changed -> new Price
	// (immutable) and the old one is deactivated. Returns the current ids ("" on the manual provider).
	SyncPlan(ctx context.Context, plan Plan, productID, priceID string, priceChanged bool) (newProductID, newPriceID string, err error)
	// EnsureCustomer creates/updates the customer at the provider with the billing data (legal
	// name, address, tax ID, financial contact's email). Returns the customer id ("" = unsupported),
	// which checkout reuses — so the invoice comes out complete and the card step doesn't ask for
	// everything again.
	EnsureCustomer(ctx context.Context, customerID string, c Customer) (string, error)
}

// Customer is the billing data sent to the provider. Mirrors what the invoice has to show.
type Customer struct {
	TenantID     string
	Kind         string // company | individual
	Name         string // legal name or personal name
	Email        string // financial contact (where the invoice goes)
	TaxID        string // already-normalized tax ID/VAT ("" = no number)
	Country      string // ISO alpha-2
	City         string
	PostalCode   string
	AddressLine1 string
	AddressLine2 string
	Phone        string
}

// ManualProvider: no external gateway. The plan is applied directly (dev, internal self-serve,
// and the path exercised by the tests). There are no webhooks.
type ManualProvider struct{}

func (ManualProvider) Name() string { return "manual" }

func (ManualProvider) Checkout(_ context.Context, _ string, _ Plan, _ int, _ CheckoutOpts) (CheckoutResult, error) {
	return CheckoutResult{Applied: true}, nil
}

func (ManualProvider) ParseWebhook([]byte, string) (*WebhookEvent, error) { return nil, nil }

func (ManualProvider) Subscription(context.Context, string) (SubscriptionInfo, error) {
	return SubscriptionInfo{}, nil
}
func (ManualProvider) UpdatePlan(context.Context, string, Plan, int, Cycle) error { return nil }
func (ManualProvider) SetCancelAtPeriodEnd(context.Context, string, bool) error   { return nil }
func (ManualProvider) PortalURL(context.Context, string, string) (string, error)  { return "", nil }
func (ManualProvider) UpdateQuantity(context.Context, string, int) error          { return nil }
func (ManualProvider) EnsureCustomer(context.Context, string, Customer) (string, error) {
	return "", nil
}
func (ManualProvider) SyncPlan(_ context.Context, _ Plan, productID, priceID string, _ bool) (string, string, error) {
	return productID, priceID, nil
}
