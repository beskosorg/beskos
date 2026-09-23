package wiki

import (
	"context"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Per-PAGE grants (docs/wiki-features.md §3.4/§5, Block 2 B3): a grant on the page ADDS TO/raises
// what the space already gives — it never lowers it. Managing a page's grants requires the manager
// role ON IT (which can come from the space or from a local grant). Same validation/subject as space grants.

// ListPageGrants lists a page's own access entries (role >= manager on the page).
func (s *Service) ListPageGrants(ctx context.Context, tenantID, userID, pageID uuid.UUID) ([]db.WikiPageGrant, error) {
	if err := s.requirePageRole(ctx, tenantID, userID, pageID, RoleManager); err != nil {
		return nil, err
	}
	return s.q.ListWikiPageGrants(ctx, db.ListWikiPageGrantsParams{TenantID: tenantID, PageID: pageID})
}

// SetPageGrant upserts one access entry on a page (role >= manager): clears any prior entry for the
// subject, then inserts — idempotent, one role per subject.
func (s *Service) SetPageGrant(ctx context.Context, tenantID, userID, pageID uuid.UUID, subjectType string, subjectID *uuid.UUID, role string) (db.WikiPageGrant, error) {
	if err := s.requirePageRole(ctx, tenantID, userID, pageID, RoleManager); err != nil {
		return db.WikiPageGrant{}, err
	}
	if err := validateGrant(subjectType, subjectID, role); err != nil {
		return db.WikiPageGrant{}, err
	}
	sid := nullableUUID(subjectID)
	if err := s.q.DeleteWikiPageGrantSubject(ctx, db.DeleteWikiPageGrantSubjectParams{
		TenantID: tenantID, PageID: pageID, SubjectType: subjectType, SubjectID: sid,
	}); err != nil {
		return db.WikiPageGrant{}, err
	}
	return s.q.InsertWikiPageGrant(ctx, db.InsertWikiPageGrantParams{
		TenantID: tenantID, PageID: pageID, SubjectType: subjectType, SubjectID: sid, Role: role,
	})
}

// RemovePageGrant deletes one access entry by id (role >= manager on the page).
func (s *Service) RemovePageGrant(ctx context.Context, tenantID, userID, pageID, grantID uuid.UUID) error {
	if err := s.requirePageRole(ctx, tenantID, userID, pageID, RoleManager); err != nil {
		return err
	}
	return s.q.DeleteWikiPageGrant(ctx, db.DeleteWikiPageGrantParams{ID: grantID, TenantID: tenantID})
}
