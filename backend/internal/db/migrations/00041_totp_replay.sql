-- +goose Up
-- TOTP anti-replay: stores the last accepted time step. A code is only valid if its step is
-- greater than the last one used, so an intercepted code cannot be reused within the window.
ALTER TABLE mail_users ADD COLUMN last_totp_step BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE mail_users DROP COLUMN last_totp_step;
