-- +goose Up
-- Persisted assistant conversations (§7.4 of the design). What used to live in a single HTTP
-- request and then be forgotten now has a thread: each conversation is a history that can be
-- reopened, and the thread context (§3.5.2) goes back to the model on subsequent executions.
--
-- Design notes:
--   - `mail_user_id` + `tenant_id` are the mandatory filter (law 5.1). A conversation is PERSONAL:
--     it is not shared between users of the same tenant (§3.5).
--   - `agent` stores the key of the profile that answered. Each agent has its own conversations
--     (§3.2), and seeing which one answered is a requirement, not an extra.
--   - `title` is derived from the user's first message — not from the model. An AI-generated title
--     would cost one call per conversation and say less than the sentence itself.
CREATE TABLE assistant_conversations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    agent        TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- The user's own list, newest first.
CREATE INDEX idx_assistant_conversations_user ON assistant_conversations (mail_user_id, updated_at DESC);

-- Messages of a conversation. Includes the action cards (§3.3): the record of the real call stays
-- with the message that triggered it, so reopening the conversation shows what was seen live.
--
-- `steps` stores the SUMMARY of each call, not the payload of what was read — e-mail bodies are
-- not duplicated in a table of ours (privacy and size). It is the same choice as
-- `assistant_tool_calls`.
CREATE TABLE assistant_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES assistant_conversations(id) ON DELETE CASCADE,
    role            TEXT NOT NULL,                  -- 'user' | 'assistant'
    content         TEXT NOT NULL DEFAULT '',
    steps           JSONB NOT NULL DEFAULT '[]',    -- action cards of this response
    stopped         TEXT NOT NULL DEFAULT '',       -- stop reason (i18n key), empty = answered
    run_id          UUID,                           -- links to the execution's audit records
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_assistant_messages_conv ON assistant_messages (conversation_id, created_at);

-- The audit log was deliberately created without a conversation (conversations did not exist
-- yet). Now they do: the column is NULLABLE because the rows written before then point to no
-- conversation at all.
ALTER TABLE assistant_tool_calls
    ADD COLUMN conversation_id UUID REFERENCES assistant_conversations(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE assistant_tool_calls DROP COLUMN conversation_id;
DROP TABLE assistant_messages;
DROP TABLE assistant_conversations;
