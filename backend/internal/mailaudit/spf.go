package mailaudit

import (
	"context"
	"net"
	"strings"
)

// SPF evaluation, and in particular the lookup count.
//
// WHY THE COUNT MATTERS MORE THAN ANYTHING ELSE HERE: RFC 7208 caps an SPF record at 10 mechanisms
// that require a DNS lookup. Cross that limit and a receiver must return "permerror" -- which most
// of them treat as no SPF at all. The record still looks perfectly fine in a text editor, the domain
// owner sees SPF published, and mail quietly starts failing. It is the most common way a working
// setup breaks by itself: each new tool asks for one more include, and the eleventh one breaks it
// without touching any of the previous ten.
//
// This is exactly the kind of failure the audit exists to surface, because nobody discovers it by
// looking at their DNS -- only by counting.

const spfLookupLimit = 10

// spfResult is what we could learn about a domain's SPF.
type spfResult struct {
	Found   bool
	Record  string
	Lookups int
	// OverLimit is true when the count exceeds what receivers will follow.
	OverLimit bool
	// Policy is the "all" qualifier: "-" fail, "~" softfail, "?" neutral, "+" pass, "" absent.
	Policy string
	// Truncated is set when we stopped resolving to avoid an unbounded walk. The count is then a
	// lower bound, and the report must not present it as exact.
	Truncated bool
}

func lookupSPF(ctx context.Context, r *net.Resolver, domain string) spfResult {
	rec, ok := spfRecord(ctx, r, domain)
	if !ok {
		return spfResult{}
	}
	out := spfResult{Found: true, Record: rec, Policy: allQualifier(rec)}
	seen := map[string]bool{}
	out.Lookups, out.Truncated = countLookups(ctx, r, rec, seen, 0)
	out.OverLimit = out.Lookups > spfLookupLimit
	return out
}

// spfRecord returns the domain's SPF TXT record. A domain publishing two of them is broken by the
// same RFC, but the audit reports what it finds rather than refusing to continue.
func spfRecord(ctx context.Context, r *net.Resolver, domain string) (string, bool) {
	txts, err := r.LookupTXT(ctx, domain)
	if err != nil {
		return "", false
	}
	for _, t := range txts {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), "v=spf1") {
			return strings.TrimSpace(t), true
		}
	}
	return "", false
}

func allQualifier(rec string) string {
	f := strings.Fields(rec)
	for i := len(f) - 1; i >= 0; i-- {
		switch strings.ToLower(f[i]) {
		case "-all":
			return "-"
		case "~all":
			return "~"
		case "?all":
			return "?"
		case "+all", "all":
			return "+"
		}
	}
	return ""
}

// countLookups walks the record counting the mechanisms that cost a DNS query, following include and
// redirect as a receiver would.
//
// The depth cap is a safety net, not a limit of the standard: a redirect loop between two domains
// would otherwise run until the request times out, and a public tool is exactly where somebody will
// eventually point at such a domain.
func countLookups(ctx context.Context, r *net.Resolver, rec string, seen map[string]bool, depth int) (int, bool) {
	if depth > 8 {
		return 0, true
	}
	n, truncated := 0, false
	for _, term := range strings.Fields(rec) {
		t := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(term, "+"), "~"), "-"))
		switch {
		case strings.HasPrefix(t, "include:"), strings.HasPrefix(t, "redirect="):
			n++
			target := t[strings.IndexAny(t, ":=")+1:]
			if target == "" || seen[target] {
				continue // a cycle: counted once, not followed again
			}
			seen[target] = true
			sub, ok := spfRecord(ctx, r, target)
			if !ok {
				continue
			}
			sn, st := countLookups(ctx, r, sub, seen, depth+1)
			n += sn
			truncated = truncated || st
		case t == "a", t == "mx", strings.HasPrefix(t, "a:"), strings.HasPrefix(t, "mx:"),
			strings.HasPrefix(t, "ptr"), strings.HasPrefix(t, "exists:"):
			// Each of these costs the receiver a query too. "ip4:" and "ip6:" do not, which is why a
			// record built from addresses never hits the limit.
			n++
		}
	}
	return n, truncated
}
