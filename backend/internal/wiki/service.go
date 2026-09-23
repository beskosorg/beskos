// Package wiki is the knowledge-base service (Confluence-style): N spaces per tenant, pages in a tree,
// version history, and per person/group/org grants. It sits between the HTTP handlers and the db (sqlc)
// layer (law 1.3). Access is resolved in SQL (WikiSpaceRole/WikiPageRole, the `vis` pattern, §5.3) and
// enforced here before any read or write; every query is scoped to the caller's tenant (law 5.1).
package wiki

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Role ranks — mirror the CASE mapping in queries_wiki.sql. Higher = more capable.
const (
	RoleNone      = 0
	RoleViewer    = 1
	RoleCommenter = 2
	RoleEditor    = 3
	RoleManager   = 4
)

// Sentinel errors; handlers map these to HTTP status + stable error_code (law 4.4).
var (
	ErrNotFound        = errors.New("wiki: not found")        // -> 404 wiki.not_found
	ErrForbidden       = errors.New("wiki: forbidden")        // -> 403 wiki.forbidden
	ErrVersionConflict = errors.New("wiki: version conflict") // -> 409 wiki.version_conflict
	ErrInvalid         = errors.New("wiki: invalid input")    // -> 400 wiki.invalid
)

// Service holds the sqlc queries. It is safe for concurrent use (db.Queries is).
type Service struct {
	q *db.Queries
}

func New(q *db.Queries) *Service { return &Service{q: q} }

// spaceRole returns the caller's effective role rank on a space (0 = no access).
func (s *Service) spaceRole(ctx context.Context, tenantID, userID, spaceID uuid.UUID) (int, error) {
	r, err := s.q.WikiSpaceRole(ctx, db.WikiSpaceRoleParams{
		UserID: userID, SpaceID: spaceID, TenantID: tenantID,
	})
	if err != nil {
		return 0, err
	}
	return int(r), nil
}

// pageRole returns the caller's effective role rank on a page (0 = no access).
func (s *Service) pageRole(ctx context.Context, tenantID, userID, pageID uuid.UUID) (int, error) {
	r, err := s.q.WikiPageRole(ctx, db.WikiPageRoleParams{
		UserID: userID, PageID: pageID, TenantID: tenantID,
	})
	if err != nil {
		return 0, err
	}
	return int(r), nil
}

// PageRole exposes the effective role on a page (for gates outside the service, e.g. the
// collaboration WS).
func (s *Service) PageRole(ctx context.Context, tenantID, userID, pageID uuid.UUID) (int, error) {
	return s.pageRole(ctx, tenantID, userID, pageID)
}

// authorize turns a role rank into an error: 0 -> 404 (don't reveal existence), below need -> 403.
func authorize(have, need int) error {
	if have <= RoleNone {
		return ErrNotFound
	}
	if have < need {
		return ErrForbidden
	}
	return nil
}
