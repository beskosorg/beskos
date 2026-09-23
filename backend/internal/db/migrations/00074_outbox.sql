-- +goose Up
-- Server-side send outbox: the message is created as a draft in JMAP and the actual send is
-- scheduled here. The worker submits it when `send_at` is due (via EmailSubmission/set with the
-- brokered credential). It covers two cases: undo-send (send_at = now + a short window; cancelling
-- before then = not sent) and manageable scheduled sending (list/cancel). It survives closing the
-- tab — the client does not need to stay alive.
CREATE TABLE outbox_emails (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    email_id    TEXT NOT NULL,            -- id of the draft in JMAP (Email)
    identity_id TEXT NOT NULL,            -- JMAP identity of the "From:"
    from_email  TEXT NOT NULL,
    rcpts       TEXT[] NOT NULL,          -- envelope recipients (to+cc+bcc)
    subject     TEXT NOT NULL DEFAULT '', -- only for the management UI
    send_at     TIMESTAMPTZ NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending', -- pending | sent | canceled | failed
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- The worker sweeps the due pending rows; the partial index keeps the sweep cheap.
CREATE INDEX idx_outbox_due ON outbox_emails(send_at) WHERE status = 'pending';
CREATE INDEX idx_outbox_user ON outbox_emails(user_id, status);

-- +goose Down
DROP TABLE outbox_emails;
