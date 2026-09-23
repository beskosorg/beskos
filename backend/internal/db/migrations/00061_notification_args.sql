-- +goose Up
-- Notification i18n (docs/engineering-standards.md §4.6): the backend no longer depends on the
-- Portuguese text stored in title/body. It now stores the event's structured data in `args` (JSONB)
-- and the frontend translates via `server.notif.<kind>` into the user's language. title/body are
-- still written as a fallback (old rows) and because e-mail/web-push are rendered in the backend
-- (there is no frontend to translate them).
ALTER TABLE notifications ADD COLUMN args JSONB NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE notifications DROP COLUMN args;
