package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// TRACKS recording mode: one Track Egress per published track (cheap: no live compositor), and
// the worker assembles the MP4 offline with ffmpeg. Tracks published AFTER the start come in via
// the track_published webhook.

// s3Output builds the S3/MinIO destination shared by the egresses.
func (m *meet) s3Output(filepath string) map[string]any {
	return map[string]any{
		"fileType": "MP4", // ignored in track egress (LiveKit decides by codec); required in composite
		"filepath": filepath,
		"s3": map[string]any{
			"accessKey": m.s3key, "secret": m.s3secret, "bucket": m.s3bucket,
			"endpoint": m.s3endpoint, "forcePathStyle": true,
		},
	}
}

// startTracksRecording creates the tracks-mode recording and starts one egress per already-published track.
func (s *Server) startTracksRecording(ctx context.Context, mu db.MailUser, mtgID pgtype.UUID, room string, priority int32) (db.Recording, error) {
	rec, err := s.q.CreateTracksRecording(ctx, db.CreateTracksRecordingParams{
		TenantID: mu.TenantID, RoomName: room, MeetingID: mtgID,
		StartedBy: mu.Email, Priority: priority,
	})
	if err != nil {
		return db.Recording{}, err
	}
	for _, t := range s.meet.listPublishedTracks(ctx, room) {
		if err := s.startTrackEgress(ctx, rec, t); err != nil {
			log.Printf("meet: track egress %s (%s): %v", t.SID, t.Kind, err)
		}
	}
	return rec, nil
}

// publishedTrack is a live track in the room (audio or video from a participant).
type publishedTrack struct {
	SID         string
	Participant string
	Kind        string // audio | video
}

// listPublishedTracks asks RoomService for the room's published (unmuted) tracks.
func (m *meet) listPublishedTracks(ctx context.Context, room string) []publishedTrack {
	out, err := m.roomAPI(ctx, "ListParticipants", map[string]any{"room": room}, room)
	if err != nil {
		log.Printf("meet: list participants: %v", err)
		return nil
	}
	var lp struct {
		Participants []struct {
			Identity string `json:"identity"`
			Tracks   []struct {
				Sid   string          `json:"sid"`
				Type  json.RawMessage `json:"type"` // "AUDIO"/"VIDEO" or numeric enum
				Muted bool            `json:"muted"`
			} `json:"tracks"`
		} `json:"participants"`
	}
	if err := json.Unmarshal(out, &lp); err != nil {
		return nil
	}
	var tracks []publishedTrack
	for _, p := range lp.Participants {
		for _, t := range p.Tracks {
			kind := trackKind(t.Type)
			if kind == "" || t.Muted {
				continue
			}
			tracks = append(tracks, publishedTrack{SID: t.Sid, Participant: p.Identity, Kind: kind})
		}
	}
	return tracks
}

// trackKind normalizes the LiveKit enum ("AUDIO"/"VIDEO" in JSON, 0/1 in numeric protobuf).
func trackKind(raw json.RawMessage) string {
	s := strings.Trim(string(raw), `"`)
	switch s {
	case "AUDIO", "0":
		return "audio"
	case "VIDEO", "1":
		return "video"
	}
	return ""
}

// startTrackEgress starts a Track Egress for a track and registers the row. The final path (with
// the extension LiveKit picks based on the codec) arrives in the egress_ended webhook.
func (s *Server) startTrackEgress(ctx context.Context, rec db.Recording, t publishedTrack) error {
	path := fmt.Sprintf("recordings/raw/%s/%s-%s", rec.ID, t.Participant, t.SID)
	req := map[string]any{
		"roomName": rec.RoomName,
		"trackId":  t.SID,
		"file":     s.meet.s3Output(path),
	}
	// The track egress destination uses DirectFileOutput (no fileType).
	delete(req["file"].(map[string]any), "fileType")
	out, err := s.meet.twirp(ctx, "StartTrackEgress", req)
	if err != nil {
		return err
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	egressID, _ := firstString(m, "egressId", "egress_id").(string)
	if egressID == "" {
		return fmt.Errorf("no egressId in response: %s", out)
	}
	_, err = s.q.InsertRecordingTrack(ctx, db.InsertRecordingTrackParams{
		RecordingID: rec.ID, EgressID: egressID, TrackID: t.SID,
		Participant: t.Participant, Kind: t.Kind,
	})
	return err
}

// handleTrackPublished (webhook): a new track in a room with an active tracks recording gets an egress.
func (s *Server) handleTrackPublished(ctx context.Context, room, participant string, rawType json.RawMessage, sid string) {
	kind := trackKind(rawType)
	if kind == "" || sid == "" {
		return
	}
	rec, err := s.q.GetActiveTracksRecordingForRoom(ctx, room)
	if err != nil {
		return // room without an active tracks recording
	}
	if err := s.startTrackEgress(ctx, rec, publishedTrack{SID: sid, Participant: participant, Kind: kind}); err != nil {
		log.Printf("meet: late track egress %s: %v", sid, err)
	}
}

// stopTracksRecording stops all active egresses and puts the recording in the assembly queue.
func (s *Server) stopTracksRecording(w http.ResponseWriter, r *http.Request, rec db.Recording) {
	tracks, err := s.q.ListActiveRecordingTracks(r.Context(), rec.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.record_stop_failed")
		return
	}
	for _, t := range tracks {
		if _, err := s.meet.twirp(r.Context(), "StopEgress", map[string]any{"egressId": t.EgressID}); err != nil {
			log.Printf("meet: stop track egress %s: %v", t.EgressID, err)
		}
	}
	_ = s.q.MarkRecordingAssemblyPending(r.Context(), rec.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "assembling"})
}
