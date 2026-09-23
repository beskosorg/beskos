package wiki

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Public read-only access (docs/wiki-features.md §5.4): a space with visibility=public becomes
// reachable via an anonymous link /w/<public_slug>. The public route resolves the tenant by the
// slug (never from the request), serves only published pages, and never exposes
// edit/history/comments/attachments/RAG.

// SetPublicAccess turns anonymous read-only access on/off for a space (role >= manager). Enabling
// mints a fresh, hard-to-enumerate slug; regenerate mints a new one (invalidating the old link);
// disabling clears the slug and reverts visibility to restricted.
func (s *Service) SetPublicAccess(ctx context.Context, tenantID, userID, spaceID uuid.UUID, enabled, regenerate bool) (db.WikiSpace, error) {
	sp, _, err := s.requireSpaceManager(ctx, tenantID, userID, spaceID)
	if err != nil {
		return db.WikiSpace{}, err
	}
	if !enabled {
		return s.q.SetWikiSpacePublic(ctx, db.SetWikiSpacePublicParams{
			Visibility: "restricted", PublicSlug: nil, ID: spaceID, TenantID: tenantID,
		})
	}
	slug := ""
	if sp.PublicSlug != nil && !regenerate {
		slug = *sp.PublicSlug
	} else {
		slug = publicSlug(sp.Key)
	}
	return s.q.SetWikiSpacePublic(ctx, db.SetWikiSpacePublicParams{
		Visibility: "public", PublicSlug: &slug, ID: spaceID, TenantID: tenantID,
	})
}

// requireSpaceManager loads a space and enforces the manager role.
func (s *Service) requireSpaceManager(ctx context.Context, tenantID, userID, spaceID uuid.UUID) (db.WikiSpace, int, error) {
	role, err := s.spaceRole(ctx, tenantID, userID, spaceID)
	if err != nil {
		return db.WikiSpace{}, 0, err
	}
	if err := authorize(role, RoleManager); err != nil {
		return db.WikiSpace{}, role, err
	}
	sp, err := s.q.GetWikiSpace(ctx, db.GetWikiSpaceParams{ID: spaceID, TenantID: tenantID})
	if err != nil {
		return db.WikiSpace{}, role, ErrNotFound
	}
	return sp, role, nil
}

// PublicSpace resolves a public space by its anonymous slug and returns it with its published-only
// page tree. No authentication — the tenant comes from the resolved row.
func (s *Service) PublicSpace(ctx context.Context, slug string) (db.WikiSpace, []*TreeNode, error) {
	sp, err := s.q.GetWikiSpaceByPublicSlug(ctx, &slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.WikiSpace{}, nil, ErrNotFound
	}
	if err != nil {
		return db.WikiSpace{}, nil, err
	}
	pages, err := s.q.ListWikiPages(ctx, db.ListWikiPagesParams{
		TenantID: sp.TenantID, SpaceID: sp.ID, IncludeDrafts: false,
	})
	if err != nil {
		return db.WikiSpace{}, nil, err
	}
	return sp, buildTree(pages), nil
}

// PublicPage returns one published page of a public space (anonymous). Drafts and non-public spaces
// resolve to ErrNotFound — never reveal existence.
func (s *Service) PublicPage(ctx context.Context, slug, pageSlug string) (db.WikiSpace, db.WikiPage, error) {
	sp, err := s.q.GetWikiSpaceByPublicSlug(ctx, &slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.WikiSpace{}, db.WikiPage{}, ErrNotFound
	}
	if err != nil {
		return db.WikiSpace{}, db.WikiPage{}, err
	}
	page, err := s.q.GetWikiPageBySlug(ctx, db.GetWikiPageBySlugParams{
		TenantID: sp.TenantID, SpaceID: sp.ID, Slug: pageSlug,
	})
	if err != nil || page.Status != "published" {
		return db.WikiSpace{}, db.WikiPage{}, ErrNotFound
	}
	return sp, page, nil
}

// publicSlug builds an unguessable, URL-safe slug from the space key plus random entropy, so public
// links can't be enumerated and won't collide across tenants.
func publicSlug(base string) string {
	var buf [5]byte
	_, _ = rand.Read(buf[:])
	prefix := slugify(base)
	if prefix == "" {
		prefix = "wiki"
	}
	return prefix + "-" + hex.EncodeToString(buf[:])
}
