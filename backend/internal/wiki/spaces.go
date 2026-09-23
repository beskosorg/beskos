package wiki

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// SpaceInput carries the mutable fields of a space (create/update).
type SpaceInput struct {
	Key         string
	Name        string
	Description string
	Icon        string
	Visibility  string // restricted | org | public
	AiIndexed   bool
}

var validVisibility = map[string]bool{"restricted": true, "org": true, "public": true}
var validSubjectType = map[string]bool{"user": true, "group": true, "org": true}
var validRole = map[string]bool{"viewer": true, "commenter": true, "editor": true, "manager": true}

// ListSpaces returns the spaces the caller can see (role >= viewer).
func (s *Service) ListSpaces(ctx context.Context, tenantID, userID uuid.UUID) ([]db.WikiSpace, error) {
	return s.q.ListVisibleWikiSpaces(ctx, db.ListVisibleWikiSpacesParams{TenantID: tenantID, UserID: userID})
}

// CreateSpace creates a new wiki. The caller becomes the owner (effective manager).
func (s *Service) CreateSpace(ctx context.Context, tenantID, userID uuid.UUID, in SpaceInput) (db.WikiSpace, error) {
	key := strings.TrimSpace(in.Key)
	name := strings.TrimSpace(in.Name)
	if key == "" || name == "" {
		return db.WikiSpace{}, ErrInvalid
	}
	vis := in.Visibility
	if vis == "" {
		vis = "restricted"
	}
	if !validVisibility[vis] {
		return db.WikiSpace{}, ErrInvalid
	}
	return s.q.CreateWikiSpace(ctx, db.CreateWikiSpaceParams{
		TenantID:    tenantID,
		Key:         key,
		Name:        name,
		Description: in.Description,
		Icon:        in.Icon,
		OwnerUserID: userID,
		Visibility:  vis,
	})
}

// GetSpace loads a space and the caller's effective role, enforcing at least viewer.
func (s *Service) GetSpace(ctx context.Context, tenantID, userID, spaceID uuid.UUID) (db.WikiSpace, int, error) {
	role, err := s.spaceRole(ctx, tenantID, userID, spaceID)
	if err != nil {
		return db.WikiSpace{}, 0, err
	}
	if err := authorize(role, RoleViewer); err != nil {
		return db.WikiSpace{}, 0, err
	}
	sp, err := s.q.GetWikiSpace(ctx, db.GetWikiSpaceParams{ID: spaceID, TenantID: tenantID})
	if err != nil {
		return db.WikiSpace{}, 0, ErrNotFound
	}
	return sp, role, nil
}

// UpdateSpace updates space settings; requires manager.
func (s *Service) UpdateSpace(ctx context.Context, tenantID, userID, spaceID uuid.UUID, in SpaceInput) (db.WikiSpace, error) {
	if err := s.requireSpaceRole(ctx, tenantID, userID, spaceID, RoleManager); err != nil {
		return db.WikiSpace{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || !validVisibility[in.Visibility] {
		return db.WikiSpace{}, ErrInvalid
	}
	return s.q.UpdateWikiSpace(ctx, db.UpdateWikiSpaceParams{
		ID:          spaceID,
		TenantID:    tenantID,
		Name:        name,
		Description: in.Description,
		Icon:        in.Icon,
		Visibility:  in.Visibility,
		AiIndexed:   in.AiIndexed,
	})
}

// DeleteSpace removes a space and all its pages (ON DELETE CASCADE). B4: destroying an entire wiki
// is for the space's OWNER or an organization admin only — being a (granted) manager is not enough.
// Whoever can't even see the space gets 404 (don't reveal existence); whoever sees it but can't, gets 403.
func (s *Service) DeleteSpace(ctx context.Context, tenantID, userID, spaceID uuid.UUID, isOrgAdmin bool) error {
	role, err := s.spaceRole(ctx, tenantID, userID, spaceID)
	if err != nil {
		return err
	}
	if role <= RoleNone && !isOrgAdmin {
		return ErrNotFound
	}
	sp, err := s.q.GetWikiSpace(ctx, db.GetWikiSpaceParams{ID: spaceID, TenantID: tenantID})
	if err != nil {
		return ErrNotFound
	}
	if sp.OwnerUserID != userID && !isOrgAdmin {
		return ErrForbidden
	}
	return s.q.DeleteWikiSpace(ctx, db.DeleteWikiSpaceParams{ID: spaceID, TenantID: tenantID})
}

// ---------- grants ----------

// ListSpaceGrants lists the sharing entries of a space; requires manager.
func (s *Service) ListSpaceGrants(ctx context.Context, tenantID, userID, spaceID uuid.UUID) ([]db.WikiSpaceGrant, error) {
	if err := s.requireSpaceRole(ctx, tenantID, userID, spaceID, RoleManager); err != nil {
		return nil, err
	}
	return s.q.ListWikiSpaceGrants(ctx, db.ListWikiSpaceGrantsParams{TenantID: tenantID, SpaceID: spaceID})
}

// SetSpaceGrant upserts a grant (delete existing subject entry, then insert); requires manager.
// subjectID is nil when subjectType == "org".
func (s *Service) SetSpaceGrant(ctx context.Context, tenantID, userID, spaceID uuid.UUID, subjectType string, subjectID *uuid.UUID, role string) (db.WikiSpaceGrant, error) {
	if err := s.requireSpaceRole(ctx, tenantID, userID, spaceID, RoleManager); err != nil {
		return db.WikiSpaceGrant{}, err
	}
	if err := validateGrant(subjectType, subjectID, role); err != nil {
		return db.WikiSpaceGrant{}, err
	}
	sid := nullableUUID(subjectID)
	if err := s.q.DeleteWikiSpaceGrantSubject(ctx, db.DeleteWikiSpaceGrantSubjectParams{
		TenantID: tenantID, SpaceID: spaceID, SubjectType: subjectType, SubjectID: sid,
	}); err != nil {
		return db.WikiSpaceGrant{}, err
	}
	return s.q.InsertWikiSpaceGrant(ctx, db.InsertWikiSpaceGrantParams{
		TenantID: tenantID, SpaceID: spaceID, SubjectType: subjectType, SubjectID: sid, Role: role,
	})
}

// RemoveSpaceGrant deletes a grant by id; requires manager.
func (s *Service) RemoveSpaceGrant(ctx context.Context, tenantID, userID, spaceID, grantID uuid.UUID) error {
	if err := s.requireSpaceRole(ctx, tenantID, userID, spaceID, RoleManager); err != nil {
		return err
	}
	return s.q.DeleteWikiSpaceGrant(ctx, db.DeleteWikiSpaceGrantParams{ID: grantID, TenantID: tenantID})
}

func (s *Service) requireSpaceRole(ctx context.Context, tenantID, userID, spaceID uuid.UUID, need int) error {
	role, err := s.spaceRole(ctx, tenantID, userID, spaceID)
	if err != nil {
		return err
	}
	return authorize(role, need)
}

func validateGrant(subjectType string, subjectID *uuid.UUID, role string) error {
	if !validSubjectType[subjectType] || !validRole[role] {
		return ErrInvalid
	}
	if subjectType == "org" && subjectID != nil {
		return ErrInvalid
	}
	if subjectType != "org" && subjectID == nil {
		return ErrInvalid
	}
	return nil
}

func nullableUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}
