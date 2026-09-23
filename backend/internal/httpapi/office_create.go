package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

//go:embed templates/empty.docx templates/empty.xlsx templates/empty.pptx
var officeTemplates embed.FS

// new document kind → (extension, embedded template file).
var newDocKinds = map[string]struct{ ext, tmpl string }{
	"document":     {"docx", "templates/empty.docx"},
	"spreadsheet":  {"xlsx", "templates/empty.xlsx"},
	"presentation": {"pptx", "templates/empty.pptx"},
}

type createOfficeReq struct {
	Kind     string `json:"kind"` // document | spreadsheet | presentation
	Name     string `json:"name"`
	FolderID string `json:"folder_id"`
}

// handleCreateOfficeFile creates a BLANK office document from an embedded template, writes it to
// MinIO, and registers the file. The frontend then opens the editor (Collabora/OnlyOffice).
func (s *Server) handleCreateOfficeFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	var req createOfficeReq
	if !decode(w, r, &req) {
		return
	}
	kind, ok := newDocKinds[req.Kind]
	if !ok {
		writeErr(w, http.StatusBadRequest, "files.invalid_kind")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "files.name_required")
		return
	}
	if !strings.HasSuffix(strings.ToLower(name), "."+kind.ext) {
		name += "." + kind.ext
	}

	data, err := officeTemplates.ReadFile(kind.tmpl)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "files.template_missing")
		return
	}
	mu := mailUserFrom(r.Context())

	var folderID pgtype.UUID
	if req.FolderID != "" {
		if id, err := uuid.Parse(req.FolderID); err == nil {
			folderID = pgtype.UUID{Bytes: id, Valid: true}
		}
	}
	rec, code := s.storeUserFile(r.Context(), mu, name, data, folderID)
	if code != "" {
		writeErr(w, storageStatusFor(code), code)
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

// storeUserFile writes a blob to the user's storage and registers the file. Shared between
// creating blank files (the UI) and generation by the assistant (docs.create): the quota,
// hash-based dedup, and registration logic is the same, and two copies would silently diverge (§5).
//
// Returns an error_code instead of writing to the response, so it can also serve callers that do
// NOT have an http.ResponseWriter — such as an assistant tool.
func (s *Server) storeUserFile(ctx context.Context, mu db.MailUser, name string, data []byte, folderID pgtype.UUID) (db.StorageFile, string) {
	if s.storage == nil {
		return db.StorageFile{}, "storage.disabled"
	}
	// Disk quota (only counted if the blob is new — dedup by hash).
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	blobKey := mu.TenantID.String() + "/" + hash
	exists, err := s.storage.Exists(ctx, blobKey)
	if err != nil {
		return db.StorageFile{}, "storage.backend"
	}
	if !exists {
		plan := s.tenantPlanByID(ctx, mu.TenantID)
		if used, _ := s.q.SumStorageBytes(ctx, mu.TenantID); used+int64(len(data)) > plan.MaxStorageBytes {
			return db.StorageFile{}, "quota.storage"
		}
		if err := s.storage.Put(ctx, blobKey, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
			return db.StorageFile{}, "storage.upload_failed"
		}
	}
	rec, err := s.q.CreateStorageFile(ctx, db.CreateStorageFileParams{
		TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
		Name: name, BlobKey: blobKey, Hash: hash, Size: int64(len(data)), Version: 1, FolderID: folderID,
	})
	if err != nil {
		return db.StorageFile{}, "storage.record_failed"
	}
	_ = s.q.InsertAudit(ctx, db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: mu.TenantID, Valid: true}, Actor: mu.Email, Action: "file.create", Target: name})
	return rec, ""
}

// storageStatusFor maps the error_code to an HTTP status. Quota is 402, storage being down is
// 502, everything else is 500 — this is how the UI tells "out of space" from "outage" apart.
func storageStatusFor(code string) int {
	switch code {
	case "quota.storage":
		return http.StatusPaymentRequired
	case "storage.backend", "storage.upload_failed":
		return http.StatusBadGateway
	case "storage.disabled":
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
