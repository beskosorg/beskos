-- +goose Up
-- Wiki page comments (docs/wiki-features.md §3.5). Commenting requires role >= commenter; reading
-- comments requires >= viewer (the same `vis` surface as the page, checked in the service). The
-- author deletes their own comment; the space/page manager deletes any. Multi-tenant: tenant_id +
-- index.
CREATE TABLE wiki_comments (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    page_id        UUID NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    author_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    body           TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_wiki_comments_page ON wiki_comments(tenant_id, page_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS wiki_comments;
