-- Getting back in after forgetting the password. See migration 00137 for why a mail provider cannot
-- simply e-mail a link.

-- name: SetRecoveryEmail :exec
UPDATE mail_users SET recovery_email = $2 WHERE id = $1;

-- name: CreatePasswordReset :one
-- One live request per person: asking twice replaces the first rather than leaving two valid links
-- in an inbox. ON CONFLICT keys on the partial unique index over unused rows.
INSERT INTO password_resets (token, user_id, tenant_id, expires_at)
VALUES (@token, @user_id, @tenant_id, @expires_at)
ON CONFLICT (user_id) WHERE used_at IS NULL
DO UPDATE SET token = EXCLUDED.token, expires_at = EXCLUDED.expires_at, created_at = now()
RETURNING *;

-- name: GetPasswordReset :one
-- Only a live one: not spent, not expired. A spent or stale token is indistinguishable from a wrong
-- one on purpose -- knowing WHICH would tell somebody holding a stolen link that it was once valid.
SELECT r.*, sqlc.embed(mail_users) FROM password_resets r
JOIN mail_users ON mail_users.id = r.user_id
WHERE r.token = $1 AND r.used_at IS NULL AND r.expires_at > now();

-- name: SpendPasswordReset :exec
UPDATE password_resets SET used_at = now() WHERE token = $1;

-- name: PurgeExpiredPasswordResets :exec
-- Rubbish holding a token is a liability, even an expired one.
DELETE FROM password_resets WHERE expires_at < now() - INTERVAL '7 days';

-- name: ListTenantAdmins :many
-- Who receives the request. Owners first: in a one-person organisation the owner is the only one,
-- and in a larger one they are the person who can act when an admin is away.
SELECT * FROM mail_users
WHERE tenant_id = $1 AND role IN ('owner', 'admin') AND status = 'active'
ORDER BY (role = 'owner') DESC, created_at;

-- name: DeleteWebmailSessionsForUser :exec
-- Every session of one person. Somebody resetting a password has often just been compromised, and
-- leaving the attacker's session alive is the one outcome that makes the reset pointless.
DELETE FROM webmail_sessions WHERE mail_user_id = $1;
