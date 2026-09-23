-- +goose Up
-- Assistant background work (§3.6 and §7.5). Long requests no longer die with the HTTP request:
-- the conversation immediately returns a task card and the work continues in the worker.
--
-- The state is saved at every step, so the task can resume after a restart.
CREATE TABLE assistant_tasks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES assistant_conversations(id) ON DELETE CASCADE,
    agent           TEXT NOT NULL,
    -- The user's message. It is the request, and it is what gets re-run on resume.
    message         TEXT NOT NULL,
    -- queued | running | awaiting_confirmation | done | failed | cancelled
    status          TEXT NOT NULL DEFAULT 'queued',
    steps           JSONB NOT NULL DEFAULT '[]',  -- action cards already done (visible progress)
    answer          TEXT NOT NULL DEFAULT '',
    -- i18n key, never raw text to display (law 4.4/4.6).
    error_code      TEXT NOT NULL DEFAULT '',
    stopped         TEXT NOT NULL DEFAULT '',
    run_id          UUID,
    -- Cancelling is a REQUEST, not an immediate effect: the task may be in the middle of a call.
    -- The worker sees the flag on the next step and stops — what it already did stays recorded
    -- (§7.5).
    cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
    -- claimed_at is the worker's lease. A 'running' task with a stale lease belongs to a worker
    -- that died, and goes back to the queue.
    claimed_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_assistant_tasks_user ON assistant_tasks (mail_user_id, created_at DESC);
-- The worker queue: what is still to be done, oldest first.
CREATE INDEX idx_assistant_tasks_queue ON assistant_tasks (status, created_at) WHERE status IN ('queued', 'running');

-- Actions awaiting human approval, DURABLE (§6.3). The wait used to live in memory, which was
-- enough for a synchronous execution — that dies with the server anyway. A background task does
-- not: it sits "awaiting confirmation" and has to still be there after a restart.
CREATE TABLE assistant_pending_actions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id      UUID NOT NULL REFERENCES assistant_tasks(id) ON DELETE CASCADE,
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    tool         TEXT NOT NULL,
    -- The EXACT arguments that will run. This is what is shown to whoever decides: a preview that
    -- diverged from what runs would give confidence without earning it.
    args         JSONB NOT NULL DEFAULT '{}',
    -- '' = waiting. Approval is never assumed by default.
    decision     TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at   TIMESTAMPTZ
);
CREATE INDEX idx_assistant_pending_user ON assistant_pending_actions (mail_user_id, created_at DESC)
    WHERE decision = '';

-- Effects already produced by a task. This is what makes the write tools IDEMPOTENT on resume
-- (law 5.3, §7.5): a task that restarts re-runs the loop, and without this it would resend the same
-- e-mail. The fingerprint is (tool + arguments), so the same send does not go out twice — but two
-- different sends in the same task both go out, as they should.
CREATE TABLE assistant_task_effects (
    task_id     UUID NOT NULL REFERENCES assistant_tasks(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, fingerprint)
);

-- +goose Down
DROP TABLE assistant_task_effects;
DROP TABLE assistant_pending_actions;
DROP TABLE assistant_tasks;
