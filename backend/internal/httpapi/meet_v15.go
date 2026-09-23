package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Meet v1.5: "organizer joins first", kicking a participant, and emailing notes on end.
// In all of these: only the room's OWNER is in charge — the query filters by owner_user_id, which is the real check.

// hostPresent reports whether the organizer is connected in the room RIGHT NOW. LiveKit is the one
// that knows: our database only knows who was invited, not who's actually in there.
func (m *meet) hostPresent(ctx context.Context, room string, hostIdentities ...string) (bool, error) {
	out, err := m.roomAPI(ctx, "ListParticipants", map[string]any{"room": room}, room)
	if err != nil {
		return false, err
	}
	var lp struct {
		Participants []struct {
			Identity string `json:"identity"`
		} `json:"participants"`
	}
	if err := json.Unmarshal(out, &lp); err != nil {
		return false, err
	}
	hosts := make(map[string]bool, len(hostIdentities))
	for _, h := range hostIdentities {
		hosts[h] = true
	}
	for _, p := range lp.Participants {
		if hosts[p.Identity] {
			return true, nil
		}
	}
	return false, nil
}

// kickReq identifies who leaves by their LiveKit identity (which is our user id).
type kickReq struct {
	Identity string `json:"identity"`
}

// handleKickParticipant removes someone from the room. Owner only; and they can't kick themselves
// (they'd accidentally leave their own room, and with the lock on nobody else could get back in).
func (s *Server) handleKickParticipant(w http.ResponseWriter, r *http.Request) {
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
	var req kickReq
	if !decode(w, r, &req) {
		return
	}
	m, ok := s.meetingAsHost(r.Context(), id, mu)
	if !ok {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	if req.Identity == "" || req.Identity == mu.ID.String() {
		writeErr(w, http.StatusBadRequest, "meet.cannot_kick_self")
		return
	}
	if _, err := s.meet.roomAPI(r.Context(), "RemoveParticipant",
		map[string]any{"room": m.RoomName, "identity": req.Identity}, m.RoomName); err != nil {
		slog.Error("meet: remove participant", "room", m.RoomName, "identity", req.Identity, "err", err)
		writeErr(w, http.StatusInternalServerError, "meet.kick_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// handleMuteParticipant mutes (server-side) a participant's microphone. The host is in charge;
// LiveKit requires the audio track_sid, which we look up in the room's participant list.
func (s *Server) handleMuteParticipant(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Identity string `json:"identity"`
	}
	if !decode(w, r, &req) {
		return
	}
	m, ok := s.meetingAsHost(r.Context(), id, mu)
	if !ok {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	if req.Identity == "" {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	// Finds the participant's microphone track in the room.
	out, err := s.meet.roomAPI(r.Context(), "ListParticipants", map[string]any{"room": m.RoomName}, m.RoomName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.failed")
		return
	}
	var lp struct {
		Participants []struct {
			Identity string `json:"identity"`
			Tracks   []struct {
				Sid    string `json:"sid"`
				Type   string `json:"type"`
				Source string `json:"source"`
			} `json:"tracks"`
		} `json:"participants"`
	}
	_ = json.Unmarshal(out, &lp)
	trackSid := ""
	for _, p := range lp.Participants {
		if p.Identity != req.Identity {
			continue
		}
		for _, tr := range p.Tracks {
			if tr.Type == "AUDIO" || tr.Source == "MICROPHONE" {
				trackSid = tr.Sid
				break
			}
		}
	}
	if trackSid == "" {
		writeErr(w, http.StatusNotFound, "meet.no_audio_track")
		return
	}
	if _, err := s.meet.roomAPI(r.Context(), "MutePublishedTrack",
		map[string]any{"room": m.RoomName, "identity": req.Identity, "track_sid": trackSid, "muted": true}, m.RoomName); err != nil {
		slog.Error("meet: mute participant", "room", m.RoomName, "identity", req.Identity, "err", err)
		writeErr(w, http.StatusInternalServerError, "meet.mute_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "muted"})
}

// handleMeetParticipants lists who's IN THE ROOM right now (via LiveKit), so the host panel can
// kick them. Our database's list is of invitees — not the same thing.
func (s *Server) handleMeetParticipants(w http.ResponseWriter, r *http.Request) {
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
	out, err := s.meet.roomAPI(r.Context(), "ListParticipants", map[string]any{"room": m.RoomName}, m.RoomName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.failed")
		return
	}
	var lp struct {
		Participants []struct {
			Identity string `json:"identity"`
			Name     string `json:"name"`
		} `json:"participants"`
	}
	_ = json.Unmarshal(out, &lp)
	// Co-hosts are also "host" in the list (badge + powers in the UI).
	hosts := map[string]bool{m.OwnerUserID.String(): true}
	if cohosts, cerr := s.q.ListCohostIDs(r.Context(), m.ID); cerr == nil {
		for _, c := range cohosts {
			hosts[c.String()] = true
		}
	}
	list := make([]map[string]any, 0, len(lp.Participants))
	for _, p := range lp.Participants {
		list = append(list, map[string]any{
			"identity": p.Identity, "name": p.Name,
			"is_host":    p.Identity == m.OwnerUserID.String(),
			"is_cohost":  hosts[p.Identity] && p.Identity != m.OwnerUserID.String(),
			"can_manage": hosts[p.Identity],
		})
	}
	writeJSON(w, http.StatusOK, list)
}

// handleSendMeetNotes emails the meeting notes to participants. Only the owner can trigger it —
// the notes belong to their room.
func (s *Server) handleSendMeetNotes(w http.ResponseWriter, r *http.Request) {
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
	notes, err := s.q.GetMeetingNotes(r.Context(), db.GetMeetingNotesParams{MeetingID: m.ID, TenantID: mu.TenantID})
	if err != nil || strings.TrimSpace(notes.Body) == "" {
		// With no notes there's nothing to send — and an empty email is worse than none.
		writeErr(w, http.StatusBadRequest, "meet.no_notes")
		return
	}
	if s.email == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.email_disabled")
		return
	}
	people, _ := s.q.ListParticipants(r.Context(), m.ID)
	sent, failed := s.mailNotes(r.Context(), m, notes.Body, people)
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "failed": failed})
}

// mailNotes sends the notes to each participant. Best-effort and counted: the caller finds out
// how many went out and how many failed, instead of an "ok" that hides half the lost emails.
func (s *Server) mailNotes(ctx context.Context, m db.Meeting, body string, people []db.MeetingParticipant) (int, int) {
	subject := fmt.Sprintf("[%s] %s", m.Title, "Meeting notes")
	text := strings.TrimSpace(fmt.Sprintf("Meeting notes \"%s\"\n\n%s\n\n---\nBeskos", m.Title, body))
	var sent, failed int
	seen := map[string]bool{}
	for _, p := range people {
		if p.Email == "" || seen[strings.ToLower(p.Email)] {
			continue
		}
		seen[strings.ToLower(p.Email)] = true
		if err := s.email.Send(p.Email, subject, text); err != nil {
			slog.Error("meet notes email failed", "to", p.Email, "err", err)
			failed++
			continue
		}
		sent++
	}
	return sent, failed
}

// lockedOut: with "organizer joins first", whoever isn't the host waits until they arrive.
func (s *Server) lockedOut(ctx context.Context, m db.Meeting, isHost bool) bool {
	if isHost || s.meet == nil {
		return false
	}
	// Room locked to new entries: no newcomer gets in (only the host, already handled above).
	if m.Locked {
		return true
	}
	if !m.LockUntilHost {
		return false
	}
	// Any host opens the room: the owner or a co-host being present already lets everyone else in.
	hostIDs := []string{m.OwnerUserID.String()}
	if cohosts, cerr := s.q.ListCohostIDs(ctx, m.ID); cerr == nil {
		for _, c := range cohosts {
			hostIDs = append(hostIDs, c.String())
		}
	}
	present, err := s.meet.hostPresent(ctx, m.RoomName, hostIDs...)
	if err != nil {
		// If LiveKit doesn't respond, we don't lock anyone out: the room existing matters more
		// than the lock, and the error is ours, not the guest's.
		slog.Error("meet: host presence check failed", "room", m.RoomName, "err", err)
		return false
	}
	return !present
}
