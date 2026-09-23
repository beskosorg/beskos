-- +goose Up
-- Optional password on the public link (on top of the already configurable expiry): the download
-- requires the password.
ALTER TABLE file_links ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE file_links DROP COLUMN password_hash;
