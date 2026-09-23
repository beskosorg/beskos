-- +goose Up
-- User status (Slack style): emoji + short text (e.g. 🌴 "On vacation"). Empty = no status.
ALTER TABLE mail_users
    ADD COLUMN status_emoji TEXT NOT NULL DEFAULT '',
    ADD COLUMN status_text  TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE mail_users DROP COLUMN status_emoji, DROP COLUMN status_text;
