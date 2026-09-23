package httpapi

import (
	"context"
	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/google/uuid"
	"net/http"
)

// handleUsage returns usage vs. the tenant's plan limits (the usage panel).
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	tid := u.TenantID
	plan := s.tenantPlan(r, tid)
	mailboxes, _ := s.q.CountMailboxesByTenant(r.Context(), tid)
	storageBytes, _ := s.q.SumStorageBytes(r.Context(), tid)
	sentToday, _ := s.q.MailSentToday(r.Context(), tid)
	writeJSON(w, http.StatusOK, map[string]any{
		"plan": plan,
		"usage": map[string]any{
			"mailboxes":       mailboxes,
			"storage_bytes":   storageBytes,
			"mail_sent_today": sentToday,
		},
		"limits": map[string]any{
			"max_mailboxes":     plan.MaxMailboxes,
			"max_storage_bytes": plan.MaxStorageBytes,
			"max_daily_send":    plan.MaxDailySend,
		},
	})
}

func (s *Server) tenantPlan(r *http.Request, tid uuid.UUID) billing.Plan {
	return s.tenantPlanByID(r.Context(), tid)
}

// tenantPlanByID is the same thing without depending on an *http.Request — for use by code that
// runs outside an HTTP request, like the assistant's file generation.
func (s *Server) tenantPlanByID(ctx context.Context, tid uuid.UUID) billing.Plan {
	t, err := s.q.GetTenant(ctx, tid)
	if err != nil {
		return s.planByKey(ctx, billing.DefaultPlanID)
	}
	return s.planByKey(ctx, t.Plan)
}
