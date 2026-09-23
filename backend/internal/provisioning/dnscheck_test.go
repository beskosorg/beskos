package provisioning

import (
	"strings"
	"testing"
)

// The value the customer publishes is almost never identical to ours character by character. These
// tests fix what counts as "published" for each type — this is where a false negative would send
// someone chasing a problem that doesn't exist, and a false positive would say "all good" while
// mail was failing.
func TestMatchesPorTipo(t *testing.T) {
	cases := []struct {
		name  string
		rec   DNSRecord
		found []string
		want  bool
	}{
		{
			name:  "MX ignores priority and the trailing dot",
			rec:   DNSRecord{Type: "MX", Value: "10 mail.exemplo.com.", Purpose: "mx"},
			found: []string{"mail.exemplo.com"},
			want:  true,
		},
		{
			name:  "MX from another provider doesn't pass",
			rec:   DNSRecord{Type: "MX", Value: "10 mail.exemplo.com.", Purpose: "mx"},
			found: []string{"aspmx.l.google.com"},
			want:  false,
		},
		{
			name:  "SPF only needs to contain our include",
			rec:   DNSRecord{Type: "TXT", Value: "v=spf1 include:_spf.beskos.com ~all", Purpose: "spf"},
			found: []string{"v=spf1 ip4:1.2.3.4 include:_spf.beskos.com include:outro.com -all"},
			want:  true,
		},
		{
			name:  "SPF without our include fails",
			rec:   DNSRecord{Type: "TXT", Value: "v=spf1 include:_spf.beskos.com ~all", Purpose: "spf"},
			found: []string{"v=spf1 include:_spf.google.com ~all"},
			want:  false,
		},
		{
			name:  "DKIM compares the key, not the tag order",
			rec:   DNSRecord{Type: "TXT", Value: "v=DKIM1; k=rsa; p=MIIBIjANBg", Purpose: "dkim"},
			found: []string{"k=rsa; p=MIIBIjANBg; v=DKIM1"},
			want:  true,
		},
		{
			name:  "DKIM with a different key fails (the dangerous case)",
			rec:   DNSRecord{Type: "TXT", Value: "v=DKIM1; k=rsa; p=MIIBIjANBg", Purpose: "dkim"},
			found: []string{"v=DKIM1; k=rsa; p=OUTRACHAVE"},
			want:  false,
		},
		{
			name:  "DMARC accepts any published policy",
			rec:   DNSRecord{Type: "TXT", Value: "v=DMARC1; p=quarantine; rua=mailto:d@b.com", Purpose: "dmarc"},
			found: []string{"v=DMARC1; p=none"},
			want:  true,
		},
		{
			name:  "SRV compares the target",
			rec:   DNSRecord{Type: "SRV", Value: "0 1 993 mail.exemplo.com.", Purpose: "other"},
			found: []string{"mail.exemplo.com"},
			want:  true,
		},
	}
	for _, c := range cases {
		if got := matches(c.rec, c.found); got != c.want {
			t.Errorf("%s: matches=%v, want %v", c.name, got, c.want)
		}
	}
}

func TestRequiredPurpose(t *testing.T) {
	for _, p := range []string{"mx", "spf", "dkim"} {
		if !requiredPurpose(p) {
			t.Errorf("%s should be required", p)
		}
	}
	// DMARC and the rest are recommendations: they warn, they don't block.
	for _, p := range []string{"dmarc", "other", ""} {
		if requiredPurpose(p) {
			t.Errorf("%s should not be required", p)
		}
	}
}

func TestZoneFileMantemOsRegistos(t *testing.T) {
	zone := "exemplo.com. IN MX 10 mail.exemplo.com.\n\n_dmarc.exemplo.com. IN TXT \"v=DMARC1; p=none\"\n"
	out := ZoneFile("exemplo.com", zone)
	for _, want := range []string{"IN MX 10 mail.exemplo.com.", "_dmarc.exemplo.com."} {
		if !strings.Contains(out, want) {
			t.Errorf("the exported zone lost %q", want)
		}
	}
	if out[0] != ';' {
		t.Error("the header must be a comment, otherwise the provider's import rejects it")
	}
}

// Classification decides what the page presents as required and what stays under "advanced". A
// mistake here asks the customer for records they don't need — or hides one they do need.
func TestClassifyEObrigatoriedade(t *testing.T) {
	cases := []struct {
		rec      DNSRecord
		purpose  string
		required bool
	}{
		{DNSRecord{Type: "MX", Host: "e.com", Value: "10 mail.e.com."}, "mx", true},
		{DNSRecord{Type: "TXT", Host: "e.com", Value: "v=spf1 include:_spf.b.com ~all"}, "spf", true},
		{DNSRecord{Type: "TXT", Host: "sel._domainkey.e.com", Value: "v=DKIM1; p=x"}, "dkim", true},
		{DNSRecord{Type: "TXT", Host: "_dmarc.e.com", Value: "v=DMARC1; p=none"}, "dmarc", false},
		{DNSRecord{Type: "SRV", Host: "_imaps._tcp.e.com", Value: "0 1 993 mail.e.com."}, "srv", false},
		{DNSRecord{Type: "CNAME", Host: "mta-sts.e.com", Value: "mail.e.com."}, "mta_sts", false},
		{DNSRecord{Type: "TXT", Host: "_smtp._tls.e.com", Value: "v=TLSRPTv1; rua=mailto:p@e.com"}, "tls_rpt", false},
		{DNSRecord{Type: "CNAME", Host: "ua-auto-config.e.com", Value: "mail.e.com."}, "autoconfig", false},
	}
	for _, c := range cases {
		if got := classify(c.rec); got != c.purpose {
			t.Errorf("classify(%s %s) = %q, want %q", c.rec.Type, c.rec.Host, got, c.purpose)
		}
		if got := requiredPurpose(c.purpose); got != c.required {
			t.Errorf("requiredPurpose(%s) = %v, want %v", c.purpose, got, c.required)
		}
	}
}
