package httpapi

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// Plan catalog with the database as the source of truth: the manager writes to the `plans`
// table; the product READS from here, with a short cache (avoids a query per request) and a
// fallback to the internal/billing code catalog when the table is empty or the read fails
// (fail-safe: the product is never left without defined limits).

const planCacheTTL = time.Minute

type planCatalog struct {
	mu      sync.Mutex
	plans   []billing.Plan
	fetched time.Time
}

// catalogPlans returns all plans (active and inactive) already converted to billing.Plan.
func (s *Server) catalogPlans(ctx context.Context) []billing.Plan {
	s.planCat.mu.Lock()
	defer s.planCat.mu.Unlock()
	if time.Since(s.planCat.fetched) < planCacheTTL && s.planCat.plans != nil {
		return s.planCat.plans
	}
	rows, err := s.q.ListPlans(ctx)
	if err != nil || len(rows) == 0 {
		// Fallback: code-based catalog. Doesn't cache the fallback for long on purpose — as soon
		// as the table responds, the DB values take over.
		return billing.Plans()
	}
	out := make([]billing.Plan, 0, len(rows))
	for _, r := range rows {
		out = append(out, planFromRow(r))
	}
	s.planCat.plans = out
	s.planCat.fetched = time.Now()
	return out
}

// activePlans filters the catalog for display (checkout/billing only shows active plans).
func (s *Server) activePlans(ctx context.Context) []billing.Plan {
	all := s.catalogPlans(ctx)
	out := make([]billing.Plan, 0, len(all))
	for _, p := range all {
		if p.IsActive {
			out = append(out, p)
		}
	}
	return out
}

// catalogPlanByKey resolves a plan by key WITHOUT a fallback (ok=false if it doesn't exist) — for
// checkout/webhook validation, where silently falling back to the trial would be wrong.
func (s *Server) catalogPlanByKey(ctx context.Context, key string) (billing.Plan, bool) {
	for _, p := range s.catalogPlans(ctx) {
		if p.ID == key {
			return p, true
		}
	}
	return billing.Plan{}, false
}

// planByKey resolves a plan by key, with the same fail-safe as billing.PlanOrDefault.
func (s *Server) planByKey(ctx context.Context, key string) billing.Plan {
	for _, p := range s.catalogPlans(ctx) {
		if p.ID == key {
			return p
		}
	}
	return billing.PlanOrDefault(key)
}

// planFromRow converts the DB row into the type the product consumes.
func planFromRow(r db.Plan) billing.Plan {
	return billing.Plan{
		ID:                  r.Key,
		Name:                r.Name,
		MailboxRange:        billing.RangeLabel(int(r.MinUsers), int(r.MaxUsers)),
		MaxMailboxes:        int(r.MaxUsers),
		MaxStorageBytes:     int64(r.MaxStorageGb) * billing.GB,
		MaxDailySend:        int(r.MaxDailySend),
		MaxMeetParticipants: int(r.MeetMaxParticipants),
		PriceCentsPerUser:   int(r.PriceCentsPerUser),
		MinUsers:            int(r.MinUsers),
		AppsEnabled:         r.AppsEnabled,
		AITokensPerWindow:   int(r.AiTokensPerWindow),
		MeetMaxMinutes:      int(r.MeetMaxMinutes),
		Recording:           r.Recording,
		RecordingDays:       int(r.RecordingRetentionDays),
		OnlyOfficeEdit:      r.OnlyofficeEdit,
		WikiPublicLinks:     r.WikiPublicLinks,
		IsActive:            r.Active,
		StripePriceID:       r.StripePriceID,
		Prices:              parsePlanPrices(r.Prices),
	}
}

// parsePlanPrices reads the JSONB {"USD":1900,...} (empty/invalid = no per-currency prices).
func parsePlanPrices(raw []byte) map[string]int {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]int
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}

// disabledAppsEffective merges the apps the tenant admin turned off with the ones EXCLUDED by the
// plan (apps_enabled enforcement). An empty apps_enabled list on the plan means all apps are on.
func (s *Server) disabledAppsEffective(ctx context.Context, tenantID uuid.UUID) []string {
	disabled, _ := s.q.GetTenantDisabledApps(ctx, tenantID)
	t, err := s.q.GetTenant(ctx, tenantID)
	if err != nil {
		return disabled
	}
	plan := s.planByKey(ctx, t.Plan)
	if len(plan.AppsEnabled) == 0 {
		return disabled
	}
	allowed := map[string]bool{}
	for _, a := range plan.AppsEnabled {
		allowed[a] = true
	}
	seen := map[string]bool{}
	for _, a := range disabled {
		seen[a] = true
	}
	for _, a := range toggleableApps {
		if !allowed[a] && !seen[a] {
			disabled = append(disabled, a)
			seen[a] = true
		}
	}
	return disabled
}
