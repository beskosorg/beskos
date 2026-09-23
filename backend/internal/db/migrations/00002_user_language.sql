-- +goose Up
-- User language preference (i18n, docs/engineering-standards.md §4). Empty = automatic (browser
-- detection); a set value (e.g. 'pt-BR', 'en') takes priority over the browser and is stored on the account.
ALTER TABLE panel_users ADD COLUMN language TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE panel_users DROP COLUMN language;
