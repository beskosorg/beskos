-- +goose Up
-- Billing: webhook idempotency (brief §10). The subscription lives in subscriptions (00001)
-- and the tenant's current plan in tenants.plano.
CREATE TABLE processed_webhooks (
    event_id     TEXT PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS processed_webhooks;
