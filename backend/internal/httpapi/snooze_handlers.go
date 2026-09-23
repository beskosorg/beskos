package httpapi

import (
	"net/http"
	"time"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Snooze: the client moves the message to the "Snoozed" folder (JMAP, with its own session) and
// records the wake time here. The worker returns it to the Inbox at wake_at, even with the app closed.
func (s *Server) handleSnoozeEmail(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		EmailID string `json:"email_id"`
		WakeAt  string `json:"wake_at"` // RFC3339
	}
	if !decode(w, r, &req) {
		return
	}
	wake, err := time.Parse(time.RFC3339, req.WakeAt)
	if err != nil || req.EmailID == "" {
		writeErr(w, http.StatusBadRequest, "snooze.invalid")
		return
	}
	if wake.Before(time.Now()) {
		writeErr(w, http.StatusBadRequest, "snooze.past")
		return
	}
	if _, err := s.q.CreateSnooze(r.Context(), db.CreateSnoozeParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, EmailID: req.EmailID, WakeAt: wake,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "snooze.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "snoozed"})
}
