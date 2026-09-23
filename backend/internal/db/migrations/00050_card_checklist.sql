-- +goose Up
-- Card checklist/subtasks: checkable, ordered items. Deleted by cascade with the card.
CREATE TABLE card_checklist_items (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    card_id    UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    text       TEXT NOT NULL,
    done       BOOLEAN NOT NULL DEFAULT false,
    position   INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_checklist_card ON card_checklist_items (card_id, position);

-- +goose Down
DROP TABLE card_checklist_items;
