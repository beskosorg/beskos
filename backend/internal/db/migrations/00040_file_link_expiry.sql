-- +goose Up
-- Public links now expire (audit finding: eternal, non-revocable link). expires_at NULL = no expiry
-- (compat.), but new links get a deadline; token resolution now checks it.
ALTER TABLE file_links ADD COLUMN expires_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE file_links DROP COLUMN expires_at;
