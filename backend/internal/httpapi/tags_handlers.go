package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Per-tenant tag catalog — shared across events, contacts and (future) tasks.

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	tags, err := s.q.ListTags(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "tags.list_failed")
		return
	}
	if tags == nil {
		tags = []db.Tag{}
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) handleUpsertTag(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "tags.name_required")
		return
	}
	if req.Color == "" {
		req.Color = "#116466"
	}
	tag, err := s.q.UpsertTag(r.Context(), db.UpsertTagParams{TenantID: mu.TenantID, Name: name, Color: req.Color})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "tags.upsert_failed")
		return
	}
	writeJSON(w, http.StatusOK, tag)
}

func (s *Server) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "tags.invalid_id")
		return
	}
	if err := s.q.DeleteTag(r.Context(), db.DeleteTagParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "tags.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
