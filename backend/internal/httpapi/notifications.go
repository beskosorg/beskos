package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// In-app notifications (bell) + email dispatch. The actual email send is optional (only if
// NOTIFY_* is configured); in-app notifications always work.

// Routes for the ELEMENTS that originate notifications. The `link` is what opens on click — in the
// bell, in the system notification (desktop app), and in the web push. It always points to the
// concrete item, not the area: clicking a shared-file alert opens the file, not the Drive.

// chatLink opens the conversation (channel or DM).
func chatLink(channelID uuid.UUID) string { return "/mail/chat/" + channelID.String() }

// fileLink opens the file (viewer or Office editor, depending on the type).
func fileLink(fileID uuid.UUID) string { return "/mail/files/file/" + fileID.String() }

// meetingLink opens the meeting page (doesn't join the room — the decision to join is up to whoever clicks).
func meetingLink(meetingID uuid.UUID) string { return "/mail/meet/m/" + meetingID.String() }

// eventLink opens the event in the calendar. The date rides along (`?d=`) because the agenda loads
// by range: without it, it would open on the current week and the event wouldn't even be loaded.
func eventLink(eventID uuid.UUID, when time.Time) string {
	return "/mail/cal/" + eventID.String() + "?d=" + url.QueryEscape(when.UTC().Format(time.RFC3339))
}

// notifyUser creates an in-app notification for a tenant user.
//
// `args` carries the event's structured data (actor, title, when…) for the frontend to translate via
// `server.notif.<kind>` in the user's language (docs/engineering-standards.md §4.6). title/body stay filled in
// English as a fallback and for email/web-push, which are rendered on the backend (no frontend).
func (s *Server) notifyUser(ctx context.Context, tenantID, userID uuid.UUID, kind, title, body, link string, meetingID uuid.UUID, args map[string]string) {
	_, err := s.q.CreateNotification(ctx, db.CreateNotificationParams{
		TenantID: tenantID, UserID: userID, Kind: kind, Title: title, Body: body, Link: link,
		MeetingID: pgtype.UUID{Bytes: meetingID, Valid: meetingID != uuid.Nil},
		Args:      notifArgs(args),
	})
	if err != nil {
		log.Printf("notify: create in-app failed: %v", err)
	}
	// Mirrors as a desktop notification (Web Push) — delivered even with Beskos closed. Best-effort.
	s.sendWebPush(ctx, userID, pushPayload{Kind: kind, Title: title, Body: body, Link: link})
}

// notifyPushOnly sends ONLY the desktop push, without writing to the notification area (the
// bell). Used by chat: unread counts already live in the channels, so messages shouldn't clutter
// the bell — but the push (app closed) still alerts the user.
func (s *Server) notifyPushOnly(ctx context.Context, userID uuid.UUID, kind, title, body, link string) {
	s.sendWebPush(ctx, userID, pushPayload{Kind: kind, Title: title, Body: body, Link: link})
}

// notifArgs serializes the notification's structured data as JSON (empty object if nothing).
func notifArgs(m map[string]string) json.RawMessage {
	if len(m) == 0 {
		return json.RawMessage("{}")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// sendEmail sends an email if the sender is configured (best-effort).
func (s *Server) sendEmail(to, subject, body string) bool {
	if s.email == nil {
		return false
	}
	if err := s.email.Send(to, subject, body); err != nil {
		log.Printf("notify: email to %s failed: %v", to, err)
		return false
	}
	return true
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	items, err := s.q.ListNotifications(r.Context(), db.ListNotificationsParams{UserID: mu.ID, Limit: 30})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "notifications.list_failed")
		return
	}
	if items == nil {
		items = []db.Notification{}
	}
	unread, _ := s.q.CountUnreadNotifications(r.Context(), mu.ID)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "unread": unread})
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "notifications.invalid_id")
		return
	}
	if err := s.q.MarkNotificationRead(r.Context(), db.MarkNotificationReadParams{ID: id, UserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "notifications.update_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	if err := s.q.MarkAllNotificationsRead(r.Context(), mu.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "notifications.update_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleMarkNotificationsReadByKind marks notifications of one kind as read (clears the area's
// badge when opening it, e.g. meeting_invite when viewing Meet, file_share when viewing Files).
func (s *Server) handleMarkNotificationsReadByKind(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		Kind string `json:"kind"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Kind == "" {
		writeErr(w, http.StatusBadRequest, "notifications.invalid_kind")
		return
	}
	if err := s.q.MarkNotificationsReadByKind(r.Context(), db.MarkNotificationsReadByKindParams{UserID: mu.ID, Kind: req.Kind}); err != nil {
		writeErr(w, http.StatusInternalServerError, "notifications.update_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
