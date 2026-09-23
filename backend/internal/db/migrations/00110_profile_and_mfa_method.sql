-- +goose Up
-- (1) The organization's MFA policy gains the METHOD required by the admin:
--     'any' = the user chooses TOTP or SMS; 'totp'/'sms' = only that method satisfies the gate.
ALTER TABLE tenants ADD COLUMN mfa_method TEXT NOT NULL DEFAULT 'any'
    CHECK (mfa_method IN ('any', 'totp', 'sms'));

-- (2) User profile (avatar menu): job title and photo. The photo is a small data URL
--     (the client resizes it to ~128px before sending; size cap in the handler).
ALTER TABLE mail_users ADD COLUMN job_title TEXT NOT NULL DEFAULT '';
ALTER TABLE mail_users ADD COLUMN avatar TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE mail_users DROP COLUMN IF EXISTS avatar;
ALTER TABLE mail_users DROP COLUMN IF EXISTS job_title;
ALTER TABLE tenants DROP COLUMN IF EXISTS mfa_method;
