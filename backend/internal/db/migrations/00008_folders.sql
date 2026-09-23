-- +goose Up
-- Folders/subfolders for the file service (hierarchy). Per owner (logged-in mailbox).

CREATE TABLE storage_folders (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    parent_id     UUID REFERENCES storage_folders(id) ON DELETE CASCADE, -- NULL = root
    name          TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE storage_files ADD COLUMN folder_id UUID REFERENCES storage_folders(id) ON DELETE SET NULL;

CREATE INDEX idx_folders_parent ON storage_folders(owner_user_id, parent_id);
CREATE INDEX idx_files_folder ON storage_files(owner_user_id, folder_id);

-- +goose Down
ALTER TABLE storage_files DROP COLUMN folder_id;
DROP TABLE IF EXISTS storage_folders;
