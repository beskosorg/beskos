package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Public access to meetings: external guests (without login) join via link. A public room means
// joining directly; an approval room means staying in the lobby until the owner (host) admits them.

// handlePublicMeetInfo returns the room's public data (title, access type) for the entry screen.
func (s *Server) handlePublicMeetInfo(w http.ResponseWriter, r *http.Request) {
	if s.meet == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.disabled")
		return
	}
	m, err := s.q.GetMeetingByRoom(r.Context(), chi.URLParam(r, "room"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	out := map[string]any{"title": m.Title, "access": m.Access, "status": m.Status}
	if ten, terr := s.q.GetTenant(r.Context(), m.TenantID); terr == nil {
		out["org"] = ten.Name
		out["brand"] = map[string]any{"color": ten.BrandColor, "logo": ten.BrandLogo, "logo_white_dark": ten.BrandLogoWhiteDark}
	}
	writeJSON(w, http.StatusOK, out)
}

type guestJoinReq struct {
	Name string `json:"name"`
}

// handlePublicMeetJoin: a guest joins. Public room → token right away; approval → creates a
// lobby request and returns the id so the guest can keep waiting.
func (s *Server) handlePublicMeetJoin(w http.ResponseWriter, r *http.Request) {
	if s.meet == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.disabled")
		return
	}
	room := chi.URLParam(r, "room")
	m, err := s.q.GetMeetingByRoom(r.Context(), room)
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	var req guestJoinReq
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "meet.name_required")
		return
	}
	identity := "guest-" + uuid.NewString()[:8]

	// Locked room: no new guest joins (not even via the lobby). Only unlocking it changes that.
	if m.Locked {
		writeJSON(w, http.StatusOK, map[string]any{"mode": "locked"})
		return
	}

	if m.Access == "approval" {
		k, err := s.q.CreateKnock(r.Context(), db.CreateKnockParams{
			MeetingID: m.ID, TenantID: m.TenantID, RoomName: room, Name: name, Identity: identity,
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "meet.knock_failed")
			return
		}
		// Notifies the organizer (in-app + push): someone is at the door waiting for approval.
		s.notifyUser(r.Context(), m.TenantID, m.OwnerUserID, "meet_knock",
			name+" wants to join", name+" is waiting for approval to join "+m.Title,
			"/mail/meet/"+room, m.ID, map[string]string{"name": name, "title": m.Title})
		writeJSON(w, http.StatusOK, map[string]any{"mode": "lobby", "lobby_id": k.ID})
		return
	}
	// Public: joins directly. A guest is never the organizer → gets the room's policy.
	tok, err := s.meet.token(identity, name, room, meetTokenTTL, meetingPolicy(m))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.token_failed")
		return
	}
	writeJSON(w, http.StatusOK, guestSession(s.meet.url, tok, room, identity, name))
}

// handlePublicLobbyPoll: the guest checks whether they've been admitted yet. If so, returns the token.
func (s *Server) handlePublicLobbyPoll(w http.ResponseWriter, r *http.Request) {
	if s.meet == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.disabled")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	k, err := s.q.GetKnock(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	if k.Status != "admitted" {
		writeJSON(w, http.StatusOK, map[string]any{"status": k.Status})
		return
	}
	// Admitted by the host: also joins with the room's policy (never organizer grants).
	policy := fullPolicy()
	if mm, merr := s.q.GetMeetingByRoom(r.Context(), k.RoomName); merr == nil {
		policy = meetingPolicy(mm)
	}
	tok, err := s.meet.token(k.Identity, k.Name, k.RoomName, meetTokenTTL, policy)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.token_failed")
		return
	}
	out := guestSession(s.meet.url, tok, k.RoomName, k.Identity, k.Name)
	out["status"] = "admitted"
	writeJSON(w, http.StatusOK, out)
}

func guestSession(url, tok, room, identity, name string) map[string]any {
	return map[string]any{"mode": "joined", "url": url, "token": tok, "room": room, "identity": identity, "name": name}
}

// --- host: manages the lobby (meeting owner only) ---

func (s *Server) ownedMeeting(w http.ResponseWriter, r *http.Request, meetingID uuid.UUID) (db.Meeting, bool) {
	mu := mailUserFrom(r.Context())
	m, err := s.q.GetMeetingInTenant(r.Context(), db.GetMeetingInTenantParams{ID: meetingID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return db.Meeting{}, false
	}
	if m.OwnerUserID != mu.ID {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return db.Meeting{}, false
	}
	return m, true
}

// handleListLobby: pending guest requests (the host approves).
func (s *Server) handleListLobby(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	if _, ok := s.ownedMeeting(w, r, id); !ok {
		return
	}
	knocks, err := s.q.ListPendingKnocks(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.lobby_failed")
		return
	}
	out := make([]map[string]any, 0, len(knocks))
	for _, k := range knocks {
		out = append(out, map[string]any{"id": k.ID, "name": k.Name, "at": k.CreatedAt.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending": out})
}

// handleLobbyDecision: the host admits or denies a guest.
func (s *Server) handleLobbyDecision(w http.ResponseWriter, r *http.Request, status string) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	mu := mailUserFrom(r.Context())
	k, err := s.q.GetKnockInTenant(r.Context(), db.GetKnockInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	if _, ok := s.ownedMeeting(w, r, k.MeetingID); !ok {
		return
	}
	if _, err := s.q.SetKnockStatus(r.Context(), db.SetKnockStatusParams{ID: id, Status: status}); err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.lobby_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) handleAdmit(w http.ResponseWriter, r *http.Request) {
	s.handleLobbyDecision(w, r, "admitted")
}
func (s *Server) handleDeny(w http.ResponseWriter, r *http.Request) {
	s.handleLobbyDecision(w, r, "denied")
}
