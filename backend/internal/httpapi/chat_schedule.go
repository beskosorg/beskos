package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Scheduling a message send + "remind me" about a message. Both are swept by the worker (see
// cmd/worker): the scheduled one is sent and published to the hub; the reminder becomes a
// notification in the bell.

func (s *Server) handleScheduleMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	var req struct {
		Body   string `json:"body"`
		SendAt string `json:"send_at"`
	}
	if !decode(w, r, &req) {
		return
	}
	body := strings.TrimSpace(req.Body)
	sendAt, perr := time.Parse(time.RFC3339, req.SendAt)
	if body == "" || perr != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	if sendAt.Before(time.Now()) {
		writeErr(w, http.StatusBadRequest, "chat.schedule_past")
		return
	}
	sm, err := s.q.CreateScheduledMessage(r.Context(), db.CreateScheduledMessageParams{
		TenantID: mu.TenantID, ChannelID: id, UserID: mu.ID, Body: body, SendAt: sendAt,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.send_failed")
		return
	}
	writeJSON(w, http.StatusCreated, sm)
}

func (s *Server) handleListScheduled(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	list, err := s.q.ListScheduledMessages(r.Context(), mu.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	if list == nil {
		list = []db.ListScheduledMessagesRow{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCancelScheduled(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if err := s.q.DeleteScheduledMessage(r.Context(), db.DeleteScheduledMessageParams{ID: id, UserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}

// handleRemindMessage creates a personal reminder about a message (the worker notifies at the time).
func (s *Server) handleRemindMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	mid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	var req struct {
		RemindAt string `json:"remind_at"`
		Note     string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	remindAt, perr := time.Parse(time.RFC3339, req.RemindAt)
	if perr != nil || remindAt.Before(time.Now()) {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	msg, err := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: mid, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	if _, ok := s.memberOr403(w, r, msg.ChannelID); !ok { // only someone who can see the channel may remind about the message
		return
	}
	rm, err := s.q.CreateMessageReminder(r.Context(), db.CreateMessageReminderParams{
		TenantID: mu.TenantID, UserID: mu.ID, ChannelID: msg.ChannelID,
		MessageID: pgtype.UUID{Bytes: mid, Valid: true}, Note: strings.TrimSpace(req.Note), RemindAt: remindAt,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	writeJSON(w, http.StatusCreated, rm)
}
