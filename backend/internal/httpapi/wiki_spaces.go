package httpapi

import (
	"errors"
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/wiki"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Wiki: thin HTTP transport over the wiki service (law 1.3). Every handler takes tenant+user from the
// authenticated session (mailUserFrom), never from the request body. The service resolves access in
// SQL and returns sentinel errors that writeWikiErr maps to a stable error_code the frontend
// translates via server.<code> (law 4.4/4.6).

// writeWikiErr maps a wiki service error to an HTTP status + error_code.
func writeWikiErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wiki.ErrNotFound):
		writeErr(w, http.StatusNotFound, "wiki.not_found")
	case errors.Is(err, wiki.ErrForbidden):
		writeErr(w, http.StatusForbidden, "wiki.forbidden")
	case errors.Is(err, wiki.ErrVersionConflict):
		writeErr(w, http.StatusConflict, "wiki.version_conflict")
	case errors.Is(err, wiki.ErrInvalid):
		writeErr(w, http.StatusBadRequest, "wiki.invalid")
	default:
		writeErr(w, http.StatusInternalServerError, "wiki.failed")
	}
}

// wikiURLID parses a UUID path param, writing 400 wiki.invalid on failure.
func wikiURLID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "wiki.invalid")
		return uuid.Nil, false
	}
	return id, true
}

// optUUID parses an optional UUID string ("" / null -> nil). A present-but-bad value is an error.
func optUUID(raw *string) (*uuid.UUID, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(*raw)
	if err != nil {
		return nil, wiki.ErrInvalid
	}
	return &id, nil
}

// ---------- spaces ----------

type wikiSpaceReq struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Visibility  string `json:"visibility"`
	AiIndexed   bool   `json:"ai_indexed"`
}

func (r wikiSpaceReq) input() wiki.SpaceInput {
	return wiki.SpaceInput{
		Key: r.Key, Name: r.Name, Description: r.Description, Icon: r.Icon,
		Visibility: r.Visibility, AiIndexed: r.AiIndexed,
	}
}

func (s *Server) handleListWikiSpaces(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	spaces, err := s.wiki.ListSpaces(r.Context(), mu.TenantID, mu.ID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(spaces))
}

func (s *Server) handleCreateWikiSpace(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req wikiSpaceReq
	if !decode(w, r, &req) {
		return
	}
	sp, err := s.wiki.CreateSpace(r.Context(), mu.TenantID, mu.ID, req.input())
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sp)
}

func (s *Server) handleGetWikiSpace(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	sp, role, err := s.wiki.GetSpace(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"space": sp, "role": role})
}

func (s *Server) handleUpdateWikiSpace(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req wikiSpaceReq
	if !decode(w, r, &req) {
		return
	}
	sp, err := s.wiki.UpdateSpace(r.Context(), mu.TenantID, mu.ID, id, req.input())
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sp)
}

func (s *Server) handleDeleteWikiSpace(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	// Deleting a wiki requires being the space owner OR an organization admin (RBAC role of the session user).
	isOrgAdmin := mu.Role == "owner" || mu.Role == "admin" || mu.Role == "internal_admin"
	if err := s.wiki.DeleteSpace(r.Context(), mu.TenantID, mu.ID, id, isOrgAdmin); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- grants ----------

type wikiGrantReq struct {
	SubjectType string  `json:"subject_type"`
	SubjectID   *string `json:"subject_id"`
	Role        string  `json:"role"`
}

func (s *Server) handleListWikiSpaceGrants(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	grants, err := s.wiki.ListSpaceGrants(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(grants))
}

func (s *Server) handleSetWikiSpaceGrant(w http.ResponseWriter, r *http.Request) {
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
	grant, err := s.wiki.SetSpaceGrant(r.Context(), mu.TenantID, mu.ID, id, req.SubjectType, sid, req.Role)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, grant)
}

func (s *Server) handleDeleteWikiSpaceGrant(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	grantID, ok := wikiURLID(w, r, "grantId")
	if !ok {
		return
	}
	if err := s.wiki.RemoveSpaceGrant(r.Context(), mu.TenantID, mu.ID, id, grantID); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
