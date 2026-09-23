-- +goose Up
-- Deal pipeline: shared across the tenant, with a stage, a value and a linked contact.
CREATE TABLE deals (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    title         TEXT NOT NULL,
    value_cents   BIGINT NOT NULL DEFAULT 0, -- value in cents (clean JSON, no pgtype.Numeric)
    stage         TEXT NOT NULL DEFAULT 'lead', -- lead | qualified | proposal | won | lost
    contact_id    UUID REFERENCES contacts(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_deals_tenant ON deals(tenant_id, stage);

-- +goose Down
DROP TABLE deals;
