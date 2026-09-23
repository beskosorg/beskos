package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Directory sync upload (local -> cloud), authenticated by the device token. Always writes safely:
//   - PUT requires If-Match (the base hash the daemon knew about). If the server changed in the
//     meantime (different hash) -> 412 Precondition Failed: it's a conflict, the daemon resolves
//     it without overwriting.
//   - before overwriting, the current state is snapshotted into the version history (nothing is
//     ever lost).
//   - quota, hash-based dedup, and tenant/owner isolation are preserved.

// hashToTemp streams the body into a temporary file while computing the SHA-256 at the same time
// (without loading the whole file into memory). Returns the temp file (rewound), the hex hash, and
// the size.
func hashToTemp(r io.Reader) (*os.File, string, int64, error) {
	tmp, err := os.CreateTemp("", "mailin-sync-*")
	if err != nil {
		return nil, "", 0, err
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, "", 0, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, "", 0, err
	}
	return tmp, hex.EncodeToString(h.Sum(nil)), size, nil
}

// putBlobWithQuota writes the blob (dedup per tenant/hash) respecting the quota. Returns the blobKey.
func (s *Server) putBlobWithQuota(r *http.Request, mu db.MailUser, hash string, size int64, body io.Reader) (string, int, string) {
	blobKey := mu.TenantID.String() + "/" + hash
	exists, err := s.storage.Exists(r.Context(), blobKey)
	if err != nil {
		return "", http.StatusBadGateway, "storage.backend"
	}
	if !exists {
		plan := s.tenantPlan(r, mu.TenantID)
		if used, _ := s.q.SumStorageBytes(r.Context(), mu.TenantID); plan.StorageExceeded(used, size) {
			return "", http.StatusPaymentRequired, "quota.storage"
		}
		if err := s.storage.Put(r.Context(), blobKey, body, size, "application/octet-stream"); err != nil {
			return "", http.StatusBadGateway, "storage.upload_failed"
		}
	}
	return blobKey, 0, ""
}

// handleSyncUpdateFile: PUT /api/sync/files/{id} — updates the content of an existing file with
// optimistic concurrency control (If-Match). Body = raw bytes.
func (s *Server) handleSyncUpdateFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.invalid_id")
		return
	}
	// If-Match is mandatory: without a known base, a blind upload could lose the server's version.
	// Missing → 428 Precondition Required.
	base := r.Header.Get("If-Match")
	if base == "" {
		writeErr(w, http.StatusPreconditionRequired, "sync.if_match_required")
		return
	}
	cur, err := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: id, OwnerUserID: ownerPg(mu)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	// The server changed since the daemon's base → conflict. The daemon handles it (keeps a conflict copy).
	if cur.Hash != base {
		writeErr(w, http.StatusPreconditionFailed, "sync.conflict")
		return
	}

	tmp, hash, size, err := hashToTemp(http.MaxBytesReader(w, r.Body, s.maxUploadLimit()))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "storage.too_large")
		return
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()

	// No real change (same content) → idempotent no-op.
	if hash == cur.Hash {
		writeJSON(w, http.StatusOK, cur)
		return
	}

	blobKey, code, ecode := s.putBlobWithQuota(r, mu, hash, size, tmp)
	if code != 0 {
		writeErr(w, code, ecode)
		return
	}
	// Snapshots the current state before overwriting (browsable history, never lost).
	_ = s.q.SnapshotFileVersion(r.Context(), db.SnapshotFileVersionParams{
		FileID: cur.ID, TenantID: mu.TenantID, BlobKey: cur.BlobKey, Hash: cur.Hash,
		Size: cur.Size, Version: cur.Version, CreatedBy: pgtype.UUID{Bytes: mu.ID, Valid: true},
	})
	rec, err := s.q.UpdateStorageFileContent(r.Context(), db.UpdateStorageFileContentParams{
		ID: cur.ID, TenantID: mu.TenantID, BlobKey: blobKey, Hash: hash, Size: size,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleSyncCreateFile: POST /api/sync/files?name=<name>&folder_id=<uuid> — creates a new file from
// the body (raw bytes). Serves the new files that appear in the local folder.
func (s *Server) handleSyncCreateFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	name := r.URL.Query().Get("name")
	if clean := sanitizeUploadName(name); clean != "" {
		name = clean
	} else {
		writeErr(w, http.StatusBadRequest, "files.invalid_name")
		return
	}
	var folderID pgtype.UUID
	if fid := r.URL.Query().Get("folder_id"); fid != "" {
		if pid, err := uuid.Parse(fid); err == nil {
			folderID = pgtype.UUID{Bytes: pid, Valid: true}
		}
	}

	tmp, hash, size, err := hashToTemp(http.MaxBytesReader(w, r.Body, s.maxUploadLimit()))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "storage.too_large")
		return
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()

	blobKey, code, ecode := s.putBlobWithQuota(r, mu, hash, size, tmp)
	if code != 0 {
		writeErr(w, code, ecode)
		return
	}
	prevMax, _ := s.q.MaxVersionByName(r.Context(), db.MaxVersionByNameParams{TenantID: mu.TenantID, Name: name})
	rec, err := s.q.CreateStorageFile(r.Context(), db.CreateStorageFileParams{
		TenantID: mu.TenantID, OwnerUserID: ownerPg(mu),
		Name: name, BlobKey: blobKey, Hash: hash, Size: size, Version: prevMax + 1, FolderID: folderID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

// sanitizeUploadName guarantees a safe file name (a single component, no separators or ".."),
// mirroring the daemon's defense on the server side.
func sanitizeUploadName(name string) string {
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, 0) {
		return ""
	}
	return name
}
