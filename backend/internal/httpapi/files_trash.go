package httpapi

import (
	"fmt"
	"io"
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// handleListTrash lists the files in the owner's trash (restorable).
func (s *Server) handleListTrash(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	files, err := s.q.ListTrashedFiles(r.Context(), ownerPg(mu))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	if files == nil {
		files = []db.StorageFile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "owner": map[string]string{"name": mu.Name, "email": mu.Email}})
}

// handleRestoreFile takes the file out of the trash (becomes active again).
func (s *Server) handleRestoreFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	f, err := s.q.RestoreFile(r.Context(), db.RestoreFileParams{ID: id, OwnerUserID: ownerPg(mu)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// handlePurgeFile permanently deletes a file (only makes sense from the trash) and frees the blob
// if no other record (dedup/version) still references it. Irreversible.
func (s *Server) handlePurgeFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	s.purgeFileAndBlobs(r, mu, f)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// purgeFileAndBlobs permanently deletes the file and frees the blobs (the file's own and the
// versions') that NO other record references. It collects the version blobs BEFORE the delete (the
// cascade removes the rows), and uses CountBlobRefsAll to avoid deleting content still shared via
// dedup.
func (s *Server) purgeFileAndBlobs(r *http.Request, mu db.MailUser, f db.StorageFile) {
	cands := map[string]bool{f.BlobKey: true}
	if vblobs, err := s.q.ListVersionBlobs(r.Context(), db.ListVersionBlobsParams{FileID: f.ID, TenantID: f.TenantID}); err == nil {
		for _, b := range vblobs {
			cands[b] = true
		}
	}
	if err := s.q.DeleteStorageFile(r.Context(), db.DeleteStorageFileParams{ID: f.ID, OwnerUserID: ownerPg(mu)}); err != nil {
		return
	}
	for b := range cands {
		if n, _ := s.q.CountBlobRefsAll(r.Context(), db.CountBlobRefsAllParams{TenantID: f.TenantID, BlobKey: b}); n == 0 {
			_ = s.storage.Remove(r.Context(), b)
		}
	}
}

// handleEmptyTrash purges ALL files in the owner's trash, freeing the orphaned blobs.
func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	files, err := s.q.ListTrashedFiles(r.Context(), ownerPg(mu))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	for _, f := range files {
		s.purgeFileAndBlobs(r, mu, f)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "emptied", "count": len(files)})
}

// handleListVersions lists a file's version history (most recent first).
func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	vs, err := s.q.ListFileVersions(r.Context(), db.ListFileVersionsParams{FileID: f.ID, TenantID: f.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"current_version": f.Version, "versions": orEmpty(vs)})
}

// handleRestoreVersion reverts the file to the content of an older version. It snapshots the
// current version first (restoring is itself a new version — reversible).
func (s *Server) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	vid, err := uuid.Parse(chi.URLParam(r, "versionId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	v, err := s.q.GetFileVersion(r.Context(), db.GetFileVersionParams{ID: vid, FileID: f.ID, TenantID: f.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	_ = s.q.SnapshotFileVersion(r.Context(), db.SnapshotFileVersionParams{
		FileID: f.ID, TenantID: f.TenantID, BlobKey: f.BlobKey, Hash: f.Hash, Size: f.Size, Version: f.Version,
	})
	updated, err := s.q.UpdateStorageFileContent(r.Context(), db.UpdateStorageFileContentParams{
		ID: f.ID, TenantID: f.TenantID, BlobKey: v.BlobKey, Hash: v.Hash, Size: v.Size,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleDownloadVersion serves the content of an older version (to inspect before restoring).
func (s *Server) handleDownloadVersion(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	vid, err := uuid.Parse(chi.URLParam(r, "versionId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	v, err := s.q.GetFileVersion(r.Context(), db.GetFileVersionParams{ID: vid, FileID: f.ID, TenantID: f.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	obj, err := s.storage.Get(r.Context(), v.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	// Older version: always a download (never inline), with the name suffixed by the version.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("v%d-%s", v.Version, f.Name)))
	_, _ = io.Copy(w, obj)
}
