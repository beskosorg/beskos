-- +goose Up
-- ORGANIZATION security policy: with mfa_required on, every member of the tenant without a
-- second factor MUST set up TOTP or SMS on their next access (a blocking gate in the webmail via
-- /me). The admin/security page manages this.
ALTER TABLE tenants ADD COLUMN mfa_required BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE tenants DROP COLUMN IF EXISTS mfa_required;
