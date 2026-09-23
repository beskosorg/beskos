package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ---------- per-page grants (B3) ----------

func (s *Server) handleListWikiPageGrants(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	grants, err := s.wiki.ListPageGrants(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(grants))
}

func (s *Server) handleSetWikiPageGrant(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req wikiGrantReq
	if !decode(w, r, &req) {
		return
	}
	sid, err := optUUID(req.SubjectID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	grant, err := s.wiki.SetPageGrant(r.Context(), mu.TenantID, mu.ID, id, req.SubjectType, sid, req.Role)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, grant)
}

func (s *Server) handleDeleteWikiPageGrant(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	grantID, ok := wikiURLID(w, r, "grantId")
	if !ok {
		return
	}
	if err := s.wiki.RemovePageGrant(r.Context(), mu.TenantID, mu.ID, id, grantID); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- public read-only access ----------

// handleSetWikiPublic toggles anonymous read-only access for a space (manager only).
func (s *Server) handleSetWikiPublic(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Enabled    bool `json:"enabled"`
		Regenerate bool `json:"regenerate"`
	}
	if !decode(w, r, &req) {
		return
	}
	// Public links are gated by plan: enabling requires the knob; disabling is always allowed.
	if req.Enabled && !s.tenantPlanByID(r.Context(), mu.TenantID).WikiPublicLinks {
		writeErr(w, http.StatusForbidden, "wiki.public_not_in_plan")
		return
	}
	sp, err := s.wiki.SetPublicAccess(r.Context(), mu.TenantID, mu.ID, id, req.Enabled, req.Regenerate)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sp)
}

// handlePublicWikiSpace serves a public space's metadata + published page tree (anonymous, no auth).
func (s *Server) handlePublicWikiSpace(w http.ResponseWriter, r *http.Request) {
	sp, tree, err := s.wiki.PublicSpace(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	// Plan without public links: existing links stop serving (fail-closed on read too).
	if !s.tenantPlanByID(r.Context(), sp.TenantID).WikiPublicLinks {
		writeErr(w, http.StatusNotFound, "wiki.not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"space": map[string]any{"name": sp.Name, "icon": sp.Icon, "description": sp.Description},
		"pages": orEmpty(tree),
	})
}

// handlePublicWikiPage serves one published page of a public space (anonymous). Only body_html leaves.
func (s *Server) handlePublicWikiPage(w http.ResponseWriter, r *http.Request) {
	spc, page, err := s.wiki.PublicPage(r.Context(), chi.URLParam(r, "slug"), chi.URLParam(r, "pageSlug"))
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	if !s.tenantPlanByID(r.Context(), spc.TenantID).WikiPublicLinks {
		writeErr(w, http.StatusNotFound, "wiki.not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"title": page.Title, "slug": page.Slug, "body_html": page.BodyHtml, "updated_at": page.UpdatedAt,
	})
}
