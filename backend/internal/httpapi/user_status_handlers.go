package httpapi

import (
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// User status (Slack-style): emoji + short text. Set by the user themselves; visible to
// colleagues on the profile card and the members list.

func (s *Server) handleSetMyStatus(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		Emoji string `json:"emoji"`
		Text  string `json:"text"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len([]rune(req.Emoji)) > 8 {
		req.Emoji = string([]rune(req.Emoji)[:8])
	}
	if len(req.Text) > 100 {
		req.Text = req.Text[:100]
	}
	if err := s.q.SetUserStatus(r.Context(), db.SetUserStatusParams{ID: mu.ID, StatusEmoji: req.Emoji, StatusText: req.Text}); err != nil {
		writeErr(w, http.StatusInternalServerError, "status.save_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"emoji": req.Emoji, "text": req.Text})
}

// handleSetPresence sets the rich presence (active | away | dnd). dnd suppresses web push.
func (s *Server) handleSetPresence(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		Presence string `json:"presence"`
	}
	if !decode(w, r, &req) {
		return
	}
	switch req.Presence {
	case "active", "away", "dnd":
	default:
		writeErr(w, http.StatusBadRequest, "status.invalid")
		return
	}
	if err := s.q.SetPresence(r.Context(), db.SetPresenceParams{ID: mu.ID, Presence: req.Presence}); err != nil {
		writeErr(w, http.StatusInternalServerError, "status.save_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"presence": req.Presence})
}

func (s *Server) handleGetUserStatus(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	email := r.URL.Query().Get("email")
	if email == "" {
		writeJSON(w, http.StatusOK, map[string]string{"emoji": "", "text": ""})
		return
	}
	st, err := s.q.GetUserStatusByEmail(r.Context(), db.GetUserStatusByEmailParams{Email: email, TenantID: mu.TenantID})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"emoji": "", "text": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"emoji": st.StatusEmoji, "text": st.StatusText})
}
