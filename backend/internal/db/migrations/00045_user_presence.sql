-- +goose Up
-- Rich chat presence set by the user: active | away | dnd (do not disturb). Combined with REAL
-- presence (Redis/online): offline always wins; online shows this state. dnd suppresses push.
ALTER TABLE mail_users ADD COLUMN presence TEXT NOT NULL DEFAULT 'active';

-- +goose Down
ALTER TABLE mail_users DROP COLUMN presence;
