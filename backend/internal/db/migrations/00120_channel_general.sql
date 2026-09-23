-- +goose Up
-- The tenant's default channel: it always exists and everyone joins it. The DB stores the marker
-- (not the translated name -- the backend does not assume a language, docs/engineering-standards.md
-- §4.6): "General" is stored and the frontend shows the translation while the name is untouched.
-- If someone renames it, the chosen name takes over.
ALTER TABLE channels ADD COLUMN is_general BOOLEAN NOT NULL DEFAULT false;
-- A single default channel per tenant (the partial index is also the defense against races between
-- two requests creating it at the same time).
CREATE UNIQUE INDEX idx_channels_general ON channels(tenant_id) WHERE is_general;

-- +goose Down
DROP INDEX idx_channels_general;
ALTER TABLE channels DROP COLUMN is_general;
