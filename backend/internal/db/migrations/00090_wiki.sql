-- +goose Up
-- Wiki / knowledge base (Confluence-style). See docs/wiki-features.md.
-- N wikis per tenant (each wiki_spaces row = one wiki), pages in a tree with body_json as the
-- source of truth, versions for diff/restore, and grants per person/group/organization.
-- Multi-tenant: everything has tenant_id + an index. Access is resolved in SQL by the `vis` CTE
-- (§5.3), like ListContacts. Public (anonymous) access is NOT a grant — it lives in
-- visibility=public + public_slug.

-- Spaces = wikis. visibility: restricted (grants only) | org (the whole tenant sees it) |
-- public (anonymous link).
CREATE TABLE wiki_spaces (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,                 -- short key, unique in the tenant (e.g. ENG, HR)
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    icon          TEXT NOT NULL DEFAULT '',      -- emoji/icon
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    visibility    TEXT NOT NULL DEFAULT 'restricted'
                  CHECK (visibility IN ('restricted', 'org', 'public')),
    public_slug   TEXT,                          -- slug of the anonymous link (when visibility=public)
    ai_indexed    BOOLEAN NOT NULL DEFAULT true, -- feeds the RAG
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, key)
);
CREATE INDEX idx_wiki_spaces_tenant ON wiki_spaces(tenant_id);
CREATE UNIQUE INDEX ux_wiki_spaces_public_slug ON wiki_spaces(tenant_id, public_slug)
    WHERE public_slug IS NOT NULL;

-- Pages: a tree inside the space. body_json (ProseMirror/TipTap) is the source of truth; body_text
-- and body_html are derived on save (§4.3). search_tsv (GENERATED) is the cheap FTS pre-filter
-- (§7.3).
CREATE TABLE wiki_pages (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    space_id       UUID NOT NULL REFERENCES wiki_spaces(id) ON DELETE CASCADE,
    parent_id      UUID REFERENCES wiki_pages(id) ON DELETE CASCADE,   -- root = null
    title          TEXT NOT NULL,
    slug           TEXT NOT NULL,                 -- derived from the title, unique per space
    body_json      JSONB NOT NULL DEFAULT '{}'::jsonb,
    body_text      TEXT NOT NULL DEFAULT '',      -- plain text for search/RAG
    body_html      TEXT NOT NULL DEFAULT '',      -- sanitized cached render
    position       INT NOT NULL DEFAULT 0,        -- order among siblings
    status         TEXT NOT NULL DEFAULT 'draft'
                   CHECK (status IN ('draft', 'published')),
    author_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    version        INT NOT NULL DEFAULT 1,        -- optimistic locking (§6)
    search_tsv     tsvector GENERATED ALWAYS AS
                   (to_tsvector('simple', coalesce(title, '') || ' ' || coalesce(body_text, ''))) STORED,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (space_id, slug)
);
CREATE INDEX idx_wiki_pages_tree ON wiki_pages(tenant_id, space_id, parent_id, position);
CREATE INDEX idx_wiki_pages_search ON wiki_pages USING GIN (search_tsv);

-- History: each save pushes the previous version here (diff/restore, §6).
CREATE TABLE wiki_page_versions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    page_id        UUID NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    version        INT NOT NULL,
    title          TEXT NOT NULL,
    body_json      JSONB NOT NULL,
    body_text      TEXT NOT NULL DEFAULT '',
    author_user_id UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (page_id, version)
);
CREATE INDEX idx_wiki_page_versions_page ON wiki_page_versions(tenant_id, page_id);

-- Grants per space. subject_type: user (person) | group (team) | org (the whole tenant).
-- subject_id is null when subject_type=org. role: viewer|commenter|editor|manager (§5.1).
CREATE TABLE wiki_space_grants (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    space_id     UUID NOT NULL REFERENCES wiki_spaces(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('user', 'group', 'org')),
    subject_id   UUID,   -- mail_users.id or user_groups.id; null when subject_type=org
    role         TEXT NOT NULL DEFAULT 'viewer'
                 CHECK (role IN ('viewer', 'commenter', 'editor', 'manager')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((subject_type = 'org') = (subject_id IS NULL))
);
CREATE INDEX idx_wiki_space_grants_space ON wiki_space_grants(tenant_id, space_id);
CREATE INDEX idx_wiki_space_grants_subject ON wiki_space_grants(subject_type, subject_id);
CREATE UNIQUE INDEX ux_wiki_space_grants_subject ON wiki_space_grants(space_id, subject_type, subject_id)
    WHERE subject_id IS NOT NULL;
CREATE UNIQUE INDEX ux_wiki_space_grants_org ON wiki_space_grants(space_id)
    WHERE subject_type = 'org';

-- Grants per page (inherited from the space; a grant on the page adds or raises, §5).
CREATE TABLE wiki_page_grants (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    page_id      UUID NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('user', 'group', 'org')),
    subject_id   UUID,
    role         TEXT NOT NULL DEFAULT 'viewer'
                 CHECK (role IN ('viewer', 'commenter', 'editor', 'manager')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((subject_type = 'org') = (subject_id IS NULL))
);
CREATE INDEX idx_wiki_page_grants_page ON wiki_page_grants(tenant_id, page_id);
CREATE INDEX idx_wiki_page_grants_subject ON wiki_page_grants(subject_type, subject_id);
CREATE UNIQUE INDEX ux_wiki_page_grants_subject ON wiki_page_grants(page_id, subject_type, subject_id)
    WHERE subject_id IS NOT NULL;
CREATE UNIQUE INDEX ux_wiki_page_grants_org ON wiki_page_grants(page_id)
    WHERE subject_type = 'org';

-- +goose Down
DROP TABLE IF EXISTS wiki_page_grants;
DROP TABLE IF EXISTS wiki_space_grants;
DROP TABLE IF EXISTS wiki_page_versions;
DROP TABLE IF EXISTS wiki_pages;
DROP TABLE IF EXISTS wiki_spaces;
