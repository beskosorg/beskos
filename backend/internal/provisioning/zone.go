package provisioning

import (
	"strings"
)

// StripStalwartDKIM removes the DKIM records Stalwart generated from the zone (TXT `..._domainkey...`
// with `DKIM1`). In relay mode Stalwart doesn't sign (Manual DKIM), so these records must not reach
// the customer when an external relay supplies its own signing key.
// A DKIM record does not fit on one line, and Stalwart writes the long ones in the parenthesised
// form that BIND allows:
//
//	v1-rsa-20260801._domainkey.acme.com. IN TXT (
//	    "v=DKIM1; k=rsa; "
//	    "p=MIIBIj..." )
//
// Removing the lines that say DKIM1 therefore deleted the CONTENTS and left the header behind --
// a record with a name and no value. It reached the customer's instructions as a line to publish
// that cannot be published, and, worse, DNS verification looked it up and waited forever for a
// record that by then nobody was going to create. Found on the second real customer domain, an
// hour into a provisioning run that had every other record correct.
func StripStalwartDKIM(zone string) string {
	var kept []string
	dropping := false
	for _, line := range strings.Split(zone, "\n") {
		if dropping {
			if strings.Contains(line, ")") {
				dropping = false
			}
			continue
		}
		if !strings.Contains(line, "_domainkey") {
			kept = append(kept, line)
			continue
		}
		// A record that opens a parenthesis without closing it continues on the lines below.
		if strings.Contains(line, "(") && !strings.Contains(line, ")") {
			dropping = true
		}
		continue
	}
	return strings.Join(kept, "\n")
}

// DNSRecord is a record ready for the customer to publish, structured for the UI (dns_pending screen).
// Purpose says WHAT the record is FOR (mx | spf | dkim | dkim_ses | dmarc | other) — the UI groups
// by it and explains that ALL of them are required to receive/send email.
type DNSRecord struct {
	Host    string `json:"host"`
	Type    string `json:"type"`
	Value   string `json:"value"`
	Purpose string `json:"purpose"`
}

// classify infers the record's purpose from its type/host/value.
func classify(r DNSRecord) string {
	switch {
	case r.Type == "MX" && strings.Contains(r.Value, "amazonses.com"):
		return "mailfrom" // MX for the custom MAIL FROM (SES bounces) — part of SENDING
	case r.Type == "MX":
		return "mx"
	case strings.Contains(r.Value, "v=spf1"):
		return "spf"
	case strings.HasPrefix(r.Host, "_dmarc."):
		return "dmarc"
	case strings.Contains(r.Host, "_domainkey") && strings.Contains(r.Value, "amazonses.com"):
		return "dkim_ses"
	case strings.Contains(r.Host, "_domainkey"):
		return "dkim"
	case r.Type == "SRV":
		return "srv" // automatic client discovery (IMAP/JMAP/CalDAV/…): a convenience, not a requirement
	case strings.Contains(r.Host, "mta-sts"):
		return "mta_sts" // strict transport policy (hardening)
	case strings.HasPrefix(r.Host, "_smtp._tls"):
		return "tls_rpt" // TLS failure reports (hardening)
	// Stalwart emits "ua-auto-config" (hyphenated); other servers use autoconfig/autodiscover.
	case strings.Contains(r.Host, "auto-config") || strings.Contains(r.Host, "autoconfig") || strings.Contains(r.Host, "autodiscover"):
		return "autoconfig" // automatic client configuration
	default:
		return "other"
	}
}

// ParseZone converts the BIND zone excerpt returned by Stalwart (dnsZoneFile) into structured
// records. Format of each line: "<host>. IN <TYPE> <value...>". Invalid lines are ignored.
func ParseZone(zone string) []DNSRecord {
	var out []DNSRecord
	for _, raw := range strings.Split(zone, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		// Smallest valid form: host IN TYPE value
		in := indexOf(fields, "IN")
		if in < 1 || in+2 >= len(fields) {
			continue
		}
		host := strings.TrimSuffix(fields[in-1], ".")
		rtype := fields[in+1]
		value := strings.Join(fields[in+2:], " ")
		value = strings.Trim(value, "\"")
		rec := DNSRecord{Host: host, Type: rtype, Value: value}
		rec.Purpose = classify(rec)
		out = append(out, rec)
	}
	return out
}

func indexOf(fields []string, target string) int {
	for i, f := range fields {
		if f == target {
			return i
		}
	}
	return -1
}

// MTASTSHost resolves the name that serves MTA-STS policies for every customer domain.
//
// Derived from the mail host by convention rather than required, so a new install does not have to
// know the name exists, and overridable for a deployment that serves the policy elsewhere. Lives
// here because the API and the worker both need the same answer: one deriving it differently from
// the other would publish a CNAME pointing at a host that serves no policy.
func MTASTSHost(mailHost, override string) string {
	if override != "" {
		return override
	}
	if i := strings.Index(mailHost, "."); i > 0 {
		return "mta-sts" + mailHost[i:]
	}
	return ""
}

// PublicHosts replaces the hostname Stalwart puts in the zone with names a customer can publish.
//
// THIS IS THE DIFFERENCE BETWEEN A ZONE THAT WORKS AND ONE THAT LOOKS RIGHT. Stalwart writes its own
// server hostname into every host-valued record, and inside a container that hostname is the
// container id -- "3517d6da4cde". Handed to a customer, the MX points at a name that resolves
// nowhere: no mail is delivered at all, and nothing in the instructions looks wrong. Our own domain
// only works because its records were published by hand.
//
// A SINGLE LABEL IS THE TELL. A public mail host always has a dot in it; a container id never does.
// That is a deterministic test, so this cannot mistake a customer's legitimate target for an
// internal one -- and it keeps working if the container id changes, which it does on every deploy.
//
// The MTA-STS record points somewhere else on purpose: the policy is served over HTTPS by the policy
// host, not by the mail server, and a CNAME to the mail host would answer with the wrong certificate.
func PublicHosts(zone, mailHost, mtaStsHost string) string {
	if mailHost == "" {
		return zone
	}
	lines := strings.Split(zone, "\n")
	for i, line := range lines {
		f := strings.Fields(line)
		in := indexOf(f, "IN")
		if in < 1 || in+2 >= len(f) {
			continue
		}
		rtype, last := strings.ToUpper(f[in+1]), len(f)-1
		target := strings.TrimSuffix(f[last], ".")
		if target == "" || strings.Contains(target, ".") {
			continue // already a public name, or not a host at all
		}
		switch rtype {
		case "MX":
			f[last] = mailHost + "."
		case "CNAME":
			host := strings.TrimSuffix(f[in-1], ".")
			if strings.HasPrefix(host, "mta-sts.") && mtaStsHost != "" {
				f[last] = mtaStsHost + "."
			} else {
				f[last] = mailHost + "."
			}
		case "SRV":
			// The target is the last field here too ("IN SRV <pri> <weight> <port> <target>"), and
			// Stalwart names the container in it just as it does in the MX. These records are the
			// ones a mail client reads to configure itself, so left alone they send Outlook and
			// Thunderbird looking for a host that does not exist.
			f[last] = mailHost + "."
		default:
			continue
		}
		lines[i] = strings.Join(f, " ")
	}
	return strings.Join(lines, "\n")
}
