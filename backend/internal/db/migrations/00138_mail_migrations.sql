-- +goose Up
-- Bringing a mailbox in from the provider the customer is leaving.
--
-- WHY THE STATE LIVES IN THE DATABASE AND NOT IN A GOROUTINE. A mailbox is measured in tens of
-- thousands of messages and hours of copying. A deploy, a restart or a crash lands in the middle of
-- every real migration, and one that cannot resume is one the customer has to start again -- which,
-- the second time, they will not.

CREATE TABLE mail_migrations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    -- The mailbox being filled. One live migration per mailbox (index below): two jobs writing the
    -- same account would race on folder creation and interleave two sets of progress counters.
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,

    source_host     TEXT NOT NULL,
    source_port     INT  NOT NULL,
    source_starttls BOOLEAN NOT NULL DEFAULT false,
    source_username TEXT NOT NULL,
    -- The password to somebody's OLD mailbox -- which, for most people, is the password to several
    -- other things too. Encrypted at rest with the same key as every other stored secret, and
    -- ERASED the moment the job stops needing it (see clear_password below).
    source_password TEXT NOT NULL,

    -- pending → running → done | failed | cancelled. 'pending' is a job nobody has picked up yet;
    -- the worker moves it to 'running' and back.
    status     TEXT NOT NULL DEFAULT 'pending',
    -- A translatable key, never a sentence (law 4.4). The panel turns it into the customer's
    -- language; the raw text from a foreign IMAP server is for the log, not for a screen.
    error_code TEXT NOT NULL DEFAULT '',
    -- The server's own words, for us. Shown to nobody, but the difference between "we cannot see
    -- why it failed" and a one-line answer to a support request.
    last_error TEXT NOT NULL DEFAULT '',

    -- Consecutive failed passes over this job (law 2.8). A single failed pass is weather; a count
    -- that keeps climbing is a broken migration that would otherwise retry in silence forever.
    consecutive_failures INT NOT NULL DEFAULT 0,

    total_messages  BIGINT NOT NULL DEFAULT 0, -- what the source said there was, at the start
    copied_messages BIGINT NOT NULL DEFAULT 0,
    -- Messages the account already had. Not an error and not padding for the progress bar: it is
    -- what a resumed or re-run job is supposed to do, and hiding it makes a correct second run look
    -- like it did nothing.
    skipped_messages BIGINT NOT NULL DEFAULT 0,
    -- Messages the source would not hand over. Reported to the customer, because a migration that
    -- quietly loses eleven messages out of forty thousand is worse than one that says so.
    failed_messages BIGINT NOT NULL DEFAULT 0,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at  TIMESTAMPTZ,
    -- Last time the worker touched this job. A 'running' row whose heartbeat stopped is a job whose
    -- worker died, and it has to be distinguishable from one that is simply slow (law 2.9).
    heartbeat_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);

-- One live migration per mailbox. A partial index rather than a plain unique: a mailbox that was
-- migrated last year must still be migratable today, so finished rows do not block a new one.
CREATE UNIQUE INDEX idx_mail_migrations_live ON mail_migrations (mail_user_id)
    WHERE status IN ('pending', 'running');

CREATE INDEX idx_mail_migrations_tenant ON mail_migrations (tenant_id, created_at DESC);

-- Per-folder progress. The unit of resuming is a folder, not a job: a migration that restarted from
-- the first folder every time would never finish a large account.
CREATE TABLE mail_migration_folders (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    migration_id UUID NOT NULL REFERENCES mail_migrations(id) ON DELETE CASCADE,

    remote_name TEXT NOT NULL, -- exactly as the old server names it
    local_name  TEXT NOT NULL, -- where it lands here; empty when mapped by role
    local_role  TEXT NOT NULL DEFAULT '', -- inbox | sent | drafts | archive
    -- Empty means the folder is copied. Set means it is not, and why -- 'virtual' for Gmail's All
    -- Mail and friends, 'trash', 'junk'. The panel lists these: a folder that vanished without a
    -- stated reason looks exactly like data loss to the person counting folders afterwards.
    skip_reason TEXT NOT NULL DEFAULT '',

    -- The pair that makes resuming safe. UIDs rise monotonically WITHIN one uid_validity, so
    -- last_uid alone is a complete record of progress. If the server changes uid_validity, every
    -- recorded UID now names a different message and the folder has to be walked again.
    uid_validity BIGINT NOT NULL DEFAULT 0,
    last_uid     BIGINT NOT NULL DEFAULT 0,

    total_messages   BIGINT NOT NULL DEFAULT 0,
    copied_messages  BIGINT NOT NULL DEFAULT 0,
    skipped_messages BIGINT NOT NULL DEFAULT 0,
    failed_messages  BIGINT NOT NULL DEFAULT 0,

    -- pending → running → done | failed | skipped
    status     TEXT NOT NULL DEFAULT 'pending',
    last_error TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The same folder listed twice would be copied twice. The source can legitimately be re-listed on a
-- resume, so this is not paranoia: it is what makes re-listing safe.
CREATE UNIQUE INDEX idx_mail_migration_folders_unique
    ON mail_migration_folders (migration_id, remote_name);

CREATE INDEX idx_mail_migration_folders_job ON mail_migration_folders (migration_id, status);

-- +goose Down
DROP TABLE IF EXISTS mail_migration_folders;
DROP TABLE IF EXISTS mail_migrations;
