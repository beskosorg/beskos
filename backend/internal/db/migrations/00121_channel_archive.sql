-- +goose Up
-- Archiving a channel (Slack-style): it leaves the lists and becomes read-only, but the history stays
-- and it can be restored. It does not replace deletion -- that still exists and is still permanent.
ALTER TABLE channels ADD COLUMN archived_at TIMESTAMPTZ;
CREATE INDEX idx_channels_archived ON channels(tenant_id) WHERE archived_at IS NOT NULL;

-- +goose Down
DROP INDEX idx_channels_archived;
ALTER TABLE channels DROP COLUMN archived_at;
