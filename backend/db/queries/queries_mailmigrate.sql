-- Bringing a mailbox in from the provider the customer is leaving. See migration 00138 for why the
-- progress lives here rather than in the worker's memory.

-- name: CreateMailMigration :one
INSERT INTO mail_migrations (
    tenant_id, mail_user_id, source_host, source_port, source_starttls,
    source_username, source_password
) VALUES (@tenant_id, @mail_user_id, @source_host, @source_port, @source_starttls,
          @source_username, @source_password)
RETURNING *;

-- name: CreateMailFileMigration :one
-- The same job row, from an uploaded file instead of a server. Host, user and password stay empty:
-- there is nothing to log in to.
INSERT INTO mail_migrations (
    tenant_id, mail_user_id, kind, source_file, source_key,
    source_host, source_port, source_username, source_password
) VALUES (@tenant_id, @mail_user_id, 'file', @source_file, @source_key, '', 0, '', '')
RETURNING *;

-- name: ClearMailMigrationFile :exec
-- Forget where the upload is parked. Called after the object itself is deleted, so a key never
-- outlives the file it names -- a row pointing at a bucket entry that is gone reads as a bug
-- forever after.
UPDATE mail_migrations SET source_key = '' WHERE id = $1;

-- name: GetMailMigration :one
-- Tenant-scoped (law 5.1): the id alone would let one organisation read another's job, password
-- column and all.
SELECT * FROM mail_migrations WHERE id = $1 AND tenant_id = $2;

-- name: LiveMailMigrationForUser :one
-- The job the panel is showing. Only one can be live at a time -- see the partial unique index.
SELECT * FROM mail_migrations
WHERE mail_user_id = $1 AND status IN ('pending', 'running');

-- name: LastMailMigrationForUser :one
-- What happened last time, for the panel to show after a job ends.
SELECT * FROM mail_migrations
WHERE mail_user_id = $1 ORDER BY created_at DESC LIMIT 1;

-- name: ListTenantMailMigrations :many
-- Every migration in the organisation, for an administrator watching a company-wide move. The
-- password column is never sent out of the API -- the handler picks the fields.
SELECT m.*, u.email AS mailbox_email FROM mail_migrations m
JOIN mail_users u ON u.id = m.mail_user_id
WHERE m.tenant_id = $1
ORDER BY m.created_at DESC LIMIT 200;

-- name: ClaimMailMigration :one
-- Hand one waiting job to one worker.
--
-- FOR UPDATE SKIP LOCKED is what allows more than one worker to exist without them copying the same
-- mailbox twice. A stalled 'running' row is claimable again after its heartbeat goes quiet: that is
-- the only way a job whose worker was killed mid-folder ever moves again.
UPDATE mail_migrations SET
    status = 'running',
    started_at = COALESCE(started_at, now()),
    heartbeat_at = now()
WHERE id = (
    SELECT id FROM mail_migrations
    WHERE status = 'pending'
       OR (status = 'running' AND heartbeat_at < now() - INTERVAL '10 minutes')
    ORDER BY created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: HeartbeatMailMigration :exec
-- Proof the worker is still on it. Without this, a job copying a slow 30 GB mailbox is
-- indistinguishable from one whose process died, and another worker would steal it mid-folder.
UPDATE mail_migrations SET heartbeat_at = now() WHERE id = $1;

-- name: FinishMailMigration :exec
-- Terminal state, and the moment the source password stops existing. Keeping it after the copy is
-- over buys nothing and stores somebody's Google password indefinitely.
UPDATE mail_migrations SET
    status = @status,
    error_code = @error_code,
    last_error = @last_error,
    source_password = '',
    finished_at = now(),
    consecutive_failures = CASE WHEN @status::text = 'done' THEN 0 ELSE consecutive_failures END
WHERE id = @id;

-- name: ReleaseMailMigration :exec
-- Put a job back in the queue after a pass that failed for a reason that may not repeat (the source
-- refused the connection, the network blinked). consecutive_failures is what stops this being an
-- infinite polite retry: the worker escalates on it (law 2.8).
UPDATE mail_migrations SET
    status = 'pending',
    consecutive_failures = consecutive_failures + 1,
    last_error = @last_error,
    error_code = @error_code,
    heartbeat_at = NULL
WHERE id = @id;

-- name: ResetMailMigrationFailures :exec
-- A pass that got through. Recovery is worth recording, not only failure -- an alert that never
-- says "fixed" trains people to ignore it.
UPDATE mail_migrations SET consecutive_failures = 0, error_code = '', last_error = ''
WHERE id = $1 AND consecutive_failures > 0;

-- name: CancelMailMigration :exec
-- The customer changed their mind. The worker notices at the next folder boundary; whatever already
-- landed stays, because deleting mail we just copied on the way out is not a cancellation, it is a
-- second, worse migration.
UPDATE mail_migrations SET
    status = 'cancelled', source_password = '', finished_at = now()
WHERE id = $1 AND tenant_id = $2 AND status IN ('pending', 'running');

-- name: SetMailMigrationTotal :exec
UPDATE mail_migrations SET total_messages = $2 WHERE id = $1;

-- name: AddMailMigrationProgress :exec
-- Rolled up from the folders as they go, so the panel has a number without summing a hundred rows
-- on every poll.
UPDATE mail_migrations SET
    copied_messages = copied_messages + @copied,
    skipped_messages = skipped_messages + @skipped,
    failed_messages = failed_messages + @failed,
    heartbeat_at = now()
WHERE id = @id;

-- ---------- Folders ----------

-- name: UpsertMailMigrationFolder :one
-- Idempotent by (migration, remote_name), because a resumed job re-lists the source and must find
-- its own progress rather than start a second copy of the same folder. The counters and last_uid are
-- deliberately NOT touched on conflict: they are the progress.
INSERT INTO mail_migration_folders (
    migration_id, remote_name, local_name, local_role, skip_reason, uid_validity, total_messages, status
) VALUES (@migration_id, @remote_name, @local_name, @local_role, @skip_reason, @uid_validity, @total_messages,
          CASE WHEN @skip_reason::text <> '' THEN 'skipped' ELSE 'pending' END)
ON CONFLICT (migration_id, remote_name) DO UPDATE SET
    total_messages = EXCLUDED.total_messages,
    updated_at = now()
RETURNING *;

-- name: ListMailMigrationFolders :many
SELECT * FROM mail_migration_folders WHERE migration_id = $1 ORDER BY remote_name;

-- name: NextMailMigrationFolder :one
-- The next folder to work on. Ordered so the inbox goes first: it is the folder the customer opens
-- to decide whether the migration worked, and the one they need before the archive of 2019.
SELECT * FROM mail_migration_folders
WHERE migration_id = $1 AND status IN ('pending', 'running')
ORDER BY (local_role = 'inbox') DESC, (local_role <> '') DESC, remote_name
LIMIT 1;

-- name: StartMailMigrationFolder :exec
UPDATE mail_migration_folders SET status = 'running', updated_at = now() WHERE id = $1;

-- name: AdvanceMailMigrationFolder :exec
-- One page of messages, recorded. last_uid only ever moves forward: a page that arrived out of order
-- (or a retry of one already counted) must not rewind the watermark and re-copy what came after it.
UPDATE mail_migration_folders SET
    last_uid = GREATEST(last_uid, @last_uid),
    copied_messages = copied_messages + @copied,
    skipped_messages = skipped_messages + @skipped,
    failed_messages = failed_messages + @failed,
    updated_at = now()
WHERE id = @id;

-- name: ResetMailMigrationFolder :exec
-- The source changed UIDVALIDITY, so every UID we recorded now names a different message. The only
-- safe reading of that is that we know nothing about this folder, so the walk starts over. Duplicate
-- imports are caught on arrival (JMAP reports alreadyExists) and counted as skipped.
UPDATE mail_migration_folders SET
    uid_validity = @uid_validity, last_uid = 0, updated_at = now()
WHERE id = @id;

-- name: FinishMailMigrationFolder :exec
UPDATE mail_migration_folders SET
    status = @status, last_error = @last_error, updated_at = now()
WHERE id = @id;

-- name: CountUnfinishedMailMigrationFolders :one
SELECT COUNT(*) FROM mail_migration_folders
WHERE migration_id = $1 AND status IN ('pending', 'running');

-- name: PurgeFinishedMailMigrations :exec
-- Old jobs are history, not data. Ninety days is long enough to answer "what happened when we
-- moved" and short enough that the row does not outlive the reason it existed.
DELETE FROM mail_migrations
WHERE finished_at IS NOT NULL AND finished_at < now() - INTERVAL '90 days';
