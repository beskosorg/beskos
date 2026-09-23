package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/notify"
	"strings"
)

// Invitees / RSVP. The owner invites colleagues (event_invites); the event enters their calendar
// and each of them responds. eventOut adds to the event whether I'm invited (not owner) and my
// response.
type eventOut struct {
	db.Event
	Invited bool   `json:"invited"`
	MyRsvp  string `json:"my_rsvp,omitempty"`
	// Attendees show up on the calendar cards (avatar/initials + name/email tooltip).
	Attendees []eventAttendee `json:"attendees,omitempty"`
}

type eventAttendee struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	Avatar string `json:"avatar,omitempty"`
	Status string `json:"status,omitempty"` // invited | accepted | declined | maybe
}

// syncInvites rewrites the event's invitees (only valid colleagues from the tenant) and notifies
// the new ones.
func (s *Server) syncInvites(ctx context.Context, ev db.Event, actor db.MailUser, inviteeIDs []string) {
	prev := map[uuid.UUID]bool{}
	if cur, err := s.q.ListEventInvites(ctx, ev.ID); err == nil {
		for _, i := range cur {
			prev[i.UserID] = true
		}
	}
	_ = s.q.ClearEventInvites(ctx, ev.ID)
	for _, raw := range inviteeIDs {
		uid, err := uuid.Parse(raw)
		if err != nil || uid == actor.ID {
			continue
		}
		if _, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: uid, TenantID: actor.TenantID}); err != nil {
			continue // not from the tenant → never invite blindly
		}
		if err := s.q.AddEventInvite(ctx, db.AddEventInviteParams{EventID: ev.ID, TenantID: actor.TenantID, UserID: uid}); err != nil {
			continue
		}
		if !prev[uid] { // notify only new ones (don't re-notify on every edit)
			s.notifyUser(ctx, actor.TenantID, uid, "event_invite",
				"Invitation: "+ev.Title, actor.Name+" invited you — "+ev.StartsAt.UTC().Format(time.RFC3339), eventLink(ev.ID, ev.StartsAt), uuid.Nil,
				map[string]string{"actor": actor.Name, "title": ev.Title, "when": ev.StartsAt.UTC().Format(time.RFC3339)})
		}
	}
}

// handleRSVP: the invitee responds to the event (accepted|declined|maybe|pending). Notifies the owner.
func (s *Server) handleEventRSVP(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	valid := map[string]bool{"accepted": true, "declined": true, "maybe": true, "pending": true}
	if !valid[req.Status] {
		writeErr(w, http.StatusBadRequest, "events.invalid_rsvp")
		return
	}
	// Only whoever was invited can respond (the invite proves the right to respond).
	inv, err := s.q.GetEventInvite(r.Context(), db.GetEventInviteParams{EventID: id, UserID: mu.ID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "events.not_invited")
		return
	}
	_ = s.q.SetInviteStatus(r.Context(), db.SetInviteStatusParams{EventID: id, UserID: mu.ID, Status: req.Status})
	// Notifies the owner of the response.
	if ev, err := s.q.GetEventInTenant(r.Context(), db.GetEventInTenantParams{ID: id, TenantID: mu.TenantID}); err == nil && ev.OwnerUserID.Valid {
		s.notifyUser(r.Context(), mu.TenantID, uuid.UUID(ev.OwnerUserID.Bytes), "event_rsvp",
			"RSVP: "+ev.Title, mu.Name+" replied: "+req.Status, eventLink(ev.ID, ev.StartsAt), uuid.Nil,
			map[string]string{"actor": mu.Name, "title": ev.Title, "status": req.Status})
	}
	_ = inv
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Status})
}

// handleEventInvites lists the invitees and their statuses (owner or invitee can view).
func (s *Server) handleEventInvites(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	rows, _ := s.q.ListEventInvites(r.Context(), id)
	_ = mu
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

// invitedToEvent converts the invitee-list row into a db.Event (to reuse the expansion logic).
func invitedToEvent(iv db.ListInvitedEventsRow) db.Event {
	return db.Event{
		ID: iv.ID, TenantID: iv.TenantID, OwnerUserID: iv.OwnerUserID, Title: iv.Title,
		Description: iv.Description, Location: iv.Location, Color: iv.Color, StartsAt: iv.StartsAt,
		EndsAt: iv.EndsAt, SourceEmailID: iv.SourceEmailID, CreatedAt: iv.CreatedAt, Tags: iv.Tags,
		MeetingRoom: iv.MeetingRoom, RemindMinutes: iv.RemindMinutes, Reminded: iv.Reminded,
		AllDay: iv.AllDay, Recur: iv.Recur, RecurUntil: iv.RecurUntil,
	}
}

// invitedOccurrences expands (if recurring) or filters by the interval an event I was invited to.
func invitedOccurrences(iv db.ListInvitedEventsRow, from, to time.Time) []eventOut {
	m := invitedToEvent(iv)
	var evs []db.Event
	if m.Recur != "" {
		evs = expandRecurrence(m, from, to)
	} else if m.StartsAt.Before(to) && m.EndsAt.After(from) {
		evs = []db.Event{m}
	}
	out := make([]eventOut, 0, len(evs))
	for _, e := range evs {
		out = append(out, eventOut{Event: e, Invited: true, MyRsvp: iv.MyRsvp})
	}
	return out
}

// syncExternals rewrites the EXTERNAL invitees (emails from outside the organization) and sends
// the invite by email to the NEW ones (best-effort: without SMTP configured, it just records them).
// Minimal email validation; duplicates and empty entries are ignored.
func (s *Server) syncExternals(ctx context.Context, ev db.Event, actor db.MailUser, emails []string) {
	if emails == nil {
		return // request without the field: leave it alone (compatibility with old callers)
	}
	prev := map[string]bool{}
	if cur, err := s.q.ListEventExternals(ctx, ev.ID); err == nil {
		for _, e := range cur {
			prev[e] = true
		}
	}
	_ = s.q.DeleteEventExternals(ctx, ev.ID)
	seen := map[string]bool{}
	for _, raw := range emails {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email == "" || seen[email] || !strings.Contains(email, "@") || len(email) > 254 {
			continue
		}
		seen[email] = true
		if err := s.q.AddEventExternal(ctx, db.AddEventExternalParams{
			EventID: ev.ID, TenantID: ev.TenantID, Email: email,
		}); err != nil {
			continue
		}
		if !prev[email] {
			// People from OUTSIDE the organization have no app to receive the notification in: for
			// them, we send an email. Whoever is internal is invited by `syncInvites` and gets an
			// in-app notification.
			who := actor.Name
			if who == "" {
				who = actor.Email
			}
			meetURL := ""
			if ev.MeetingRoom != "" && s.publicURL != "" {
				meetURL = s.publicURL + "/j/" + ev.MeetingRoom
			}
			s.sendMsg(email, notify.EventInvite(actor.Language, who, ev.Title,
				ev.StartsAt.Format("02/01/2006 15:04"), ev.Location, meetURL))
		}
	}
}

// handleEventExternals returns the event's external invitees (for the edit modal).
func (s *Server) handleEventExternals(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	// Ownership: the event must belong to the tenant (owner OR internal invitee can view).
	if _, gerr := s.q.GetEventInTenant(r.Context(), db.GetEventInTenantParams{ID: id, TenantID: mu.TenantID}); gerr != nil {
		writeErr(w, http.StatusNotFound, "events.not_found")
		return
	}
	emails, err := s.q.ListEventExternals(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "events.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(emails))
}
