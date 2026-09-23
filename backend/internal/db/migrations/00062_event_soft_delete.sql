-- +goose Up
-- Soft-delete for events to allow "undo delete" (Gmail-style undo): deleting sets deleted_at and
-- restoring clears it, preserving the id, the invitations and the link to the meeting. Listings
-- filter on deleted_at IS NULL. A permanent purge may come later (worker), outside this scope.
ALTER TABLE events ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX idx_events_live ON events(owner_user_id, starts_at) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_events_live;
ALTER TABLE events DROP COLUMN deleted_at;
