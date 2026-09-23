package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Sharing a note: with a person, with a team, or with the whole organisation -- the same three
// subjects the wiki uses, so that the sharing dialog people already know keeps meaning the same
// thing here.
//
// ONLY THE OWNER SHARES. An editor may rewrite the note; passing it on to others is a different
// decision, and letting write access grow into the power to widen the audience is how a note ends
// up somewhere its author never agreed to.

func (s *Server) handleListNoteGrants(w http.ResponseWriter, r *http.Request) {
	id, ok := s.requireNote(w, r, noteRoleViewer)
	if !ok {
		return
	}
	mu := mailUserFrom(r.Context())
	grants, err := s.q.ListNoteGrants(r.Context(), db.ListNoteGrantsParams{NoteID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "notes.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": orEmpty(grants)})
}

func (s *Server) handleSetNoteGrant(w http.ResponseWriter, r *http.Request) {
	id, ok := s.requireNote(w, r, noteRoleOwner)
	if !ok {
		return
	}
	mu := mailUserFrom(r.Context())
	var req struct {
		SubjectType string `json:"subject_type"` // user | group | org
		SubjectID   string `json:"subject_id"`
		Role        string `json:"role"` // viewer | editor
	}
	if !decode(w, r, &req) {
		return
	}
	if req.SubjectType != "user" && req.SubjectType != "group" && req.SubjectType != "org" {
		writeErr(w, http.StatusBadRequest, "notes.invalid_subject")
		return
	}
	role := req.Role
	if role != "editor" {
		role = "viewer" // anything unrecognised reads, it does not write
	}
	// org has no subject; user and group must have one. The table enforces this too, but a 400 says
	// what is wrong and a constraint violation would surface as an opaque 500.
	var subject pgtype.UUID
	if req.SubjectType == "org" {
		if req.SubjectID != "" {
			writeErr(w, http.StatusBadRequest, "notes.invalid_subject")
			return
		}
	} else {
		sid, err := uuid.Parse(req.SubjectID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "notes.invalid_subject")
			return
		}
		subject = pgtype.UUID{Bytes: sid, Valid: true}
	}

	g, err := s.q.UpsertNoteGrant(r.Context(), db.UpsertNoteGrantParams{
		TenantID: mu.TenantID, NoteID: id, SubjectType: req.SubjectType, SubjectID: subject, Role: role,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "notes.share_failed")
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleDeleteNoteGrant(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNote(w, r, noteRoleOwner); !ok {
		return
	}
	gid, err := uuid.Parse(chi.URLParam(r, "grantId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "notes.invalid_id")
		return
	}
	mu := mailUserFrom(r.Context())
	if err := s.q.DeleteNoteGrant(r.Context(), db.DeleteNoteGrantParams{ID: gid, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "notes.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
