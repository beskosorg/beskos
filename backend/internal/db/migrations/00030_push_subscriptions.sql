-- +goose Up
-- Web Push subscriptions (desktop notifications while Beskos is closed). One row per browser/
-- device: `endpoint` is the browser's push service URL and is globally unique. p256dh/auth are the
-- subscription's public keys, used to encrypt the payload (RFC 8291).
CREATE TABLE push_subscriptions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    endpoint   TEXT NOT NULL UNIQUE,
    p256dh     TEXT NOT NULL,
    auth       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_push_subscriptions_user ON push_subscriptions (user_id);

-- +goose Down
DROP TABLE push_subscriptions;
