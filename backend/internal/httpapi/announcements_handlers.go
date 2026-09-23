package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/rbac"
)

// Organization announcements board. Admins/owner publish (RBAC org.settings) and can unpublish;
// all members read the published ones. Search and pagination happen server-side.

const announcePageSize = 10

// isOrgAdmin reports whether the user can manage announcements (see drafts, publish/unpublish).
func (s *Server) isOrgAdmin(mu db.MailUser) bool {
	return rbac.Has(rbac.Role(mu.Role), rbac.PermAnnouncements)
}

func (s *Server) handleListAnnouncements(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	q := r.URL.Query().Get("q")
	page := 1
	if p, _ := strconv.Atoi(r.URL.Query().Get("page")); p > 1 {
		page = p
	}
	admin := s.isOrgAdmin(mu) // admin also sees the unpublished ones
	items, err := s.q.ListAnnouncements(r.Context(), db.ListAnnouncementsParams{
		TenantID: mu.TenantID, Column2: q, Limit: announcePageSize, Offset: int32((page - 1) * announcePageSize), Column5: admin,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "announce.load_failed")
		return
	}
	total, _ := s.q.CountAnnouncements(r.Context(), db.CountAnnouncementsParams{TenantID: mu.TenantID, Column2: q, Column3: admin})
	out := make([]map[string]any, 0, len(items))
	for _, a := range items {
		out = append(out, map[string]any{
			"id": a.ID, "title": a.Title, "body": a.Body, "author": a.AuthorName,
			"published": a.Published, "created_at": iso(a.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total, "page": page, "can_manage": admin})
}

type announceReq struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	Published *bool  `json:"published"`
}

func (s *Server) handleCreateAnnouncement(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req announceReq
	if !decode(w, r, &req) {
		return
	}
	if req.Title == "" {
		writeErr(w, http.StatusBadRequest, "announce.title_required")
		return
	}
	published := req.Published == nil || *req.Published
	a, err := s.q.CreateAnnouncement(r.Context(), db.CreateAnnouncementParams{
		TenantID: mu.TenantID, AuthorID: mu.ID, AuthorName: mu.Name, Title: req.Title, Body: req.Body, Published: published,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "announce.save_failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": a.ID})
}

func (s *Server) handleSetAnnouncementPublished(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "announce.invalid_id")
		return
	}
	var req struct {
		Published bool `json:"published"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.q.SetAnnouncementPublished(r.Context(), db.SetAnnouncementPublishedParams{ID: id, TenantID: mu.TenantID, Published: req.Published}); err != nil {
		writeErr(w, http.StatusInternalServerError, "announce.save_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteAnnouncement(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "announce.invalid_id")
		return
	}
	if err := s.q.DeleteAnnouncement(r.Context(), db.DeleteAnnouncementParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "announce.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
