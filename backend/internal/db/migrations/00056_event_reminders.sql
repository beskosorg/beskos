-- +goose Up
-- Event reminders: remind_minutes = minutes before the start to notify (0 = no reminder). The worker
-- fires the notification (bell + push) when the time comes and sets reminded so it does not repeat.
-- Editing the event re-arms it (reminded goes back to false), so changing the time notifies again.
ALTER TABLE events ADD COLUMN remind_minutes INT NOT NULL DEFAULT 10;
ALTER TABLE events ADD COLUMN reminded BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX idx_events_remind ON events (starts_at) WHERE remind_minutes > 0 AND NOT reminded;

-- +goose Down
DROP INDEX IF EXISTS idx_events_remind;
ALTER TABLE events DROP COLUMN reminded;
ALTER TABLE events DROP COLUMN remind_minutes;
