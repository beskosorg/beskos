//go:build pro

package ee

import "testing"

func TestQuoteFor(t *testing.T) {
	cases := []struct {
		name          string
		kind, country string
		taxID         string
		wantRate      int
		wantReverse   bool
		wantTotal     int // subtotal 10000 (100,00 × 1)
	}{
		{"polish company pays polish VAT", "company", "PL", "PL1234567890", PolishVATRate, false, 12300},
		{"polish individual pays polish VAT", "individual", "PL", "", PolishVATRate, false, 12300},
		{"EU company with tax ID reverse-charges", "company", "PT", "PT123456789", 0, true, 10000},
		{"EU company without tax ID pays polish VAT", "company", "PT", "", PolishVATRate, false, 12300},
		{"EU company with invalid tax ID pays VAT", "company", "PT", "PT12", PolishVATRate, false, 12300},
		{"EU individual pays polish VAT", "individual", "DE", "", PolishVATRate, false, 12300},
		{"outside the EU carries no VAT", "company", "BR", "", 0, false, 10000},
		{"individual outside the EU carries no VAT", "individual", "US", "", 0, false, 10000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := QuoteFor("EUR", 10000, 1, c.kind, c.country, c.taxID)
			if q.VATRateBps != c.wantRate {
				t.Errorf("rate = %d, want %d", q.VATRateBps, c.wantRate)
			}
			if q.ReverseCharge != c.wantReverse {
				t.Errorf("reverse charge = %v, want %v", q.ReverseCharge, c.wantReverse)
			}
			if q.TotalCents != c.wantTotal {
				t.Errorf("total = %d, want %d", q.TotalCents, c.wantTotal)
			}
		})
	}
}

func TestQuoteForSeatsAndRounding(t *testing.T) {
	// 3 × 7.77 = 23.31 → 23% VAT = 5.3613 → rounds to 5.36; total 28.67.
	q := QuoteFor("EUR", 777, 3, "individual", "PL", "")
	if q.SubtotalCents != 2331 || q.VATCents != 536 || q.TotalCents != 2867 {
		t.Fatalf("subtotal/VAT/total = %d/%d/%d, want 2331/536/2867", q.SubtotalCents, q.VATCents, q.TotalCents)
	}
	if got := QuoteFor("EUR", 1000, 0, "individual", "PL", "").Seats; got != 1 {
		t.Errorf("minimum seats = %d, want 1", got)
	}
}

func TestNormalizeVATID(t *testing.T) {
	cases := map[string]struct{ in, country, want string }{
		"strips spaces and dots": {"PL 123-456.7890", "PL", "PL1234567890"},
		"prepends the country":   {"1234567890", "pl", "PL1234567890"},
		"greece declares as EL":  {"123456789", "GR", "EL123456789"},
		"already has the prefix": {"de123456789", "DE", "DE123456789"},
		"empty stays empty":      {"", "PT", ""},
	}
	for name, c := range cases {
		if got := NormalizeVATID(c.in, c.country); got != c.want {
			t.Errorf("%s: %q → %q, want %q", name, c.in, got, c.want)
		}
	}
}

func TestValidVATID(t *testing.T) {
	ok := map[string]string{"PL": "PL1234567890", "PT": "PT123456789", "DE": "DE123456789", "NL": "NL123456789B01", "GR": "EL123456789"}
	for cc, id := range ok {
		if !ValidVATID(id, cc) {
			t.Errorf("%s: %q should be valid", cc, id)
		}
	}
	bad := map[string]string{"PL": "PL123", "PT": "PT12345678", "DE": "DEABCDEFGHI", "BR": "12345678901"}
	for cc, id := range bad {
		if ValidVATID(id, cc) {
			t.Errorf("%s: %q should not be valid", cc, id)
		}
	}
}

func TestIsEU(t *testing.T) {
	for _, cc := range []string{"PL", "pt", " DE "} {
		if !IsEU(cc) {
			t.Errorf("%q should be in the EU", cc)
		}
	}
	for _, cc := range []string{"BR", "US", "GB", "CH", ""} {
		if IsEU(cc) {
			t.Errorf("%q should not be in the EU", cc)
		}
	}
}
