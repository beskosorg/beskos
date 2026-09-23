-- +goose Up
-- Wiki page attachments (docs/wiki-features.md §3.6): links files from Files (storage_files) to a
-- page, reusing the existing storage (upload is not reinvented). Attaching/detaching requires
-- editor; downloading requires viewer on the page (checked in the service). Multi-tenant:
-- tenant_id + index.
CREATE TABLE wiki_attachments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    page_id    UUID NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    file_id    UUID NOT NULL REFERENCES storage_files(id) ON DELETE CASCADE,
    added_by   UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (page_id, file_id)
);
CREATE INDEX idx_wiki_attachments_page ON wiki_attachments(tenant_id, page_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS wiki_attachments;
