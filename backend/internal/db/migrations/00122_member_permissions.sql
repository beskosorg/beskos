-- +goose Up
-- Permission exceptions PER MEMBER, on top of the role: `allow` grants a capability the role does
-- not give; `allow=false` removes one it does give. The role remains the starting point -- this is
-- for cases no built-in role covers (e.g. only the office's finance person may delete domains, or
-- an admin who must not touch billing).
CREATE TABLE member_permissions (
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    allow      BOOLEAN NOT NULL,
    created_by UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, permission)
);
CREATE INDEX idx_member_permissions_tenant ON member_permissions(tenant_id);

-- +goose Down
DROP TABLE member_permissions;
