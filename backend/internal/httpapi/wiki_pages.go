package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/beskosorg/beskos/backend/internal/wiki"
	"github.com/go-chi/chi/v5"
)

// ---------- pages ----------

type wikiPageReq struct {
	ParentID        *string         `json:"parent_id"`
	Title           string          `json:"title"`
	BodyJSON        json.RawMessage `json:"body_json"`
	Status          string          `json:"status"`
	ExpectedVersion int32           `json:"expected_version"` // required on update (optimistic lock)
}

func (s *Server) handleWikiTree(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	tree, err := s.wiki.Tree(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(tree))
}

func (s *Server) handleCreateWikiPage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	spaceID, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req wikiPageReq
	if !decode(w, r, &req) {
		return
	}
	parent, err := optUUID(req.ParentID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	page, err := s.wiki.CreatePage(r.Context(), mu.TenantID, mu.ID, spaceID, wiki.PageInput{
		ParentID: parent, Title: req.Title, BodyJSON: req.BodyJSON, Status: req.Status,
	})
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, page)
}

func (s *Server) handleGetWikiPage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	page, role, err := s.wiki.GetPage(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": page, "role": role})
}

func (s *Server) handleUpdateWikiPage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req wikiPageReq
	if !decode(w, r, &req) {
		return
	}
	page, err := s.wiki.UpdatePage(r.Context(), mu.TenantID, mu.ID, id, wiki.PageInput{
		Title: req.Title, BodyJSON: req.BodyJSON, Status: req.Status,
	}, req.ExpectedVersion)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleDeleteWikiPage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	if err := s.wiki.DeletePage(r.Context(), mu.TenantID, mu.ID, id); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type wikiMoveReq struct {
	ParentID *string `json:"parent_id"`
	Position int32   `json:"position"`
}

func (s *Server) handleMoveWikiPage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req wikiMoveReq
	if !decode(w, r, &req) {
		return
	}
	parent, err := optUUID(req.ParentID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	if err := s.wiki.MovePage(r.Context(), mu.TenantID, mu.ID, id, parent, req.Position); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "moved"})
}

func (s *Server) handleListWikiPageVersions(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	versions, err := s.wiki.ListVersions(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(versions))
}

// wikiVersionNum parses the {version} path param as a positive int.
func wikiVersionNum(w http.ResponseWriter, r *http.Request) (int32, bool) {
	n, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil || n < 1 {
		writeErr(w, http.StatusBadRequest, "wiki.invalid")
		return 0, false
	}
	return int32(n), true
}

func (s *Server) handleGetWikiPageVersion(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	ver, ok := wikiVersionNum(w, r)
	if !ok {
		return
	}
	v, err := s.wiki.GetVersion(r.Context(), mu.TenantID, mu.ID, id, ver)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// ---------- templates (B7) ----------

func (s *Server) handleSetWikiPageTemplate(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		IsTemplate bool `json:"is_template"`
	}
	if !decode(w, r, &req) {
		return
	}
	page, err := s.wiki.SetTemplate(r.Context(), mu.TenantID, mu.ID, id, req.IsTemplate)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleListWikiTemplates(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	rows, err := s.wiki.ListTemplates(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

// ---------- app home (B5) ----------

func (s *Server) handleGetWikiHome(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	h, err := s.wiki.GetHome(r.Context(), mu.TenantID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// handleSetWikiHome grava a home (a rota exige PermWikiManage via requirePerm).
func (s *Server) handleSetWikiHome(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		BodyJSON json.RawMessage `json:"body_json"`
	}
	if !decode(w, r, &req) {
		return
	}
	h, err := s.wiki.SetHome(r.Context(), mu.TenantID, mu.ID, req.BodyJSON)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) handleRestoreWikiPageVersion(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	ver, ok := wikiVersionNum(w, r)
	if !ok {
		return
	}
	var req struct {
		ExpectedVersion int32 `json:"expected_version"`
	}
	if !decode(w, r, &req) {
		return
	}
	page, err := s.wiki.RestoreVersion(r.Context(), mu.TenantID, mu.ID, id, ver, req.ExpectedVersion)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
