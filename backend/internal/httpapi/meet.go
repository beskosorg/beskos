package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Meet — video calling via LiveKit. Our Go signs the room token (JWT HS256 with the LiveKit
// secret, same pattern as OnlyOffice) and the browser joins with the SDK. LiveKit runs as a
// network-only service, unmodified (laws §2/§5.4).

type meet struct {
	url    string // WebSocket the BROWSER uses (ws://…)
	apiKey string
	secret []byte

	// Recording (Egress). httpURL = LiveKit's Twirp API (reached by our Go); s3* = MinIO destination.
	httpURL    string
	s3endpoint string
	s3key      string
	s3secret   string
	s3bucket   string
}

// EnableMeet turns Meet on. Without URL/keys, it stays off.
func (s *Server) EnableMeet(url, apiKey, secret string) {
	if url == "" || apiKey == "" || secret == "" {
		return
	}
	s.meet = &meet{url: url, apiKey: apiKey, secret: []byte(secret)}
}

// EnableMeetRecording turns on recording via Egress (optional). Needs LiveKit's Twirp API and the
// S3/MinIO destination.
func (s *Server) EnableMeetRecording(httpURL, s3endpoint, s3key, s3secret, s3bucket string) {
	if s.meet == nil || httpURL == "" || s3bucket == "" {
		return
	}
	s.meet.httpURL = strings.TrimRight(httpURL, "/")
	s.meet.s3endpoint, s.meet.s3key, s.meet.s3secret, s.meet.s3bucket = s3endpoint, s3key, s3secret, s3bucket
}

func (m *meet) recordingEnabled() bool { return m != nil && m.httpURL != "" && m.s3bucket != "" }

// signVideo signs a LiveKit token with an arbitrary `video` grant (join, record, etc.).
func (m *meet) signVideo(identity string, video map[string]any, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := map[string]any{"iss": m.apiKey, "sub": identity, "nbf": now.Unix(), "exp": now.Add(ttl).Unix(), "video": video}
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := header + "." + enc.EncodeToString(body)
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(signing))
	return signing + "." + enc.EncodeToString(mac.Sum(nil)), nil
}

// videoGrant holds the LiveKit room grants embedded in the token.
type videoGrant struct {
	Room              string   `json:"room"`
	RoomJoin          bool     `json:"roomJoin"`
	CanPublish        bool     `json:"canPublish"`
	CanSubscribe      bool     `json:"canSubscribe"`
	CanPublishData    bool     `json:"canPublishData"`
	CanPublishSources []string `json:"canPublishSources,omitempty"`
}

// roomPolicy is what the organizer grants to participants (Meet-style controls).
type roomPolicy struct{ Mic, Cam, Share, Chat bool }

func fullPolicy() roomPolicy { return roomPolicy{true, true, true, true} }

// meetingPolicy is the room's policy (what applies to participants and guests; the owner gets
// fullPolicy).
func meetingPolicy(m db.Meeting) roomPolicy {
	return roomPolicy{Mic: m.AllowMic, Cam: m.AllowCam, Share: m.AllowShare, Chat: m.AllowChat}
}

// sources translates the policy into the token's publishable sources (lowercase names, grant format).
func (p roomPolicy) sources() []string {
	s := []string{}
	if p.Mic {
		s = append(s, "microphone")
	}
	if p.Cam {
		s = append(s, "camera")
	}
	if p.Share {
		s = append(s, "screen_share", "screen_share_audio")
	}
	return s
}

// grant builds the videoGrant from the policy. WARNING: in LiveKit, an empty/absent
// canPublishSources means "all sources"; so if nothing is allowed, we deny canPublish entirely.
func (p roomPolicy) grant(room string) videoGrant {
	// CanPublishData is always true: the data channel is app-controlled (chat "typing" + presentation —
	// slides/pen/laser). Without this, a non-organizer presenter couldn't broadcast the
	// presentation to the others. The annotation permission is applied client-side by the presenter.
	g := videoGrant{Room: room, RoomJoin: true, CanSubscribe: true, CanPublishData: true}
	srcs := p.sources()
	switch {
	case len(srcs) == 0:
		g.CanPublish = false
	case p.Mic && p.Cam && p.Share:
		g.CanPublish = true // everything allowed: omit the list
	default:
		g.CanPublish = true
		g.CanPublishSources = srcs
	}
	return g
}

// token signs a LiveKit access token for (identity, room), with the policy's grants.
func (m *meet) token(identity, name, room string, ttl time.Duration, p roomPolicy) (string, error) {
	now := time.Now()
	claims := map[string]any{
		"iss":   m.apiKey,
		"sub":   identity,
		"name":  name,
		"nbf":   now.Unix(),
		"exp":   now.Add(ttl).Unix(),
		"video": p.grant(room),
	}
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := header + "." + enc.EncodeToString(body)
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(signing))
	return signing + "." + enc.EncodeToString(mac.Sum(nil)), nil
}

// verifyWebhook checks the JWT LiveKit sends in the Authorization header (HS256 with the secret),
// including the body's sha256 hash. Ensures the webhook came from our LiveKit.
func (m *meet) verifyWebhook(authz string, body []byte) bool {
	tok := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return false
	}
	enc := base64.RawURLEncoding
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := enc.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return false
	}
	raw, err := enc.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Sha256 string `json:"sha256"`
		Exp    int64  `json:"exp"`
		Nbf    int64  `json:"nbf"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return false
	}
	// Rejects an expired / not-yet-valid token: without this, a captured webhook would be
	// replayable indefinitely (audit). LiveKit emits exp; require its presence and validity.
	now := time.Now().Unix()
	if claims.Exp == 0 || now > claims.Exp || (claims.Nbf != 0 && now < claims.Nbf) {
		return false
	}
	sum := sha256.Sum256(body)
	return claims.Sha256 == base64.StdEncoding.EncodeToString(sum[:])
}

// protoSources translates the policy into the protobuf TrackSource enum (used in UpdateParticipant).
func (p roomPolicy) protoSources() []string {
	s := []string{}
	if p.Mic {
		s = append(s, "MICROPHONE")
	}
	if p.Cam {
		s = append(s, "CAMERA")
	}
	if p.Share {
		s = append(s, "SCREEN_SHARE", "SCREEN_SHARE_AUDIO")
	}
	return s
}

// roomAPI calls LiveKit's RoomService API with a room admin token.
func (m *meet) roomAPI(ctx context.Context, method string, req any, room string) ([]byte, error) {
	tok, err := m.signVideo("admin", map[string]any{"roomAdmin": true, "room": room}, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.httpURL+"/twirp/livekit.RoomService/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("roomservice %s: HTTP %d: %s", method, resp.StatusCode, out)
	}
	return out, nil
}

// applyPolicy applies the policy to those ALREADY connected (the token only applies to those who
// join afterward). The organizer keeps the full grants.
func (m *meet) applyPolicy(ctx context.Context, room, hostIdentity string, p roomPolicy) error {
	out, err := m.roomAPI(ctx, "ListParticipants", map[string]any{"room": room}, room)
	if err != nil {
		return err
	}
	var lp struct {
		Participants []struct {
			Identity string `json:"identity"`
		} `json:"participants"`
	}
	if err := json.Unmarshal(out, &lp); err != nil {
		return err
	}
	// canPublishData is ALWAYS true (same as the token grant): the data channel is app-controlled
	// (presentation/pen/laser/"typing"). Tying it to chat would break the presentation for anyone
	// already connected when the host turned chat off. Chat on/off is applied client-side (allow_chat).
	perm := map[string]any{"canSubscribe": true, "canPublishData": true}
	if srcs := p.protoSources(); len(srcs) == 0 {
		perm["canPublish"] = false
	} else {
		perm["canPublish"] = true
		perm["canPublishSources"] = srcs
	}
	for _, pt := range lp.Participants {
		if pt.Identity == hostIdentity {
			continue
		}
		if _, err := m.roomAPI(ctx, "UpdateParticipant", map[string]any{
			"room": room, "identity": pt.Identity, "permission": perm,
		}, room); err != nil {
			return err
		}
	}
	return nil
}

const meetTokenTTL = 6 * time.Hour

type joinMeetReq struct {
	Room   string `json:"room"`   // empty → creates a new room (ad-hoc)
	Access string `json:"access"` // public | approval (only when creating a new room)
}

// handleJoinMeet returns the LiveKit URL and a token to join the room. If room comes in empty,
// it creates an instant meeting (with a record, so it can be shared and have access control).
func (s *Server) handleJoinMeet(w http.ResponseWriter, r *http.Request) {
	if s.meet == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.disabled")
		return
	}
	var req joinMeetReq
	if !decode(w, r, &req) {
		return
	}
	mu := mailUserFrom(r.Context())
	room := strings.TrimSpace(req.Room)
	if room == "" {
		room = "beskos-" + uuid.NewString()[:8]
	}
	// Every room has a record (owner = whoever creates/joins first). This is what gives an
	// organizer, controls, and access control to ad-hoc rooms too (e.g. a call started in chat).
	m, err := s.q.GetMeetingByRoom(r.Context(), room)
	if err != nil {
		m, err = s.q.CreateMeeting(r.Context(), db.CreateMeetingParams{
			TenantID: mu.TenantID, OwnerUserID: mu.ID, Title: "Instant meeting",
			RoomName: room, Access: normalizeAccess(req.Access),
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "meet.create_failed")
			return
		}
		_, _ = s.q.AddParticipant(r.Context(), db.AddParticipantParams{
			MeetingID: m.ID, TenantID: mu.TenantID, Email: mu.Email,
			UserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, Role: "host",
		})
	} else if m.TenantID != mu.TenantID {
		// The room is indexed only by name (GetMeetingByRoom doesn't filter by tenant). Without this
		// check, a user from another tenant who guesses/enumerates the name would get a media token
		// for someone else's room, even bypassing the approval lobby (audit H3). Never issue a token here.
		writeErr(w, http.StatusForbidden, "meet.forbidden")
		return
	}

	// The organizer (and co-hosts) publish everything; everyone else gets the room policy's grants.
	isHost := s.isHostOf(r.Context(), m, mu.ID)

	// "Organizer joins first": without them in the room, nobody joins. Refuse BEFORE signing the
	// token — an already-issued token would grant access even if the UI showed a waiting room.
	if s.lockedOut(r.Context(), m, isHost) {
		writeJSON(w, http.StatusOK, map[string]any{
			"locked": true, "room": room, "title": m.Title, "meeting_id": m.ID,
		})
		return
	}

	// Participant limit per tenant plan. Hosts/co-hosts always join (so they can open the room and
	// moderate); it only counts for everyone else. A LiveKit error (-1) doesn't block (the room matters more).
	if !isHost && s.meet != nil {
		if t, terr := s.q.GetTenant(r.Context(), mu.TenantID); terr == nil {
			plan := s.planByKey(r.Context(), t.Plan)
			if n := s.meet.participantCount(r.Context(), room); n >= 0 && plan.MeetParticipantsExceeded(n) {
				writeJSON(w, http.StatusOK, map[string]any{
					"full": true, "room": room, "title": m.Title, "meeting_id": m.ID, "limit": plan.MaxMeetParticipants,
				})
				return
			}
		}
	}
	policy := roomPolicy{Mic: m.AllowMic, Cam: m.AllowCam, Share: m.AllowShare, Chat: m.AllowChat}
	if isHost {
		policy = fullPolicy()
	}
	tok, err := s.meet.token(mu.ID.String(), mu.Name, room, meetTokenTTL, policy)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.token_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":        s.meet.url,
		"token":      tok,
		"room":       room,
		"identity":   mu.ID.String(),
		"name":       mu.Name,
		"meeting_id": m.ID,
		"title":      m.Title,
		"is_host":    isHost,
		"is_owner":   m.OwnerUserID == mu.ID,
		"access":     m.Access,
		"controls":   controlsDTO(m),
	})
}

// controlsDTO exposes the organizer's controls to the frontend.
func controlsDTO(m db.Meeting) map[string]bool {
	return map[string]bool{
		"allow_mic": m.AllowMic, "allow_cam": m.AllowCam,
		"allow_share": m.AllowShare, "allow_chat": m.AllowChat,
		"lock_until_host": m.LockUntilHost, "locked": m.Locked,
	}
}

// handleUpdateMeetControls — ONLY THE OWNER of the room can change this: the query filters by
// owner_user_id, so a non-owner gets "no rows" → 403. Also applies to those already connected.
func (s *Server) handleUpdateMeetControls(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	var req struct {
		AllowMic      bool `json:"allow_mic"`
		AllowCam      bool `json:"allow_cam"`
		AllowShare    bool `json:"allow_share"`
		AllowChat     bool `json:"allow_chat"`
		LockUntilHost bool `json:"lock_until_host"`
		Locked        bool `json:"locked"` // lock the room to new entries
	}
	if !decode(w, r, &req) {
		return
	}
	m, err := s.q.UpdateMeetingControls(r.Context(), db.UpdateMeetingControlsParams{
		ID: id, OwnerUserID: mu.ID,
		AllowMic: req.AllowMic, AllowCam: req.AllowCam, AllowShare: req.AllowShare, AllowChat: req.AllowChat,
		LockUntilHost: req.LockUntilHost, Locked: req.Locked,
	})
	if err != nil {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	if s.meet != nil {
		policy := roomPolicy{Mic: m.AllowMic, Cam: m.AllowCam, Share: m.AllowShare, Chat: m.AllowChat}
		if perr := s.meet.applyPolicy(r.Context(), m.RoomName, m.OwnerUserID.String(), policy); perr != nil {
			log.Printf("meet: apply policy in room %s: %v", m.RoomName, perr)
		}
	}
	writeJSON(w, http.StatusOK, controlsDTO(m))
}

// handleUpdateMeetAccess — OWNER ONLY: switches open ↔ approval without leaving the room.
func (s *Server) handleUpdateMeetAccess(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	var req struct {
		Access string `json:"access"`
	}
	if !decode(w, r, &req) {
		return
	}
	m, err := s.q.UpdateMeetingAccess(r.Context(), db.UpdateMeetingAccessParams{
		ID: id, OwnerUserID: mu.ID, Access: normalizeAccess(req.Access),
	})
	if err != nil {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"access": m.Access})
}

// handleMeetWebhook receives LiveKit events (server-to-server) and updates the meeting's actual
// start/end times and participant presence. Authenticated by the LiveKit signature, not a session.
func (s *Server) handleMeetWebhook(w http.ResponseWriter, r *http.Request) {
	if s.meet == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !s.meet.verifyWebhook(r.Header.Get("Authorization"), body) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	var ev struct {
		Event string `json:"event"`
		Room  struct {
			Name string `json:"name"`
		} `json:"room"`
		Participant struct {
			Identity string `json:"identity"`
		} `json:"participant"`
		Track struct {
			Sid  string          `json:"sid"`
			Type json.RawMessage `json:"type"`
		} `json:"track"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	switch ev.Event {
	case "room_started":
		_ = s.q.SetMeetingStarted(ctx, ev.Room.Name)
	case "room_finished":
		_ = s.q.SetMeetingEnded(ctx, ev.Room.Name)
	case "participant_joined":
		if id, err := uuid.Parse(ev.Participant.Identity); err == nil {
			_ = s.q.MarkParticipantJoined(ctx, db.MarkParticipantJoinedParams{RoomName: ev.Room.Name, UserID: id})
		}
	case "participant_left":
		if id, err := uuid.Parse(ev.Participant.Identity); err == nil {
			_ = s.q.MarkParticipantLeft(ctx, db.MarkParticipantLeftParams{RoomName: ev.Room.Name, UserID: pgtype.UUID{Bytes: id, Valid: true}})
			// If whoever left was the presenter, end the room's active presentation (otherwise it's
			// left orphaned: on rejoining it would still be "presenting"). Only affects presentations they created.
			_ = s.q.DeactivateRoomPresentationsByCreator(ctx, db.DeactivateRoomPresentationsByCreatorParams{Room: ev.Room.Name, CreatedBy: id})
		}
	case "track_published":
		// New track in a room with an active tracks-mode recording → start egress for it.
		s.handleTrackPublished(ctx, ev.Room.Name, ev.Participant.Identity, ev.Track.Type, ev.Track.Sid)
	case "egress_started", "egress_updated", "egress_ended", "egress_failed":
		s.handleEgressEvent(ctx, ev.Event, body)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
