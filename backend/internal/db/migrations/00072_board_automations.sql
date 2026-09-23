-- +goose Up
-- Board automations: when a card enters a column (trigger), an action is applied.
-- action: assign (assignee_id = value) | label (label = value) | archive (archives the card).
CREATE TABLE board_automations (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    board_id          UUID NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    trigger_column_id UUID NOT NULL REFERENCES board_columns(id) ON DELETE CASCADE,
    action            TEXT NOT NULL,           -- assign | label | archive
    action_value      TEXT NOT NULL DEFAULT '', -- user_id (assign) | label (label) | '' (archive)
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_automations_board ON board_automations(board_id);
CREATE INDEX idx_automations_trigger ON board_automations(trigger_column_id);

-- +goose Down
DROP TABLE board_automations;
