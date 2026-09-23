package wiki

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Page attachments (docs/wiki-features.md §3.6): reuses Files (storage_files). Attaching/detaching
// requires editor; listing and downloading require viewer on the page (the same `vis` surface).

// ListAttachments returns a page's attachments (role >= viewer).
func (s *Service) ListAttachments(ctx context.Context, tenantID, userID, pageID uuid.UUID) ([]db.ListWikiAttachmentsRow, error) {
	if _, _, err := s.GetPage(ctx, tenantID, userID, pageID); err != nil {
		return nil, err
	}
	return s.q.ListWikiAttachments(ctx, db.ListWikiAttachmentsParams{PageID: pageID, TenantID: tenantID})
}

// AttachFile links a Drive file the caller owns to a page (role >= editor). Idempotent (ON CONFLICT).
func (s *Service) AttachFile(ctx context.Context, tenantID, userID, pageID, fileID uuid.UUID) error {
	if err := s.requirePageRole(ctx, tenantID, userID, pageID, RoleEditor); err != nil {
		return err
	}
	// Only attach a file the caller owns — never link by id blindly (mirrors the card attachments rule).
	if _, err := s.q.GetStorageFile(ctx, db.GetStorageFileParams{ID: fileID, OwnerUserID: pgtype.UUID{Bytes: userID, Valid: true}}); err != nil {
		return ErrNotFound
	}
	return s.q.AttachFileToWikiPage(ctx, db.AttachFileToWikiPageParams{
		TenantID: tenantID, PageID: pageID, FileID: fileID, AddedBy: pgtype.UUID{Bytes: userID, Valid: true},
	})
}

// DetachAttachment removes an attachment link (role >= editor).
func (s *Service) DetachAttachment(ctx context.Context, tenantID, userID, pageID, attID uuid.UUID) error {
	if err := s.requirePageRole(ctx, tenantID, userID, pageID, RoleEditor); err != nil {
		return err
	}
	return s.q.DetachWikiAttachment(ctx, db.DetachWikiAttachmentParams{ID: attID, TenantID: tenantID})
}

// AttachmentBlob resolves an attachment to its stored file after checking the caller can view the
// page it belongs to (role >= viewer). The handler streams the returned blob key.
func (s *Service) AttachmentBlob(ctx context.Context, tenantID, userID, attID uuid.UUID) (db.GetWikiAttachmentBlobRow, error) {
	row, err := s.q.GetWikiAttachmentBlob(ctx, db.GetWikiAttachmentBlobParams{ID: attID, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.GetWikiAttachmentBlobRow{}, ErrNotFound
	}
	if err != nil {
		return db.GetWikiAttachmentBlobRow{}, err
	}
	if _, _, err := s.GetPage(ctx, tenantID, userID, row.PageID); err != nil {
		return db.GetWikiAttachmentBlobRow{}, err
	}
	return row, nil
}
