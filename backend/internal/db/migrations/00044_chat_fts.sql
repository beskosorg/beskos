-- +goose Up
-- Chat full-text search: GENERATED tsvector column (auto-updating) + GIN index. 'simple' (no
-- per-language stemming) works for mixed PT/EN. Replaces the ILIKE in message searches.
ALTER TABLE messages ADD COLUMN body_tsv tsvector GENERATED ALWAYS AS (to_tsvector('simple', body)) STORED;
CREATE INDEX idx_messages_body_tsv ON messages USING GIN (body_tsv);

-- +goose Down
DROP INDEX IF EXISTS idx_messages_body_tsv;
ALTER TABLE messages DROP COLUMN IF EXISTS body_tsv;
