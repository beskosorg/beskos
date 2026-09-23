-- +goose Up
-- Channel canvas: a shared rich document (HTML), editable by members (Slack Canvas style).
-- Pins: marks a message as pinned in the channel (pinned_at). Files and links derive from messages.
ALTER TABLE channels ADD COLUMN canvas TEXT NOT NULL DEFAULT '', ADD COLUMN canvas_updated_at TIMESTAMPTZ;
ALTER TABLE messages ADD COLUMN pinned_at TIMESTAMPTZ;
CREATE INDEX idx_messages_pinned ON messages (channel_id) WHERE pinned_at IS NOT NULL;

-- +goose Down
ALTER TABLE channels DROP COLUMN canvas, DROP COLUMN canvas_updated_at;
DROP INDEX IF EXISTS idx_messages_pinned;
ALTER TABLE messages DROP COLUMN pinned_at;
