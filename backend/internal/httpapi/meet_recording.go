package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Meeting recording via LiveKit Egress (RoomComposite) → MinIO. Optional: only when the user
// starts it. We call LiveKit's Twirp API; Egress composites the room and uploads the MP4.

const recordTokenTTL = 6 * time.Hour

// twirp calls a LiveKit (Egress) API method with a roomRecord token.
func (m *meet) twirp(ctx context.Context, method string, req any) ([]byte, error) {
	tok, err := m.signVideo("egress", map[string]any{"roomRecord": true}, recordTokenTTL)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.httpURL+"/twirp/livekit.Egress/"+method, bytes.NewReader(body))
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
		return nil, fmt.Errorf("egress %s: HTTP %d: %s", method, resp.StatusCode, out)
	}
	return out, nil
}

// effectiveRecordingMode resolves what recording a tenant actually gets, from two independent
// sources: the hosted billing plan's Recording tier, and a self-hosted Pro license's Recording
// capability.
//
// A self-hosted install has no hosted plan tier to grant this from -- its catalog is the single
// self_hosted row, permanently "none" -- so the capability is the second, independent way in: a
// valid Pro license upgrades "none" to the standard tracks pipeline, the same one a hosted
// "offline" plan gets. Without this, buying a self-hosted Pro key could never actually turn
// recording on, since nothing ever read the capability it grants.
func effectiveRecordingMode(planRecording string, hasProCapability bool) string {
	if planRecording == "none" && hasProCapability {
		return "offline"
	}
	return planRecording
}

// handleStartRecording starts the room's recording and registers it.
func (s *Server) handleStartRecording(w http.ResponseWriter, r *http.Request) {
	if !s.meet.recordingEnabled() {
		writeErr(w, http.StatusServiceUnavailable, "meet.recording_disabled")
		return
	}
	var req struct {
		Room   string `json:"room"`
		Layout string `json:"layout"` // grid (default) | speaker | single-speaker
	}
	if !decode(w, r, &req) || req.Room == "" {
		writeErr(w, http.StatusBadRequest, "meet.invalid_room")
		return
	}
	mu := mailUserFrom(r.Context())
	// The room must be a meeting of THIS tenant and the requester must be the host. Without this,
	// any user could start the egress of another tenant's room and the recording would be stored
	// under their own tenant (and later playable by them) — cross-tenant capture (audit H4).
	mtg, err := s.q.GetMeetingByRoom(r.Context(), req.Room)
	if err != nil || mtg.TenantID != mu.TenantID {
		writeErr(w, http.StatusNotFound, "meet.meeting_not_found")
		return
	}
	if mtg.OwnerUserID != mu.ID {
		writeErr(w, http.StatusForbidden, "meet.host_only")
		return
	}
	// Recording per plan: the entry plan doesn't record; "offline"/"offline_priority" use the
	// TRACKS PIPELINE (default — cheap live, MP4 assembled offline by the worker); any
	// other value falls back to the legacy RoomComposite.
	recording := effectiveRecordingMode(s.tenantPlanByID(r.Context(), mu.TenantID).Recording, s.edition.Recording)
	if recording == "none" {
		writeErr(w, http.StatusForbidden, "meet.recording_not_in_plan")
		return
	}
	if recording == "offline" || recording == "offline_priority" {
		priority := int32(0)
		if recording == "offline_priority" {
			priority = 1
		}
		rec, err := s.startTracksRecording(r.Context(), mu, pgtype.UUID{Bytes: mtg.ID, Valid: true}, req.Room, priority)
		if err != nil {
			log.Printf("meet: tracks recording start failed: %v", err)
			writeErr(w, http.StatusInternalServerError, "meet.record_start_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"recording_id": rec.ID, "mode": "tracks"})
		return
	}
	// Tenant prefix in the key: this is what makes the recording fall under PER-TENANT encryption
	// instead of being wrapped by the KEK. Deleting a tenant's key must make everything of theirs
	// unreadable, recordings included. Old recordings stay where they are — read via the key stored
	// in the database — and will be sealed with the KEK by the sealing pass.
	filepath := mu.TenantID.String() + "/recordings/" + req.Room + "-" +
		strconv.FormatInt(time.Now().Unix(), 10) + ".mp4"

	egReq := map[string]any{
		"roomName": req.Room,
		"layout":   recordLayout(req.Layout),
		"fileOutputs": []map[string]any{{
			"fileType": "MP4",
			"filepath": filepath,
			"s3": map[string]any{
				"accessKey": s.meet.s3key, "secret": s.meet.s3secret, "bucket": s.meet.s3bucket,
				"endpoint": s.meet.s3endpoint, "forcePathStyle": true,
			},
		}},
	}
	out, err := s.meet.twirp(r.Context(), "StartRoomCompositeEgress", egReq)
	if err != nil {
		log.Printf("meet: egress start failed: %v", err)
		writeErr(w, http.StatusBadGateway, "meet.record_start_failed")
		return
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	egressID, _ := firstString(m, "egressId", "egress_id").(string)
	if egressID == "" {
		log.Printf("meet: egress start: no egressId in response: %s", out)
		writeErr(w, http.StatusBadGateway, "meet.record_start_failed")
		return
	}
	// Links to the meeting already validated above (same tenant, host).
	meetingID := pgtype.UUID{Bytes: mtg.ID, Valid: true}
	rec, err := s.q.CreateRecording(r.Context(), db.CreateRecordingParams{
		TenantID: mu.TenantID, RoomName: req.Room, MeetingID: meetingID,
		EgressID: egressID, BlobKey: filepath, StartedBy: mu.Email,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.record_persist_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recording_id": rec.ID, "egress_id": egressID})
}

// handleStopRecording ends the room's active recording.
func (s *Server) handleStopRecording(w http.ResponseWriter, r *http.Request) {
	if !s.meet.recordingEnabled() {
		writeErr(w, http.StatusServiceUnavailable, "meet.recording_disabled")
		return
	}
	var req struct {
		Room string `json:"room"`
	}
	if !decode(w, r, &req) || req.Room == "" {
		writeErr(w, http.StatusBadRequest, "meet.invalid_room")
		return
	}
	mu := mailUserFrom(r.Context())
	rec, err := s.q.GetActiveRecordingForRoom(r.Context(), req.Room)
	if err != nil || rec.TenantID != mu.TenantID {
		// Nonexistent recording OR from another tenant → 404 (doesn't confirm cross-tenant existence).
		writeErr(w, http.StatusNotFound, "meet.no_active_recording")
		return
	}
	// Only the meeting's host can stop the recording (audit H4).
	if mtg, err := s.q.GetMeetingByRoom(r.Context(), req.Room); err != nil || mtg.OwnerUserID != mu.ID {
		writeErr(w, http.StatusForbidden, "meet.host_only")
		return
	}
	if rec.Mode == "tracks" {
		s.stopTracksRecording(w, r, rec)
		return
	}
	if _, err := s.meet.twirp(r.Context(), "StopEgress", map[string]any{"egressId": rec.EgressID}); err != nil {
		writeErr(w, http.StatusBadGateway, "meet.record_stop_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
}

// handleListRecordings lists the tenant's recordings, paginated server-side (Meet home).
func (s *Server) handleListRecordings(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	// Upper-bounded too: an unbounded page overflows the int32 offset below.
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 || page > 1_000_000 {
		page = 1
	}
	per, _ := strconv.Atoi(r.URL.Query().Get("per"))
	if per < 1 || per > 50 {
		per = 6
	}
	rows, err := s.q.ListRecordingsPage(r.Context(), db.ListRecordingsPageParams{
		TenantID: mu.TenantID, Limit: int32(per), Offset: int32((page - 1) * per),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.recordings_list_failed")
		return
	}
	total := int64(0)
	recs := make([]db.Recording, 0, len(rows))
	for _, rc := range rows {
		total = rc.Total
		recs = append(recs, db.Recording{
			ID: rc.ID, TenantID: rc.TenantID, MeetingID: rc.MeetingID, RoomName: rc.RoomName,
			EgressID: rc.EgressID, Status: rc.Status, BlobKey: rc.BlobKey, SizeBytes: rc.SizeBytes,
			DurationSeconds: rc.DurationSeconds, StartedBy: rc.StartedBy, StartedAt: rc.StartedAt,
			EndedAt: rc.EndedAt, Mode: rc.Mode, AssemblyStatus: rc.AssemblyStatus, Priority: rc.Priority,
			AssemblyClaimedAt: rc.AssemblyClaimedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"recordings": recs, "total": total, "page": page, "per": per})
}

// handlePlayRecording streams the MP4 from MinIO (inline) to the player.
func (s *Server) handlePlayRecording(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	rec, err := s.q.GetRecordingInTenant(r.Context(), db.GetRecordingInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil || rec.Status != "ready" || rec.BlobKey == "" {
		writeErr(w, http.StatusNotFound, "meet.recording_not_ready")
		return
	}
	obj, err := s.storage.GetSeek(r.Context(), rec.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	w.Header().Set("Content-Type", "video/mp4")
	// ServeContent honors Range: the player can seek forward/back instead of downloading everything.
	http.ServeContent(w, r, "recording.mp4", rec.StartedAt, obj)
}

// handleDeleteRecording deletes a recording: removes the blob from MinIO and the row. Only whoever
// started the recording or the meeting's owner can delete it (content ownership).
func (s *Server) handleDeleteRecording(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	rec, err := s.q.GetRecordingInTenant(r.Context(), db.GetRecordingInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.recording_not_found")
		return
	}
	// Ownership: whoever started it, OR the owner of the linked meeting.
	allowed := rec.StartedBy == mu.Email
	if !allowed && rec.MeetingID.Valid {
		if m, merr := s.q.GetMeetingOwned(r.Context(), db.GetMeetingOwnedParams{ID: uuid.UUID(rec.MeetingID.Bytes), OwnerUserID: mu.ID}); merr == nil {
			allowed = m.ID == uuid.UUID(rec.MeetingID.Bytes)
		}
	}
	if !allowed {
		writeErr(w, http.StatusForbidden, "meet.not_owner")
		return
	}
	if rec.BlobKey != "" && s.storage != nil {
		_ = s.storage.Remove(r.Context(), rec.BlobKey)
	}
	if err := s.q.DeleteRecording(r.Context(), db.DeleteRecordingParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.recording_delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleEgressEvent handles the recording events coming from the LiveKit webhook. First tries the
// TRACK egresses table (offline pipeline); if the egress_id isn't a track's, falls back to the
// legacy path (RoomComposite in recordings.egress_id).
func (s *Server) handleEgressEvent(ctx context.Context, event string, raw []byte) {
	var ev struct {
		EgressInfo struct {
			EgressID    string `json:"egressId"`
			Status      string `json:"status"`
			FileResults []struct {
				Filename string `json:"filename"`
				Size     string `json:"size"`
				Duration string `json:"duration"`
			} `json:"fileResults"`
		} `json:"egressInfo"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil || ev.EgressInfo.EgressID == "" {
		return
	}
	eg := ev.EgressInfo
	// No file (aborted) or failure status → mark as failed.
	failed := event == "egress_failed" || len(eg.FileResults) == 0 || eg.Status == "EGRESS_ABORTED" || eg.Status == "EGRESS_FAILED"
	if event != "egress_ended" && event != "egress_failed" {
		return
	}
	// Track egress? The final filename (extension chosen by the codec) arrives here.
	tStatus, blob := "done", ""
	if failed {
		tStatus = "failed"
	} else {
		blob = blobKeyFromFilename(eg.FileResults[0].Filename)
	}
	if n, err := s.q.FinishRecordingTrack(ctx, db.FinishRecordingTrackParams{
		EgressID: eg.EgressID, Status: tStatus, BlobKey: blob,
	}); err == nil && n > 0 {
		return
	}
	// Legado: RoomComposite.
	switch {
	case !failed:
		size, _ := strconv.ParseInt(eg.FileResults[0].Size, 10, 64)
		ns, _ := strconv.ParseInt(eg.FileResults[0].Duration, 10, 64)
		dur := int32(ns / 1_000_000_000) // ns → s
		_ = s.q.SetRecordingReady(ctx, db.SetRecordingReadyParams{EgressID: eg.EgressID, DurationSeconds: &dur, SizeBytes: &size})
	default:
		_ = s.q.SetRecordingFailed(ctx, eg.EgressID)
	}
}

// blobKeyFromFilename normalizes the EgressInfo filename into the bucket key (LiveKit can return
// either the local absolute path or one with the bucket name in front).
//
// New keys carry the tenant up front (`<tenant>/recordings/…`), so the cut is made at the UUID
// when it exists — cutting at "recordings/" would drop the prefix and the recording would stop
// being encrypted with the tenant's key.
func blobKeyFromFilename(name string) string {
	i := strings.Index(name, "recordings/")
	if i < 0 {
		return name
	}
	// Backs up to the start of the previous segment, if it's a tenant UUID.
	if i > 0 {
		anterior := strings.LastIndex(name[:i-1], "/")
		candidato := name[anterior+1 : i-1]
		if _, err := uuid.Parse(candidato); err == nil {
			return name[anterior+1:]
		}
	}
	return name[i:]
}

// recordLayout validates the RoomComposite layout requested by the host against the list
// supported by LiveKit Egress; any unknown (or empty) value falls back to "grid" so egress never fails.
func recordLayout(v string) string {
	switch v {
	case "speaker", "single-speaker", "grid":
		return v
	default:
		return "grid"
	}
}

// firstString returns the first non-empty value among the given keys of a JSON map.
func firstString(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}
