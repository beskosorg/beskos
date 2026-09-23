package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ---------- attachments (reuse of the Files storage) ----------

func (s *Server) handleListWikiAttachments(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	rows, err := s.wiki.ListAttachments(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

func (s *Server) handleAttachWikiFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		FileID string `json:"file_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	fid, err := uuid.Parse(req.FileID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "wiki.invalid")
		return
	}
	if err := s.wiki.AttachFile(r.Context(), mu.TenantID, mu.ID, id, fid); err != nil {
		writeWikiErr(w, err)
		return
	}
	rows, err := s.wiki.ListAttachments(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

func (s *Server) handleDetachWikiFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	attID, err := uuid.Parse(chi.URLParam(r, "attId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "wiki.invalid")
		return
	}
	if err := s.wiki.DetachAttachment(r.Context(), mu.TenantID, mu.ID, id, attID); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "detached"})
}

// handleDownloadWikiAttachment streams an attachment blob after the service verifies page access.
func (s *Server) handleDownloadWikiAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	attID, err := uuid.Parse(chi.URLParam(r, "attId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "wiki.invalid")
		return
	}
	f, err := s.wiki.AttachmentBlob(r.Context(), mu.TenantID, mu.ID, attID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	obj, err := s.storage.Get(r.Context(), f.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	ct := mime.TypeByExtension("." + extOf(f.Name))
	if r.URL.Query().Get("inline") == "1" && safeInlineType(ct) {
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", f.Name))
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.Name))
	}
	_, _ = io.Copy(w, obj)
}

// ---------- comments ----------

func (s *Server) handleListWikiComments(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	comments, err := s.wiki.ListComments(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(comments))
}

func (s *Server) handleCreateWikiComment(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		BodyJSON json.RawMessage `json:"body_json"`
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := s.wiki.AddComment(r.Context(), mu.TenantID, mu.ID, id, req.BodyJSON)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleDeleteWikiComment(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	if err := s.wiki.DeleteComment(r.Context(), mu.TenantID, mu.ID, id); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
