-- Wiki queries. See docs/wiki-features.md. Access resolved in SQL (§5.3): a user's effective role
-- on a space/page = the MAXIMUM among owner, org/public visibility, and grants (user/group/org).
-- Role ranks: viewer=1, commenter=2, editor=3, manager=4. 0 = no access (404). Everything filters
-- by tenant_id from the authenticated context (law 5.1). A grant's subject matches when it's
-- 'org', or the user themselves, or a group (user_group) the user is a member of.

-- ---------- Spaces (wikis) ----------

-- name: CreateWikiSpace :one
INSERT INTO wiki_spaces (tenant_id, key, name, description, icon, owner_user_id, visibility)
VALUES (@tenant_id, @key, @name, @description, @icon, @owner_user_id, @visibility)
RETURNING *;

-- name: GetWikiSpace :one
SELECT * FROM wiki_spaces WHERE id = @id AND tenant_id = @tenant_id;

-- name: UpdateWikiSpace :one
UPDATE wiki_spaces SET
    name = @name, description = @description, icon = @icon,
    visibility = @visibility, ai_indexed = @ai_indexed, updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id
RETURNING *;

-- name: DeleteWikiSpace :exec
DELETE FROM wiki_spaces WHERE id = @id AND tenant_id = @tenant_id;

-- name: SetWikiSpacePublic :one
-- Turns public access on/off: sets visibility and public_slug (null turns the link off).
UPDATE wiki_spaces SET visibility = @visibility, public_slug = sqlc.narg('public_slug'), updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id
RETURNING *;

-- name: GetWikiSpaceByPublicSlug :one
-- Resolves a public space by its anonymous slug (the tenant comes from the row, never the request).
SELECT * FROM wiki_spaces WHERE public_slug = @slug AND visibility = 'public';

-- name: ListVisibleWikiSpaces :many
-- Spaces the user can see (role >= viewer): owner, org/public, or a grant (user/group/org).
SELECT s.* FROM wiki_spaces s
WHERE s.tenant_id = @tenant_id AND (
    s.owner_user_id = @user_id
    OR s.visibility IN ('org', 'public')
    OR EXISTS (
        SELECT 1 FROM wiki_space_grants g
        WHERE g.space_id = s.id AND (
            g.subject_type = 'org'
            OR (g.subject_type = 'user' AND g.subject_id = @user_id)
            OR (g.subject_type = 'group' AND EXISTS (
                SELECT 1 FROM user_group_members ugm
                WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id))
        )
    )
)
ORDER BY s.name;

-- name: WikiSpaceRole :one
-- Effective role (rank) of the user in a space. 0 = no access.
SELECT COALESCE(MAX(rank), 0)::int AS role_rank FROM (
    SELECT CASE WHEN s.owner_user_id = @user_id THEN 4
                WHEN s.visibility IN ('org', 'public') THEN 1 ELSE 0 END AS rank
    FROM wiki_spaces s WHERE s.id = @space_id AND s.tenant_id = @tenant_id
    UNION ALL
    SELECT CASE g.role WHEN 'manager' THEN 4 WHEN 'editor' THEN 3 WHEN 'commenter' THEN 2 ELSE 1 END
    FROM wiki_space_grants g
    WHERE g.space_id = @space_id AND g.tenant_id = @tenant_id AND (
        g.subject_type = 'org'
        OR (g.subject_type = 'user' AND g.subject_id = @user_id)
        OR (g.subject_type = 'group' AND EXISTS (
            SELECT 1 FROM user_group_members ugm
            WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id))
    )
) t;

-- ---------- Pages (tree) ----------

-- name: CreateWikiPage :one
INSERT INTO wiki_pages (
    tenant_id, space_id, parent_id, title, slug, body_json, body_text, body_html,
    position, status, author_user_id)
VALUES (
    @tenant_id, @space_id, @parent_id, @title, @slug, @body_json, @body_text, @body_html,
    @position, @status, @author_user_id)
RETURNING *;

-- name: GetWikiPage :one
SELECT * FROM wiki_pages WHERE id = @id AND tenant_id = @tenant_id;

-- name: GetWikiPageBySlug :one
SELECT * FROM wiki_pages
WHERE tenant_id = @tenant_id AND space_id = @space_id AND slug = @slug;

-- name: ListWikiPages :many
-- A space's tree. Drafts are included only when include_drafts (the service turns this on when role >= editor).
SELECT * FROM wiki_pages
WHERE tenant_id = @tenant_id AND space_id = @space_id
  AND (status = 'published' OR @include_drafts::bool)
ORDER BY position, title;

-- name: UpdateWikiPage :one
-- Optimistic locking: only saves if the version matches; otherwise 0 rows -> the service returns 409 (§6).
UPDATE wiki_pages SET
    title = @title, slug = @slug, body_json = @body_json, body_text = @body_text,
    body_html = @body_html, status = @status, author_user_id = @author_user_id,
    version = version + 1, updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND version = @expected_version
RETURNING *;

-- name: MoveWikiPage :exec
UPDATE wiki_pages SET parent_id = @parent_id, position = @position, updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id;

-- name: DeleteWikiPage :exec
DELETE FROM wiki_pages WHERE id = @id AND tenant_id = @tenant_id;

-- name: WikiPageRole :one
-- Effective role (rank) of the user on a page = max(role inherited from the space, page grants).
SELECT COALESCE(MAX(rank), 0)::int AS role_rank FROM (
    SELECT CASE WHEN s.owner_user_id = @user_id THEN 4
                WHEN s.visibility IN ('org', 'public') THEN 1 ELSE 0 END AS rank
    FROM wiki_pages p JOIN wiki_spaces s ON s.id = p.space_id
    WHERE p.id = @page_id AND p.tenant_id = @tenant_id
    UNION ALL
    SELECT CASE g.role WHEN 'manager' THEN 4 WHEN 'editor' THEN 3 WHEN 'commenter' THEN 2 ELSE 1 END
    FROM wiki_pages p JOIN wiki_space_grants g ON g.space_id = p.space_id
    WHERE p.id = @page_id AND p.tenant_id = @tenant_id AND (
        g.subject_type = 'org'
        OR (g.subject_type = 'user' AND g.subject_id = @user_id)
        OR (g.subject_type = 'group' AND EXISTS (
            SELECT 1 FROM user_group_members ugm
            WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id))
    )
    UNION ALL
    SELECT CASE g.role WHEN 'manager' THEN 4 WHEN 'editor' THEN 3 WHEN 'commenter' THEN 2 ELSE 1 END
    FROM wiki_page_grants g
    WHERE g.page_id = @page_id AND g.tenant_id = @tenant_id AND (
        g.subject_type = 'org'
        OR (g.subject_type = 'user' AND g.subject_id = @user_id)
        OR (g.subject_type = 'group' AND EXISTS (
            SELECT 1 FROM user_group_members ugm
            WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id))
    )
) t;

-- ---------- Versions ----------

-- name: InsertWikiPageVersion :exec
INSERT INTO wiki_page_versions (tenant_id, page_id, version, title, body_json, body_text, author_user_id)
VALUES (@tenant_id, @page_id, @version, @title, @body_json, @body_text, @author_user_id);

-- name: ListWikiPageVersions :many
SELECT id, tenant_id, page_id, version, title, body_text, author_user_id, created_at
FROM wiki_page_versions
WHERE tenant_id = @tenant_id AND page_id = @page_id
ORDER BY version DESC;

-- name: GetWikiPageVersion :one
SELECT * FROM wiki_page_versions
WHERE tenant_id = @tenant_id AND page_id = @page_id AND version = @version;

-- ---------- Space grants ----------

-- name: ListWikiSpaceGrants :many
SELECT * FROM wiki_space_grants
WHERE tenant_id = @tenant_id AND space_id = @space_id
ORDER BY subject_type, created_at;

-- name: DeleteWikiSpaceGrantSubject :exec
DELETE FROM wiki_space_grants
WHERE tenant_id = @tenant_id AND space_id = @space_id
  AND subject_type = @subject_type AND subject_id IS NOT DISTINCT FROM @subject_id;

-- name: InsertWikiSpaceGrant :one
INSERT INTO wiki_space_grants (tenant_id, space_id, subject_type, subject_id, role)
VALUES (@tenant_id, @space_id, @subject_type, @subject_id, @role)
RETURNING *;

-- name: DeleteWikiSpaceGrant :exec
DELETE FROM wiki_space_grants WHERE id = @id AND tenant_id = @tenant_id;

-- ---------- Page grants ----------

-- name: ListWikiPageGrants :many
SELECT * FROM wiki_page_grants
WHERE tenant_id = @tenant_id AND page_id = @page_id
ORDER BY subject_type, created_at;

-- name: DeleteWikiPageGrantSubject :exec
DELETE FROM wiki_page_grants
WHERE tenant_id = @tenant_id AND page_id = @page_id
  AND subject_type = @subject_type AND subject_id IS NOT DISTINCT FROM @subject_id;

-- name: InsertWikiPageGrant :one
INSERT INTO wiki_page_grants (tenant_id, page_id, subject_type, subject_id, role)
VALUES (@tenant_id, @page_id, @subject_type, @subject_id, @role)
RETURNING *;

-- name: DeleteWikiPageGrant :exec
DELETE FROM wiki_page_grants WHERE id = @id AND tenant_id = @tenant_id;

-- ---------- Comments ----------

-- name: ListWikiComments :many
SELECT c.id, c.tenant_id, c.page_id, c.author_user_id, c.body, c.body_html, c.created_at,
       u.name AS author_name, u.email AS author_email
FROM wiki_comments c
JOIN mail_users u ON u.id = c.author_user_id
WHERE c.tenant_id = @tenant_id AND c.page_id = @page_id
ORDER BY c.created_at;

-- name: CreateWikiComment :one
INSERT INTO wiki_comments (tenant_id, page_id, author_user_id, body, body_json, body_html)
VALUES (@tenant_id, @page_id, @author_user_id, @body, @body_json, @body_html)
RETURNING *;

-- name: GetWikiComment :one
SELECT * FROM wiki_comments WHERE id = @id AND tenant_id = @tenant_id;

-- name: DeleteWikiComment :exec
DELETE FROM wiki_comments WHERE id = @id AND tenant_id = @tenant_id;

-- ---------- Attachments (reusing Files/storage_files) ----------

-- name: AttachFileToWikiPage :exec
INSERT INTO wiki_attachments (tenant_id, page_id, file_id, added_by)
VALUES (@tenant_id, @page_id, @file_id, @added_by)
ON CONFLICT (page_id, file_id) DO NOTHING;

-- name: ListWikiAttachments :many
SELECT a.id, a.file_id, f.name, f.size, a.created_at
FROM wiki_attachments a JOIN storage_files f ON f.id = a.file_id
WHERE a.page_id = @page_id AND a.tenant_id = @tenant_id ORDER BY a.created_at;

-- name: DetachWikiAttachment :exec
DELETE FROM wiki_attachments WHERE id = @id AND tenant_id = @tenant_id;

-- name: GetWikiAttachmentBlob :one
-- Returns the file and the page of the attachment so the service can check access before serving the blob.
SELECT f.*, a.page_id FROM wiki_attachments a JOIN storage_files f ON f.id = a.file_id
WHERE a.id = @id AND a.tenant_id = @tenant_id;

-- ---------- RAG: chunks + semantic search (pgvector, §7.2) ----------

-- name: UpsertWikiChunk :exec
-- Idempotent on (page_id, chunk_index); created_at=now() marks the chunk as fresh relative to updated_at.
INSERT INTO wiki_chunks (tenant_id, page_id, space_id, chunk_index, chunk_text, embedding)
VALUES (@tenant_id, @page_id, @space_id, @chunk_index, @chunk_text, @embedding::vector)
ON CONFLICT (page_id, chunk_index) DO UPDATE
SET chunk_text = EXCLUDED.chunk_text, embedding = EXCLUDED.embedding, created_at = now();

-- name: DeleteWikiChunksFrom :exec
-- Trims chunks past the page's new size (the page shrank when re-saved).
DELETE FROM wiki_chunks WHERE page_id = @page_id AND chunk_index >= @from_index;

-- name: DeleteStaleWikiChunks :execrows
-- Cleans up chunks from unpublished pages or spaces with ai_indexed turned off (never left orphaned).
DELETE FROM wiki_chunks c
USING wiki_pages p, wiki_spaces s
WHERE c.page_id = p.id AND s.id = p.space_id
  AND (p.status <> 'published' OR s.ai_indexed = false);

-- name: ListWikiPagesNeedingEmbedding :many
-- Published/indexable pages with no chunk fresher than updated_at (need a (re)embed).
SELECT p.id, p.tenant_id, p.space_id, p.title, p.body_text
FROM wiki_pages p JOIN wiki_spaces s ON s.id = p.space_id
WHERE p.status = 'published' AND s.ai_indexed = true AND p.body_text <> ''
  AND NOT EXISTS (
      SELECT 1 FROM wiki_chunks c WHERE c.page_id = p.id AND c.created_at >= p.updated_at)
LIMIT @max_pages;

-- name: SearchWikiChunks :many
-- Cosine kNN FILTERED by the asker's permission (same visibility surface, §7.4) + tenant +
-- ai_indexed + published. Smaller distance = more relevant.
SELECT c.page_id, c.chunk_index, c.chunk_text, p.title, p.slug, p.space_id,
       (c.embedding <=> @query_embedding::vector)::float8 AS distance
FROM wiki_chunks c
JOIN wiki_pages p ON p.id = c.page_id
JOIN wiki_spaces s ON s.id = c.space_id
WHERE c.tenant_id = @tenant_id AND p.status = 'published' AND s.ai_indexed = true
  -- B9: cheap FTS pre-filter (GIN on search_tsv) before the kNN; the service retries without it on 0 results.
  AND (@use_fts::bool = false OR p.search_tsv @@ websearch_to_tsquery('simple', @query_text::text))
  AND (
    s.owner_user_id = @user_id
    OR s.visibility IN ('org', 'public')
    OR EXISTS (
        SELECT 1 FROM wiki_space_grants g
        WHERE g.space_id = s.id AND (
            g.subject_type = 'org'
            OR (g.subject_type = 'user' AND g.subject_id = @user_id)
            OR (g.subject_type = 'group' AND EXISTS (
                SELECT 1 FROM user_group_members ugm
                WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id))))
    OR EXISTS (
        SELECT 1 FROM wiki_page_grants g
        WHERE g.page_id = p.id AND (
            g.subject_type = 'org'
            OR (g.subject_type = 'user' AND g.subject_id = @user_id)
            OR (g.subject_type = 'group' AND EXISTS (
                SELECT 1 FROM user_group_members ugm
                WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id))))
  )
ORDER BY c.embedding <=> @query_embedding::vector
LIMIT @max_results;

-- name: SearchWikiPagesText :many
-- LEXICAL search, straight against PAGES, with the same visibility surface as the kNN. This is
-- the safety net for when there's no EMBEDDINGS_API_KEY: then no chunks exist at all (they're
-- only born in embedPage), so searching the chunks would always come back empty. `search_tsv` is
-- a GENERATED column -- always there, no pipeline needed. `distance` keeps the kNN's semantics
-- (smaller = more relevant).
SELECT p.id AS page_id, p.title, p.slug, p.space_id,
       left(p.body_text, 1200) AS snippet,
       (1.0 - ts_rank_cd(p.search_tsv, websearch_to_tsquery('simple', @query_text::text)))::float8 AS distance
FROM wiki_pages p
JOIN wiki_spaces s ON s.id = p.space_id
WHERE p.tenant_id = @tenant_id AND p.status = 'published' AND s.ai_indexed = true
  AND p.search_tsv @@ websearch_to_tsquery('simple', @query_text::text)
  AND (
    s.owner_user_id = @user_id
    OR s.visibility IN ('org', 'public')
    OR EXISTS (
        SELECT 1 FROM wiki_space_grants g
        WHERE g.space_id = s.id AND (
            g.subject_type = 'org'
            OR (g.subject_type = 'user' AND g.subject_id = @user_id)
            OR (g.subject_type = 'group' AND EXISTS (
                SELECT 1 FROM user_group_members ugm
                WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id))))
    OR EXISTS (
        SELECT 1 FROM wiki_page_grants g
        WHERE g.page_id = p.id AND (
            g.subject_type = 'org'
            OR (g.subject_type = 'user' AND g.subject_id = @user_id)
            OR (g.subject_type = 'group' AND EXISTS (
                SELECT 1 FROM user_group_members ugm
                WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id))))
  )
ORDER BY ts_rank_cd(p.search_tsv, websearch_to_tsquery('simple', @query_text::text)) DESC
LIMIT @max_results;

-- ---------- Wiki app home (B5) ----------

-- name: GetWikiHome :one
SELECT * FROM wiki_home WHERE tenant_id = @tenant_id;

-- name: UpsertWikiHome :one
INSERT INTO wiki_home (tenant_id, body_json, body_text, body_html, updated_by, updated_at)
VALUES (@tenant_id, @body_json, @body_text, @body_html, @updated_by, now())
ON CONFLICT (tenant_id) DO UPDATE SET
    body_json = EXCLUDED.body_json, body_text = EXCLUDED.body_text,
    body_html = EXCLUDED.body_html, updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING *;

-- ---------- Page templates (B7) ----------

-- name: SetWikiPageTemplate :one
UPDATE wiki_pages SET is_template = @is_template, updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id
RETURNING *;

-- name: ListWikiTemplates :many
SELECT id, title FROM wiki_pages
WHERE tenant_id = @tenant_id AND space_id = @space_id AND is_template = true
ORDER BY title;
