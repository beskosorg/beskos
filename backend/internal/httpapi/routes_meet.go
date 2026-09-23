package httpapi

// Meet routes: rooms and meetings, host controls, recording, presentation and lobby.
// Split out of the route table because they were its largest single-product block (law 1.4).

import "github.com/go-chi/chi/v5"

// routesMeet registers the Meet routes on the authenticated group (session and CSRF already applied).
func (s *Server) routesMeet(r chi.Router) {
	// Meet (LiveKit): rooms, scheduled meetings, RSVP.
	r.Post("/meet/join", s.handleJoinMeet)
	r.Get("/meet/meetings", s.handleListMeetings)
	r.Post("/meet/meetings", s.handleCreateMeeting)
	r.Post("/meet/meetings/{id}/rsvp", s.handleRSVP)
	r.Post("/meet/meetings/{id}/cancel", s.handleCancelMeeting)
	r.Patch("/meet/meetings/{id}", s.handleUpdateMeeting) // edit/reschedule
	// Organizer controls (the query itself filters by owner_user_id → only the owner may change it).
	r.Patch("/meet/meetings/{id}/controls", s.handleUpdateMeetControls)
	// v1.5: who's in the room, kicking, and the notes by email. Owner only for the last two.
	r.Get("/meet/meetings/{id}/participants", s.handleMeetParticipants)
	r.Post("/meet/meetings/{id}/kick", s.handleKickParticipant)
	r.Post("/meet/meetings/{id}/mute", s.handleMuteParticipant)
	r.Get("/meet/meetings/{id}/cohosts", s.handleListCohosts)                    // room co-hosts
	r.Post("/meet/meetings/{id}/cohosts", s.handleAddCohost)                     // promote (owner)
	r.Delete("/meet/meetings/{id}/cohosts/{uid}", s.handleRemoveCohost)          // demote (owner)
	r.Get("/meet/meetings/{id}/breakouts", s.handleListBreakouts)                // breakout rooms
	r.Post("/meet/meetings/{id}/breakouts", s.handleCreateBreakouts)             // create (host)
	r.Delete("/meet/meetings/{id}/breakouts", s.handleCloseBreakouts)            // close all (host)
	r.Post("/meet/meetings/{id}/breakouts/{bid}/assign", s.handleAssignBreakout) // assign (host)
	r.Get("/meet/meetings/{id}/breakouts/{bid}/token", s.handleBreakoutToken)    // join a breakout
	r.Post("/meet/meetings/{id}/notes/email", s.handleSendMeetNotes)
	r.Patch("/meet/meetings/{id}/access", s.handleUpdateMeetAccess)
	// Meeting notes (turned into Kanban tasks via AI).
	r.Get("/meet/meetings/{id}/notes", s.handleGetMeetNotes)
	r.Put("/meet/meetings/{id}/notes", s.handleSaveMeetNotes)

	// Recording (LiveKit Egress → MinIO). Optional.
	r.Post("/meet/record/start", s.handleStartRecording)
	r.Post("/meet/record/stop", s.handleStopRecording)
	r.Get("/meet/recordings", s.handleListRecordings)
	r.Get("/meet/recordings/{id}/play", s.handlePlayRecording)
	r.Delete("/meet/recordings/{id}", s.handleDeleteRecording)

	// Meet: presentation (BBB-style). Start by upload or from Drive; current slide; stop.
	r.Post("/meet/presentation/upload", s.handleStartPresentationUpload)
	r.Post("/meet/presentation/drive", s.handleStartPresentationDrive)
	r.Get("/meet/presentation", s.handleGetPresentation)
	r.Post("/meet/presentation/{id}/slide", s.handleSetPresentationSlide)
	r.Post("/meet/presentation/{id}/stop", s.handleStopPresentation)

	// Meet: lobby (guest approval) — host only.
	r.Get("/meet/meetings/{id}/lobby", s.handleListLobby)
	r.Post("/meet/lobby/{id}/admit", s.handleAdmit)
	r.Post("/meet/lobby/{id}/deny", s.handleDeny)
}
