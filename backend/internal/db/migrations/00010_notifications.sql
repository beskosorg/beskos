-- +goose Up
-- In-app notifications (bell) per user. Meeting invites, cancellations, etc.

CREATE TABLE notifications (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,            -- meeting_invite | meeting_canceled | ...
    title       TEXT NOT NULL,
    body        TEXT NOT NULL DEFAULT '',
    link        TEXT NOT NULL DEFAULT '', -- destination on click (e.g. /mail/meet)
    meeting_id  UUID,
    read_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_notif_user ON notifications(user_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS notifications;
