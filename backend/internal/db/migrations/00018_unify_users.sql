-- +goose Up
-- Unifies identity: `mail_users` becomes the ONLY users table — with or without a mailbox —
-- and `panel_users` goes away.
--
-- Context: there were two worlds. `panel_users` (/app login, password, MFA, RBAC) and `mail_users`
-- (webmail, mailbox in Stalwart). Creating a user created both; changing the role updated only the
-- panel, leaving `mail_users.role` stale. Since RBAC now reads THAT column, the mismatch
-- would become a silent authorization failure.
--
-- Decisions:
--  * An admin is a user with role 'admin' — the "internal_admin" capability (global operator with
--    a null tenant) is discontinued. That is why `tenant_id` stays NOT NULL and no webmail handler
--    has to deal with a null tenant.
--  * Accounts are created by the admin (signup already assigns 'owner' to the creator).
--  * A user without a mailbox (signup, before any domain exists) is legitimate: `stalwart_account_id` is null.

-- 1) mail_users takes over the account fields (password, language, MFA).
ALTER TABLE mail_users ADD COLUMN password_hash    TEXT    NOT NULL DEFAULT '';
ALTER TABLE mail_users ADD COLUMN language         TEXT    NOT NULL DEFAULT '';
ALTER TABLE mail_users ADD COLUMN totp_secret      TEXT    NOT NULL DEFAULT '';
ALTER TABLE mail_users ADD COLUMN mfa_totp_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE mail_users ADD COLUMN phone            TEXT    NOT NULL DEFAULT '';
ALTER TABLE mail_users ADD COLUMN mfa_sms_enabled  BOOLEAN NOT NULL DEFAULT false;

-- 2) Where the email exists on both sides, the mail_user takes over password/MFA/language and the
--    panel ROLE — which is authoritative today (mail_users.role never authorized anything).
UPDATE mail_users m SET
    password_hash    = p.password_hash,
    language         = p.language,
    totp_secret      = p.totp_secret,
    mfa_totp_enabled = p.mfa_totp_enabled,
    phone            = p.phone,
    mfa_sms_enabled  = p.mfa_sms_enabled,
    role             = p.role
FROM panel_users p
WHERE p.email = m.email;

-- 3) Bring over the panel_users that have no mail_user (signup/invite accounts, no mailbox).
--    Preserves the id, so sessions/recovery_codes/mfa_challenges keep pointing at the right row.
--    Rows with a null tenant (the old internal_admin) are NOT migrated — capability discontinued.
INSERT INTO mail_users (id, tenant_id, email, name, role, stalwart_account_id, quota_bytes,
                        password_hash, language, totp_secret, mfa_totp_enabled, phone,
                        mfa_sms_enabled, created_at)
SELECT p.id, p.tenant_id, p.email, '', p.role, NULL, 0,
       p.password_hash, p.language, p.totp_secret, p.mfa_totp_enabled, p.phone,
       p.mfa_sms_enabled, p.created_at
FROM panel_users p
WHERE p.tenant_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM mail_users m WHERE m.email = p.email);

-- 4) Repoint the rows matched by email: the mail_user id differs from the panel_user id.
UPDATE sessions s        SET user_id = m.id FROM panel_users p JOIN mail_users m ON m.email = p.email WHERE s.user_id = p.id AND m.id <> p.id;
UPDATE recovery_codes rc SET user_id = m.id FROM panel_users p JOIN mail_users m ON m.email = p.email WHERE rc.user_id = p.id AND m.id <> p.id;
UPDATE mfa_challenges mc SET user_id = m.id FROM panel_users p JOIN mail_users m ON m.email = p.email WHERE mc.user_id = p.id AND m.id <> p.id;

-- 5) Discard what was left orphaned (e.g. a session of a global operator that was not migrated).
DELETE FROM sessions       WHERE user_id NOT IN (SELECT id FROM mail_users);
DELETE FROM recovery_codes WHERE user_id NOT IN (SELECT id FROM mail_users);
DELETE FROM mfa_challenges WHERE user_id NOT IN (SELECT id FROM mail_users);

-- 6) Point the FKs at mail_users.
ALTER TABLE sessions       DROP CONSTRAINT sessions_user_id_fkey;
ALTER TABLE sessions       ADD  CONSTRAINT sessions_user_id_fkey       FOREIGN KEY (user_id) REFERENCES mail_users(id) ON DELETE CASCADE;
ALTER TABLE recovery_codes DROP CONSTRAINT recovery_codes_user_id_fkey;
ALTER TABLE recovery_codes ADD  CONSTRAINT recovery_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES mail_users(id) ON DELETE CASCADE;
ALTER TABLE mfa_challenges DROP CONSTRAINT mfa_challenges_user_id_fkey;
ALTER TABLE mfa_challenges ADD  CONSTRAINT mfa_challenges_user_id_fkey FOREIGN KEY (user_id) REFERENCES mail_users(id) ON DELETE CASCADE;

-- 7) Normalize the role vocabulary. `mail_users.role` used 'user', which does NOT exist in RBAC
--    (owner|admin|member|billing) — and an unknown role is denied (fail-closed). Without this, those
--    users would be left with no permission at all once RBAC reads this column.
UPDATE mail_users SET role = 'member' WHERE role NOT IN ('owner', 'admin', 'member', 'billing');

-- 8) panel_users goes away.
DROP TABLE panel_users;

-- +goose Down
-- Recreates the table and restores the accounts WITH a password (the ones from the panel). Restores the FKs.
-- ASYMMETRIC ON PURPOSE: the migrated accounts also remain in mail_users, and 'user' is not
-- restored (it became 'member'). Deleting them would cascade to contacts/boards/files they
-- created — in an emergency rollback, harmless residue is better than data loss.
CREATE TABLE panel_users (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID REFERENCES tenants(id) ON DELETE CASCADE,
    email            TEXT NOT NULL UNIQUE,
    password_hash    TEXT NOT NULL,
    role             TEXT NOT NULL DEFAULT 'tenant_admin',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    language         TEXT NOT NULL DEFAULT '',
    totp_secret      TEXT NOT NULL DEFAULT '',
    mfa_totp_enabled BOOLEAN NOT NULL DEFAULT false,
    phone            TEXT NOT NULL DEFAULT '',
    mfa_sms_enabled  BOOLEAN NOT NULL DEFAULT false
);
INSERT INTO panel_users (id, tenant_id, email, password_hash, role, created_at, language,
                         totp_secret, mfa_totp_enabled, phone, mfa_sms_enabled)
SELECT id, tenant_id, email, password_hash, role, created_at, language,
       totp_secret, mfa_totp_enabled, phone, mfa_sms_enabled
FROM mail_users WHERE password_hash <> '';

DELETE FROM sessions       WHERE user_id NOT IN (SELECT id FROM panel_users);
DELETE FROM recovery_codes WHERE user_id NOT IN (SELECT id FROM panel_users);
DELETE FROM mfa_challenges WHERE user_id NOT IN (SELECT id FROM panel_users);

ALTER TABLE sessions       DROP CONSTRAINT sessions_user_id_fkey;
ALTER TABLE sessions       ADD  CONSTRAINT sessions_user_id_fkey       FOREIGN KEY (user_id) REFERENCES panel_users(id) ON DELETE CASCADE;
ALTER TABLE recovery_codes DROP CONSTRAINT recovery_codes_user_id_fkey;
ALTER TABLE recovery_codes ADD  CONSTRAINT recovery_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES panel_users(id) ON DELETE CASCADE;
ALTER TABLE mfa_challenges DROP CONSTRAINT mfa_challenges_user_id_fkey;
ALTER TABLE mfa_challenges ADD  CONSTRAINT mfa_challenges_user_id_fkey FOREIGN KEY (user_id) REFERENCES panel_users(id) ON DELETE CASCADE;

ALTER TABLE mail_users DROP COLUMN password_hash;
ALTER TABLE mail_users DROP COLUMN language;
ALTER TABLE mail_users DROP COLUMN totp_secret;
ALTER TABLE mail_users DROP COLUMN mfa_totp_enabled;
ALTER TABLE mail_users DROP COLUMN phone;
ALTER TABLE mail_users DROP COLUMN mfa_sms_enabled;
