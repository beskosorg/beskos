package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/wiki"
)

// Notes: personal notes and meeting notes (migration 00133).
//
// PRIVATE BY DEFAULT, and that is the absence of a grant rather than a flag: a note nobody was given
// is the owner's alone. There is no boolean to get wrong and no default to forget.
//
// The role ranks mirror the boards and the wiki so that all three read alike: 0 no access, 1 read,
// 3 write, 4 owner. Anything below 1 is answered with 404 and never 403 -- telling someone "you may
// not read this note" confirms the note exists, which is itself the leak.

const (
	noteRoleViewer = 1
	noteRoleEditor = 3
	noteRoleOwner  = 4
)

// noteRole returns the caller's effective rank on a note (0 = no access).
func (s *Server) noteRole(r *http.Request, noteID uuid.UUID) int {
	mu := mailUserFrom(r.Context())
	rank, err := s.q.NoteRole(r.Context(), db.NoteRoleParams{
		UserID: mu.ID, NoteID: noteID, TenantID: mu.TenantID,
	})
	if err != nil {
		return 0
	}
	return int(rank)
}

// requireNote resolves the id and checks the rank, writing the response when it refuses.
func (s *Server) requireNote(w http.ResponseWriter, r *http.Request, need int) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "notes.invalid_id")
		return uuid.Nil, false
	}
	rank := s.noteRole(r, id)
	if rank <= 0 {
		writeErr(w, http.StatusNotFound, "notes.not_found")
		return uuid.Nil, false
	}
	if rank < need {
		writeErr(w, http.StatusForbidden, "notes.forbidden")
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListNotes(r.Context(), db.ListNotesParams{
		UserID: mu.ID, TenantID: mu.TenantID, Lim: 500,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "notes.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": orEmpty(rows)})
}

func (s *Server) handleGetNote(w http.ResponseWriter, r *http.Request) {
	id, ok := s.requireNote(w, r, noteRoleViewer)
	if !ok {
		return
	}
	mu := mailUserFrom(r.Context())
	n, err := s.q.GetNote(r.Context(), db.GetNoteParams{UserID: mu.ID, NoteID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "notes.not_found")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

type noteBody struct {
	Title    string          `json:"title"`
	BodyJSON json.RawMessage `json:"body_json"`
}

// blocksOrEmpty keeps the column valid whatever the client sends. An absent or malformed body is an
// empty block array, not null: the editor cannot open a note whose body is null, and a note that
// will not open is worse than one that opens blank.
func blocksOrEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage("[]")
	}
	return raw
}

// noteText projects the blocks to plain text with the wiki's renderer, on the SERVER. The client
// could send its own copy and save a little work, but then a bug or a stale field there would leave
// a note whose searchable text and whose AI-extracted tasks describe something the note does not
// say. The blocks are the truth; this is derived from them, in one place, on every write.
func noteText(blocks json.RawMessage) string {
	text, _ := wiki.Derive(blocks)
	return text
}

func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req noteBody
	if !decode(w, r, &req) {
		return
	}
	n, err := s.q.CreateNote(r.Context(), db.CreateNoteParams{
		TenantID: mu.TenantID, OwnerUserID: mu.ID,
		Title:    strings.TrimSpace(req.Title),
		BodyJson: blocksOrEmpty(req.BodyJSON),
		BodyText: noteText(blocksOrEmpty(req.BodyJSON)),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "notes.create_failed")
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	id, ok := s.requireNote(w, r, noteRoleEditor)
	if !ok {
		return
	}
	mu := mailUserFrom(r.Context())
	var req noteBody
	if !decode(w, r, &req) {
		return
	}
	n, err := s.q.UpdateNote(r.Context(), db.UpdateNoteParams{
		NoteID: id, TenantID: mu.TenantID,
		Title:     strings.TrimSpace(req.Title),
		BodyJson:  blocksOrEmpty(req.BodyJSON),
		BodyText:  noteText(blocksOrEmpty(req.BodyJSON)),
		UpdatedBy: pgtype.UUID{Bytes: mu.ID, Valid: true},
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "notes.save_failed")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// handleDeleteNote: only the owner. An editor may rewrite a note; destroying someone else's is a
// different act, and the grant that allows writing was never a grant to remove.
func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	id, ok := s.requireNote(w, r, noteRoleOwner)
	if !ok {
		return
	}
	mu := mailUserFrom(r.Context())
	if err := s.q.DeleteNote(r.Context(), db.DeleteNoteParams{NoteID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "notes.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handlePinNote: pinning needs only read access -- it is the reader's own shelf, not a change to
// the note, and nobody else's list moves because of it.
func (s *Server) handlePinNote(w http.ResponseWriter, r *http.Request) {
	id, ok := s.requireNote(w, r, noteRoleViewer)
	if !ok {
		return
	}
	mu := mailUserFrom(r.Context())
	var err error
	if r.Method == http.MethodDelete {
		err = s.q.UnpinNote(r.Context(), db.UnpinNoteParams{NoteID: id, UserID: mu.ID})
	} else {
		err = s.q.PinNote(r.Context(), db.PinNoteParams{TenantID: mu.TenantID, NoteID: id, UserID: mu.ID})
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "notes.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"pinned": r.Method != http.MethodDelete})
}
