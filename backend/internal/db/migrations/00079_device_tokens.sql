-- +goose Up
-- Device tokens for headless clients (the file sync daemon, written in Rust, which runs outside
-- the browser and has no session cookie). Unlike the webmail session:
--   - long-lived (the daemon stays on for months) → only the SHA-256 HASH is stored, never the raw
--     token;
--   - restricted scope ('sync' = read the changes feed + download) — no access to the rest of the API;
--   - revocable per device, with optional expiry.
CREATE TABLE device_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,                     -- device label ("Work MacBook")
    token_hash   TEXT NOT NULL UNIQUE,              -- hex SHA-256 of the token; the raw one is shown only once
    token_prefix TEXT NOT NULL,                     -- first chars, to identify it in the list
    scope        TEXT NOT NULL DEFAULT 'sync',
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,                       -- NULL = never expires
    revoked_at   TIMESTAMPTZ,                       -- set = revoked (no longer authenticates)
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Lists a user's active tokens (the index covers the common case).
CREATE INDEX idx_device_tokens_user ON device_tokens (mail_user_id, created_at DESC);

-- +goose Down
DROP TABLE device_tokens;
