-- +goose Up
-- Change feed for the sync client (desktop daemon, written in Rust): the daemon asks "what changed
-- since cursor X?" and reconciles the local folder. `updated_at` is set automatically by a trigger
-- on ANY UPDATE — it captures rename (name), move (folder_id), new version (hash/`versao`/blob_key),
-- moving to the trash (deleted_at) and restore, without touching the handlers.
-- New files inherit updated_at = created_at through the default.
ALTER TABLE storage_files ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION storage_files_touch_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_storage_files_updated_at
  BEFORE UPDATE ON storage_files
  FOR EACH ROW EXECUTE FUNCTION storage_files_touch_updated_at();

-- Feed index: keyset on (owner, updated_at, id) — sorts and paginates without scanning the table.
CREATE INDEX idx_files_changes ON storage_files (owner_user_id, updated_at, id);

-- +goose Down
DROP INDEX IF EXISTS idx_files_changes;
DROP TRIGGER IF EXISTS trg_storage_files_updated_at ON storage_files;
DROP FUNCTION IF EXISTS storage_files_touch_updated_at();
ALTER TABLE storage_files DROP COLUMN updated_at;
