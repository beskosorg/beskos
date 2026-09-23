-- +goose Up
-- Public download links (large attachments sent by email, Gmail style). The `token` is a long,
-- unpredictable secret: whoever holds it downloads the file, without login. No expiry, by customer
-- decision: the link lives as long as the file exists (ON DELETE CASCADE cleans up on delete/revoke).
-- A file has at most one active link (reused); hence file_id is unique.
CREATE TABLE file_links (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    file_id    UUID NOT NULL UNIQUE REFERENCES storage_files(id) ON DELETE CASCADE,
    token      TEXT NOT NULL UNIQUE,
    created_by UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE file_links;
