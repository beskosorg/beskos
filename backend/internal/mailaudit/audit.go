package mailaudit

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

// Audits the mail configuration of any domain, from public DNS alone.
//
// WHAT THIS IS NOT: a deliverability percentage. Whether mail reaches an inbox depends on IP
// reputation, sending history, content and how each receiver weighs its own signals -- none of which
// is visible in DNS. A number derived from SPF and DMARC would be invented, and a made-up score is
// the fastest way to lose the reader who knows enough to buy. So the audit COUNTS CHECKS: "7 of 12
// passed" is true, verifiable, and worse to read than "58%", which is the point.
//
// EVERY UNKNOWN IS ITS OWN ANSWER. A DKIM key we did not find is "not found among the common
// selectors", never "you have no DKIM" -- selectors are arbitrary and probing is inherently partial.
// A blacklist we could not query is "could not check", never "clean". Reporting a guess as a fact,
// in a tool whose whole purpose is to be trusted about mail, would be self-defeating.

type Status string

const (
	Pass    Status = "pass"    // configured, and configured well
	Warn    Status = "warn"    // present but weak, or good practice missing
	Fail    Status = "fail"    // absent or broken
	Unknown Status = "unknown" // we could not find out; NOT the same as pass
)

// Check is one verified item.
type Check struct {
	Key    string `json:"key"`
	Status Status `json:"status"`
	// Detail is the raw evidence -- the record found, the count, the host. The report shows it so the
	// reader can verify us instead of believing us.
	Detail string `json:"detail,omitempty"`
}

// Report is the whole audit. Passed/Total is the honest score; there is no percentage.
type Report struct {
	Domain string  `json:"domain"`
	Checks []Check `json:"checks"`
	Passed int     `json:"passed"`
	Total  int     `json:"total"`
	// SharedHosting is the qualifying signal, not a verdict about quality: mail served by a shared
	// hosting provider means the sending reputation is shared with strangers.
	SharedHosting bool   `json:"shared_hosting"`
	MailProvider  string `json:"mail_provider,omitempty"`
}

// Common DKIM selectors. Probing cannot be exhaustive -- a selector is any label the sender chose --
// so a miss is reported as a miss, never as an absence.
var dkimSelectors = []string{
	"default", "google", "selector1", "selector2", "k1", "k2", "s1", "s2",
	"mail", "dkim", "smtp", "zoho", "mandrill", "sendgrid", "mailjet", "protonmail",
	"beskos", "everlytickey1", "pic", "hs1-*",
}

// Hosts that mean the domain's mail rides on shared infrastructure. Matching is by SUFFIX on the MX
// host, so "mail.locaweb.com.br" and "smtp.locaweb.com.br" both count.
var sharedMXSuffixes = map[string]string{
	"hostinger.com": "Hostinger", "hostgator.com": "HostGator", "hostgator.com.br": "HostGator",
	"locaweb.com.br": "Locaweb", "uolhost.com.br": "UOL Host", "kinghost.net": "KingHost",
	"secureserver.net": "GoDaddy", "godaddy.com": "GoDaddy",
	"registrar-servers.com": "Namecheap", "privateemail.com": "Namecheap",
	"ionos.com": "IONOS", "1and1.com": "IONOS", "ovh.net": "OVH", "ovh.com": "OVH",
	"siteground.com": "SiteGround", "bluehost.com": "Bluehost", "dreamhost.com": "DreamHost",
	"cpanel.net": "cPanel", "webapps.net": "shared hosting", "securemail.pro": "shared hosting",
}

// DNSBLs queried for the MX addresses. Each returns an A record when the address is listed.
var blocklists = []string{"zen.spamhaus.org", "b.barracudacentral.org", "bl.spamcop.net"}

// resolver is what every lookup goes through. It defaults to the system's, and UseResolver points
// it somewhere else.
var resolver = &net.Resolver{}

// UseResolver sends the audit's lookups to a specific DNS server.
//
// This exists for one reason: the anti-spam lists REFUSE queries that arrive through a shared
// resolver. Spamhaus answers 127.255.255.254, which means "go away" and not "not listed" -- so
// through the datacenter's resolver, or through 1.1.1.1, the blocklist check could never say
// anything at all. Measured from the production host: forwarded, a refusal; recursing from the root
// on our own address, real listing codes. Passing an empty address keeps the system resolver, which
// is what a developer's machine wants.
func UseResolver(addr string) {
	if addr == "" {
		return
	}
	resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
		},
	}
}

// Audit runs every check. It never returns an error for a domain that simply has nothing published:
// that IS the finding.
func Audit(ctx context.Context, domain string) Report {
	domain = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(domain, ".")))
	r := resolver
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	rep := Report{Domain: domain}
	mx, _ := r.LookupMX(ctx, domain)

	// --- mail at all ---
	if len(mx) == 0 {
		rep.add(Check{Key: "mx", Status: Fail, Detail: "no MX record"})
	} else {
		host := strings.TrimSuffix(mx[0].Host, ".")
		rep.add(Check{Key: "mx", Status: Pass, Detail: host})
		if name, shared := sharedProvider(host); shared {
			rep.SharedHosting, rep.MailProvider = true, name
		}
	}

	// The remaining groups do not depend on one another, so they run at once. In series this was
	// seven seconds of DNS round trips; nobody waits seven seconds for a free report, and a tool
	// nobody waits for generates no leads.
	var (
		wg                                      sync.WaitGroup
		spfChecks, dkim, dmarc, sts, tls, block []Check
	)
	wg.Add(5)
	go func() {
		defer wg.Done()
		spfChecks = spfSection(ctx, r, domain)
	}()
	go func() {
		defer wg.Done()
		dkim = []Check{dkimCheck(ctx, r, domain)}
	}()
	go func() {
		defer wg.Done()
		dmarc = dmarcChecks(ctx, r, domain)
	}()
	go func() {
		defer wg.Done()
		sts = []Check{mtaStsCheck(ctx, r, domain)}
		tls = []Check{txtCheck(ctx, r, "_smtp._tls."+domain, "tls_rpt", "v=TLSRPTv1")}
	}()
	go func() {
		defer wg.Done()
		block = []Check{blocklistCheck(ctx, r, mx)}
	}()
	wg.Wait()
	// Appended in a fixed order: the report reads the same every time, whatever finished first.
	rep.addAll(spfChecks)
	rep.addAll(dkim)
	rep.addAll(dmarc)
	rep.addAll(sts)
	rep.addAll(tls)
	rep.addAll(block)

	for _, c := range rep.Checks {
		if c.Status == Pass {
			rep.Passed++
		}
	}
	rep.Total = len(rep.Checks)
	return rep
}

func (r *Report) add(c Check)       { r.Checks = append(r.Checks, c) }
func (r *Report) addAll(cs []Check) { r.Checks = append(r.Checks, cs...) }

func spfLookupCheck(spf spfResult) Check {
	switch {
	case spf.Truncated:
		return Check{Key: "spf_lookups", Status: Unknown, Detail: "too many redirects to count reliably"}
	case spf.OverLimit:
		return Check{Key: "spf_lookups", Status: Fail, Detail: itoa(spf.Lookups) + " of 10 allowed"}
	case spf.Lookups >= 8:
		// Not broken, but one more tool breaks it. Worth saying before it happens.
		return Check{Key: "spf_lookups", Status: Warn, Detail: itoa(spf.Lookups) + " of 10 allowed"}
	default:
		return Check{Key: "spf_lookups", Status: Pass, Detail: itoa(spf.Lookups) + " of 10 allowed"}
	}
}

// dkimCheck probes the common selectors CONCURRENTLY. In series it was eighteen round trips, and it
// alone accounted for most of a seven-second wait -- which for a public tool is the difference
// between a report somebody reads and a tab somebody closes.
func dkimCheck(ctx context.Context, r *net.Resolver, domain string) Check {
	type found struct{ sel string }
	hits := make(chan found, len(dkimSelectors))
	var wg sync.WaitGroup
	for _, s := range dkimSelectors {
		if strings.Contains(s, "*") {
			continue
		}
		wg.Add(1)
		go func(sel string) {
			defer wg.Done()
			txts, err := r.LookupTXT(ctx, sel+"._domainkey."+domain)
			if err != nil {
				return
			}
			for _, t := range txts {
				if strings.Contains(strings.ToLower(t), "v=dkim1") || strings.Contains(t, "p=") {
					hits <- found{sel}
					return
				}
			}
		}(s)
	}
	wg.Wait()
	close(hits)
	if h, ok := <-hits; ok {
		return Check{Key: "dkim", Status: Pass, Detail: "selector " + h.sel}
	}
	// Unknown and not Fail: the sender may use a selector we did not guess. Saying "you have no
	// DKIM" to somebody who does would end the conversation with us being wrong.
	return Check{Key: "dkim", Status: Unknown, Detail: "not found among common selectors"}
}

func dmarcChecks(ctx context.Context, r *net.Resolver, domain string) []Check {
	txts, err := r.LookupTXT(ctx, "_dmarc."+domain)
	rec := ""
	if err == nil {
		for _, t := range txts {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), "v=dmarc1") {
				rec = strings.TrimSpace(t)
			}
		}
	}
	if rec == "" {
		return []Check{
			{Key: "dmarc", Status: Fail, Detail: "no DMARC record"},
			{Key: "dmarc_policy", Status: Fail, Detail: "nothing to enforce"},
			{Key: "dmarc_reports", Status: Fail, Detail: "not collecting reports"},
		}
	}
	low := strings.ToLower(rec)
	out := []Check{{Key: "dmarc", Status: Pass, Detail: rec}}
	switch {
	case strings.Contains(low, "p=reject"):
		out = append(out, Check{Key: "dmarc_policy", Status: Pass, Detail: "p=reject"})
	case strings.Contains(low, "p=quarantine"):
		out = append(out, Check{Key: "dmarc_policy", Status: Pass, Detail: "p=quarantine"})
	default:
		// p=none publishes a policy that asks receivers to do nothing. It is a monitoring stage, not
		// protection, and a domain left there for years is protected by exactly nothing.
		out = append(out, Check{Key: "dmarc_policy", Status: Warn, Detail: "p=none"})
	}
	if strings.Contains(low, "rua=") {
		out = append(out, Check{Key: "dmarc_reports", Status: Pass, Detail: "rua set"})
	} else {
		out = append(out, Check{Key: "dmarc_reports", Status: Warn, Detail: "no rua: nobody is reading the results"})
	}
	return out
}

func txtCheck(ctx context.Context, r *net.Resolver, host, key, prefix string) Check {
	txts, err := r.LookupTXT(ctx, host)
	if err == nil {
		for _, t := range txts {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), strings.ToLower(prefix)) {
				return Check{Key: key, Status: Pass, Detail: strings.TrimSpace(t)}
			}
		}
	}
	// Warn, not Fail: mail works without these. They are the difference between adequate and careful.
	return Check{Key: key, Status: Warn, Detail: "not published"}
}

func blocklistCheck(ctx context.Context, r *net.Resolver, mx []*net.MX) Check {
	if len(mx) == 0 {
		return Check{Key: "blocklist", Status: Unknown}
	}
	ips, err := r.LookupIP(ctx, "ip4", strings.TrimSuffix(mx[0].Host, "."))
	if err != nil || len(ips) == 0 {
		return Check{Key: "blocklist", Status: Unknown, Detail: "could not resolve the mail host"}
	}
	rev := reverse(ips[0])
	checked := false
	for _, bl := range blocklists {
		addrs, err := r.LookupHost(ctx, rev+"."+bl)
		if err != nil {
			// NXDOMAIN IS THE ANSWER "not listed" -- a DNSBL says clean by having no record. Treating
			// it as a failed query is why this check reported "could not check" for every clean
			// domain, which is every domain that matters: the ones that ARE listed answer, so only
			// the innocent looked unverifiable.
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				checked = true
			}
			continue
		}
		refused := false
		for _, a := range addrs {
			// 127.255.255.x is not a listing -- it is the list telling us to go away, which Spamhaus
			// does for every query from a public resolver. Counting that answer as "checked" would
			// end with the report saying "not listed" about a list that never looked. That is the
			// one error this tool cannot afford: it is the direction that costs the reader money.
			if strings.HasPrefix(a, "127.255.255.") {
				refused = true
				continue
			}
			if strings.HasPrefix(a, "127.") {
				return Check{Key: "blocklist", Status: Fail, Detail: "listed on " + bl}
			}
		}
		if !refused {
			checked = true
		}
	}
	if !checked {
		// Never "clean" when nothing answered. A tool that reports an unanswered query as a pass is
		// lying in the direction that costs its reader money.
		return Check{Key: "blocklist", Status: Unknown, Detail: "the lists did not answer this query"}
	}
	return Check{Key: "blocklist", Status: Pass, Detail: "not listed on the lists checked"}
}

func sharedProvider(mxHost string) (string, bool) {
	h := strings.ToLower(strings.TrimSuffix(mxHost, "."))
	for suffix, name := range sharedMXSuffixes {
		if h == suffix || strings.HasSuffix(h, "."+suffix) {
			return name, true
		}
	}
	return "", false
}

func reverse(ip net.IP) string {
	v4 := ip.To4()
	if v4 == nil {
		return ""
	}
	return itoa(int(v4[3])) + "." + itoa(int(v4[2])) + "." + itoa(int(v4[1])) + "." + itoa(int(v4[0]))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// spfSection is the three SPF checks, kept together because they share one lookup.
func spfSection(ctx context.Context, r *net.Resolver, domain string) []Check {
	spf := lookupSPF(ctx, r, domain)
	if !spf.Found {
		return []Check{
			{Key: "spf", Status: Fail, Detail: "no SPF record"},
			{Key: "spf_policy", Status: Fail, Detail: "no policy without SPF"},
			{Key: "spf_lookups", Status: Unknown},
		}
	}
	out := []Check{{Key: "spf", Status: Pass, Detail: spf.Record}}
	switch spf.Policy {
	case "-":
		out = append(out, Check{Key: "spf_policy", Status: Pass, Detail: "-all"})
	case "~":
		// Softfail asks the receiver to accept and mark it. It is where everybody starts and where
		// most stay, which is why a forger's mail still arrives.
		out = append(out, Check{Key: "spf_policy", Status: Warn, Detail: "~all"})
	default:
		out = append(out, Check{Key: "spf_policy", Status: Fail, Detail: spf.Policy + "all"})
	}
	return append(out, spfLookupCheck(spf))
}
