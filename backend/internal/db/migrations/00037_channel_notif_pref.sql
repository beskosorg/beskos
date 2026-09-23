-- +goose Up
-- Per-channel notification preference (per member): all = everything; mentions = direct mentions
-- only; none = muted. Applied when generating chat mention notifications.
ALTER TABLE channel_members ADD COLUMN notif_pref TEXT NOT NULL DEFAULT 'all'; -- all | mentions | none

-- +goose Down
ALTER TABLE channel_members DROP COLUMN notif_pref;
