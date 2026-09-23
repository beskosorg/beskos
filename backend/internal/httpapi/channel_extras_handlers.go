package httpapi

import (
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// isoPtr formats a nullable timestamp (nil when absent).
func isoPtr(t pgtype.Timestamptz) any {
	if !t.Valid {
		return nil
	}
	return t.Time.UTC().Format(time.RFC3339)
}

// Slack-style channel extras: canvas (shared doc), pins, and the file and link views.

// ---- Canvas ----

func (s *Server) handleGetCanvas(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	ch, err := s.q.GetChannel(r.Context(), db.GetChannelParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"canvas": ch.Canvas, "updated_at": isoPtr(ch.CanvasUpdatedAt)})
}

func (s *Server) handleSaveCanvas(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	var req struct {
		Canvas string `json:"canvas"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.q.UpdateChannelCanvas(r.Context(), db.UpdateChannelCanvasParams{ID: id, TenantID: mu.TenantID, Canvas: req.Canvas}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	s.publish(r.Context(), "canvas", id, s.channelRecipients(r.Context(), id), map[string]any{"channel_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- Pins ----

func (s *Server) handlePinMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	mid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	msg, err := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: mid, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	if _, ok := s.memberOr403(w, r, msg.ChannelID); !ok {
		return
	}
	var req struct {
		Pinned bool `json:"pinned"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.q.SetMessagePinned(r.Context(), db.SetMessagePinnedParams{ID: mid, Column2: req.Pinned}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	s.publish(r.Context(), "pins", msg.ChannelID, s.channelRecipients(r.Context(), msg.ChannelID), map[string]any{"channel_id": msg.ChannelID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListPins(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	rows, _ := s.q.ListPinnedByChannel(r.Context(), id)
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, map[string]any{
			"id": m.ID, "body": m.Body, "author": authorName(m.AuthorName, m.AuthorEmail),
			"created_at": iso(m.CreatedAt), "pinned_at": isoPtr(m.PinnedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"pins": out})
}

// ---- Bookmarks (curated files & links) ----

func (s *Server) handleListBookmarks(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	rows, _ := s.q.ListChannelBookmarks(r.Context(), id)
	out := make([]map[string]any, 0, len(rows))
	for _, b := range rows {
		out = append(out, map[string]any{
			"id": b.ID, "kind": b.Kind, "title": b.Title, "url": b.Url,
			"file_name": b.FileName, "file_size": b.FileSize, "created_at": iso(b.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"bookmarks": out})
}

func (s *Server) handleAddBookmark(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	var req struct {
		Kind     string `json:"kind"`
		Title    string `json:"title"`
		URL      string `json:"url"`
		FileID   string `json:"file_id"`
		FileName string `json:"file_name"`
		FileSize int64  `json:"file_size"`
	}
	if !decode(w, r, &req) {
		return
	}
	var fileID pgtype.UUID
	if req.Kind == "file" {
		fid, perr := uuid.Parse(req.FileID)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "chat.invalid")
			return
		}
		fileID = pgtype.UUID{Bytes: fid, Valid: true}
	} else if req.URL == "" {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	b, err := s.q.CreateChannelBookmark(r.Context(), db.CreateChannelBookmarkParams{
		ChannelID: id, TenantID: mu.TenantID, Kind: req.Kind, Title: req.Title, Url: req.URL,
		FileID: fileID, FileName: req.FileName, FileSize: req.FileSize, AddedBy: mu.ID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	s.publish(r.Context(), "bookmarks", id, s.channelRecipients(r.Context(), id), map[string]any{"channel_id": id})
	writeJSON(w, http.StatusCreated, map[string]any{"id": b.ID})
}

func (s *Server) handleDeleteBookmark(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	bid, err2 := uuid.Parse(chi.URLParam(r, "bid"))
	if err != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	if err := s.q.DeleteChannelBookmark(r.Context(), db.DeleteChannelBookmarkParams{ID: bid, ChannelID: id}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	s.publish(r.Context(), "bookmarks", id, s.channelRecipients(r.Context(), id), map[string]any{"channel_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleBookmarkDownload serves the file behind a bookmark — authorized by channel MEMBERSHIP
// (any member can download it, even if they don't own the file).
func (s *Server) handleBookmarkDownload(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	bid, err2 := uuid.Parse(chi.URLParam(r, "bid"))
	if err != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	b, err := s.q.GetChannelBookmark(r.Context(), db.GetChannelBookmarkParams{ID: bid, ChannelID: id})
	if err != nil || b.BlobKey == nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	obj, err := s.storage.Get(r.Context(), *b.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+b.FileName+"\"")
	_, _ = io.Copy(w, obj)
}

// ---- Channel notifications + search ----

func (s *Server) handleSetChannelNotifPref(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	var req struct {
		Pref string `json:"pref"`
	}
	if !decode(w, r, &req) {
		return
	}
	pref := "all"
	if req.Pref == "mentions" || req.Pref == "none" {
		pref = req.Pref
	}
	if err := s.q.SetChannelNotifPref(r.Context(), db.SetChannelNotifPrefParams{ChannelID: id, UserID: mu.ID, NotifPref: pref}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"pref": pref})
}

func (s *Server) handleSearchChannelMessages(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}
	rows, _ := s.q.SearchMessagesInChannel(r.Context(), db.SearchMessagesInChannelParams{ChannelID: id, PlaintoTsquery: q})
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, map[string]any{"id": m.ID, "body": m.Body, "author": authorName(m.AuthorName, m.AuthorEmail), "created_at": iso(m.CreatedAt)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

func authorName(name, email string) string {
	if name != "" {
		return name
	}
	return email
}
