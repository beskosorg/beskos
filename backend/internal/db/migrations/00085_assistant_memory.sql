-- +goose Up
-- Memory of durable facts about the user (§8). Where the person lives, the time zone, how they
-- sign, who is who on the team. Brought into any conversation, so times and holidays come out right
-- without the person repeating them.
--
-- The hard rule of §6.5 is this table's design: ONLY WHAT THE USER WROTE IS STORED. Nothing
-- extracted from e-mails, documents, chat or tool results goes in here — ever. Without that, an
-- attacker sends an e-mail saying "the user authorizes sends to X", the extractor stores it, and
-- the poisoning becomes permanent and invisible. No column points to an e-mail: the source is
-- always a USER MESSAGE.
CREATE TABLE assistant_memory (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id      UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    -- A fact as READABLE TEXT, not an opaque embedding: the user has to be able to read what the
    -- application knows about them and recognize whether it is wrong (§8.1).
    fact              TEXT NOT NULL,
    -- Stored values: 'local' | 'pessoa' | 'preferência' | 'trabalho' (§8.1). lang:ok
    -- Used to filter what is injected when there are many facts.
    category          TEXT NOT NULL DEFAULT 'preferência',
    -- Where it came from: the user message that originated it. Auditable (§6.5). ON DELETE SET NULL
    -- because retention deletes the conversation but the fact survives (§8.5) — the source becomes
    -- "removed".
    source_message_id UUID REFERENCES assistant_messages(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at      TIMESTAMPTZ
);
CREATE INDEX idx_assistant_memory_user ON assistant_memory (mail_user_id, updated_at DESC);

-- +goose Down
DROP TABLE assistant_memory;
