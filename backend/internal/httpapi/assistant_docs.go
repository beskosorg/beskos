package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/office"
)

// assistantDocsRead reads the text content of a file from the user's Drive, at the current
// version. Ownership is the owner's (the same filter as the UI): the assistant sees what the user
// sees, no more (§6.2). The text is what the model reads before editing (§5.1).
func (s *Server) assistantDocsRead(ctx context.Context, id assistant.Identity, fileID string) (assistant.FileContent, error) {
	if s.storage == nil {
		return assistant.FileContent{}, fmt.Errorf("file storage is not configured")
	}
	fid, err := uuid.Parse(fileID)
	if err != nil {
		return assistant.FileContent{}, fmt.Errorf("invalid file id")
	}
	f, err := s.q.GetStorageFile(ctx, db.GetStorageFileParams{
		ID: fid, OwnerUserID: pgtype.UUID{Bytes: id.UserID, Valid: true},
	})
	if err != nil {
		return assistant.FileContent{}, fmt.Errorf("file not found")
	}

	kind, editable := office.KindFromName(f.Name)
	fc := assistant.FileContent{
		ID: f.ID.String(), Name: f.Name, Version: f.Version, Hash: f.Hash, Editable: editable,
	}
	if !editable {
		// Binaries (images, PDF) can't be edited blindly — they can only be replaced by a new
		// upload (§5.1). Saying so is better than returning bytes the model would interpret as garbage.
		fc.Kind = "other"
		return fc, nil
	}
	fc.Kind = kind

	rc, err := s.storage.Get(ctx, f.BlobKey)
	if err != nil {
		return assistant.FileContent{}, fmt.Errorf("read blob: %w", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, 20<<20))
	if err != nil {
		return assistant.FileContent{}, fmt.Errorf("read blob: %w", err)
	}
	text, err := office.ExtractText(kind, data)
	if err != nil {
		// The file has an Office extension but doesn't open as one. No content is invented: it
		// returns empty, and the model sees there's no text to edit.
		fc.Text = ""
		return fc, nil
	}
	fc.Text = text
	return fc, nil
}

// assistantDocsEdit saves a NEW version of a file, built in Go from the plan, but only if the
// current version still matches expectVersion (§5.1, rule 3). The five rules of §5.1:
//
//  1. read before writing — enforced in the tool (the engine requires the read marker);
//  2. every change is a new version — SnapshotFileVersion + increment, never in place;
//  3. edit on top of the version that was read — UpdateStorageFileContentIfVersion, conditional;
//  4. real permission check — the owner_user_id filter, the same as the UI;
//  5. say what changed — the summary comes back in the action card (done in the tool).
func (s *Server) assistantDocsEdit(ctx context.Context, id assistant.Identity, fileID string, plan office.Plan, expectVersion int32) (assistant.DriveHit, error) {
	if s.storage == nil {
		return assistant.DriveHit{}, fmt.Errorf("file storage is not configured")
	}
	fid, err := uuid.Parse(fileID)
	if err != nil {
		return assistant.DriveHit{}, fmt.Errorf("invalid file id")
	}
	owner := pgtype.UUID{Bytes: id.UserID, Valid: true}
	f, err := s.q.GetStorageFile(ctx, db.GetStorageFileParams{ID: fid, OwnerUserID: owner})
	if err != nil {
		return assistant.DriveHit{}, fmt.Errorf("file not found")
	}
	// Files that aren't Office files can't be edited through here (§5.1): blindly rewriting a
	// binary means losing the file.
	if _, ok := office.KindFromName(f.Name); !ok {
		return assistant.DriveHit{}, fmt.Errorf("this file type cannot be edited")
	}
	// The cheap concurrency guard first: if the version already doesn't match, don't even build
	// the file. The real, atomic check is the conditional query below.
	if f.Version != expectVersion {
		return assistant.DriveHit{}, fmt.Errorf("the file changed since you read it; read it again")
	}

	data, err := office.Build(plan)
	if err != nil {
		return assistant.DriveHit{}, fmt.Errorf("build file: %w", err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	blobKey := id.TenantID.String() + "/" + hash

	// Quota, with hash-based dedup — the same as creation (§5).
	if exists, err := s.storage.Exists(ctx, blobKey); err != nil {
		return assistant.DriveHit{}, fmt.Errorf("storage backend")
	} else if !exists {
		plan := s.tenantPlanByID(ctx, id.TenantID)
		if used, _ := s.q.SumStorageBytes(ctx, id.TenantID); used+int64(len(data)) > plan.MaxStorageBytes {
			return assistant.DriveHit{}, fmt.Errorf("storage quota exceeded")
		}
		if err := s.storage.Put(ctx, blobKey, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
			return assistant.DriveHit{}, fmt.Errorf("upload failed")
		}
	}

	// RULE 2: the current version is snapshotted BEFORE it's overwritten. This is what makes a
	// mistake reversible — a click to restore, not the file itself.
	_ = s.q.SnapshotFileVersion(ctx, db.SnapshotFileVersionParams{
		FileID: f.ID, TenantID: f.TenantID, BlobKey: f.BlobKey, Hash: f.Hash, Size: f.Size, Version: f.Version,
	})
	// RULE 3: the write is CONDITIONAL on the version. Between the GetStorageFile call and here,
	// another write may have landed; if it did, this one updates nothing and backs off, instead of
	// losing that other write's work.
	updated, err := s.q.UpdateStorageFileContentIfVersion(ctx, db.UpdateStorageFileContentIfVersionParams{
		ID: f.ID, TenantID: f.TenantID, OwnerUserID: owner,
		BlobKey: blobKey, Hash: hash, Size: int64(len(data)), ExpectedVersion: expectVersion,
	})
	if err != nil {
		return assistant.DriveHit{}, fmt.Errorf("the file changed since you read it; read it again")
	}
	_ = s.q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: id.TenantID, Valid: true},
		Actor:    id.Email, Action: "file.edit", Target: f.Name,
	})
	return assistant.DriveHit{
		ID: updated.ID.String(), Name: updated.Name, Kind: "file",
		Size: updated.Size, Version: updated.Version,
	}, nil
}
