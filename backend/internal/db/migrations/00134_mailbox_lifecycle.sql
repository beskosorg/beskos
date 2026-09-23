-- +goose Up
-- Mailbox lifecycle: active -> suspended -> deleted (recoverable) -> purged.
--
-- WHY A FUNNEL AND NOT A DELETE BUTTON. A mailbox is not a row: it is somebody's correspondence,
-- including letters written to them by people who never signed anything with us. Article 28(3)(g)
-- of the GDPR says that at the end of the service the processor deletes OR RETURNS the data, at the
-- controller's choice -- so an irreversible delete with no export offered is not a feature we are
-- allowed to ship. The recovery window is what makes "or returns" possible after somebody clicks.
--
-- THE ROW SURVIVES DELETION ON PURPOSE. `email` is UNIQUE, so a deleted-but-not-purged mailbox
-- still holds its address, and nobody can be given it in the meantime. Mail sent to the person who
-- left would otherwise land in the inbox of whoever inherited their address -- which is how you
-- turn a resignation into a data breach. Google reserves the address for the same 20 days for the
-- same reason; we reserve it for the whole recovery window.
--
-- WHAT WE DO NOT DELETE: invoices and accounting records, which tax law requires us to keep. That
-- is the "unless Union or Member State law requires storage" carve-out of the same article, and it
-- lives in the billing tables, untouched by any of this.

ALTER TABLE mail_users
    -- active | suspended | deleted. Deliberately not a boolean: "can this person log in" and "is
    -- this address spoken for" are different questions, and a flag can only answer one.
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active',
    -- When the deletion was requested. The purge worker reads this and nothing else.
    ADD COLUMN deleted_at TIMESTAMPTZ,
    -- Who took over. Empty means mail to a deleted mailbox is rejected at the door, which is the
    -- safe default: silently forwarding somebody's mail to their ex-manager is exactly what
    -- European regulators have been fining employers for.
    ADD COLUMN successor_email TEXT NOT NULL DEFAULT '';

-- The purge worker asks one question every minute: whose window has closed? Without this it reads
-- every mailbox of every tenant to find the none that are usually due.
CREATE INDEX idx_mail_users_pending_purge ON mail_users (deleted_at)
    WHERE status = 'deleted';

-- A wrong value here is a mailbox in a state no code handles -- reachable by nobody, purged by
-- nothing, and invisible in every list that filters by the three we know about.
ALTER TABLE mail_users
    ADD CONSTRAINT mail_users_status_known CHECK (status IN ('active', 'suspended', 'deleted'));

-- +goose Down
ALTER TABLE mail_users DROP CONSTRAINT IF EXISTS mail_users_status_known;
DROP INDEX IF EXISTS idx_mail_users_pending_purge;
ALTER TABLE mail_users
    DROP COLUMN IF EXISTS successor_email,
    DROP COLUMN IF EXISTS deleted_at,
    DROP COLUMN IF EXISTS status;
