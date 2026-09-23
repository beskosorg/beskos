package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// User preferences (signature, per-app prefs). Organized by app in the `settings` JSONB column;
// each section saves in isolation (top-level merge in the query), so saving E-mail doesn't touch Chat.

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	raw, err := s.q.GetUserSettings(r.Context(), mu.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "settings.failed")
		return
	}
	// raw is json.RawMessage (the column's JSONB); return it as-is.
	w.Header().Set("Content-Type", "application/json")
	if len(raw) == 0 {
		_, _ = w.Write([]byte("{}"))
		return
	}
	_, _ = w.Write(raw)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	// Accepts an object {app: {...}} and merges it. Validates that it's object JSON, not a bare value.
	var patch map[string]json.RawMessage
	if !decode(w, r, &patch) {
		return
	}
	blob, err := json.Marshal(patch)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "settings.invalid")
		return
	}
	if err := s.q.UpdateUserSettings(r.Context(), db.UpdateUserSettingsParams{ID: mu.ID, Settings: blob}); err != nil {
		writeErr(w, http.StatusInternalServerError, "settings.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
