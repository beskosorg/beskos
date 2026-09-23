-- +goose Up
-- Slack-style chat: channels (public/private/DM), members with a read marker, messages
-- with threads, and reactions. Everything scoped by tenant.

CREATE TABLE channels (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL DEFAULT 'public',   -- public | private | dm
    name       TEXT NOT NULL DEFAULT '',         -- empty for a DM (name derived from the members)
    topic      TEXT NOT NULL DEFAULT '',
    dm_key     TEXT NOT NULL DEFAULT '',         -- canonical DM key (sorted ids) to avoid duplicates
    created_by UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One unique DM per pair (or group) within the tenant.
CREATE UNIQUE INDEX idx_channels_dm ON channels(tenant_id, dm_key) WHERE kind = 'dm';
CREATE INDEX idx_channels_tenant ON channels(tenant_id);

CREATE TABLE channel_members (
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'member',   -- owner | member
    last_read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX idx_chan_members_user ON channel_members(user_id);

CREATE TABLE messages (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    parent_id  UUID REFERENCES messages(id) ON DELETE CASCADE,  -- thread (reply)
    body       TEXT NOT NULL,
    edited_at  TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_messages_channel ON messages(channel_id, created_at);
CREATE INDEX idx_messages_parent ON messages(parent_id);

CREATE TABLE message_reactions (
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    emoji      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (message_id, user_id, emoji)
);

-- +goose Down
DROP TABLE IF EXISTS message_reactions;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS channel_members;
DROP TABLE IF EXISTS channels;
