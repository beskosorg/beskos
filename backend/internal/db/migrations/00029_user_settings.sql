-- +goose Up
-- User preferences, organized by app: {"email":{"signature":"…"}, "chat":{…}}.
--
-- JSONB (decided on 2026-07-16): adding a new preference never again needs a
-- migration — it is just one more key. The existing ones (language, notify_email) stay as their own
-- columns: they are already in use and have a fixed type. This is only for what grows.
--
-- Default '{}' (not NULL): the code always reads an object, without testing for null on every access.
ALTER TABLE mail_users ADD COLUMN settings JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE mail_users DROP COLUMN settings;
