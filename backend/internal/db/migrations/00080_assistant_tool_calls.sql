-- +goose Up
-- Audit log of EVERY tool call made by the AI assistant (§6.4 of the design).
-- It exists to answer "why was this e-mail sent?" — without it, an agent that acts on the user's
-- behalf is a black box.
--
-- Design notes:
--   - `run_id` groups the calls of one execution (one turn). It is NOT a foreign key: conversations
--     came later, and the log had to work before they existed. Conversations add
--     `conversation_id` as a new column, without touching this one.
--   - `result` stores the readable SUMMARY, not the whole payload: the content of the e-mails read
--     is not duplicated in a second table (privacy and size).
--   - the log is readable by the user themselves, not only by the administrator — hence the index
--     on mail_user_id.
CREATE TABLE assistant_tool_calls (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    run_id       UUID NOT NULL,                    -- groups the calls of one turn
    agent        TEXT NOT NULL,                    -- agent key (e.g. 'assistant')
    tool         TEXT NOT NULL,                    -- tool name (e.g. 'mail.search')
    args         JSONB NOT NULL DEFAULT '{}',      -- arguments as the model produced them
    result       TEXT NOT NULL DEFAULT '',         -- readable summary of what happened
    error        TEXT NOT NULL DEFAULT '',         -- empty = success
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- The user's own log, newest first (the audit screen).
CREATE INDEX idx_assistant_tool_calls_user ON assistant_tool_calls (mail_user_id, created_at DESC);
-- All calls of one execution, in order (the action cards of a conversation).
CREATE INDEX idx_assistant_tool_calls_run ON assistant_tool_calls (run_id, created_at);

-- +goose Down
DROP TABLE assistant_tool_calls;
