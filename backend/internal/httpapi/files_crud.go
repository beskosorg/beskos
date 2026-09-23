package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const defaultMaxUploadBytes = 100 << 30 // 100 GiB (default if FILES_MAX_UPLOAD_BYTES isn't set)

// maxUploadLimit is the per-file ceiling (configurable via env; the plan's total quota still applies on top).
func (s *Server) maxUploadLimit() int64 {
	if s.maxUpload > 0 {
		return s.maxUpload
	}
	return defaultMaxUploadBytes
}

// handleFileLimits exposes the per-file ceiling to the frontend (for the warning before an unfeasible upload).
func (s *Server) handleFileLimits(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]int64{"max_upload_bytes": s.maxUploadLimit()})
}

func (s *Server) storageReady(w http.ResponseWriter) bool {
	if s.storage == nil {
		writeErr(w, http.StatusServiceUnavailable, "storage.disabled")
		return false
	}
	return true
}

// handleUploadFile receives a file (multipart), dedups by hash (doesn't resend identical content),
// versions by name, and saves the logical record. Tenant isolation via the key prefix.
func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadLimit())
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "storage.too_large")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.no_file")
		return
	}
	defer file.Close()
	name := sanitizeUploadName(header.Filename)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "storage.invalid_name")
		return
	}

	// Content hash (dedup) — then rewinds for the upload.
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.hash_failed")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.seek_failed")
		return
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	blobKey := mu.TenantID.String() + "/" + hash // per-tenant prefix (isolation)

	// Dedup: only sends to MinIO if the blob doesn't already exist.
	exists, err := s.storage.Exists(r.Context(), blobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.backend")
		return
	}
	// Disk quota: only counted if the blob is new (dedup doesn't consume extra space).
	if !exists {
		plan := s.tenantPlan(r, mu.TenantID)
		if used, _ := s.q.SumStorageBytes(r.Context(), mu.TenantID); plan.StorageExceeded(used, header.Size) {
			writeErr(w, http.StatusPaymentRequired, "quota.storage")
			return
		}
	}
	if !exists {
		ct := header.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		if err := s.storage.Put(r.Context(), blobKey, file, header.Size, ct); err != nil {
			writeErr(w, http.StatusBadGateway, "storage.upload_failed")
			return
		}
	}

	// Destination folder (optional): folder_id field from the form.
	var folderID pgtype.UUID
	if fid := r.FormValue("folder_id"); fid != "" {
		if id, err := uuid.Parse(fid); err == nil {
			folderID = pgtype.UUID{Bytes: id, Valid: true}
		}
	}
	// Versioning by name (same name → new version).
	prevMax, _ := s.q.MaxVersionByName(r.Context(), db.MaxVersionByNameParams{TenantID: mu.TenantID, Name: name})
	rec, err := s.q.CreateStorageFile(r.Context(), db.CreateStorageFileParams{
		TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
		Name: name, BlobKey: blobKey, Hash: hash, Size: header.Size, Version: prevMax + 1, FolderID: folderID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: mu.TenantID, Valid: true}, Actor: mu.Email, Action: "file.upload", Target: name})
	writeJSON(w, http.StatusCreated, rec)
}

func ownerPg(mu db.MailUser) pgtype.UUID { return pgtype.UUID{Bytes: mu.ID, Valid: true} }

// handleListFiles lists the current folder (?folder=<id>, absent = root): subfolders + files + breadcrumb.
func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var folderID pgtype.UUID
	if fid := r.URL.Query().Get("folder"); fid != "" {
		if id, err := uuid.Parse(fid); err == nil {
			folderID = pgtype.UUID{Bytes: id, Valid: true}
		}
	}
	folders, err := s.q.ListFolders(r.Context(), db.ListFoldersParams{OwnerUserID: mu.ID, ParentID: folderID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	files, err := s.q.ListFilesInFolder(r.Context(), db.ListFilesInFolderParams{OwnerUserID: ownerPg(mu), FolderID: folderID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	if folders == nil { // never null in the JSON (the frontend does .length)
		folders = []db.StorageFolder{}
	}
	if files == nil {
		files = []db.StorageFile{}
	}
	// Breadcrumb: walks up to the root.
	type crumb struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}
	trail := []crumb{}
	cur := folderID
	for cur.Valid {
		f, err := s.q.GetFolder(r.Context(), db.GetFolderParams{ID: cur.Bytes, OwnerUserID: mu.ID})
		if err != nil {
			break
		}
		trail = append([]crumb{{ID: f.ID, Name: f.Name}}, trail...)
		cur = f.ParentID
	}

	// IDs of files with sharing enabled (for the "shared" badge in the UI).
	shared := []uuid.UUID{}
	if ids, err := s.q.SharedFileIDs(r.Context(), mu.TenantID); err == nil {
		shared = ids
	}
	// Storage usage vs. plan quota (Drive-style indicator).
	plan := s.tenantPlan(r, mu.TenantID)
	used, _ := s.q.SumStorageBytes(r.Context(), mu.TenantID)

	writeJSON(w, http.StatusOK, map[string]any{
		"folders":    folders,
		"files":      files,
		"breadcrumb": trail,
		"shared":     shared,
		"owner":      map[string]string{"name": mu.Name, "email": mu.Email},
		"quota":      map[string]int64{"used": used, "max": plan.MaxStorageBytes},
	})
}

// likePattern turns the user's text into a LIKE pattern, escaping the wildcards so that '%' and
// '_' are treated as literal text. Shared by the UI search and by the assistant's drive.search
// tool — two copies would drift apart and one of them would silently start returning wrong results.
func likePattern(q string) string {
	esc := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(q)
	return "%" + esc + "%"
}

// handleSearchFiles: global (recursive) search by name, across all of the owner's folders.
// Replaces the client-side filter that only saw the open folder. Matches files and folders;
// returns the flat list.
func (s *Server) handleSearchFiles(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"files": []db.StorageFile{}, "folders": []db.StorageFolder{}})
		return
	}
	pattern := likePattern(q)
	files, err := s.q.SearchStorageFiles(r.Context(), db.SearchStorageFilesParams{OwnerUserID: ownerPg(mu), Name: pattern})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.search_failed")
		return
	}
	folders, err := s.q.SearchStorageFolders(r.Context(), db.SearchStorageFoldersParams{OwnerUserID: mu.ID, Name: pattern})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.search_failed")
		return
	}
	if files == nil {
		files = []db.StorageFile{}
	}
	if folders == nil {
		folders = []db.StorageFolder{}
	}
	shared := []uuid.UUID{}
	if ids, err := s.q.SharedFileIDs(r.Context(), mu.TenantID); err == nil {
		shared = ids
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"files":   files,
		"folders": folders,
		"shared":  shared,
		"owner":   map[string]string{"name": mu.Name, "email": mu.Email},
	})
}

func (s *Server) fileOwned(w http.ResponseWriter, r *http.Request) (db.StorageFile, bool) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return db.StorageFile{}, false
	}
	f, err := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: id, OwnerUserID: ownerPg(mu)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return db.StorageFile{}, false
	}
	return f, true
}

// fileForDownload resolves the file for reading: accepts the owner OR whoever received the share
// (principal = my email). Writes 404 and returns false if neither.
func (s *Server) fileForDownload(w http.ResponseWriter, r *http.Request) (db.StorageFile, bool) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return db.StorageFile{}, false
	}
	if f, err := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: id, OwnerUserID: ownerPg(mu)}); err == nil {
		return f, true
	}
	// Not the owner: accepts if it was shared directly with them OR with the whole organization.
	f, err := s.q.GetStorageFileInTenant(r.Context(), db.GetStorageFileInTenantParams{ID: id, TenantID: mu.TenantID})
	if err == nil {
		shares, _ := s.q.ListSharesByFile(r.Context(), db.ListSharesByFileParams{FileID: id, TenantID: mu.TenantID})
		if _, has := shareAccess(shares, mu.Email); has {
			return f, true
		}
	}
	writeErr(w, http.StatusNotFound, "storage.not_found")
	return db.StorageFile{}, false
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	f, ok := s.fileForDownload(w, r)
	if !ok {
		return
	}
	obj, err := s.storage.Get(r.Context(), f.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	// nosniff always: stops the browser from reinterpreting the octet-stream as HTML/JS.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// inline=1 is only honored for types safe to preview (image/pdf/audio/video). An .html/.svg
	// served inline would execute JS on our origin (stored XSS) — for those, force a download.
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

// safeInlineType reports whether a Content-Type can be shown inline without risk of script execution.
func safeInlineType(ct string) bool {
	ct = strings.ToLower(ct)
	if strings.HasPrefix(ct, "image/") && !strings.Contains(ct, "svg") {
		return true // SVG is HTML-capable: never inline
	}
	return strings.HasPrefix(ct, "audio/") || strings.HasPrefix(ct, "video/") ||
		strings.HasPrefix(ct, "application/pdf") || strings.HasPrefix(ct, "text/plain")
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	// Soft delete: goes to the trash, doesn't disappear. The blob stays until purged (emptying the
	// trash). Without this, a misclick would delete the data for good — the biggest file-loss risk
	// (Files audit finding).
	if _, err := s.q.TrashFile(r.Context(), db.TrashFileParams{ID: f.ID, OwnerUserID: ownerPg(mu)}); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "trashed"})
}

// --- Move / rename file ---

type patchFileReq struct {
	FolderID *string `json:"folder_id"` // move (null = root)
	Name     *string `json:"name"`      // rename
}

func (s *Server) handlePatchFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	var req patchFileReq
	if !decode(w, r, &req) {
		return
	}
	if req.Name != nil {
		if _, err := s.q.RenameStorageFile(r.Context(), db.RenameStorageFileParams{ID: id, OwnerUserID: ownerPg(mu), Name: *req.Name}); err != nil {
			writeErr(w, http.StatusNotFound, "storage.not_found")
			return
		}
	}
	if req.FolderID != nil {
		var folder pgtype.UUID
		if *req.FolderID != "" {
			if fid, err := uuid.Parse(*req.FolderID); err == nil {
				folder = pgtype.UUID{Bytes: fid, Valid: true}
			}
		}
		if _, err := s.q.MoveStorageFile(r.Context(), db.MoveStorageFileParams{ID: id, OwnerUserID: ownerPg(mu), FolderID: folder}); err != nil {
			writeErr(w, http.StatusNotFound, "storage.not_found")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
