package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// handleEditCrmNote edits the body of a CRM note (author only).
func (s *Server) handleEditCrmNote(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	nid, err := uuid.Parse(chi.URLParam(r, "noteId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "contacts.note_empty")
		return
	}
	n, err := s.q.EditCrmNote(r.Context(), db.EditCrmNoteParams{ID: nid, AuthorID: mu.ID, Body: strings.TrimSpace(req.Body)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "contacts.not_owner")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// handleTransferContact passes ownership of the contact to a colleague in the same tenant. Only the current owner may.
func (s *Server) handleTransferContact(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	newOwner, err := uuid.Parse(req.UserID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return
	}
	if _, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: newOwner, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "contacts.not_found") // the new owner must be a colleague in the tenant
		return
	}
	c, err := s.q.TransferContactOwner(r.Context(), db.TransferContactOwnerParams{ID: id, OwnerUserID: mu.ID, OwnerUserID_2: newOwner})
	if err != nil {
		writeErr(w, http.StatusNotFound, "contacts.not_owner") // only the current owner may transfer
		return
	}
	writeJSON(w, http.StatusOK, c)
}
