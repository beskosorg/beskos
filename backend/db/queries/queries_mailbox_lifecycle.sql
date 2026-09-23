-- Mailbox lifecycle: suspend, delete with a way back, and the purge that finally forgets.
-- See migration 00134 for why deletion is a funnel rather than a DELETE.

-- name: SetMailUserStatus :one
-- Suspend or reactivate. Clears the deletion fields on the way out of 'deleted', so a restored
-- mailbox cannot be carrying a purge date nobody can see.
UPDATE mail_users
SET status          = $3,
    deleted_at      = CASE WHEN $3 = 'deleted' THEN deleted_at ELSE NULL END,
    successor_email = CASE WHEN $3 = 'deleted' THEN successor_email ELSE '' END
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: SoftDeleteMailUser :one
-- Starts the clock. The row stays, which is what keeps the address reserved: `email` is UNIQUE, so
-- nobody can be given this address while its previous owner is still recoverable.
UPDATE mail_users
SET status          = 'deleted',
    deleted_at      = now(),
    successor_email = $3
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: RestoreMailUser :one
UPDATE mail_users
SET status = 'active', deleted_at = NULL, successor_email = ''
WHERE id = $1 AND tenant_id = $2 AND status = 'deleted'
RETURNING *;

-- name: MailUsersToPurge :many
-- Whose window has closed. The cutoff is computed by the caller so the retention period lives in
-- one place in the code instead of being spelled out in SQL.
SELECT * FROM mail_users
WHERE status = 'deleted' AND deleted_at IS NOT NULL AND deleted_at < $1
ORDER BY deleted_at
LIMIT $2;

-- name: PurgeMailUser :exec
-- The only statement in the system that actually forgets a mailbox. Guarded by status so a mailbox
-- restored between the worker reading the list and acting on it is not deleted anyway.
DELETE FROM mail_users WHERE id = $1 AND status = 'deleted';

-- name: GetEmailAliasByAddress :one
-- The successor's forwarding alias, found by the address it carries. Used to take it away again
-- when the mailbox is restored: without it, a restored person's mail would keep going to whoever
-- was standing in for them.
SELECT * FROM email_aliases WHERE tenant_id = $1 AND address = $2;

-- name: DomainMailboxState :one
-- What still stands between a domain and its removal, split so the answer can say WHICH: mailboxes
-- somebody still uses, or mailboxes already deleted whose recovery window has not run out. The two
-- need opposite advice -- "delete them" and "wait, or nothing at all" -- and one count cannot tell
-- them apart (law 2.9).
SELECT count(*)                                                   AS total,
       count(*) FILTER (WHERE status = 'deleted')                  AS deleted,
       -- COALESCE so the column stays non-null and typed: with no deleted mailboxes there is
       -- no date to give, and the caller only reads this when `deleted` is above zero.
       COALESCE(min(deleted_at) FILTER (WHERE status = 'deleted'), now())::timestamptz AS oldest_deleted
FROM mail_users
WHERE tenant_id = @tenant_id AND email LIKE '%@' || @domain;

-- name: MailUsersNearingPurge :many
-- Deleted mailboxes whose window is about to close and whose warning has not gone out yet.
-- `purge_warned_at` is what stops it going out every ten minutes for a week.
SELECT * FROM mail_users
WHERE status = 'deleted' AND deleted_at IS NOT NULL
  AND deleted_at < $1 AND purge_warned_at IS NULL
ORDER BY deleted_at
LIMIT 50;

-- name: MarkPurgeWarned :exec
UPDATE mail_users SET purge_warned_at = now() WHERE id = $1;
