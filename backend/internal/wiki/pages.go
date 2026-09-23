package wiki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// PageInput carries the mutable fields of a page. body_json (ProseMirror/TipTap) is the source of
// truth; body_text and body_html are derived here on write (render.go, §4.3) so the stored projection
// is always consistent with the document.
type PageInput struct {
	ParentID *uuid.UUID
	Title    string
	BodyJSON json.RawMessage
	Status   string // draft | published
}

func normStatus(v string) string {
	if v == "published" {
		return "published"
	}
	return "draft"
}

// TreeNode is a lightweight sidebar node (no bodies) built from the flat page list.
type TreeNode struct {
	ID       uuid.UUID   `json:"id"`
	ParentID *uuid.UUID  `json:"parent_id"`
	Title    string      `json:"title"`
	Slug     string      `json:"slug"`
	Position int32       `json:"position"`
	Status   string      `json:"status"`
	Children []*TreeNode `json:"children"`
}

// Tree returns the page hierarchy of a space. Drafts are included only when the caller can edit
// (role >= editor); others see published pages only.
func (s *Service) Tree(ctx context.Context, tenantID, userID, spaceID uuid.UUID) ([]*TreeNode, error) {
	role, err := s.spaceRole(ctx, tenantID, userID, spaceID)
	if err != nil {
		return nil, err
	}
	if err := authorize(role, RoleViewer); err != nil {
		return nil, err
	}
	pages, err := s.q.ListWikiPages(ctx, db.ListWikiPagesParams{
		TenantID: tenantID, SpaceID: spaceID, IncludeDrafts: role >= RoleEditor,
	})
	if err != nil {
		return nil, err
	}
	return buildTree(pages), nil
}

// buildTree nests a flat, position-ordered page list into a parent/child tree.
func buildTree(pages []db.WikiPage) []*TreeNode {
	nodes := make(map[uuid.UUID]*TreeNode, len(pages))
	for _, p := range pages {
		nodes[p.ID] = &TreeNode{
			ID: p.ID, ParentID: uuidPtr(p.ParentID), Title: p.Title, Slug: p.Slug,
			Position: p.Position, Status: p.Status, Children: []*TreeNode{},
		}
	}
	roots := []*TreeNode{}
	for _, p := range pages { // SQL already orders by (position, title)
		n := nodes[p.ID]
		if parent := n.ParentID; parent != nil {
			if pn, ok := nodes[*parent]; ok {
				pn.Children = append(pn.Children, n)
				continue
			}
		}
		roots = append(roots, n)
	}
	return roots
}

// GetPage loads a page for reading (role >= viewer). A draft is hidden from non-editors.
func (s *Service) GetPage(ctx context.Context, tenantID, userID, pageID uuid.UUID) (db.WikiPage, int, error) {
	role, err := s.pageRole(ctx, tenantID, userID, pageID)
	if err != nil {
		return db.WikiPage{}, 0, err
	}
	if err := authorize(role, RoleViewer); err != nil {
		return db.WikiPage{}, 0, err
	}
	page, err := s.q.GetWikiPage(ctx, db.GetWikiPageParams{ID: pageID, TenantID: tenantID})
	if err != nil {
		return db.WikiPage{}, 0, ErrNotFound
	}
	if page.Status == "draft" && role < RoleEditor {
		return db.WikiPage{}, 0, ErrNotFound
	}
	return page, role, nil
}

// CreatePage adds a page to a space (role >= editor). The slug is derived from the title and made
// unique within the space; body_text/body_html are derived from body_json.
func (s *Service) CreatePage(ctx context.Context, tenantID, userID, spaceID uuid.UUID, in PageInput) (db.WikiPage, error) {
	if err := s.requireSpaceRole(ctx, tenantID, userID, spaceID, RoleEditor); err != nil {
		return db.WikiPage{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return db.WikiPage{}, ErrInvalid
	}
	slug, err := s.uniqueSlug(ctx, tenantID, spaceID, title)
	if err != nil {
		return db.WikiPage{}, err
	}
	body := normalizeBody(in.BodyJSON)
	text, html := derive(body)
	return s.q.CreateWikiPage(ctx, db.CreateWikiPageParams{
		TenantID: tenantID, SpaceID: spaceID, ParentID: nullableUUID(in.ParentID),
		Title: title, Slug: slug, BodyJson: body, BodyText: text, BodyHtml: html,
		Position: s.nextPosition(ctx, tenantID, spaceID, in.ParentID), Status: normStatus(in.Status),
		AuthorUserID: userID,
	})
}

// UpdatePage saves a new revision (role >= editor). Optimistic lock: when expectedVersion no longer
// matches, the UPDATE touches 0 rows and we return ErrVersionConflict (§6). On success the previous
// revision is snapshotted into wiki_page_versions. The slug is kept stable so internal links survive.
func (s *Service) UpdatePage(ctx context.Context, tenantID, userID, pageID uuid.UUID, in PageInput, expectedVersion int32) (db.WikiPage, error) {
	if err := s.requirePageRole(ctx, tenantID, userID, pageID, RoleEditor); err != nil {
		return db.WikiPage{}, err
	}
	prev, err := s.q.GetWikiPage(ctx, db.GetWikiPageParams{ID: pageID, TenantID: tenantID})
	if err != nil {
		return db.WikiPage{}, ErrNotFound
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return db.WikiPage{}, ErrInvalid
	}
	body := normalizeBody(in.BodyJSON)
	text, html := derive(body)
	updated, err := s.q.UpdateWikiPage(ctx, db.UpdateWikiPageParams{
		Title: title, Slug: prev.Slug, BodyJson: body, BodyText: text, BodyHtml: html,
		Status: normStatus(in.Status), AuthorUserID: userID,
		ID: pageID, TenantID: tenantID, ExpectedVersion: expectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.WikiPage{}, ErrVersionConflict
	}
	if err != nil {
		return db.WikiPage{}, err
	}
	// Snapshot the pre-update revision for history/diff (the save already succeeded, so best-effort).
	_ = s.q.InsertWikiPageVersion(ctx, db.InsertWikiPageVersionParams{
		TenantID: tenantID, PageID: pageID, Version: prev.Version, Title: prev.Title,
		BodyJson: prev.BodyJson, BodyText: prev.BodyText,
		AuthorUserID: pgtype.UUID{Bytes: prev.AuthorUserID, Valid: true},
	})
	return updated, nil
}

// DeletePage removes a page and its subtree (role >= manager; FK cascade handles children/versions).
func (s *Service) DeletePage(ctx context.Context, tenantID, userID, pageID uuid.UUID) error {
	if err := s.requirePageRole(ctx, tenantID, userID, pageID, RoleManager); err != nil {
		return err
	}
	return s.q.DeleteWikiPage(ctx, db.DeleteWikiPageParams{ID: pageID, TenantID: tenantID})
}

// MovePage reparents/reorders a page within its space (role >= manager).
func (s *Service) MovePage(ctx context.Context, tenantID, userID, pageID uuid.UUID, parent *uuid.UUID, position int32) error {
	if err := s.requirePageRole(ctx, tenantID, userID, pageID, RoleManager); err != nil {
		return err
	}
	return s.q.MoveWikiPage(ctx, db.MoveWikiPageParams{
		ParentID: nullableUUID(parent), Position: position, ID: pageID, TenantID: tenantID,
	})
}

// ListVersions returns the revision history metadata of a page (role >= viewer).
func (s *Service) ListVersions(ctx context.Context, tenantID, userID, pageID uuid.UUID) ([]db.ListWikiPageVersionsRow, error) {
	if _, _, err := s.GetPage(ctx, tenantID, userID, pageID); err != nil {
		return nil, err
	}
	return s.q.ListWikiPageVersions(ctx, db.ListWikiPageVersionsParams{TenantID: tenantID, PageID: pageID})
}

// GetVersion returns one historical revision in full (role >= viewer), for diff/preview.
func (s *Service) GetVersion(ctx context.Context, tenantID, userID, pageID uuid.UUID, version int32) (db.WikiPageVersion, error) {
	if _, _, err := s.GetPage(ctx, tenantID, userID, pageID); err != nil {
		return db.WikiPageVersion{}, err
	}
	v, err := s.q.GetWikiPageVersion(ctx, db.GetWikiPageVersionParams{TenantID: tenantID, PageID: pageID, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.WikiPageVersion{}, ErrNotFound
	}
	return v, err
}

// RestoreVersion writes a past revision back as a NEW revision (role >= editor). It never erases
// history: the current content is snapshotted just like any save. Optimistic lock via expectedVersion.
func (s *Service) RestoreVersion(ctx context.Context, tenantID, userID, pageID uuid.UUID, version, expectedVersion int32) (db.WikiPage, error) {
	v, err := s.q.GetWikiPageVersion(ctx, db.GetWikiPageVersionParams{TenantID: tenantID, PageID: pageID, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.WikiPage{}, ErrNotFound
	}
	if err != nil {
		return db.WikiPage{}, err
	}
	cur, err := s.q.GetWikiPage(ctx, db.GetWikiPageParams{ID: pageID, TenantID: tenantID})
	if err != nil {
		return db.WikiPage{}, ErrNotFound
	}
	// UpdatePage enforces role >= editor, derives body_text/html, bumps version and snapshots current.
	return s.UpdatePage(ctx, tenantID, userID, pageID, PageInput{
		Title: v.Title, BodyJSON: v.BodyJson, Status: cur.Status,
	}, expectedVersion)
}

// SetTemplate marks/unmarks a page as a space template (role >= editor). B7.
func (s *Service) SetTemplate(ctx context.Context, tenantID, userID, pageID uuid.UUID, isTemplate bool) (db.WikiPage, error) {
	if err := s.requirePageRole(ctx, tenantID, userID, pageID, RoleEditor); err != nil {
		return db.WikiPage{}, err
	}
	return s.q.SetWikiPageTemplate(ctx, db.SetWikiPageTemplateParams{
		IsTemplate: isTemplate, ID: pageID, TenantID: tenantID,
	})
}

// ListTemplates lists a space's templates (role >= viewer; using them to create requires editor,
// checked in CreatePage).
func (s *Service) ListTemplates(ctx context.Context, tenantID, userID, spaceID uuid.UUID) ([]db.ListWikiTemplatesRow, error) {
	if err := s.requireSpaceRole(ctx, tenantID, userID, spaceID, RoleViewer); err != nil {
		return nil, err
	}
	return s.q.ListWikiTemplates(ctx, db.ListWikiTemplatesParams{TenantID: tenantID, SpaceID: spaceID})
}

// requirePageRole enforces a minimum effective role on a page.
func (s *Service) requirePageRole(ctx context.Context, tenantID, userID, pageID uuid.UUID, need int) error {
	role, err := s.pageRole(ctx, tenantID, userID, pageID)
	if err != nil {
		return err
	}
	return authorize(role, need)
}

// ---------- helpers ----------

func uuidPtr(p pgtype.UUID) *uuid.UUID {
	if !p.Valid {
		return nil
	}
	u := uuid.UUID(p.Bytes)
	return &u
}

// normalizeBody guarantees a valid JSON object for the NOT NULL body_json column.
func normalizeBody(raw json.RawMessage) []byte {
	if len(raw) == 0 || !json.Valid(raw) {
		return []byte("{}")
	}
	return raw
}

// nextPosition places a new page after its current siblings (best-effort; 0 on any error).
func (s *Service) nextPosition(ctx context.Context, tenantID, spaceID uuid.UUID, parent *uuid.UUID) int32 {
	pages, err := s.q.ListWikiPages(ctx, db.ListWikiPagesParams{
		TenantID: tenantID, SpaceID: spaceID, IncludeDrafts: true,
	})
	if err != nil {
		return 0
	}
	var n int32
	for _, p := range pages {
		if equalParent(uuidPtr(p.ParentID), parent) {
			n++
		}
	}
	return n
}

func equalParent(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// uniqueSlug derives a slug from the title and disambiguates collisions within the space.
func (s *Service) uniqueSlug(ctx context.Context, tenantID, spaceID uuid.UUID, title string) (string, error) {
	base := slugify(title)
	if base == "" {
		base = "page"
	}
	candidate := base
	for i := 2; i < 100; i++ {
		_, err := s.q.GetWikiPageBySlug(ctx, db.GetWikiPageBySlugParams{
			TenantID: tenantID, SpaceID: spaceID, Slug: candidate,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return "", ErrInvalid
}

// slugify turns a title into a URL-safe slug: lowercase word runs joined by single hyphens.
func slugify(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevHyphen = false
		case !prevHyphen && b.Len() > 0:
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}
