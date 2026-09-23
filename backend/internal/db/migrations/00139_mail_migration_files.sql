-- +goose Up
-- Importing from a FILE, not from a server.
--
-- WHY A SECOND KIND AT ALL. IMAP reaches mail that a provider still holds. It reaches nothing at
-- all for the mail somebody has on their own disk: years of POP3 sitting in Thunderbird's Local
-- Folders, an archive exported before an account was closed, a mailbox handed over on a memory
-- stick when an employee left. That mail exists nowhere on the network, so no amount of IMAP will
-- ever find it, and until now the answer to those customers was nothing at all.
--
-- Everything else is deliberately shared with the IMAP kind: the same job row, the same per-folder
-- progress, the same watermark, the same worker. A file import is a different SOURCE, not a
-- different migration -- and giving it its own tables would have meant two of every screen, two
-- progress bars and two places for the resume logic to be wrong.

ALTER TABLE mail_migrations
    -- 'imap' (a server we log in to) | 'file' (something the customer uploaded).
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'imap',
    -- The name the customer's own file had. Shown back to them, because "Local Folders.mbox" is how
    -- they think of it and the storage key below means nothing to anybody.
    ADD COLUMN source_file TEXT NOT NULL DEFAULT '',
    -- Where the upload is parked while the job runs. ERASED when the job ends, exactly like the
    -- source password: holding a second copy of somebody's whole mailbox after the reason for it
    -- has passed is storage we are not entitled to and a breach we would have to report.
    ADD COLUMN source_key TEXT NOT NULL DEFAULT '';

-- For a file job, last_uid counts MESSAGES ALREADY READ from that entry rather than an IMAP UID.
-- Same column, same "only ever moves forward" rule, same resume: the worker walks the file and
-- skips what it has already imported. Saying so here because the column name lies to a reader who
-- has only seen the IMAP side.
COMMENT ON COLUMN mail_migration_folders.last_uid IS
    'IMAP jobs: highest UID copied. File jobs: how many messages of this entry were read.';

-- +goose Down
ALTER TABLE mail_migrations
    DROP COLUMN IF EXISTS kind,
    DROP COLUMN IF EXISTS source_file,
    DROP COLUMN IF EXISTS source_key;
