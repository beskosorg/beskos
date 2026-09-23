-- +goose Up
-- Card attachments: links a Files file to a card. Download is TENANT-scoped (any board member can
-- open it), unlike personal Files (owner-scoped); that is why the table stores the tenant.
-- Deleted by cascade with the card OR the file (if the file is purged, the attachment disappears).
CREATE TABLE card_attachments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    card_id    UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    file_id    UUID NOT NULL REFERENCES storage_files(id) ON DELETE CASCADE,
    added_by   UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (card_id, file_id)
);
CREATE INDEX idx_card_attachments ON card_attachments (card_id);

-- +goose Down
DROP TABLE card_attachments;
