-- +goose Up
-- New-email push with the app closed: records up to when each user's mailbox has been seen. The
-- worker compares it with the most recent arrival and notifies only what is new since the last check.
-- NULL = baseline not yet established (the first check only records, it does not notify, to avoid a flood).
ALTER TABLE mail_users ADD COLUMN mail_seen_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE mail_users DROP COLUMN mail_seen_at;
