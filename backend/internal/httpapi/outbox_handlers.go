package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Server-side send outbox: the client creates the draft in JMAP and registers the send here
// (email_id + envelope + send_at). The worker submits it when it's due. Powers undo-send (short,
// cancelable window) and manageable scheduling. See mailbox.UserClient.SubmitEmail and cmd/worker
// (scanOutbox).

func (s *Server) handleCreateOutbox(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		EmailID    string   `json:"email_id"`
		IdentityID string   `json:"identity_id"`
		From       string   `json:"from"`
		Rcpts      []string `json:"rcpts"`
		Subject    string   `json:"subject"`
		SendAt     string   `json:"send_at"` // RFC3339
	}
	if !decode(w, r, &req) {
		return
	}
	sendAt, err := time.Parse(time.RFC3339, req.SendAt)
	if err != nil || req.EmailID == "" || req.IdentityID == "" || req.From == "" || len(req.Rcpts) == 0 {
		writeErr(w, http.StatusBadRequest, "outbox.invalid")
		return
	}
	// The plan's daily send limit: reject at creation time already — better UX than failing later
	// in the worker (which re-validates anyway, since the limit can be exhausted in the meantime).
	if !s.mailSendAllowed(r.Context(), mu.TenantID, len(req.Rcpts)) {
		writeErr(w, http.StatusTooManyRequests, "mail.daily_limit_reached")
		return
	}
	row, err := s.q.CreateOutbox(r.Context(), db.CreateOutboxParams{
		TenantID: mu.TenantID, UserID: mu.ID, EmailID: req.EmailID, IdentityID: req.IdentityID,
		FromEmail: req.From, Rcpts: req.Rcpts, Subject: req.Subject, SendAt: sendAt,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "outbox.failed")
		return
	}
	// Wakes the send worker (LISTEN/NOTIFY): it sleeps until the next send_at, no polling — the
	// email goes out almost right after the "undo" window closes, instead of waiting for a tick.
	_ = s.q.NotifyOutbox(r.Context())
	writeJSON(w, http.StatusCreated, map[string]string{"id": row.ID.String()})
}

func (s *Server) handleListOutbox(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListPendingOutbox(r.Context(), db.ListPendingOutboxParams{UserID: mu.ID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "outbox.failed")
		return
	}
	if rows == nil {
		rows = []db.ListPendingOutboxRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleCancelOutbox(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "outbox.invalid")
		return
	}
	n, err := s.q.CancelOutbox(r.Context(), db.CancelOutboxParams{ID: id, UserID: mu.ID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "outbox.failed")
		return
	}
	if n == 0 {
		// Already sent or nonexistent: nothing to cancel (the undo window may have passed).
		writeErr(w, http.StatusConflict, "outbox.too_late")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}
