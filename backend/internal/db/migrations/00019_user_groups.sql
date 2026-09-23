-- +goose Up
-- USER groups (≠ contact_groups, which is a contact catalog).
-- They serve to: (a) organize people, (b) share contacts with a group, and
-- (c) each group automatically gets a private chat channel.

CREATE TABLE user_groups (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    -- Chat channel created together with the group. ON DELETE SET NULL: deleting the group does NOT destroy
    -- the conversation — the channel keeps existing with its history.
    channel_id UUID REFERENCES channels(id) ON DELETE SET NULL,
    created_by UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);
CREATE INDEX idx_user_groups_tenant ON user_groups(tenant_id);

CREATE TABLE user_group_members (
    group_id  UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    user_id   UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    added_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX idx_ugm_user ON user_group_members(user_id);

-- +goose Down
DROP TABLE IF EXISTS user_group_members;
DROP TABLE IF EXISTS user_groups;
