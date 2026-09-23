package provisioning

import (
	"strings"
	"testing"
)

// The zone handed to a customer is the whole of what they will publish. If a host in it is wrong,
// their mail does not arrive and nothing in the instructions looks broken -- which is the worst
// shape a bug can take, because the customer blames their own DNS.

const containerZone = `example.com. IN MX 10 3517d6da4cde.
mta-sts.example.com. IN CNAME 3517d6da4cde.
autoconfig.example.com. IN CNAME 3517d6da4cde.
example.com. IN TXT "v=spf1 mx -all"
_dmarc.example.com. IN TXT "v=DMARC1; p=reject; rua=mailto:postmaster@example.com"`

func TestPublicHostsReplacesTheContainerName(t *testing.T) {
	out := PublicHosts(containerZone, "mail.beskos.com", "mta-sts.beskos.com")
	if strings.Contains(out, "3517d6da4cde") {
		t.Fatalf("the container id survived:\n%s", out)
	}
	if !strings.Contains(out, "IN MX 10 mail.beskos.com.") {
		t.Errorf("MX not rewritten:\n%s", out)
	}
	// The policy is served by the policy host over HTTPS. Pointing it at the mail server would
	// answer with a certificate for the wrong name, and the policy would never be readable.
	if !strings.Contains(out, "mta-sts.example.com. IN CNAME mta-sts.beskos.com.") {
		t.Errorf("MTA-STS must point at the policy host:\n%s", out)
	}
	if !strings.Contains(out, "autoconfig.example.com. IN CNAME mail.beskos.com.") {
		t.Errorf("other CNAMEs go to the mail host:\n%s", out)
	}
}

// TXT values often end in a word without a dot ("-all", a DMARC address). Rewriting those would
// corrupt the SPF and the DMARC -- the two records the audit checks hardest.
func TestPublicHostsLeavesTextRecordsAlone(t *testing.T) {
	out := PublicHosts(containerZone, "mail.beskos.com", "mta-sts.beskos.com")
	if !strings.Contains(out, `"v=spf1 mx -all"`) {
		t.Errorf("SPF was altered:\n%s", out)
	}
	if !strings.Contains(out, "rua=mailto:postmaster@example.com") {
		t.Errorf("DMARC was altered:\n%s", out)
	}
}

// A target that is already public belongs to the customer, or to a provider they chose. Rewriting it
// would silently redirect their mail to us.
func TestPublicHostsNeverTouchesAPublicTarget(t *testing.T) {
	zone := "example.com. IN MX 10 aspmx.l.google.com."
	if out := PublicHosts(zone, "mail.beskos.com", "mta-sts.beskos.com"); out != zone {
		t.Errorf("a public MX was rewritten:\n%s", out)
	}
}

// Without a configured public host there is nothing to substitute, and inventing one would be worse
// than leaving the zone as it is: the customer at least sees something obviously wrong.
func TestPublicHostsIsANoOpWithoutAHost(t *testing.T) {
	if out := PublicHosts(containerZone, "", ""); out != containerZone {
		t.Error("with no mail host configured the zone must be left untouched")
	}
}

// The API and the worker each ask for this name separately. If they ever disagree, the customer
// publishes a CNAME to a host that serves no policy -- and MTA-STS fails closed for that domain.
func TestMTASTSHostFollowsTheMailHost(t *testing.T) {
	for _, c := range []struct{ mail, override, want string }{
		{"mail.beskos.com", "", "mta-sts.beskos.com"},
		{"mail.beskos.com", "policy.example.net", "policy.example.net"},
		{"", "", ""},             // nothing configured: no record to write
		{"3517d6da4cde", "", ""}, // a container id has no dot, so there is nothing to derive from
		{"", "policy.net", "policy.net"},
	} {
		if got := MTASTSHost(c.mail, c.override); got != c.want {
			t.Errorf("MTASTSHost(%q, %q) = %q, want %q", c.mail, c.override, got, c.want)
		}
	}
}

// SRV is how a mail client configures itself. Stalwart names the container in the target, exactly
// as it does in the MX -- so leaving it alone sends Outlook and Thunderbird to a host that does not
// resolve. Found on the first real customer domain, after the MX case had already been fixed.
func TestPublicHostsRewritesSRVTargets(t *testing.T) {
	zone := "_imaps._tcp.acme.com. IN SRV 0 1 993 2ba8572f3dec.\n" +
		"_jmap._tcp.acme.com. IN SRV 0 1 443 mail.elsewhere.net."
	got := PublicHosts(zone, "mail.beskos.com", "mta-sts.beskos.com")
	if !strings.Contains(got, "993 mail.beskos.com.") {
		t.Errorf("container target left in the SRV record:\n%s", got)
	}
	if !strings.Contains(got, "443 mail.elsewhere.net.") {
		t.Errorf("a target that was already public must not be touched:\n%s", got)
	}
}

// Stalwart writes long DKIM records across several lines, in the parenthesised form BIND allows.
// Deleting only the lines that say DKIM1 removed the CONTENTS and left the name behind -- a record
// the customer cannot publish, which DNS verification then waited for forever.
func TestStripStalwartDKIMRemovesMultiLineRecords(t *testing.T) {
	zone := "acme.com. IN MX 10 mail.example.\n" +
		"v1-rsa-1._domainkey.acme.com. IN TXT (\n" +
		"    \"v=DKIM1; k=rsa; \"\n" +
		"    \"p=MIIBIj...\" )\n" +
		"_dmarc.acme.com. IN TXT \"v=DMARC1; p=reject\""
	got := StripStalwartDKIM(zone)
	if strings.Contains(got, "_domainkey") {
		t.Errorf("the header of the multi-line record survived:\n%s", got)
	}
	if strings.Contains(got, "p=MIIBIj") || strings.Contains(got, "k=rsa") {
		t.Errorf("a continuation line survived:\n%s", got)
	}
	for _, keep := range []string{"IN MX 10 mail.example.", "v=DMARC1"} {
		if !strings.Contains(got, keep) {
			t.Errorf("removed a record it had no business touching (%s):\n%s", keep, got)
		}
	}
}

// The verifier must look up the record that actually carries a key. It used to take the first line
// merely NAMING a _domainkey host, which after the strip above was a header with no value.
func TestDKIMHostIgnoresAHeaderWithoutAKey(t *testing.T) {
	zone := "v1-rsa-1._domainkey.acme.com. IN TXT (\n" +
		"beskos._domainkey.acme.com. IN TXT \"v=DKIM1; k=rsa; p=AAA\""
	if got := dkimHost(zone, "acme.com"); got != "beskos._domainkey.acme.com" {
		t.Errorf("dkimHost = %q, want the record that carries the key", got)
	}
}
