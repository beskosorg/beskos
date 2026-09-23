-- +goose Up
-- Reply templates (canned responses): the owner's reusable texts to insert while composing.
-- Scoped per owner (not shared) and per tenant, like deals/contacts.
CREATE TABLE mail_templates (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    subject       TEXT NOT NULL DEFAULT '',
    body_html     TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_templates_owner ON mail_templates(owner_user_id, tenant_id);

-- +goose Down
DROP TABLE mail_templates;
