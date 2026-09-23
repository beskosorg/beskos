package httpapi

import "github.com/go-chi/chi/v5"

// Notes routes (migration 00133): personal and meeting notes, shared per person, team or the
// whole organisation. Pinning is per person, so it needs only read access.
func (s *Server) mountNotes(r chi.Router) {
	// The meeting's note: created on first use and shared with everyone who was in the meeting.
	r.Get("/meet/meetings/{id}/note", s.handleMeetingNote)
	r.Get("/notes", s.handleListNotes)
	r.Post("/notes", s.handleCreateNote)
	r.Get("/notes/{id}", s.handleGetNote)
	r.Patch("/notes/{id}", s.handleUpdateNote)
	r.Delete("/notes/{id}", s.handleDeleteNote)
	r.Post("/notes/{id}/pin", s.handlePinNote)
	r.Delete("/notes/{id}/pin", s.handlePinNote)
	r.Get("/notes/{id}/grants", s.handleListNoteGrants)
	r.Post("/notes/{id}/grants", s.handleSetNoteGrant)
	r.Delete("/notes/{id}/grants/{grantId}", s.handleDeleteNoteGrant)
}
