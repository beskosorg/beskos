package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Labels: per-tenant catalog. Applying/removing them on emails is via JMAP keywords in the frontend.

func (s *Server) handleListLabels(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	labels, err := s.q.ListLabels(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "labels.failed")
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

type labelReq struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (s *Server) handleCreateLabel(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req labelReq
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "labels.name_required")
		return
	}
	if req.Color == "" {
		req.Color = "#116466"
	}
	label, err := s.q.CreateLabel(r.Context(), db.CreateLabelParams{
		TenantID: mu.TenantID, Name: req.Name, Color: req.Color,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "labels.exists")
		return
	}
	writeJSON(w, http.StatusCreated, label)
}

func (s *Server) handleDeleteLabel(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "labels.invalid_id")
		return
	}
	if err := s.q.DeleteLabel(r.Context(), db.DeleteLabelParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "labels.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
