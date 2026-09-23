-- +goose Up
-- Kanban: WIP limit per column, card start date, multiple assignees and dependencies.
ALTER TABLE board_columns ADD COLUMN wip_limit INT NOT NULL DEFAULT 0; -- 0 = no limit
ALTER TABLE cards ADD COLUMN start_date TEXT NOT NULL DEFAULT '';

-- Additional assignees (besides the "main" assignee_id): M:N.
CREATE TABLE card_assignees (
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, user_id)
);

-- Dependencies: a card depends on (is blocked by) another card on the same board.
CREATE TABLE card_deps (
    card_id       UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    depends_on_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, depends_on_id),
    CHECK (card_id <> depends_on_id)
);

-- +goose Down
DROP TABLE card_deps;
DROP TABLE card_assignees;
ALTER TABLE cards DROP COLUMN start_date;
ALTER TABLE board_columns DROP COLUMN wip_limit;
