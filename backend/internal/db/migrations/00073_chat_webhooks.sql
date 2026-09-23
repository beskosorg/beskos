-- +goose Up
-- Incoming webhooks (bots/integrations): one token per channel; an external POST becomes a message.
-- The message stores bot_name so the frontend shows it as an integration (instead of a human
-- author).
ALTER TABLE messages ADD COLUMN bot_name TEXT NOT NULL DEFAULT '';

CREATE TABLE chat_webhooks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    token      TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_by UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_webhooks_channel ON chat_webhooks(channel_id);

-- +goose Down
DROP TABLE chat_webhooks;
ALTER TABLE messages DROP COLUMN bot_name;
