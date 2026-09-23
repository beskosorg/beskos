package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Kanban board permissions (same pattern as wiki grants): person/team/org with roles
// viewer|editor|manager. LEGACY boards (no owner and no grants) stay open to the organization
// (compatibility); whoever has a card assigned on the board is automatically an editor (rule in SQL).

const (
	boardRoleViewer  = 1
	boardRoleEditor  = 3
	boardRoleManager = 4
)

// boardRole returns the user's effective role on the board (0 = no access). Org admins are
// managers of every board in the tenant — that's what gives management access to legacy boards
// (without an owner) and mirrors the wiki-deletion rule.
func (s *Server) boardRole(ctx context.Context, mu db.MailUser, boardID uuid.UUID) int {
	role := 0
	if r, err := s.q.BoardRole(ctx, db.BoardRoleParams{
		UserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, BoardID: boardID, TenantID: mu.TenantID,
	}); err == nil {
		role = int(r)
	}
	if mu.Role == "owner" || mu.Role == "admin" || mu.Role == "internal_admin" {
		// Only elevate if the board exists in the tenant (rank>0 already proves it; otherwise confirm).
		if role > 0 {
			return boardRoleManager
		}
		if _, err := s.q.GetBoard(ctx, db.GetBoardParams{ID: boardID, TenantID: mu.TenantID}); err == nil {
			return boardRoleManager
		}
	}
	return role
}

// requireBoardRole writes 404 (no access — doesn't reveal existence) or 403 (insufficient role).
func (s *Server) requireBoardRole(w http.ResponseWriter, r *http.Request, boardID uuid.UUID, need int) bool {
	mu := mailUserFrom(r.Context())
	role := s.boardRole(r.Context(), mu, boardID)
	if role <= 0 {
		writeErr(w, http.StatusNotFound, "boards.not_found")
		return false
	}
	if role < need {
		writeErr(w, http.StatusForbidden, "boards.forbidden")
		return false
	}
	return true
}

func (s *Server) handleBoardAccess(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"role": s.boardRole(r.Context(), mu, id)})
}

func (s *Server) handleListBoardGrants(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if !s.requireBoardRole(w, r, id, boardRoleManager) {
		return
	}
	grants, err := s.q.ListBoardGrants(r.Context(), db.ListBoardGrantsParams{TenantID: mu.TenantID, BoardID: id})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(grants))
}

func (s *Server) handleSetBoardGrant(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if !s.requireBoardRole(w, r, id, boardRoleManager) {
		return
	}
	var req wikiGrantReq // same shape: subject_type/subject_id/role
	if !decode(w, r, &req) {
		return
	}
	if (req.Role != "viewer" && req.Role != "editor" && req.Role != "manager") ||
		(req.SubjectType != "user" && req.SubjectType != "group" && req.SubjectType != "org") {
		writeErr(w, http.StatusBadRequest, "boards.invalid")
		return
	}
	sid := pgtype.UUID{}
	if req.SubjectType != "org" {
		parsed, perr := uuid.Parse(deref(req.SubjectID))
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "boards.invalid")
			return
		}
		sid = pgtype.UUID{Bytes: parsed, Valid: true}
	}
	// Idempotent upsert: one role per subject.
	_ = s.q.DeleteBoardGrantSubject(r.Context(), db.DeleteBoardGrantSubjectParams{
		TenantID: mu.TenantID, BoardID: id, SubjectType: req.SubjectType, SubjectID: sid,
	})
	g, err := s.q.InsertBoardGrant(r.Context(), db.InsertBoardGrantParams{
		TenantID: mu.TenantID, BoardID: id, SubjectType: req.SubjectType, SubjectID: sid, Role: req.Role,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleDeleteBoardGrant(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if !s.requireBoardRole(w, r, id, boardRoleManager) {
		return
	}
	gid, err := uuid.Parse(chi.URLParam(r, "grantId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if err := s.q.DeleteBoardGrant(r.Context(), db.DeleteBoardGrantParams{ID: gid, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// requireCardEditor resolves the card's board and requires an editor role on it (card mutations).
func (s *Server) requireCardEditor(w http.ResponseWriter, r *http.Request, cardID uuid.UUID) bool {
	mu := mailUserFrom(r.Context())
	boardID, err := s.q.CardBoard(r.Context(), db.CardBoardParams{ID: cardID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "cards.not_found")
		return false
	}
	return s.requireBoardRole(w, r, boardID, boardRoleEditor)
}
