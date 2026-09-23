package httpapi

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// The note of a meeting: created on first use, shared with whoever was in the meeting.
//
// AUTOMATIC SHARING RUNS ON EVERY FETCH, not once at creation. People join late, and someone who
// arrives ten minutes in would otherwise never receive the note they helped write. Re-running is
// cheap and, because the grant insert does nothing on conflict, it only ever ADDS people -- a role
// the owner changed by hand, or an access they revoked, is not quietly restored by the next fetch.
//
// Only participants of THIS organisation get a grant. An external guest has no account to grant to;
// the host sends them the notes by e-mail, which is the existing button in the panel.

// handleMeetingNote returns the meeting's note, creating and sharing it as needed.
func (s *Server) handleMeetingNote(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	meetingID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	// Being able to open the room is what earns the note. The check is the meeting's, not the
	// note's: the note may not exist yet, and its grants are derived from this very membership.
	if !s.inMeeting(r.Context(), meetingID, mu) {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}

	note, err := s.q.GetOrCreateMeetingNote(r.Context(), db.GetOrCreateMeetingNoteParams{
		MeetingID: meetingID, TenantID: mu.TenantID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "notes.create_failed")
		return
	}
	// Failing to share is not failing to open: the note still exists and the person asking is in the
	// meeting, so they can read it. Losing the note over a grant that can be retried would be worse.
	if err := s.q.SyncMeetingNoteGrants(r.Context(), db.SyncMeetingNoteGrantsParams{
		NoteID: note.ID, TenantID: mu.TenantID,
	}); err != nil {
		log.Printf("notes: sharing meeting note %s: %v", note.ID, err)
	}

	full, err := s.q.GetNote(r.Context(), db.GetNoteParams{UserID: mu.ID, NoteID: note.ID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "notes.failed")
		return
	}
	writeJSON(w, http.StatusOK, full)
}

// inMeeting reports whether this person belongs to the meeting -- owner, invited, or recorded as
// having joined. The webhook records the last case, so someone who walked in from a link counts.
func (s *Server) inMeeting(ctx context.Context, meetingID uuid.UUID, mu db.MailUser) bool {
	m, err := s.q.GetMeetingInTenant(ctx, db.GetMeetingInTenantParams{ID: meetingID, TenantID: mu.TenantID})
	if err != nil {
		return false
	}
	if m.OwnerUserID == mu.ID {
		return true
	}
	parts, err := s.q.ListParticipants(ctx, meetingID)
	if err != nil {
		return false
	}
	for _, p := range parts {
		if p.UserID.Valid && p.UserID.Bytes == mu.ID {
			return true
		}
	}
	return false
}
