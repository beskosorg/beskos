-- +goose Up
-- Scheduled chat messages + "remind me" about a message. The worker sweeps the due rows: it sends
-- the scheduled message (and publishes it to the hub) or creates a reminder notification.
CREATE TABLE scheduled_messages (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    send_at    TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sched_msgs_due ON scheduled_messages(send_at);

CREATE TABLE message_reminders (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    message_id UUID REFERENCES messages(id) ON DELETE CASCADE,
    note       TEXT NOT NULL DEFAULT '',
    remind_at  TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_msg_reminders_due ON message_reminders(remind_at);

-- +goose Down
DROP TABLE message_reminders;
DROP TABLE scheduled_messages;
