-- +goose Up
-- Custom workspace emojis (:name:). The image is a storage_files row (reuses upload + /thumb).
CREATE TABLE workspace_emojis (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    file_id    UUID NOT NULL REFERENCES storage_files(id) ON DELETE CASCADE,
    created_by UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

-- +goose Down
DROP TABLE workspace_emojis;
