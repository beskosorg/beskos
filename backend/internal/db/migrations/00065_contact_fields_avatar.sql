-- +goose Up
-- Custom fields (free-form JSONB) and photo/avatar (points at a storage_files row -- reuses /thumb).
ALTER TABLE contacts ADD COLUMN custom_fields JSONB NOT NULL DEFAULT '{}';
ALTER TABLE contacts ADD COLUMN avatar_file_id UUID REFERENCES storage_files(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE contacts DROP COLUMN avatar_file_id;
ALTER TABLE contacts DROP COLUMN custom_fields;
