-- +goose Up
-- Browsable version history: each OnlyOffice save used to overwrite the blob and lose the previous
-- one. Now, before writing, the CURRENT version is snapshotted here, so it can be listed and restored.
-- Blobs are content-addressed (tenant/hash) and deduplicated, so a version may share a blob with the
-- file or another version: the purge deletes the blob only when NO record (file OR version) uses it.
CREATE TABLE file_versions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id    UUID NOT NULL REFERENCES storage_files(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    blob_key   TEXT NOT NULL,
    hash       TEXT NOT NULL,
    tamanho    BIGINT NOT NULL,
    versao     INT NOT NULL,
    created_by UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_file_versions ON file_versions (file_id, versao DESC);

-- +goose Down
DROP TABLE file_versions;
