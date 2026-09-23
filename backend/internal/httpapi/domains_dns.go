package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/provisioning"
)

// On-demand DNS verification + zone export (docs/DNS-PAGE.md).
//
// This is INFORMATIONAL: whoever promotes the domain from `dns_pending` to `dns_verified` is still
// the worker (stepVerifyDNS). The page says what's missing; it doesn't change the domain's state.
// This way a lookup hitting a poisoned cache can't "activate" a domain by mistake.

// dnsCheckTTL: DNS doesn't change from second to second, and each check is N network lookups.
// A short window is enough to keep the "check now" button from turning into a load lever.
const dnsCheckTTL = 30 * time.Second

type dnsCheckEntry struct {
	at      time.Time
	records []provisioning.RecordCheck
}

type dnsCheckCache struct {
	mu sync.Mutex
	m  map[uuid.UUID]dnsCheckEntry
}

func (c *dnsCheckCache) get(id uuid.UUID) ([]provisioning.RecordCheck, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[id]
	if !ok || time.Since(e.at) > dnsCheckTTL {
		return nil, time.Time{}, false
	}
	return e.records, e.at, true
}

func (c *dnsCheckCache) put(id uuid.UUID, recs []provisioning.RecordCheck) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[uuid.UUID]dnsCheckEntry{}
	}
	now := time.Now()
	c.m[id] = dnsCheckEntry{at: now, records: recs}
	return now
}

var dnsChecks dnsCheckCache

// handleCheckDomainDNS checks record by record and returns the status of each one.
func (s *Server) handleCheckDomainDNS(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownedDomain(w, r) // tenant ownership first: this resolves DNS, it can't become someone else's scanner
	if !ok {
		return
	}
	zone := s.zoneOf(d)
	if strings.TrimSpace(zone) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"records": []any{}, "checked_at": nil, "summary": dnsSummary(nil)})
		return
	}
	recs, at, cached := dnsChecks.get(d.ID)
	if !cached {
		// Time cap: a misconfigured domain can have several records hitting the resolver's timeout,
		// and the page can't be left hanging because of it.
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		recs = provisioning.NewNetDNSVerifier().CheckZone(ctx, zone)
		at = dnsChecks.put(d.ID, recs)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"records":    orEmpty(recs),
		"checked_at": at.UTC().Format(time.RFC3339),
		"cached":     cached,
		"summary":    dnsSummary(recs),
	})
}

// dnsSummary counts what matters to the page's header: what's missing and whether what's missing is required.
func dnsSummary(recs []provisioning.RecordCheck) map[string]any {
	var ok, missing, mismatch, requiredPending int
	for _, r := range recs {
		switch r.Status {
		case provisioning.RecordOK:
			ok++
		case provisioning.RecordMissing:
			missing++
		case provisioning.RecordMismatch:
			mismatch++
		}
		if r.Required && r.Status != provisioning.RecordOK {
			requiredPending++
		}
	}
	return map[string]any{
		"total": len(recs), "ok": ok, "missing": missing, "mismatch": mismatch,
		"required_pending": requiredPending,
	}
}

// handleExportDomainDNS returns the zone for importing into the provider. `zone` (BIND) is what
// Cloudflare and Route 53 import; `csv` is for whoever prefers to edit it by hand.
func (s *Server) handleExportDomainDNS(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownedDomain(w, r)
	if !ok {
		return
	}
	zone := s.zoneOf(d)
	switch r.URL.Query().Get("format") {
	case "csv":
		var b strings.Builder
		b.WriteString("type,host,value\n")
		for _, rec := range provisioning.ParseZone(zone) {
			fmt.Fprintf(&b, "%s,%s,%q\n", rec.Type, rec.Host, rec.Value)
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", d.Domain+"-dns.csv"))
		_, _ = w.Write([]byte(b.String()))
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", d.Domain+".zone"))
		_, _ = w.Write([]byte(provisioning.ZoneFile(d.Domain, zone)))
	}
}
