//go:build pro

package ee

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/billing"
)

// StripeProvider talks to the Stripe API over HTTP (no SDK). Checkout Session (subscription) +
// webhook signature verification (HMAC-SHA256, Stripe's official scheme).
type StripeProvider struct {
	cfg  billing.Config
	http *http.Client
}

func NewStripe(cfg billing.Config) *StripeProvider {
	return &StripeProvider{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}}
}

func (p *StripeProvider) Name() string { return "stripe" }

func unknownProvider(name string) error { return fmt.Errorf("billing: unknown provider %q", name) }

// Checkout creates a Checkout Session in subscription mode using inline price_data (does not
// require pre-registering prices at Stripe). client_reference_id = tenant; metadata[plan] = plan.
// planCurrency: the operation's effective currency (the tenant's, resolved by the caller) or the global one.
func (p *StripeProvider) planCurrency(plan billing.Plan) string {
	if plan.Currency != "" {
		return strings.ToLower(plan.Currency)
	}
	return strings.ToLower(p.cfg.Settings().Currency)
}

func (p *StripeProvider) Checkout(ctx context.Context, tenantID string, plan billing.Plan, seats int, opts billing.CheckoutOpts) (billing.CheckoutResult, error) {
	if seats < 1 {
		seats = 1
	}
	cycle := opts.Cycle.Normalize()
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("success_url", p.cfg.SuccessURL)
	form.Set("cancel_url", p.cfg.CancelURL)
	form.Set("client_reference_id", tenantID)
	form.Set("metadata[plan]", plan.ID)
	form.Set("metadata[tenant_id]", tenantID)
	form.Set("metadata[cycle]", string(cycle))
	form.Set("subscription_data[metadata][plan]", plan.ID)
	form.Set("subscription_data[metadata][tenant_id]", tenantID)
	form.Set("line_items[0][quantity]", strconv.Itoa(seats)) // price per user × seats
	if opts.TrialDays > 0 {
		// Card requested upfront, charge only at the end of the trial. `cancel` at period end
		// with no payment method: without this, an abandoned trial would keep trying to charge forever.
		form.Set("subscription_data[trial_period_days]", strconv.Itoa(opts.TrialDays))
		form.Set("subscription_data[trial_settings][end_behavior][missing_payment_method]", "cancel")
	}
	// Customer already created by us (legal name, address, tax ID, financial contact's email):
	// checkout only asks for the card and the invoice comes out complete. Without it, at least
	// the email comes pre-filled.
	if opts.CustomerID != "" {
		form.Set("customer", opts.CustomerID)
		// The address can change at checkout (the card decides); saving it on the customer keeps
		// the invoice correct.
		form.Set("customer_update[address]", "auto")
		form.Set("customer_update[name]", "auto")
	} else if opts.InvoiceEmail != "" {
		form.Set("customer_email", opts.InvoiceEmail)
	}
	// The mirrored Price is MONTHLY; the annual one is built here (price_data with interval=year),
	// otherwise picking "annual" would charge every month all the same.
	if plan.StripePriceID != "" && cycle == billing.CycleMonth {
		// Mirrored catalog: use the real Price so invoices and reports group by product.
		// currency selects the price's currency_option (per-country pricing).
		form.Set("line_items[0][price]", plan.StripePriceID)
		form.Set("currency", p.planCurrency(plan))
	} else {
		form.Set("line_items[0][price_data][currency]", p.planCurrency(plan))
		form.Set("line_items[0][price_data][recurring][interval]", string(cycle))
		form.Set("line_items[0][price_data][unit_amount]", strconv.Itoa(plan.UnitCents(cycle)))
		form.Set("line_items[0][price_data][product_data][name]", "Beskos "+plan.Name)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.stripe.com/v1/checkout/sessions", strings.NewReader(form.Encode()))
	req.Header.Set("Authorization", "Bearer "+p.cfg.SecretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.http.Do(req)
	if err != nil {
		return billing.CheckoutResult{}, fmt.Errorf("stripe checkout: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return billing.CheckoutResult{}, fmt.Errorf("stripe checkout HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return billing.CheckoutResult{}, err
	}
	return billing.CheckoutResult{URL: out.URL}, nil
}

// ParseWebhook verifies the signature (Stripe-Signature: t=...,v1=...) and normalizes the event.
func (p *StripeProvider) ParseWebhook(payload []byte, sigHeader string) (*billing.WebhookEvent, error) {
	// Fails closed when the secret is not configured: with an empty secret the HMAC is
	// reproducible by anyone, and a forged webhook would grant a paid plan to any tenant (audit M).
	if p.cfg.WebhookSecret == "" {
		return nil, fmt.Errorf("stripe webhook secret not configured")
	}
	if err := verifyStripeSignature(payload, sigHeader, p.cfg.WebhookSecret, 5*time.Minute); err != nil {
		return nil, err
	}
	var ev struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID                string            `json:"id"`
				ClientReferenceID string            `json:"client_reference_id"`
				Subscription      string            `json:"subscription"`
				Status            string            `json:"status"`
				CurrentPeriodEnd  int64             `json:"current_period_end"`
				Metadata          map[string]string `json:"metadata"`
				AmountPaid        int64             `json:"amount_paid"` // invoice.paid
				Currency          string            `json:"currency"`    // invoice.paid
				HostedInvoiceURL  string            `json:"hosted_invoice_url"`
				Lines             struct {
					Data []struct {
						Metadata map[string]string `json:"metadata"`
					} `json:"data"`
				} `json:"lines"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return nil, err
	}
	o := ev.Data.Object
	tenant := o.ClientReferenceID
	if tenant == "" {
		tenant = o.Metadata["tenant_id"]
	}
	switch ev.Type {
	case "checkout.session.completed":
		return &billing.WebhookEvent{EventID: ev.ID, Type: "activated", TenantID: tenant, Plan: o.Metadata["plan"],
			Status: "active", SubscriptionID: o.Subscription, Cycle: o.Metadata["cycle"]}, nil
	case "invoice.paid":
		// Money actually charged (end of trial or renewal). The tenant comes from the invoice
		// line, which inherits the subscription's metadata — the invoice itself has no client_reference_id.
		if tenant == "" && len(o.Lines.Data) > 0 {
			tenant = o.Lines.Data[0].Metadata["tenant_id"]
		}
		if o.AmountPaid <= 0 {
			return nil, nil // $0 invoice (trial start): not a charge, no notice
		}
		plan := o.Metadata["plan"]
		if plan == "" && len(o.Lines.Data) > 0 {
			plan = o.Lines.Data[0].Metadata["plan"]
		}
		return &billing.WebhookEvent{EventID: ev.ID, Type: "charged", TenantID: tenant, Plan: "", Status: "",
			SubscriptionID: o.Subscription, AmountCents: int(o.AmountPaid), Currency: strings.ToUpper(o.Currency),
			InvoiceURL: o.HostedInvoiceURL, PlanKey: plan}, nil
	case "customer.subscription.deleted":
		return &billing.WebhookEvent{EventID: ev.ID, Type: "canceled", TenantID: tenant, Plan: billing.DefaultPlanID, Status: "canceled",
			SubscriptionID: o.ID, PeriodEnd: o.CurrentPeriodEnd}, nil
	case "customer.subscription.updated":
		// Only syncs the STATUS (past_due, active…): empty Plan = the handler does not touch the plan.
		return &billing.WebhookEvent{EventID: ev.ID, Type: "status", TenantID: tenant, Plan: "", Status: o.Status,
			SubscriptionID: o.ID, PeriodEnd: o.CurrentPeriodEnd}, nil
	default:
		return nil, nil // event with no effect
	}
}

// verifyStripeSignature validates the Stripe-Signature header. Exported indirectly for testing.
func verifyStripeSignature(payload []byte, header, secret string, tolerance time.Duration) error {
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts = kv[1]
		case "v1":
			sigs = append(sigs, kv[1])
		}
	}
	if ts == "" || len(sigs) == 0 {
		return fmt.Errorf("signature missing")
	}
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp")
	}
	if tolerance > 0 && absDuration(time.Since(time.Unix(tsInt, 0))) > tolerance {
		return fmt.Errorf("signature outside the tolerance window")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(payload)))
	expected := hex.EncodeToString(mac.Sum(nil))
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(expected)) {
			return nil
		}
	}
	return fmt.Errorf("invalid signature")
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// doForm performs a form-encoded call to the Stripe API and returns the body (error on non-2xx).
func (p *StripeProvider) doForm(ctx context.Context, method, url string, form url.Values) ([]byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.SecretKey)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stripe: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("stripe HTTP %d: %s", resp.StatusCode, raw)
	}
	return raw, nil
}

// Subscription reads the subscription (status, customer, item, and period end).
func (p *StripeProvider) Subscription(ctx context.Context, subID string) (billing.SubscriptionInfo, error) {
	raw, err := p.doForm(ctx, http.MethodGet, "https://api.stripe.com/v1/subscriptions/"+url.PathEscape(subID), nil)
	if err != nil {
		return billing.SubscriptionInfo{}, err
	}
	var out struct {
		Customer          string `json:"customer"`
		Status            string `json:"status"`
		CancelAtPeriodEnd bool   `json:"cancel_at_period_end"`
		CurrentPeriodEnd  int64  `json:"current_period_end"`
		Items             struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return billing.SubscriptionInfo{}, err
	}
	info := billing.SubscriptionInfo{Customer: out.Customer, Status: out.Status, CancelAtPeriodEnd: out.CancelAtPeriodEnd, CurrentPeriodEnd: out.CurrentPeriodEnd}
	if len(out.Items.Data) > 0 {
		info.ItemID = out.Items.Data[0].ID
	}
	return info, nil
}

// UpdatePlan swaps the price (inline price_data, as at checkout) and the quantity of the active
// subscription, with proration — upgrade/downgrade without a new checkout.
func (p *StripeProvider) UpdatePlan(ctx context.Context, subID string, plan billing.Plan, seats int, cycle billing.Cycle) error {
	info, err := p.Subscription(ctx, subID)
	if err != nil {
		return err
	}
	if info.ItemID == "" {
		return fmt.Errorf("stripe: subscription %s has no items", subID)
	}
	if seats < 1 {
		seats = 1
	}
	form := url.Values{}
	form.Set("items[0][id]", info.ItemID)
	form.Set("items[0][quantity]", strconv.Itoa(seats))
	cycle = cycle.Normalize()
	if plan.StripePriceID != "" && cycle == billing.CycleMonth {
		form.Set("items[0][price]", plan.StripePriceID)
	} else {
		form.Set("items[0][price_data][currency]", p.planCurrency(plan))
		form.Set("items[0][price_data][recurring][interval]", string(cycle))
		form.Set("items[0][price_data][unit_amount]", strconv.Itoa(plan.UnitCents(cycle)))
		form.Set("items[0][price_data][product_data][name]", "Beskos "+plan.Name)
	}
	form.Set("metadata[plan]", plan.ID)
	form.Set("proration_behavior", "create_prorations")
	form.Set("cancel_at_period_end", "false") // switching plans resumes a subscription headed toward cancellation
	_, err = p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/subscriptions/"+url.PathEscape(subID), form)
	return err
}

func (p *StripeProvider) SetCancelAtPeriodEnd(ctx context.Context, subID string, cancel bool) error {
	form := url.Values{}
	form.Set("cancel_at_period_end", strconv.FormatBool(cancel))
	_, err := p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/subscriptions/"+url.PathEscape(subID), form)
	return err
}

// PortalURL creates a Billing Portal session (payment method, invoices, history).
func (p *StripeProvider) PortalURL(ctx context.Context, customerID, returnURL string) (string, error) {
	form := url.Values{}
	form.Set("customer", customerID)
	if returnURL != "" {
		form.Set("return_url", returnURL)
	}
	raw, err := p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/billing_portal/sessions", form)
	if err != nil {
		return "", err
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

// SyncPlan mirrors the plan at Stripe: ensures the Product (creates/renames) and the recurring
// Price. Prices are immutable — if the amount changed, create a new one, mark it default, and
// deactivate the old one.
func (p *StripeProvider) SyncPlan(ctx context.Context, plan billing.Plan, productID, priceID string, priceChanged bool) (string, string, error) {
	// 1) Product
	if productID == "" {
		form := url.Values{}
		form.Set("name", "Beskos "+plan.Name)
		form.Set("metadata[plan]", plan.ID)
		raw, err := p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/products", form)
		if err != nil {
			return "", "", err
		}
		var out struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", "", err
		}
		productID = out.ID
	} else {
		form := url.Values{}
		form.Set("name", "Beskos "+plan.Name)
		if _, err := p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/products/"+url.PathEscape(productID), form); err != nil {
			return "", "", err
		}
	}
	// 2) Price — only created when it does not exist or the amount changed.
	if priceID == "" || priceChanged {
		base := strings.ToLower(p.cfg.Settings().Currency)
		form := url.Values{}
		form.Set("product", productID)
		form.Set("currency", base)
		form.Set("unit_amount", strconv.Itoa(plan.PriceCentsPerUser))
		form.Set("recurring[interval]", "month")
		form.Set("metadata[plan]", plan.ID)
		// Per-country currency: the other currencies go in as currency_options of the SAME price.
		for cur, cents := range plan.Prices {
			lc := strings.ToLower(cur)
			if lc == base {
				continue
			}
			form.Set("currency_options["+lc+"][unit_amount]", strconv.Itoa(cents))
		}
		raw, err := p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/prices", form)
		if err != nil {
			return productID, priceID, err
		}
		var out struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return productID, priceID, err
		}
		if priceID != "" { // deactivates the old price (does not affect existing subscriptions)
			off := url.Values{}
			off.Set("active", "false")
			_, _ = p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/prices/"+url.PathEscape(priceID), off)
		}
		priceID = out.ID
	}
	return productID, priceID, nil
}

// UpdateQuantity adjusts the subscription's seats with proration (create_prorations): whoever
// joins mid-period pays only the remaining fraction; whoever leaves generates a credit on the
// next invoice.
func (p *StripeProvider) UpdateQuantity(ctx context.Context, subID string, seats int) error {
	info, err := p.Subscription(ctx, subID)
	if err != nil {
		return err
	}
	if info.ItemID == "" {
		return fmt.Errorf("stripe: subscription %s has no items", subID)
	}
	if seats < 1 {
		seats = 1
	}
	form := url.Values{}
	form.Set("items[0][id]", info.ItemID)
	form.Set("items[0][quantity]", strconv.Itoa(seats))
	form.Set("proration_behavior", "create_prorations")
	_, err = p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/subscriptions/"+url.PathEscape(subID), form)
	return err
}

// EnsureCustomer creates (or updates) the customer at Stripe with the billing data. It is what
// makes the invoice come out with legal name, address, and tax ID instead of just an email —
// what a company needs to deduct VAT. The tax ID goes as a separate tax_id (Stripe keeps it in
// its own list).
func (p *StripeProvider) EnsureCustomer(ctx context.Context, customerID string, c billing.Customer) (string, error) {
	form := url.Values{}
	if c.Name != "" {
		form.Set("name", c.Name)
	}
	if c.Email != "" {
		// This is the FINANCIAL CONTACT's email — this is where Stripe sends invoices and
		// receipts, not whoever happened to be signing up.
		form.Set("email", c.Email)
	}
	if c.Phone != "" {
		form.Set("phone", c.Phone)
	}
	if c.Country != "" {
		form.Set("address[country]", strings.ToUpper(c.Country))
	}
	if c.City != "" {
		form.Set("address[city]", c.City)
	}
	if c.PostalCode != "" {
		form.Set("address[postal_code]", c.PostalCode)
	}
	if c.AddressLine1 != "" {
		form.Set("address[line1]", c.AddressLine1)
	}
	if c.AddressLine2 != "" {
		form.Set("address[line2]", c.AddressLine2)
	}
	form.Set("metadata[tenant_id]", c.TenantID)
	form.Set("metadata[kind]", c.Kind)
	if c.TaxID != "" {
		form.Set("metadata[tax_id]", c.TaxID)
	}

	endpoint := "https://api.stripe.com/v1/customers"
	if customerID != "" {
		endpoint += "/" + url.PathEscape(customerID)
	}
	raw, err := p.doForm(ctx, http.MethodPost, endpoint, form)
	if err != nil {
		// Customer deleted at Stripe (or id from another account): start over instead of getting stuck.
		if customerID != "" {
			return p.EnsureCustomer(ctx, "", c)
		}
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if c.TaxID != "" {
		p.syncTaxID(ctx, out.ID, c)
	}
	return out.ID, nil
}

// syncTaxID sets the customer's tax ID at Stripe (eu_vat for the EU). Best-effort: a rejected
// number cannot block the subscription — the amount to charge is decided by us, not by Stripe.
func (p *StripeProvider) syncTaxID(ctx context.Context, customerID string, c billing.Customer) {
	kind := "eu_vat"
	if !IsEU(c.Country) {
		return // outside the EU each country has its own type; we do not risk guessing
	}
	base := "https://api.stripe.com/v1/customers/" + url.PathEscape(customerID) + "/tax_ids"
	// Is the same number already there? Do not repeat it (Stripe rejects duplicates).
	if raw, err := p.doForm(ctx, http.MethodGet, base, nil); err == nil {
		var list struct {
			Data []struct {
				ID    string `json:"id"`
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &list) == nil {
			for _, it := range list.Data {
				if strings.EqualFold(it.Value, c.TaxID) {
					return
				}
				if it.Type == kind { // number changed: remove the old one
					_, _ = p.doForm(ctx, http.MethodDelete, base+"/"+url.PathEscape(it.ID), nil)
				}
			}
		}
	}
	form := url.Values{}
	form.Set("type", kind)
	form.Set("value", c.TaxID)
	if _, err := p.doForm(ctx, http.MethodPost, base, form); err != nil {
		log.Printf("stripe: tax_id %q rejected for %s: %v", c.TaxID, customerID, err)
	}
}
