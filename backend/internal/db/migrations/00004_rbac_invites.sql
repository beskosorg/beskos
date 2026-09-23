-- +goose Up
-- RBAC: user invites to the panel (fixed roles + permissions in internal/rbac).

CREATE TABLE invites (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    role        TEXT NOT NULL,
    token       TEXT NOT NULL UNIQUE,
    invited_by  TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_invites_tenant ON invites(tenant_id) WHERE accepted_at IS NULL;

-- The first user of an organization becomes "owner" (previously: tenant_admin).
UPDATE panel_users SET role = 'owner' WHERE role = 'tenant_admin';

-- +goose Down
DROP TABLE IF EXISTS invites CASCADE;
