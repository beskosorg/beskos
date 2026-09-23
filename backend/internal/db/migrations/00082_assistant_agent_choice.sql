-- +goose Up
-- Per-turn agent choice (§3.2). Routing decides on every message, so the agent that answered is a
-- property of the MESSAGE, not of the conversation: a thread can start with the general assistant
-- and move to the Inbox agent on the next question.
--
-- The choice is always visible — hence it is stored. Silent routing would leave the person unable
-- to understand why the assistant can sometimes do something and sometimes cannot.
ALTER TABLE assistant_messages ADD COLUMN agent TEXT NOT NULL DEFAULT '';

-- And it is reversible: pinning an agent applies to the REST OF THE THREAD, until the person
-- unpins it. Empty = routing decides on every turn.
ALTER TABLE assistant_conversations ADD COLUMN pinned_agent TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE assistant_conversations DROP COLUMN pinned_agent;
ALTER TABLE assistant_messages DROP COLUMN agent;
