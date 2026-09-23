package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Meet — meeting persistence: schedule, list (mine + invited), RSVP, cancel.
// Integrates with the calendar (event_id) and LiveKit (room_name + real-time webhooks).

type createMeetingReq struct {
	Title          string   `json:"title"`
	ScheduledStart string   `json:"scheduled_start"` // RFC3339; empty = instant
	ScheduledEnd   string   `json:"scheduled_end"`
	Participants   []string `json:"participants"` // invited emails
	AddToCalendar  bool     `json:"add_to_calendar"`
	Access         string   `json:"access"` // public | approval
}

func normalizeAccess(a string) string {
	// Default is approval: the organizer admits who enters; only an explicit "public" value opens the room.
	if a == "public" {
		return "public"
	}
	return "approval"
}

func parseTS(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// handleCreateMeeting schedules (or creates an instant) meeting, adds the owner as host and the
// invitees as participants, and optionally creates the calendar event.
func (s *Server) handleCreateMeeting(w http.ResponseWriter, r *http.Request) {
	if s.meet == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.disabled")
		return
	}
	var req createMeetingReq
	if !decode(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeErr(w, http.StatusBadRequest, "meet.title_required")
		return
	}
	mu := mailUserFrom(r.Context())
	start, end := parseTS(req.ScheduledStart), parseTS(req.ScheduledEnd)

	// Calendar event (optional) when a time is set.
	var eventID pgtype.UUID
	if req.AddToCalendar && start.Valid && end.Valid {
		ev, err := s.q.CreateEvent(r.Context(), db.CreateEventParams{
			TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
			Title: title, Description: "Beskos Meet", Location: "Beskos Meet", Color: "#116466",
			StartsAt: start.Time, EndsAt: end.Time, Tags: []string{}, RemindMinutes: 10,
		})
		if err == nil {
			eventID = pgtype.UUID{Bytes: ev.ID, Valid: true}
		}
	}

	m, err := s.q.CreateMeeting(r.Context(), db.CreateMeetingParams{
		TenantID: mu.TenantID, OwnerUserID: mu.ID, Title: title,
		RoomName: "beskos-" + uuid.NewString()[:8], ScheduledStart: start, ScheduledEnd: end, EventID: eventID,
		Access: normalizeAccess(req.Access),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.create_failed")
		return
	}

	// Owner = host, already accepted.
	_, _ = s.q.AddParticipant(r.Context(), db.AddParticipantParams{
		MeetingID: m.ID, TenantID: mu.TenantID, Email: mu.Email,
		UserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, Role: "host",
	})
	_, _ = s.q.SetRSVP(r.Context(), db.SetRSVPParams{MeetingID: m.ID, Email: mu.Email, Rsvp: "accepted"})

	// Invitees (deduplicated; matches the tenant's mail_user when one exists).
	seen := map[string]bool{strings.ToLower(mu.Email): true}
	for _, raw := range req.Participants {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true
		var uid pgtype.UUID
		if u, err := s.q.GetMailUserByEmail(r.Context(), email); err == nil && u.TenantID == mu.TenantID {
			uid = pgtype.UUID{Bytes: u.ID, Valid: true}
		}
		_, _ = s.q.AddParticipant(r.Context(), db.AddParticipantParams{
			MeetingID: m.ID, TenantID: mu.TenantID, Email: email, UserID: uid, Role: "guest",
		})
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: mu.TenantID, Valid: true}, Actor: mu.Email, Action: "meet.create", Target: title})

	// Invitee notifications (email + in-app) aren't sent yet; we still mark them as sent so the
	// "notified" tracking advances once the notification worker is wired in.
	s.notifyMeetingInvitees(r, m, mu)

	s.writeMeetingDetail(w, r, m, mu)
}

// handleListMeetings lists the user's meetings (that they created or were invited to), each with
// my RSVP and the participant list (to see who confirmed / was notified).
func (s *Server) handleListMeetings(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	// With ?bucket=upcoming|past the response is PAGINATED server-side (Meet home). Without a bucket,
	// return everything — the agenda (day/week/month) needs the full set for the period.
	if bucket := r.URL.Query().Get("bucket"); bucket == "upcoming" || bucket == "past" {
		// Upper-bounded too: an unbounded page overflows the int32 offset below.
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 || page > 1_000_000 {
			page = 1
		}
		per, _ := strconv.Atoi(r.URL.Query().Get("per"))
		if per < 1 || per > 50 {
			per = 6
		}
		rows, err := s.q.ListMyMeetingsPage(r.Context(), db.ListMyMeetingsPageParams{
			TenantID: mu.TenantID, OwnerUserID: mu.ID, Email: mu.Email, Bucket: bucket,
			Lim: int32(per), Off: int32((page - 1) * per),
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "meet.list_failed")
			return
		}
		total := int64(0)
		out := make([]map[string]any, 0, len(rows))
		for _, m := range rows {
			total = m.Total
			parts, _ := s.q.ListParticipants(r.Context(), m.ID)
			out = append(out, meetingJSON(pageRow(m), m.MyRsvp, m.IsOwner, parts))
		}
		writeJSON(w, http.StatusOK, map[string]any{"meetings": out, "total": total, "page": page, "per": per})
		return
	}
	rows, err := s.q.ListMyMeetings(r.Context(), db.ListMyMeetingsParams{TenantID: mu.TenantID, OwnerUserID: mu.ID, Email: mu.Email})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.list_failed")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		parts, _ := s.q.ListParticipants(r.Context(), m.ID)
		out = append(out, meetingJSON(meetingRow(m), m.MyRsvp, m.IsOwner, parts))
	}
	writeJSON(w, http.StatusOK, map[string]any{"meetings": out})
}

func pageRow(r db.ListMyMeetingsPageRow) db.Meeting {
	return db.Meeting{
		ID: r.ID, TenantID: r.TenantID, OwnerUserID: r.OwnerUserID, Title: r.Title, RoomName: r.RoomName,
		ScheduledStart: r.ScheduledStart, ScheduledEnd: r.ScheduledEnd, ActualStart: r.ActualStart,
		ActualEnd: r.ActualEnd, Status: r.Status, EventID: r.EventID, CreatedAt: r.CreatedAt,
	}
}

// handleRSVP changes the user's participation (accepted / declined / tentative).
func (s *Server) handleRSVP(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &body) {
		return
	}
	switch body.Status {
	case "accepted", "declined", "tentative", "pending":
	default:
		writeErr(w, http.StatusBadRequest, "meet.invalid_rsvp")
		return
	}
	// I can only change MY OWN participation (by email), and only if I was invited (isolated by tenant).
	if _, err := s.q.GetMeetingInTenant(r.Context(), db.GetMeetingInTenantParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	p, err := s.q.SetRSVP(r.Context(), db.SetRSVPParams{MeetingID: id, Email: mu.Email, Rsvp: body.Status})
	if err != nil {
		writeErr(w, http.StatusForbidden, "meet.not_invited")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rsvp": p.Rsvp})
}

// handleCancelMeeting cancels the meeting (owner only).
type updateMeetingReq struct {
	Title          string `json:"title"`
	ScheduledStart string `json:"scheduled_start"`
	ScheduledEnd   string `json:"scheduled_end"`
}

// handleUpdateMeeting edits/reschedules a meeting (owner only). Syncs the linked calendar event
// and notifies invitees of the change.
func (s *Server) handleUpdateMeeting(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	var req updateMeetingReq
	if !decode(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeErr(w, http.StatusBadRequest, "meet.title_required")
		return
	}
	start, end := parseTS(req.ScheduledStart), parseTS(req.ScheduledEnd)
	m, err := s.q.UpdateMeeting(r.Context(), db.UpdateMeetingParams{
		ID: id, TenantID: mu.TenantID, OwnerUserID: mu.ID, Title: title, ScheduledStart: start, ScheduledEnd: end,
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	// The linked calendar event follows the meeting (title + time).
	if m.EventID.Valid && start.Valid && end.Valid {
		_ = s.q.SetEventTimes(r.Context(), db.SetEventTimesParams{ID: uuid.UUID(m.EventID.Bytes), StartsAt: start.Time, EndsAt: end.Time})
	}
	s.notifyMeetingInvitees(r, m, mu)
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleCancelMeeting(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	if err := s.q.CancelMeeting(r.Context(), db.CancelMeetingParams{ID: id, TenantID: mu.TenantID, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.cancel_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}

// notifyMeetingInvitees notifies each invitee: in-app notification (if they're in the tenant) +
// email (if configured). Marks notified_at once any channel was delivered.
func (s *Server) notifyMeetingInvitees(r *http.Request, m db.Meeting, mu db.MailUser) {
	parts, _ := s.q.ListParticipants(r.Context(), m.ID)
	when := ""
	if m.ScheduledStart.Valid {
		when = m.ScheduledStart.Time.UTC().Format(time.RFC3339)
	}
	// title/body in English (fallback + email/push); the bell translates via server.notif.meeting_invite.
	title := mu.Name + " invited you: " + m.Title
	body := "You were invited to the meeting \"" + m.Title + "\" on Beskos Meet."
	args := map[string]string{"actor": mu.Name, "title": m.Title, "when": when}
	for _, p := range parts {
		if p.Email == mu.Email {
			continue
		}
		delivered := false
		if p.UserID.Valid { // tenant user → in-app notification
			s.notifyUser(r.Context(), m.TenantID, p.UserID.Bytes, "meeting_invite", title, body, meetingLink(m.ID), m.ID, args)
			delivered = true
		}
		if s.sendEmail(p.Email, title, body) {
			delivered = true
		}
		if delivered {
			_ = s.q.MarkParticipantNotified(r.Context(), db.MarkParticipantNotifiedParams{MeetingID: m.ID, Email: p.Email})
		}
	}
}

func (s *Server) writeMeetingDetail(w http.ResponseWriter, r *http.Request, m db.Meeting, mu db.MailUser) {
	parts, _ := s.q.ListParticipants(r.Context(), m.ID)
	myRsvp := ""
	for _, p := range parts {
		if p.Email == mu.Email {
			myRsvp = p.Rsvp
		}
	}
	writeJSON(w, http.StatusCreated, meetingJSON(m, myRsvp, m.OwnerUserID == mu.ID, parts))
}

// meetingRow converts the listing row (flattened columns) into a db.Meeting.
func meetingRow(r db.ListMyMeetingsRow) db.Meeting {
	return db.Meeting{
		ID: r.ID, TenantID: r.TenantID, OwnerUserID: r.OwnerUserID, Title: r.Title, RoomName: r.RoomName,
		ScheduledStart: r.ScheduledStart, ScheduledEnd: r.ScheduledEnd, ActualStart: r.ActualStart,
		ActualEnd: r.ActualEnd, Status: r.Status, EventID: r.EventID, CreatedAt: r.CreatedAt,
	}
}

func tsPtr(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.UTC().Format(time.RFC3339)
	return &s
}

// meetingJSON builds the JSON for a meeting with participants and duration (for those already ended).
func meetingJSON(m db.Meeting, myRsvp string, isOwner bool, parts []db.MeetingParticipant) map[string]any {
	var durationSec *int64
	if m.ActualStart.Valid && m.ActualEnd.Valid {
		d := int64(m.ActualEnd.Time.Sub(m.ActualStart.Time).Seconds())
		durationSec = &d
	}
	ps := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		ps = append(ps, map[string]any{
			"email": p.Email, "role": p.Role, "rsvp": p.Rsvp,
			"notified": p.NotifiedAt.Valid, "joined": p.JoinedAt.Valid,
		})
	}
	return map[string]any{
		"id": m.ID, "title": m.Title, "room": m.RoomName, "status": m.Status, "access": m.Access,
		"scheduled_start": tsPtr(m.ScheduledStart), "scheduled_end": tsPtr(m.ScheduledEnd),
		"actual_start": tsPtr(m.ActualStart), "actual_end": tsPtr(m.ActualEnd),
		"duration_seconds": durationSec, "my_rsvp": myRsvp, "is_owner": isOwner,
		"participants": ps,
	}
}
