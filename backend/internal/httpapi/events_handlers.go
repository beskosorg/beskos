package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Calendar: events per end user (our layer). Filters by owner (the logged-in mailbox).

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	from, err1 := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, err2 := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_range")
		return
	}
	// Generated params: EndsAt = lower bound (ends_at > from), StartsAt = upper bound (starts_at < to).
	events, err := s.q.ListEventsRange(r.Context(), db.ListEventsRangeParams{
		OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, EndsAt: from, StartsAt: to,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "events.failed")
		return
	}
	// Non-recurring only: recurring ones come from the expansion (otherwise the master would show
	// up twice in the range).
	out := make([]eventOut, 0, len(events))
	for _, e := range events {
		if e.Recur == "" {
			out = append(out, eventOut{Event: e})
		}
	}
	// Expands the (own) recurring masters into occurrences within the [from, to] range.
	if recurring, rerr := s.q.ListRecurringByOwner(r.Context(), pgtype.UUID{Bytes: mu.ID, Valid: true}); rerr == nil {
		for _, m := range recurring {
			for _, occ := range expandRecurrence(m, from, to) {
				out = append(out, eventOut{Event: occ})
			}
		}
	}
	// Events I was INVITED to go into my calendar (marked invited + my response).
	if invited, ierr := s.q.ListInvitedEvents(r.Context(), mu.ID); ierr == nil {
		for _, iv := range invited {
			out = append(out, invitedOccurrences(iv, from, to)...)
		}
	}
	// Attaches the INVITEES of every event in a single pass (avatars on the cards).
	ids := make([]uuid.UUID, 0, len(out))
	seen := map[uuid.UUID]bool{}
	for _, e := range out {
		if !seen[e.ID] {
			seen[e.ID] = true
			ids = append(ids, e.ID)
		}
	}
	if len(ids) > 0 {
		byEvent := map[uuid.UUID][]eventAttendee{}
		invitedEmails := map[uuid.UUID]map[string]bool{}
		if rows, aerr := s.q.ListEventInviteesForEvents(r.Context(), ids); aerr == nil {
			for _, a := range rows {
				if a.Email == mu.Email {
					continue // the user doesn't need to see their own avatar on everything
				}
				byEvent[a.EventID] = append(byEvent[a.EventID], eventAttendee{
					Name: a.Name, Email: a.Email, Avatar: a.Avatar, Status: a.Status,
				})
				if invitedEmails[a.EventID] == nil {
					invitedEmails[a.EventID] = map[string]bool{}
				}
				invitedEmails[a.EventID][a.Email] = true
			}
		}
		// The ORGANIZER also "is in the event": added first, unless it's the current user or they
		// are already in the invitee list.
		if cols, cerr := s.q.ListColleagues(r.Context(), mu.TenantID); cerr == nil {
			byID := map[uuid.UUID]db.ListColleaguesRow{}
			for _, c := range cols {
				byID[c.ID] = c
			}
			for i := range out {
				if !out[i].OwnerUserID.Valid {
					continue
				}
				ownerID := uuid.UUID(out[i].OwnerUserID.Bytes)
				if ownerID == mu.ID {
					continue
				}
				if c, ok := byID[ownerID]; ok && !invitedEmails[out[i].ID][c.Email] {
					owner := eventAttendee{Name: c.Name, Email: c.Email, Avatar: c.Avatar, Status: "organizer"}
					byEvent[out[i].ID] = append([]eventAttendee{owner}, byEvent[out[i].ID]...)
				}
			}
		}
		// Externals also "are in the event": initials + external badge (no avatar/account).
		if ext, eerr := s.q.ListEventExternalsForEvents(r.Context(), ids); eerr == nil {
			for _, x := range ext {
				byEvent[x.EventID] = append(byEvent[x.EventID], eventAttendee{
					Email: x.Email, Status: "external",
				})
			}
		}
		for i := range out {
			out[i].Attendees = byEvent[out[i].ID]
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// expandRecurrence generates the occurrences of a recurring master that fall within [from, to].
// Each occurrence is a copy with starts_at/ends_at shifted (same id as the master — editing or
// deleting affects the whole series).
func expandRecurrence(m db.Event, from, to time.Time) []db.Event {
	dur := m.EndsAt.Sub(m.StartsAt)
	limit := to
	if m.RecurUntil.Valid && m.RecurUntil.Time.Before(limit) {
		limit = m.RecurUntil.Time
	}
	next := func(t time.Time) time.Time {
		switch m.Recur {
		case "daily":
			return t.AddDate(0, 0, 1)
		case "weekly":
			return t.AddDate(0, 0, 7)
		case "monthly":
			return t.AddDate(0, 1, 0)
		case "yearly":
			return t.AddDate(1, 0, 0)
		}
		return t.AddDate(100, 0, 0) // unknown: closes quickly
	}
	out := []db.Event{}
	occ := m.StartsAt
	for i := 0; i < 1000 && !occ.After(limit); i++ { // safety cap against an infinite loop
		end := occ.Add(dur)
		if end.After(from) { // overlaps the requested range
			e := m
			e.StartsAt = occ
			e.EndsAt = end
			out = append(out, e)
		}
		occ = next(occ)
	}
	return out
}

type createEventReq struct {
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Location       string   `json:"location"`
	Color          string   `json:"color"`
	StartsAt       string   `json:"starts_at"`
	EndsAt         string   `json:"ends_at"`
	SourceEmail    string   `json:"source_email_id"`
	Tags           []string `json:"tags"`
	CreateMeet     bool     `json:"create_meet"`     // creates a linked Meet room
	Force          bool     `json:"force"`           // confirms even when there's a time conflict
	RemindMinutes  *int     `json:"remind_minutes"`  // minutes before to remind (nil = default 10; 0 = none)
	AllDay         bool     `json:"all_day"`         // all-day event (no time)
	Recur          string   `json:"recur"`           // '' | daily | weekly | monthly | yearly
	RecurUntil     string   `json:"recur_until"`     // recurrence limit (RFC3339, optional)
	InviteeIDs     []string `json:"invitee_ids"`     // invited colleagues (RSVP)
	ExternalEmails []string `json:"external_emails"` // outside guests (receive an email)
	CalendarID     string   `json:"calendar_id"`     // event's calendar ("" = no category)
}

// calendarID validates that the calendar belongs to the user; otherwise returns NULL.
func (s *Server) calendarID(ctx context.Context, mu db.MailUser, raw string) pgtype.UUID {
	if raw == "" {
		return pgtype.UUID{}
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return pgtype.UUID{}
	}
	if _, err := s.q.GetCalendar(ctx, db.GetCalendarParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// recurValues normalizes the recurrence: only the fixed scale is allowed; anything else becomes ""
// (one-off).
func recurValues(recur, until string) (string, pgtype.Timestamptz) {
	switch recur {
	case "daily", "weekly", "monthly", "yearly":
	default:
		recur = ""
	}
	var u pgtype.Timestamptz
	if recur != "" && until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			u = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}
	return recur, u
}

// remindOrDefault: nil = 10 (default, same as Google Calendar); otherwise the requested value (0 = none).
func remindOrDefault(p *int) int32 {
	if p == nil {
		return 10
	}
	if *p < 0 {
		return 0
	}
	return int32(*p)
}

func (s *Server) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req createEventReq
	if !decode(w, r, &req) {
		return
	}
	starts, err1 := time.Parse(time.RFC3339, req.StartsAt)
	ends, err2 := time.Parse(time.RFC3339, req.EndsAt)
	if err1 != nil || err2 != nil || req.Title == "" {
		writeErr(w, http.StatusBadRequest, "events.invalid")
		return
	}
	if req.Color == "" {
		req.Color = "#116466"
	}
	// Conflict warning: if there's already an event overlapping the time slot and the user hasn't
	// confirmed (force), returns 409 with the conflicts for the frontend to ask about (modal).
	if !req.Force {
		if conflicts, err := s.q.EventsOverlapping(r.Context(), db.EventsOverlappingParams{
			OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, EndsAt: starts, StartsAt: ends,
		}); err == nil && len(conflicts) > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error_code": "events.conflict", "conflicts": conflicts})
			return
		}
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	recurN, recurU := recurValues(req.Recur, req.RecurUntil)
	event, err := s.q.CreateEvent(r.Context(), db.CreateEventParams{
		TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
		Title: req.Title, Description: req.Description, Location: req.Location, Color: req.Color,
		StartsAt: starts, EndsAt: ends, SourceEmailID: req.SourceEmail, Tags: req.Tags,
		RemindMinutes: remindOrDefault(req.RemindMinutes), AllDay: req.AllDay,
		Recur: recurN, RecurUntil: recurU, CalendarID: s.calendarID(r.Context(), mu, req.CalendarID),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "events.create_failed")
		return
	}
	// Meet integration: creates the room and links it to the event.
	if req.CreateMeet {
		event.MeetingRoom = s.attachMeetToEvent(r.Context(), mu, event.ID, req.Title, starts, ends)
	}
	s.syncInvites(r.Context(), event, mu, req.InviteeIDs)
	s.syncExternals(r.Context(), event, mu, req.ExternalEmails)
	writeJSON(w, http.StatusCreated, event)
}

type updateEventReq struct {
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Location       string   `json:"location"`
	Color          string   `json:"color"`
	StartsAt       string   `json:"starts_at"`
	EndsAt         string   `json:"ends_at"`
	Tags           []string `json:"tags"`
	Force          bool     `json:"force"`           // confirms even with a time conflict
	RemindMinutes  *int     `json:"remind_minutes"`  // minutes before to remind (nil = 10; 0 = none)
	AllDay         bool     `json:"all_day"`         // all-day event
	Recur          string   `json:"recur"`           // '' | daily | weekly | monthly | yearly
	RecurUntil     string   `json:"recur_until"`     // limit (RFC3339, optional)
	InviteeIDs     []string `json:"invitee_ids"`     // invited colleagues (RSVP)
	ExternalEmails []string `json:"external_emails"` // outside guests (receive an email)
	CalendarID     string   `json:"calendar_id"`     // event's calendar ("" = no category)
}

func (s *Server) handleUpdateEvent(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	var req updateEventReq
	if !decode(w, r, &req) {
		return
	}
	starts, err1 := time.Parse(time.RFC3339, req.StartsAt)
	ends, err2 := time.Parse(time.RFC3339, req.EndsAt)
	if err1 != nil || err2 != nil || req.Title == "" {
		writeErr(w, http.StatusBadRequest, "events.invalid")
		return
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	// Conflict warning ALSO on edit (previously only on create): same semantics as create, but
	// ignoring the event itself. Without force and if there's a clash → 409 with the conflicts.
	if !req.Force {
		if conflicts, cerr := s.q.EventsOverlappingExcept(r.Context(), db.EventsOverlappingExceptParams{
			OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, EndsAt: starts, StartsAt: ends, ID: id,
		}); cerr == nil && len(conflicts) > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error_code": "events.conflict", "conflicts": conflicts})
			return
		}
	}
	recurN, recurU := recurValues(req.Recur, req.RecurUntil)
	// owner_user_id in the WHERE: events are personal. Without this, a colleague in the same tenant
	// could edit someone else's calendar by passing the event's UUID (audit H7). "no rows" when
	// they're not the owner → 404.
	event, err := s.q.UpdateEvent(r.Context(), db.UpdateEventParams{
		ID: id, TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
		Title: req.Title, Description: req.Description,
		Location: req.Location, Color: req.Color, StartsAt: starts, EndsAt: ends, Tags: req.Tags,
		RemindMinutes: remindOrDefault(req.RemindMinutes), AllDay: req.AllDay,
		Recur: recurN, RecurUntil: recurU, CalendarID: s.calendarID(r.Context(), mu, req.CalendarID),
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "events.not_found")
		return
	}
	s.syncInvites(r.Context(), event, mu, req.InviteeIDs)
	s.syncExternals(r.Context(), event, mu, req.ExternalEmails)
	writeJSON(w, http.StatusOK, event)
}

func (s *Server) handleDeleteEvent(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	if err := s.q.DeleteEvent(r.Context(), db.DeleteEventParams{ID: id, TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}}); err != nil {
		writeErr(w, http.StatusInternalServerError, "events.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// attachMeetToEvent creates a Meet room, adds the owner as host, and links it to the event.
// Returns the room name (or "" if Meet is disabled or the creation failed). Shared by
// create-with-meet and add-meet-to-existing-event.
func (s *Server) attachMeetToEvent(ctx context.Context, mu db.MailUser, eventID uuid.UUID, title string, starts, ends time.Time) string {
	if s.meet == nil {
		return ""
	}
	room := "beskos-" + uuid.NewString()[:8]
	m, err := s.q.CreateMeeting(ctx, db.CreateMeetingParams{
		TenantID: mu.TenantID, OwnerUserID: mu.ID, Title: title, RoomName: room,
		ScheduledStart: pgtype.Timestamptz{Time: starts, Valid: true}, ScheduledEnd: pgtype.Timestamptz{Time: ends, Valid: true},
		EventID: pgtype.UUID{Bytes: eventID, Valid: true}, Access: "public",
	})
	if err != nil {
		return ""
	}
	_, _ = s.q.AddParticipant(ctx, db.AddParticipantParams{MeetingID: m.ID, TenantID: mu.TenantID, Email: mu.Email, UserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, Role: "host"})
	_ = s.q.SetEventMeetingRoom(ctx, db.SetEventMeetingRoomParams{ID: eventID, TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, MeetingRoom: room})
	return room
}

// handleAddMeetToEvent links a Meet room to an already-existing event (idempotent: if it already
// has a room, returns it). Only the event's owner.
func (s *Server) handleAddMeetToEvent(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	ev, err := s.q.GetEventInTenant(r.Context(), db.GetEventInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil || !ev.OwnerUserID.Valid || uuid.UUID(ev.OwnerUserID.Bytes) != mu.ID || ev.DeletedAt.Valid {
		writeErr(w, http.StatusNotFound, "events.not_found")
		return
	}
	if ev.MeetingRoom != "" { // already linked
		writeJSON(w, http.StatusOK, map[string]string{"meeting_room": ev.MeetingRoom})
		return
	}
	if s.meet == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.disabled")
		return
	}
	room := s.attachMeetToEvent(r.Context(), mu, ev.ID, ev.Title, ev.StartsAt, ev.EndsAt)
	if room == "" {
		writeErr(w, http.StatusInternalServerError, "events.meet_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"meeting_room": room})
}

// handleRestoreEvent undoes a recent deletion (Gmail-style undo): clears deleted_at. Only the owner.
func (s *Server) handleRestoreEvent(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	ev, err := s.q.RestoreEvent(r.Context(), db.RestoreEventParams{ID: id, TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}})
	if err != nil {
		writeErr(w, http.StatusNotFound, "events.not_found")
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

// handleDuplicateEvent creates an independent copy of the event (same owner/tenant): new id, no
// invites or meeting link. The title comes from the frontend (which knows the locale for the
// "(copy)" suffix); empty = literal copy. Doesn't check for time conflicts — it's an intentional
// copy.
func (s *Server) handleDuplicateEvent(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // optional body; absence/error = literal copy, without writing to w
	src, err := s.q.GetEventInTenant(r.Context(), db.GetEventInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil || !src.OwnerUserID.Valid || uuid.UUID(src.OwnerUserID.Bytes) != mu.ID || src.DeletedAt.Valid {
		writeErr(w, http.StatusNotFound, "events.not_found")
		return
	}
	title := src.Title
	if t := strings.TrimSpace(req.Title); t != "" {
		title = t
	}
	dup, err := s.q.CreateEvent(r.Context(), db.CreateEventParams{
		TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
		Title: title, Description: src.Description, Location: src.Location, Color: src.Color,
		StartsAt: src.StartsAt, EndsAt: src.EndsAt, SourceEmailID: "", Tags: src.Tags,
		RemindMinutes: src.RemindMinutes, AllDay: src.AllDay, Recur: src.Recur, RecurUntil: src.RecurUntil,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "events.create_failed")
		return
	}
	writeJSON(w, http.StatusCreated, dup)
}
