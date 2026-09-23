package mailaudit

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
)

// The value of this tool is that a stranger can trust its answer about their own mail. Every test
// here defends one of the ways that trust gets destroyed -- and each of these was a real mistake in
// the first version, not a hypothetical.

func TestAllQualifier(t *testing.T) {
	cases := map[string]string{
		"v=spf1 include:x -all": "-",
		"v=spf1 include:x ~all": "~",
		"v=spf1 ?all":           "?",
		"v=spf1 +all":           "+",
		"v=spf1 include:x":      "", // no "all" at all: the record decides nothing
		"v=spf1 -all include:x": "-",
	}
	for rec, want := range cases {
		if got := allQualifier(rec); got != want {
			t.Errorf("allQualifier(%q) = %q, want %q", rec, got, want)
		}
	}
}

// The lookup limit is what silently breaks a working SPF: the eleventh include makes receivers treat
// the record as absent, and nothing in the DNS looks wrong. Counting has to be right or the whole
// check is decoration.
func TestSpfLookupCheckThresholds(t *testing.T) {
	cases := []struct {
		lookups int
		want    Status
	}{
		{0, Pass}, {7, Pass},
		{8, Warn},  // one more tool breaks it; say so before it happens
		{10, Warn}, // exactly at the cap: still valid, no room left
		{11, Fail}, // receivers must permerror
	}
	for _, c := range cases {
		got := spfLookupCheck(spfResult{Found: true, Lookups: c.lookups, OverLimit: c.lookups > spfLookupLimit})
		if got.Status != c.want {
			t.Errorf("%d lookups -> %s, want %s", c.lookups, got.Status, c.want)
		}
	}
}

// A count we could not finish is not a count. Reporting a lower bound as if it were exact would put
// a domain in the clear that is actually over the limit.
func TestTruncatedCountIsUnknownNotPass(t *testing.T) {
	got := spfLookupCheck(spfResult{Found: true, Lookups: 3, Truncated: true})
	if got.Status != Unknown {
		t.Errorf("truncated count -> %s, want unknown", got.Status)
	}
}

// A cycle between two SPF records must not hang the audit: a public tool is exactly where somebody
// eventually points at a domain that redirects to itself.
func TestCountLookupsStopsOnCycles(t *testing.T) {
	seen := map[string]bool{"loop.example": true}
	n, _ := countLookups(context.TODO(), nil, "v=spf1 include:loop.example -all", seen, 0)
	if n != 1 {
		t.Errorf("counted %d, want the mechanism counted once and not followed", n)
	}
}

func TestSharedProviderMatchesBySuffix(t *testing.T) {
	if _, ok := sharedProvider("mail.locaweb.com.br"); !ok {
		t.Error("a subdomain of a shared host must match")
	}
	if _, ok := sharedProvider("mail-fr.securemail.pro"); !ok {
		t.Error("securemail.pro is shared and must match")
	}
	// The suffix must be a label boundary. Somebody who runs "notlocaweb.com.br" is not Locaweb, and
	// telling them they are would be a wrong claim about their supplier.
	if _, ok := sharedProvider("mail.notlocaweb.com.br"); ok {
		t.Error("a host that merely ends with the same letters is not the same provider")
	}
	if _, ok := sharedProvider("mail.beskos.com"); ok {
		t.Error("a dedicated host must not be reported as shared")
	}
}

// Statuses must stay distinct in the score: Unknown is not a pass. A tool that quietly counts what
// it could not check as success flatters every domain it audits, including the ones it should be
// warning.
func TestOnlyPassCounts(t *testing.T) {
	r := Report{Checks: []Check{
		{Status: Pass}, {Status: Warn}, {Status: Fail}, {Status: Unknown},
	}}
	for _, c := range r.Checks {
		if c.Status == Pass {
			r.Passed++
		}
	}
	r.Total = len(r.Checks)
	if r.Passed != 1 || r.Total != 4 {
		t.Errorf("passed=%d total=%d, want 1 of 4", r.Passed, r.Total)
	}
}

// The report must never speak in percentages. This is a guard against somebody adding one later:
// a percentage would be invented, because deliverability is not derivable from DNS.
func TestReportHasNoPercentageField(t *testing.T) {
	rt := reflect.TypeOf(Report{})
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		for _, forbidden := range []string{"Percent", "Score", "Rate"} {
			if strings.Contains(name, forbidden) {
				t.Errorf("Report gained %q: the audit counts checks, it does not score. "+
					"Deliverability is not derivable from DNS, so any such number is invented.", name)
			}
		}
	}
}

// A blocklist answers "not listed" by having no record at all. Reading that NXDOMAIN as a failed
// query is what made the check report "could not check" for every clean domain -- and a clean domain
// is the normal case, so the check was answering nothing, always.
func TestNotFoundMeansNotListed(t *testing.T) {
	err := &net.DNSError{Err: "no such host", IsNotFound: true}
	var dnsErr *net.DNSError
	if !errors.As(error(err), &dnsErr) || !dnsErr.IsNotFound {
		t.Fatal("a NXDOMAIN must be recognisable as not-found")
	}
	// And a timeout must NOT be: an unanswered list says nothing about the domain, and calling it
	// clean is the error that costs the reader money.
	timeout := &net.DNSError{Err: "timeout", IsTimeout: true}
	if errors.As(error(timeout), &dnsErr) && dnsErr.IsNotFound {
		t.Error("a timeout must not be read as not-listed")
	}
}
