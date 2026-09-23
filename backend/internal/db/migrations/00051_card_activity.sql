-- +goose Up
-- Card history/activity: who did what and when (created, moved, edited). Deleted with the card.
-- detail holds free-form event data (e.g. the destination column_id in a "moved"); the UI resolves the name.
CREATE TABLE card_activity (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    card_id    UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    actor_id   UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    kind       TEXT NOT NULL,          -- created | moved | updated
    detail     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_card_activity ON card_activity (card_id, created_at DESC);

-- +goose Down
DROP TABLE card_activity;
