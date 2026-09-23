-- +goose Up
-- Removes the deal pipeline: the product is mail + collaboration, not a sales CRM.
-- Product decision (we are not a CRM). The feature and its code were removed entirely.
DROP TABLE IF EXISTS deals;

-- +goose Down
-- Recreates the table as it was (00071), in case of a rollback.
CREATE TABLE deals (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    title         TEXT NOT NULL,
    value_cents   BIGINT NOT NULL DEFAULT 0,
    stage         TEXT NOT NULL DEFAULT 'lead',
    contact_id    UUID REFERENCES contacts(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_deals_tenant ON deals(tenant_id, stage);
