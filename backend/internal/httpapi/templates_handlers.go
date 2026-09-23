package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Response templates (canned responses): CRUD per owner. The body is HTML (the same as the
// compose editor). No sharing at this stage — each user manages their own.

type templateReq struct {
	Name     string `json:"name"`
	Subject  string `json:"subject"`
	BodyHTML string `json:"body_html"`
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListTemplates(r.Context(), db.ListTemplatesParams{OwnerUserID: mu.ID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "templates.failed")
		return
	}
	if rows == nil {
		rows = []db.MailTemplate{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req templateReq
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "templates.name_required")
		return
	}
	row, err := s.q.CreateTemplate(r.Context(), db.CreateTemplateParams{
		TenantID: mu.TenantID, OwnerUserID: mu.ID, Name: strings.TrimSpace(req.Name),
		Subject: req.Subject, BodyHtml: req.BodyHTML,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "templates.failed")
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "templates.invalid_id")
		return
	}
	var req templateReq
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "templates.name_required")
		return
	}
	row, err := s.q.UpdateTemplate(r.Context(), db.UpdateTemplateParams{
		ID: id, OwnerUserID: mu.ID, TenantID: mu.TenantID,
		Name: strings.TrimSpace(req.Name), Subject: req.Subject, BodyHtml: req.BodyHTML,
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "templates.not_found")
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "templates.invalid_id")
		return
	}
	n, err := s.q.DeleteTemplate(r.Context(), db.DeleteTemplateParams{ID: id, OwnerUserID: mu.ID, TenantID: mu.TenantID})
	if err != nil || n == 0 {
		writeErr(w, http.StatusNotFound, "templates.not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
