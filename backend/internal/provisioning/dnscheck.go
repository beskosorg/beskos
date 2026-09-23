package provisioning

import (
	"context"
	"strings"
)

// Record-by-record DNS verification, so the domains page can say WHAT is missing instead of a mute
// "waiting". It's informational: whoever promotes `dns_pending → dns_verified` remains
// stepVerifyDNS (Verify), which only looks at the minimum. Here we look at everything we asked to
// be published.
//
// This does not compare text to text: an SPF TXT published by the customer is rarely identical to
// ours character by character (quoting, mechanism order, one extra include). It compares what
// matters for each type — see `matches`.

// RecordStatus is the state of a record in real DNS.
const (
	RecordOK       = "ok"       // published and matching
	RecordMissing  = "missing"  // nothing exists at that host/type
	RecordMismatch = "mismatch" // exists, but isn't what it should be
)

// RecordCheck is the per-record result: what we asked for, what's published, and the verdict.
type RecordCheck struct {
	Host     string   `json:"host"`
	Type     string   `json:"type"`
	Expected string   `json:"expected"`
	Purpose  string   `json:"purpose"`
	Found    []string `json:"found"`
	Status   string   `json:"status"`
	// Required says whether mail stops working without this record. MX/SPF/DKIM are mandatory;
	// DMARC, MTA-STS, autodiscover, and the SRVs are recommendations — the page warns, doesn't block.
	Required bool `json:"required"`
}

// CheckZone resolves every record in the zone and returns the state of all of them. Resolution
// errors aren't fatal: a record that doesn't resolve is "missing", which is the useful information
// for whoever is publishing.
func (v *NetDNSVerifier) CheckZone(ctx context.Context, zone string) []RecordCheck {
	recs := ParseZone(zone)
	out := make([]RecordCheck, 0, len(recs))
	for _, r := range recs {
		c := RecordCheck{
			Host: r.Host, Type: r.Type, Expected: r.Value, Purpose: r.Purpose,
			Required: requiredPurpose(r.Purpose),
			Status:   RecordMissing,
		}
		c.Found = v.lookup(ctx, r)
		switch {
		case len(c.Found) == 0:
			c.Status = RecordMissing
		case matches(r, c.Found):
			c.Status = RecordOK
		default:
			c.Status = RecordMismatch
		}
		out = append(out, c)
	}
	return out
}

// requiredPurpose separates what's needed for mail to work from what's just good practice.
func requiredPurpose(p string) bool {
	switch p {
	case "mx", "spf", "dkim", "dkim_ses", "mailfrom":
		return true
	default:
		return false
	}
}

// lookup returns the published values for the record's host/type.
func (v *NetDNSVerifier) lookup(ctx context.Context, r DNSRecord) []string {
	switch strings.ToUpper(r.Type) {
	case "MX":
		mxs, err := v.resolver.LookupMX(ctx, r.Host)
		if err != nil {
			return nil
		}
		var out []string
		for _, m := range mxs {
			out = append(out, strings.TrimSuffix(m.Host, "."))
		}
		return out
	case "TXT":
		txts, err := v.resolver.LookupTXT(ctx, r.Host)
		if err != nil {
			return nil
		}
		return txts
	case "CNAME":
		cname, err := v.resolver.LookupCNAME(ctx, r.Host)
		if err != nil || cname == "" {
			return nil
		}
		return []string{strings.TrimSuffix(cname, ".")}
	case "SRV":
		// The SRV host already comes in the _service._proto.domain format; LookupSRV with an empty
		// service/proto uses the name as-is.
		_, srvs, err := v.resolver.LookupSRV(ctx, "", "", r.Host)
		if err != nil {
			return nil
		}
		var out []string
		for _, s := range srvs {
			out = append(out, strings.TrimSuffix(s.Target, "."))
		}
		return out
	case "A", "AAAA":
		ips, err := v.resolver.LookupHost(ctx, r.Host)
		if err != nil {
			return nil
		}
		return ips
	}
	return nil
}

// matches decides whether what's published satisfies what's expected. The comparison is by TYPE,
// because literal equality would give false negatives in almost every real case.
func matches(r DNSRecord, found []string) bool {
	exp := strings.ToLower(strings.TrimSpace(r.Value))
	switch strings.ToUpper(r.Type) {
	case "MX":
		// The value carries priority + host ("10 mail.example.com."); only the host matters.
		fields := strings.Fields(exp)
		want := strings.TrimSuffix(fields[len(fields)-1], ".")
		return containsFold(found, want)
	case "TXT":
		switch r.Purpose {
		case "spf":
			// It's enough to be an SPF that includes our include — the customer can have other mechanisms.
			inc := extractInclude(exp)
			for _, f := range found {
				lf := strings.ToLower(f)
				if strings.Contains(lf, "v=spf1") && (inc == "" || strings.Contains(lf, inc)) {
					return true
				}
			}
			return false
		case "dmarc":
			for _, f := range found {
				if strings.Contains(strings.ToLower(f), "v=dmarc1") {
					return true
				}
			}
			return false
		case "dkim", "dkim_ses":
			// Compares the KEY (p=), not the whole record: the customer may reorder the tags.
			key := tagValue(exp, "p=")
			for _, f := range found {
				lf := strings.ToLower(f)
				if key != "" && strings.Contains(strings.ReplaceAll(lf, " ", ""), key) {
					return true
				}
				if key == "" && strings.Contains(lf, "dkim1") {
					return true
				}
			}
			return false
		}
		return containsFold(found, exp)
	case "CNAME", "SRV":
		fields := strings.Fields(exp)
		want := strings.TrimSuffix(fields[len(fields)-1], ".")
		return containsFold(found, want)
	}
	return containsFold(found, exp)
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(v), "."), want) {
			return true
		}
	}
	return false
}

// extractInclude pulls the first include:... out of an SPF.
func extractInclude(spf string) string {
	for _, f := range strings.Fields(spf) {
		if strings.HasPrefix(f, "include:") {
			return f
		}
	}
	return ""
}

// tagValue reads the value of a tag ("p=") from a TXT record with tags separated by ";".
func tagValue(txt, tag string) string {
	for _, part := range strings.Split(txt, ";") {
		p := strings.TrimSpace(part)
		if strings.HasPrefix(p, tag) {
			return strings.ReplaceAll(strings.TrimPrefix(p, tag), " ", "")
		}
	}
	return ""
}

// ZoneFile returns the zone ready to import (BIND format) — it's what Cloudflare, Route 53, and
// most panels accept. No format is invented here: it's what Stalwart already produces, with a
// comment header.
func ZoneFile(domain, zone string) string {
	var b strings.Builder
	// English, like everything else the backend writes (law 4.6). The alternative would be guessing
	// the reader's language from the server side, which is what §4.4 exists to prevent -- and a zone
	// file is read next to a DNS control panel that is in English anyway. If these lines ever need
	// translating, they belong in the frontend, which knows who is asking.
	b.WriteString("; DNS records for " + domain + "\n")
	b.WriteString("; Import these in your DNS provider (Cloudflare: DNS > Records > Import).\n")
	b.WriteString("; TTL is up to your provider.\n\n")
	for _, line := range strings.Split(strings.TrimSpace(zone), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString(strings.TrimSpace(line) + "\n")
	}
	return b.String()
}
