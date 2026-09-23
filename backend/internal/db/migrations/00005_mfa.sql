-- +goose Up
-- Panel MFA: TOTP + SMS + recovery codes. Two-step login.
-- SECURITY NOTE: totp_secret is sensitive. Currently stored in plaintext; future hardening =
-- encrypt at rest with an application/KMS key. sms_code/recovery are stored as a HASH.

ALTER TABLE panel_users
    ADD COLUMN totp_secret      TEXT    NOT NULL DEFAULT '',
    ADD COLUMN mfa_totp_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN phone            TEXT    NOT NULL DEFAULT '',
    ADD COLUMN mfa_sms_enabled  BOOLEAN NOT NULL DEFAULT false;

-- Recovery codes (single use). Stored with bcrypt.
CREATE TABLE recovery_codes (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   UUID NOT NULL REFERENCES panel_users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    used_at   TIMESTAMPTZ
);
CREATE INDEX idx_recovery_user ON recovery_codes(user_id) WHERE used_at IS NULL;

-- Login challenge: issued after the password when MFA is on; exchanged for a session once the 2nd factor is verified.
CREATE TABLE mfa_challenges (
    token         TEXT PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES panel_users(id) ON DELETE CASCADE,
    sms_code_hash TEXT NOT NULL DEFAULT '',
    attempts      INT  NOT NULL DEFAULT 0,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS mfa_challenges, recovery_codes CASCADE;
ALTER TABLE panel_users
    DROP COLUMN totp_secret, DROP COLUMN mfa_totp_enabled,
    DROP COLUMN phone, DROP COLUMN mfa_sms_enabled;
