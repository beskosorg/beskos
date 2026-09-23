package wiki

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Page comments (docs/wiki-features.md §3.5). Reading comments requires >= viewer (see the page);
// commenting requires >= commenter; deleting is up to the author or a manager of the page.

// ListComments returns a page's comments with author name/email (role >= viewer).
func (s *Service) ListComments(ctx context.Context, tenantID, userID, pageID uuid.UUID) ([]db.ListWikiCommentsRow, error) {
	if _, _, err := s.GetPage(ctx, tenantID, userID, pageID); err != nil {
		return nil, err
	}
	return s.q.ListWikiComments(ctx, db.ListWikiCommentsParams{TenantID: tenantID, PageID: pageID})
}

// AddComment posts a rich comment on a page (role >= commenter). body_json (BlockNote blocks) is
// the source; text and HTML are derived here — the same safe path as pages.
func (s *Service) AddComment(ctx context.Context, tenantID, userID, pageID uuid.UUID, bodyJSON json.RawMessage) (db.WikiComment, error) {
	if err := s.requirePageRole(ctx, tenantID, userID, pageID, RoleCommenter); err != nil {
		return db.WikiComment{}, err
	}
	body := normalizeBody(bodyJSON)
	text, html := derive(body)
	if strings.TrimSpace(text) == "" {
		return db.WikiComment{}, ErrInvalid
	}
	return s.q.CreateWikiComment(ctx, db.CreateWikiCommentParams{
		TenantID: tenantID, PageID: pageID, AuthorUserID: userID,
		Body: text, BodyJson: body, BodyHtml: html,
	})
}

// DeleteComment removes a comment: the author may delete their own; a page manager may delete any.
func (s *Service) DeleteComment(ctx context.Context, tenantID, userID, commentID uuid.UUID) error {
	c, err := s.q.GetWikiComment(ctx, db.GetWikiCommentParams{ID: commentID, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if c.AuthorUserID != userID {
		if err := s.requirePageRole(ctx, tenantID, userID, c.PageID, RoleManager); err != nil {
			return err
		}
	} else {
		// The author still needs to at least see the page (guards against a stale/forbidden context).
		if _, _, err := s.GetPage(ctx, tenantID, userID, c.PageID); err != nil {
			return err
		}
	}
	return s.q.DeleteWikiComment(ctx, db.DeleteWikiCommentParams{ID: commentID, TenantID: tenantID})
}
