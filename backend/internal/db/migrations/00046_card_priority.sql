-- +goose Up
-- Task priority (brought back from tickets, removed in 00024). Fixed scale; the UI shows color/icon.
-- 'normal' as the default keeps existing cards unchanged.
ALTER TABLE cards ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal'
    CONSTRAINT cards_priority_ck CHECK (priority IN ('low', 'normal', 'high', 'urgent'));

-- +goose Down
ALTER TABLE cards DROP COLUMN priority;
