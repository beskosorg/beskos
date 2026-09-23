-- +goose Up
-- Archive card and board: removes it from view without deleting it (deleted_at IS NULL stays
-- active). Recoverable.
ALTER TABLE cards ADD COLUMN archived_at TIMESTAMPTZ;
ALTER TABLE boards ADD COLUMN archived_at TIMESTAMPTZ;
CREATE INDEX idx_cards_live ON cards(board_id, column_id, position) WHERE archived_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_cards_live;
ALTER TABLE boards DROP COLUMN archived_at;
ALTER TABLE cards DROP COLUMN archived_at;
