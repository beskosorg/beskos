package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Multiple assignees and card dependencies. The "primary" assignee stays in assignee_id; these
// are additional. Dependency = the card is blocked by another card in the SAME board.

func (s *Server) cardInTenant(r *http.Request, mu db.MailUser) (db.Card, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return db.Card{}, false
	}
	c, err := s.q.GetCard(r.Context(), db.GetCardParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		return db.Card{}, false
	}
	return c, true
}

func (s *Server) handleAddCardAssignee(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	card, ok := s.cardInTenant(r, mu)
	if !ok {
		writeErr(w, http.StatusNotFound, "cards.not_found")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	uid, err := uuid.Parse(req.UserID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cards.invalid")
		return
	}
	if _, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: uid, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "cards.not_found") // must be a colleague in the tenant
		return
	}
	_ = s.q.AddCardAssignee(r.Context(), db.AddCardAssigneeParams{CardID: card.ID, UserID: uid})
	s.publishBoard(r.Context(), mu.TenantID, card.BoardID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRemoveCardAssignee(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	card, ok := s.cardInTenant(r, mu)
	if !ok {
		writeErr(w, http.StatusNotFound, "cards.not_found")
		return
	}
	uid, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cards.invalid")
		return
	}
	_ = s.q.RemoveCardAssignee(r.Context(), db.RemoveCardAssigneeParams{CardID: card.ID, UserID: uid})
	s.publishBoard(r.Context(), mu.TenantID, card.BoardID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAddCardDep(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	card, ok := s.cardInTenant(r, mu)
	if !ok {
		writeErr(w, http.StatusNotFound, "cards.not_found")
		return
	}
	var req struct {
		DependsOnID string `json:"depends_on_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	dep, err := uuid.Parse(req.DependsOnID)
	if err != nil || dep == card.ID {
		writeErr(w, http.StatusBadRequest, "cards.invalid")
		return
	}
	depCard, err := s.q.GetCard(r.Context(), db.GetCardParams{ID: dep, TenantID: mu.TenantID})
	if err != nil || depCard.BoardID != card.BoardID { // may only depend on a card from the same board
		writeErr(w, http.StatusBadRequest, "cards.dep_invalid")
		return
	}
	_ = s.q.AddCardDep(r.Context(), db.AddCardDepParams{CardID: card.ID, DependsOnID: dep})
	s.publishBoard(r.Context(), mu.TenantID, card.BoardID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRemoveCardDep(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	card, ok := s.cardInTenant(r, mu)
	if !ok {
		writeErr(w, http.StatusNotFound, "cards.not_found")
		return
	}
	dep, err := uuid.Parse(chi.URLParam(r, "depId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cards.invalid")
		return
	}
	_ = s.q.RemoveCardDep(r.Context(), db.RemoveCardDepParams{CardID: card.ID, DependsOnID: dep})
	s.publishBoard(r.Context(), mu.TenantID, card.BoardID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
