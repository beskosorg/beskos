package httpapi

import (
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Card attachments: links Files files to a card. Download is tenant-scoped (any board member can
// open it), unlike personal Files. Attaching requires owning the file.

func (s *Server) handleListCardAttachments(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	c, ok := s.cardOr404(w, r)
	if !ok {
		return
	}
	rows, _ := s.q.ListCardAttachments(r.Context(), db.ListCardAttachmentsParams{CardID: c.ID, TenantID: mu.TenantID})
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

func (s *Server) handleAttachFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	c, ok := s.cardOr404(w, r)
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
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	// Only attaches a file the user owns (avoids attaching blindly by id).
	if _, err := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: fid, OwnerUserID: ownerPg(mu)}); err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	if err := s.q.AttachFileToCard(r.Context(), db.AttachFileToCardParams{
		TenantID: mu.TenantID, CardID: c.ID, FileID: fid, AddedBy: pgUUID(mu.ID),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "cards.attach_failed")
		return
	}
	s.logCard(r.Context(), mu.TenantID, c.ID, mu.ID, "attached", "")
	rows, _ := s.q.ListCardAttachments(r.Context(), db.ListCardAttachmentsParams{CardID: c.ID, TenantID: mu.TenantID})
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

func (s *Server) handleDetachFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	if _, ok := s.cardOr404(w, r); !ok {
		return
	}
	attID, err := uuid.Parse(chi.URLParam(r, "attId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	if err := s.q.DetachFile(r.Context(), db.DetachFileParams{ID: attID, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "cards.detach_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "detached"})
}

// handleDownloadCardAttachment serves the attachment blob, tenant-scoped (not owner-scoped): any
// board member can open it. The attachment authorizes — the file must be linked to a tenant card.
func (s *Server) handleDownloadCardAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	if _, ok := s.cardOr404(w, r); !ok {
		return
	}
	attID, err := uuid.Parse(chi.URLParam(r, "attId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	f, err := s.q.GetCardAttachmentBlob(r.Context(), db.GetCardAttachmentBlobParams{ID: attID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
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
