//go:build pro

package ee

import (
	"regexp"
	"strings"
)

// VAT on the sale. The seller is in POLAND, so the reference rate is the Polish one (23%).
//
// Rules applied (B2B/B2C digital service):
//   - Outside the EU ...................... 0% (out of scope of EU VAT)
//   - Poland (company or individual) ...... 23%
//   - EU, company with an intra-community tax ID   0%, reverse charge by the customer (art. 196
//     of Directive 2006/112/EC) — charging VAT here would charge the customer twice
//   - EU, individual or company without a tax ID ... 23%
//
// Tax ID validation is by FORMAT (country prefix + mask). Confirming it against VIES is a next
// step: until it exists, an EU company with a mistyped number pays VAT instead of reverse-charging
// — the error is on the safe side (overcharging), never undercharging.

const (
	// SellerCountry is the seller's country: it decides the applied rate and who reverse-charges.
	SellerCountry = "PL"
	// PolishVATRate in basis points (2300 = 23.00%) — an integer so no floating-point rounding
	// error creeps into the charged amount.
	PolishVATRate = 2300
)

// euCountries are the 27 member states (ISO 3166-1 alpha-2).
var euCountries = map[string]bool{
	"AT": true, "BE": true, "BG": true, "HR": true, "CY": true, "CZ": true, "DK": true,
	"EE": true, "FI": true, "FR": true, "DE": true, "GR": true, "HU": true, "IE": true,
	"IT": true, "LV": true, "LT": true, "LU": true, "MT": true, "NL": true, "PL": true,
	"PT": true, "RO": true, "SK": true, "SI": true, "ES": true, "SE": true,
}

// IsEU says whether the country (alpha-2) belongs to the European Union.
func IsEU(country string) bool { return euCountries[strings.ToUpper(strings.TrimSpace(country))] }

// Intra-community tax ID mask per country. Greece uses the prefix EL (not GR) in the number.
var vatPatterns = map[string]*regexp.Regexp{
	"AT": regexp.MustCompile(`^ATU[0-9]{8}$`),
	"BE": regexp.MustCompile(`^BE[01][0-9]{9}$`),
	"BG": regexp.MustCompile(`^BG[0-9]{9,10}$`),
	"HR": regexp.MustCompile(`^HR[0-9]{11}$`),
	"CY": regexp.MustCompile(`^CY[0-9]{8}[A-Z]$`),
	"CZ": regexp.MustCompile(`^CZ[0-9]{8,10}$`),
	"DK": regexp.MustCompile(`^DK[0-9]{8}$`),
	"EE": regexp.MustCompile(`^EE[0-9]{9}$`),
	"FI": regexp.MustCompile(`^FI[0-9]{8}$`),
	"FR": regexp.MustCompile(`^FR[0-9A-Z]{2}[0-9]{9}$`),
	"DE": regexp.MustCompile(`^DE[0-9]{9}$`),
	"GR": regexp.MustCompile(`^EL[0-9]{9}$`),
	"HU": regexp.MustCompile(`^HU[0-9]{8}$`),
	"IE": regexp.MustCompile(`^IE([0-9]{7}[A-W]|[0-9][A-Z*+][0-9]{5}[A-W]|[0-9]{7}[A-W][AH])$`),
	"IT": regexp.MustCompile(`^IT[0-9]{11}$`),
	"LV": regexp.MustCompile(`^LV[0-9]{11}$`),
	"LT": regexp.MustCompile(`^LT([0-9]{9}|[0-9]{12})$`),
	"LU": regexp.MustCompile(`^LU[0-9]{8}$`),
	"MT": regexp.MustCompile(`^MT[0-9]{8}$`),
	"NL": regexp.MustCompile(`^NL[0-9]{9}B[0-9]{2}$`),
	"PL": regexp.MustCompile(`^PL[0-9]{10}$`),
	"PT": regexp.MustCompile(`^PT[0-9]{9}$`),
	"RO": regexp.MustCompile(`^RO[0-9]{2,10}$`),
	"SK": regexp.MustCompile(`^SK[0-9]{10}$`),
	"SI": regexp.MustCompile(`^SI[0-9]{8}$`),
	"ES": regexp.MustCompile(`^ES[0-9A-Z][0-9]{7}[0-9A-Z]$`),
	"SE": regexp.MustCompile(`^SE[0-9]{12}$`),
}

// NormalizeVATID strips spaces/dots and uppercases. With no country prefix, it uses the one from
// the address — the most common mistake when filling it in ("1234567890" instead of "PL1234567890").
func NormalizeVATID(taxID, country string) string {
	s := strings.ToUpper(taxID)
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '.' || r == '-' || r == '/' {
			return -1
		}
		return r
	}, s)
	if s == "" {
		return ""
	}
	cc := strings.ToUpper(strings.TrimSpace(country))
	if cc == "GR" {
		cc = "EL" // Greece declares itself as EL in the number
	}
	if cc != "" && len(s) > 1 && (s[0] < 'A' || s[0] > 'Z' || s[1] < 'A' || s[1] > 'Z') {
		s = cc + s
	}
	return s
}

// ValidVATID validates the FORMAT of the intra-community tax ID for the given country.
func ValidVATID(taxID, country string) bool {
	cc := strings.ToUpper(strings.TrimSpace(country))
	re, ok := vatPatterns[cc]
	if !ok {
		return false
	}
	return re.MatchString(NormalizeVATID(taxID, cc))
}

// Quote is the value summary of a subscription, VAT included.
type Quote struct {
	Currency      string `json:"currency"`
	Seats         int    `json:"seats"`
	UnitCents     int    `json:"unit_cents"`     // price per user/month
	SubtotalCents int    `json:"subtotal_cents"` // unit × seats
	VATRateBps    int    `json:"vat_rate_bps"`   // 2300 = 23%
	VATCents      int    `json:"vat_cents"`
	TotalCents    int    `json:"total_cents"`
	ReverseCharge bool   `json:"reverse_charge"` // VAT reverse-charged by the customer (EU, B2B)
	TaxIDValid    bool   `json:"tax_id_valid"`   // tax ID format accepted
	Country       string `json:"country"`
}

// QuoteFor computes the VAT-included summary. `kind` is "company" or "individual".
func QuoteFor(currency string, unitCents, seats int, kind, country, taxID string) Quote {
	if seats < 1 {
		seats = 1
	}
	cc := strings.ToUpper(strings.TrimSpace(country))
	q := Quote{
		Currency:      currency,
		Seats:         seats,
		UnitCents:     unitCents,
		SubtotalCents: unitCents * seats,
		Country:       cc,
	}
	isCompany := kind == "company"
	q.TaxIDValid = isCompany && ValidVATID(taxID, cc)

	switch {
	case !IsEU(cc):
		// Outside the EU, no Polish VAT is charged.
	case cc == SellerCountry:
		q.VATRateBps = PolishVATRate
	case isCompany && q.TaxIDValid:
		q.ReverseCharge = true // 0% — the customer settles it in their own country
	default:
		q.VATRateBps = PolishVATRate
	}
	// Rounds to the cent (half up), as it appears on the invoice.
	q.VATCents = (q.SubtotalCents*q.VATRateBps + 5000) / 10000
	q.TotalCents = q.SubtotalCents + q.VATCents
	return q
}
