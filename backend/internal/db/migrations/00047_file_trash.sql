-- +goose Up
-- Trash: soft delete. deleted_at NULL = active; set = in the trash (restorable). Delete is no longer
-- destructive: only the purge (emptying the trash) deletes for good and frees the blob. Trashed files
-- still count toward the quota until purged (as in Drive). A partial index speeds up trash listing.
ALTER TABLE storage_files ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX idx_files_trash ON storage_files (owner_user_id, deleted_at) WHERE deleted_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_files_trash;
ALTER TABLE storage_files DROP COLUMN deleted_at;
