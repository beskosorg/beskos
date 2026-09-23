package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Card history/activity. logCard records an event; it never fails the main operation because of the
// log (the error is swallowed on purpose — the history is auxiliary).
func (s *Server) logCard(ctx context.Context, tenantID, cardID, actorID uuid.UUID, kind, detail string) {
	_ = s.q.LogCardActivity(ctx, db.LogCardActivityParams{
		TenantID: tenantID, CardID: cardID, ActorID: pgUUID(actorID), Kind: kind, Detail: detail,
	})
}

func (s *Server) handleListCardActivity(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	c, ok := s.cardOr404(w, r)
	if !ok {
		return
	}
	rows, _ := s.q.ListCardActivity(r.Context(), db.ListCardActivityParams{CardID: c.ID, TenantID: mu.TenantID})
	writeJSON(w, http.StatusOK, orEmpty(rows))
}
