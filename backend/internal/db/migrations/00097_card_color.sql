-- +goose Up
-- Kanban card color: a soft tint chosen by the user; '' = default.
-- Content contrast is guaranteed in the frontend (palette of light tones + a fixed text color).
ALTER TABLE cards ADD COLUMN color TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE cards DROP COLUMN IF EXISTS color;
