-- +goose Up
-- Email snooze: the message leaves the mailbox (the client moves it to the "Snoozed" folder) and the
-- worker returns it to the Inbox at wake_at, even with the app closed. We store only the JMAP id and
-- the time.
CREATE TABLE snoozed_emails (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    email_id     TEXT NOT NULL,   -- JMAP id of the message
    wake_at      TIMESTAMPTZ NOT NULL,
    done         BOOLEAN NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_snooze_due ON snoozed_emails (wake_at) WHERE NOT done;

-- +goose Down
DROP TABLE snoozed_emails;
