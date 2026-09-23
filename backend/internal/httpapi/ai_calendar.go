package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Calendar AI: create from natural language, suggest a free time slot, day agenda.

func (s *Server) handleAIParseEvent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req struct {
		Text string `json:"text"`
		TZ   string `json:"tz"`
	}
	if !decode(w, r, &req) {
		return
	}
	// User's timezone (coming from the browser). Validates against the IANA database; invalid/empty -> fallback.
	tz := req.TZ
	if tz == "" {
		tz = "America/Sao_Paulo"
	}
	loc, lerr := time.LoadLocation(tz)
	if lerr != nil {
		tz = "America/Sao_Paulo"
		loc, _ = time.LoadLocation(tz)
	}
	now := time.Now()
	if loc != nil {
		now = now.In(loc)
	}
	draft, err := a.ParseEvent(r.Context(), req.Text, now.Format(time.RFC3339), tz)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.parse_failed")
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) handleAISuggestSlots(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req struct {
		DurationMin int    `json:"duration_min"`
		From        string `json:"from"`
		To          string `json:"to"`
	}
	if !decode(w, r, &req) {
		return
	}
	from, err1 := time.Parse(time.RFC3339, req.From)
	to, err2 := time.Parse(time.RFC3339, req.To)
	if err1 != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_range")
		return
	}
	if req.DurationMin <= 0 {
		req.DurationMin = 30
	}
	mu := mailUserFrom(r.Context())
	busy, _ := s.q.ListEventsRange(r.Context(), db.ListEventsRangeParams{
		OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, EndsAt: from, StartsAt: to,
	})
	busyJSON, _ := json.Marshal(compactEvents(busy))
	slots, err := a.SuggestSlots(r.Context(), string(busyJSON), req.From, req.To, req.DurationMin)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.suggest_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slots": slots})
}

func (s *Server) handleAIAgenda(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !decode(w, r, &req) {
		return
	}
	from, err1 := time.Parse(time.RFC3339, req.From)
	to, err2 := time.Parse(time.RFC3339, req.To)
	if err1 != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_range")
		return
	}
	mu := mailUserFrom(r.Context())
	events, _ := s.q.ListEventsRange(r.Context(), db.ListEventsRangeParams{
		OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, EndsAt: from, StartsAt: to,
	})
	if len(events) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"agenda": ""})
		return
	}
	evJSON, _ := json.Marshal(compactEvents(events))
	agenda, err := a.DayAgenda(r.Context(), string(evJSON))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.agenda_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"agenda": agenda})
}

// compactEvents reduces events to the essentials for the AI (title + time).
func compactEvents(events []db.Event) []map[string]string {
	out := make([]map[string]string, 0, len(events))
	for _, e := range events {
		out = append(out, map[string]string{"title": e.Title, "starts_at": e.StartsAt.Format(time.RFC3339), "ends_at": e.EndsAt.Format(time.RFC3339)})
	}
	return out
}
