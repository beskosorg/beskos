package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Board automations: when a card enters a trigger column, apply the action (assign / label /
// archive). Evaluated on move and on reorder (drag), only for cards that ENTERED the column.

var automationActions = map[string]bool{"assign": true, "label": true, "archive": true}

// applyAutomations runs the column's rules on a card that has just entered it.
func (s *Server) applyAutomations(ctx context.Context, tenantID, colID, cardID uuid.UUID) {
	rules, err := s.q.AutomationsForColumn(ctx, colID)
	if err != nil {
		return
	}
	for _, a := range rules {
		switch a.Action {
		case "assign":
			if uid, e := uuid.Parse(a.ActionValue); e == nil {
				_ = s.q.SetCardAssigneeOnly(ctx, db.SetCardAssigneeOnlyParams{ID: cardID, TenantID: tenantID, AssigneeID: pgtype.UUID{Bytes: uid, Valid: true}})
			}
		case "label":
			_ = s.q.SetCardLabelOnly(ctx, db.SetCardLabelOnlyParams{ID: cardID, TenantID: tenantID, Label: a.ActionValue})
		case "archive":
			_, _ = s.q.SetCardArchived(ctx, db.SetCardArchivedParams{ID: cardID, TenantID: tenantID, Archived: true})
		}
	}
}

func (s *Server) handleListAutomations(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	boardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if _, err := s.q.GetBoard(r.Context(), db.GetBoardParams{ID: boardID, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "boards.not_found")
		return
	}
	rules, err := s.q.ListAutomations(r.Context(), boardID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	if rules == nil {
		rules = []db.BoardAutomation{}
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) handleCreateAutomation(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	boardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if _, err := s.q.GetBoard(r.Context(), db.GetBoardParams{ID: boardID, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "boards.not_found")
		return
	}
	var req struct {
		TriggerColumnID string `json:"trigger_column_id"`
		Action          string `json:"action"`
		ActionValue     string `json:"action_value"`
	}
	if !decode(w, r, &req) {
		return
	}
	colID, err := uuid.Parse(req.TriggerColumnID)
	if err != nil || !automationActions[req.Action] {
		writeErr(w, http.StatusBadRequest, "boards.automation_invalid")
		return
	}
	// The trigger column must belong to this board/tenant.
	if col, cerr := s.q.GetColumn(r.Context(), db.GetColumnParams{ID: colID, TenantID: mu.TenantID}); cerr != nil || col.BoardID != boardID {
		writeErr(w, http.StatusBadRequest, "boards.automation_invalid")
		return
	}
	a, err := s.q.CreateAutomation(r.Context(), db.CreateAutomationParams{
		BoardID: boardID, TenantID: mu.TenantID, TriggerColumnID: colID, Action: req.Action, ActionValue: req.ActionValue,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleDeleteAutomation(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if err := s.q.DeleteAutomation(r.Context(), db.DeleteAutomationParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
