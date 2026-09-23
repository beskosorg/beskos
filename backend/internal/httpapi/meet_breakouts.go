package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Breakout rooms: parallel rooms inside a meeting. The host creates N rooms (and can auto-distribute
// those present), assigns people, and closes everything when done. Each breakout is its own LiveKit
// room (room_name = <room>-b<idx>); joining a breakout means requesting a token for that room.

const maxBreakouts = 20

type breakoutDTO struct {
	ID       string   `json:"id"`
	Idx      int32    `json:"idx"`
	Name     string   `json:"name"`
	RoomName string   `json:"room_name"`
	Members  []string `json:"members"` // ids of assigned users
}

// handleCreateBreakouts creates breakout rooms (host). Accepts `count` (automatic names) and an
// optional `auto_assign` to round-robin distribute the participants present.
func (s *Server) handleCreateBreakouts(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	m, ok := s.meetingAsHost(r.Context(), id, mu)
	if !ok {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	var req struct {
		Count      int  `json:"count"`
		AutoAssign bool `json:"auto_assign"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Count < 1 || req.Count > maxBreakouts {
		writeErr(w, http.StatusBadRequest, "meet.breakout_count")
		return
	}
	// Recreate from scratch: closing the old ones avoids orphan rooms and keeps numbering predictable.
	_ = s.q.DeleteBreakouts(r.Context(), m.ID)
	created := make([]db.MeetingBreakout, 0, req.Count)
	for i := 1; i <= req.Count; i++ {
		b, cerr := s.q.CreateBreakout(r.Context(), db.CreateBreakoutParams{
			MeetingID: m.ID, TenantID: mu.TenantID, Idx: int32(i),
			Name: fmt.Sprintf("Room %d", i), RoomName: fmt.Sprintf("%s-b%d", m.RoomName, i),
		})
		if cerr != nil {
			writeErr(w, http.StatusInternalServerError, "meet.failed")
			return
		}
		created = append(created, b)
	}
	if req.AutoAssign && s.meet != nil {
		s.autoAssign(r.Context(), m, created)
	}
	s.writeBreakouts(w, r, m.ID)
}

// autoAssign round-robin distributes the participants present (minus the host) across the rooms.
func (s *Server) autoAssign(ctx context.Context, m db.Meeting, rooms []db.MeetingBreakout) {
	ids := s.meet.presentNonHost(ctx, m.RoomName, m.OwnerUserID.String())
	for i, uid := range ids {
		room := rooms[i%len(rooms)]
		if pid, err := uuid.Parse(uid); err == nil {
			_ = s.q.AssignBreakout(ctx, db.AssignBreakoutParams{MeetingID: m.ID, UserID: pid, BreakoutID: room.ID})
		}
	}
}

func (s *Server) handleListBreakouts(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	// Any tenant member who can see the meeting can list (to know their own assignment).
	if _, err := s.q.GetMeetingInTenant(r.Context(), db.GetMeetingInTenantParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	s.writeBreakouts(w, r, id)
}

// handleAssignBreakout moves a participant into a room (host).
func (s *Server) handleAssignBreakout(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	m, ok := s.meetingAsHost(r.Context(), id, mu)
	if !ok {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	bid, err := uuid.Parse(chi.URLParam(r, "bid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	if _, err := s.q.GetBreakout(r.Context(), db.GetBreakoutParams{ID: bid, MeetingID: m.ID}); err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	uid, err := uuid.Parse(strings.TrimSpace(req.UserID))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	if err := s.q.AssignBreakout(r.Context(), db.AssignBreakoutParams{MeetingID: m.ID, UserID: uid, BreakoutID: bid}); err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "assigned"})
}

// handleCloseBreakouts closes all rooms (host). Clients detect it (poll) and return to the main room.
func (s *Server) handleCloseBreakouts(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	m, ok := s.meetingAsHost(r.Context(), id, mu)
	if !ok {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	_ = s.q.DeleteBreakouts(r.Context(), m.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

// handleBreakoutToken returns a token for the breakout room. The user must be assigned to it —
// or be the host (who can visit any breakout).
func (s *Server) handleBreakoutToken(w http.ResponseWriter, r *http.Request) {
	if s.meet == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.disabled")
		return
	}
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	m, err := s.q.GetMeetingInTenant(r.Context(), db.GetMeetingInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	bid, err := uuid.Parse(chi.URLParam(r, "bid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	b, err := s.q.GetBreakout(r.Context(), db.GetBreakoutParams{ID: bid, MeetingID: m.ID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	isHost := s.isHostOf(r.Context(), m, mu.ID)
	if !isHost {
		mine, merr := s.q.GetMyBreakout(r.Context(), db.GetMyBreakoutParams{MeetingID: m.ID, UserID: mu.ID})
		if merr != nil || mine.ID != b.ID {
			writeErr(w, http.StatusForbidden, "meet.breakout_not_yours")
			return
		}
	}
	// Everyone publishes fully inside the breakout (it's a small working room).
	tok, err := s.meet.token(mu.ID.String(), mu.Name, b.RoomName, meetTokenTTL, fullPolicy())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.token_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url": s.meet.url, "token": tok, "room": b.RoomName, "name": b.Name, "breakout_id": b.ID.String(),
	})
}

// writeBreakouts serializes the rooms + assigned members.
func (s *Server) writeBreakouts(w http.ResponseWriter, r *http.Request, meetingID uuid.UUID) {
	rooms, err := s.q.ListBreakouts(r.Context(), meetingID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.failed")
		return
	}
	assigns, _ := s.q.ListBreakoutAssignments(r.Context(), meetingID)
	byRoom := map[uuid.UUID][]string{}
	for _, a := range assigns {
		byRoom[a.BreakoutID] = append(byRoom[a.BreakoutID], a.UserID.String())
	}
	out := make([]breakoutDTO, 0, len(rooms))
	for _, b := range rooms {
		out = append(out, breakoutDTO{
			ID: b.ID.String(), Idx: b.Idx, Name: b.Name, RoomName: b.RoomName, Members: byRoom[b.ID],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"breakouts": out})
}
