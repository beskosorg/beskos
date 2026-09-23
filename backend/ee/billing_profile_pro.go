//go:build pro

package ee

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/httpx"
	"github.com/beskosorg/beskos/backend/internal/reqauth"
)

// Customer billing data + VAT simulation. Issuing the invoice is our obligation, so the data
// lives here; Stripe receives a COPY (customer with address/tax ID/finance email) so checkout
// only has to ask for the card.

type billingProfileReq struct {
	Kind         string `json:"kind"` // company | individual
	LegalName    string `json:"legal_name"`
	TaxID        string `json:"tax_id"`
	Country      string `json:"country"`
	City         string `json:"city"`
	PostalCode   string `json:"postal_code"`
	AddressLine1 string `json:"address_line1"`
	AddressLine2 string `json:"address_line2"`
	InvoiceEmail string `json:"invoice_email"`
	InvoiceName  string `json:"invoice_name"`
	Phone        string `json:"phone"`
}

// GetBillingProfile returns the tenant's saved billing profile, or an empty skeleton when none
// has been filled in yet.
func GetBillingProfile(w http.ResponseWriter, r *http.Request, deps Deps) {
	u := reqauth.MailUserFrom(r.Context())
	p, err := deps.Queries.GetBillingProfile(r.Context(), u.TenantID)
	if err != nil {
		// Not filled in yet: return the skeleton (the form opens empty, not in an error state).
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"kind": "company", "filled": false})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"kind": p.Kind, "legal_name": p.LegalName, "tax_id": p.TaxID, "country": p.Country,
		"city": p.City, "postal_code": p.PostalCode, "address_line1": p.AddressLine1,
		"address_line2": p.AddressLine2, "invoice_email": p.InvoiceEmail, "invoice_name": p.InvoiceName,
		"phone": p.Phone, "filled": p.LegalName != "" && p.InvoiceEmail != "",
	})
}

// validateProfile returns the error code for the first missing field ("" = all good).
func validateProfile(req *billingProfileReq) string {
	req.Kind = strings.ToLower(strings.TrimSpace(req.Kind))
	if req.Kind != "individual" {
		req.Kind = "company"
	}
	req.Country = strings.ToUpper(strings.TrimSpace(req.Country))
	req.LegalName = strings.TrimSpace(req.LegalName)
	req.InvoiceEmail = strings.TrimSpace(req.InvoiceEmail)
	req.TaxID = NormalizeVATID(req.TaxID, req.Country)

	switch {
	case req.LegalName == "":
		return "billing.legal_name_required"
	case len(req.Country) != 2:
		return "billing.country_required"
	case req.City == "" || req.AddressLine1 == "":
		return "billing.address_required"
	case !strings.Contains(req.InvoiceEmail, "@"):
		return "billing.invoice_email_required"
	// A Polish company without a tax ID can't deduct VAT — and the Polish invoice requires it.
	case req.Kind == "company" && req.Country == SellerCountry && req.TaxID == "":
		return "billing.tax_id_required"
	case req.Kind == "company" && req.TaxID != "" && IsEU(req.Country) && !ValidVATID(req.TaxID, req.Country):
		return "billing.tax_id_invalid"
	}
	return ""
}

// SaveBillingProfile validates and persists the tenant's billing profile.
func SaveBillingProfile(w http.ResponseWriter, r *http.Request, deps Deps) {
	u := reqauth.MailUserFrom(r.Context())
	var req billingProfileReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	if code := validateProfile(&req); code != "" {
		httpx.WriteErr(w, http.StatusBadRequest, code)
		return
	}
	if _, err := deps.Queries.UpsertBillingProfile(r.Context(), db.UpsertBillingProfileParams{
		TenantID: u.TenantID, Kind: req.Kind, LegalName: req.LegalName, TaxID: req.TaxID,
		Country: req.Country, City: req.City, PostalCode: req.PostalCode,
		AddressLine1: req.AddressLine1, AddressLine2: req.AddressLine2,
		InvoiceEmail: req.InvoiceEmail, InvoiceName: req.InvoiceName, Phone: req.Phone,
	}); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "billing.profile_save_failed")
		return
	}
	_ = deps.Queries.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "billing.profile", Target: req.LegalName})
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type quoteReq struct {
	Plan    string `json:"plan"`
	Cycle   string `json:"cycle"`
	Kind    string `json:"kind"`
	Country string `json:"country"`
	TaxID   string `json:"tax_id"`
}

// BillingQuote simulates the amount due: price × users + VAT (or reverse charge). It's the
// same calculation that goes on the invoice — the subscription page doesn't repeat the rule in
// JavaScript, otherwise the displayed total and the charged amount would diverge on the first adjustment.
func BillingQuote(w http.ResponseWriter, r *http.Request, deps Deps) {
	u := reqauth.MailUserFrom(r.Context())
	var req quoteReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	plan, ok := deps.CatalogPlanByKey(r.Context(), req.Plan)
	if !ok || !plan.IsActive {
		httpx.WriteErr(w, http.StatusBadRequest, "billing.invalid_plan")
		return
	}
	plan = localPlan(plan, tenantCurrency(r.Context(), u.TenantID, deps), deps)
	cycle := billing.Cycle(req.Cycle).Normalize()
	seats := checkoutSeats(r, u.TenantID, plan, deps)
	q := QuoteFor(plan.Currency, plan.UnitCents(cycle), seats, req.Kind, req.Country, req.TaxID)

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"quote":            q,
		"cycle":            string(cycle),
		"plan":             plan.ID,
		"plan_name":        plan.Name,
		"monthly_cents":    plan.PriceCentsPerUser,
		"months_charged":   billing.AnnualMonthsCharged,
		"seller_country":   SellerCountry,
		"vat_rate_percent": float64(q.VATRateBps) / 100,
	})
}

// checkoutSeats is the number of users charged: the current ones, never below the global minimum
// nor the plan's floor.
func checkoutSeats(r *http.Request, tid uuid.UUID, plan billing.Plan, deps Deps) int {
	seats := billableSeats(r, tid, deps)
	if seats < plan.MinUsers {
		seats = plan.MinUsers
	}
	return seats
}

// billingCustomer builds the customer data for the provider from the saved profile.
func billingCustomer(tenantID string, p db.BillingProfile) billing.Customer {
	return billing.Customer{
		TenantID: tenantID, Kind: p.Kind, Name: p.LegalName, Email: p.InvoiceEmail,
		TaxID: p.TaxID, Country: p.Country, City: p.City, PostalCode: p.PostalCode,
		AddressLine1: p.AddressLine1, AddressLine2: p.AddressLine2, Phone: p.Phone,
	}
}
