-- +goose Up
-- Getting back in after forgetting the password.
--
-- WHY A MAIL PROVIDER CANNOT DO THE USUAL THING. Everywhere else, "forgot your password" sends a
-- link to your e-mail. Here the e-mail IS the thing you are locked out of, so that link would be
-- delivered to the mailbox nobody can open. The way back has to run outside the mail.
--
-- Two ways, and the default is the one that needs no new secrets: the request goes to the
-- organisation's administrators, who already have a button for it. The second exists for the case
-- the first cannot cover -- the owner of a one-person organisation, who has no administrator above
-- them and would otherwise be locked out permanently.

-- An address OUTSIDE the domain, so it survives losing access to the mailbox. Empty means the only
-- way back is an administrator, which for an employee is the right answer anyway.
ALTER TABLE mail_users ADD COLUMN recovery_email TEXT NOT NULL DEFAULT '';

CREATE TABLE password_resets (
    token      TEXT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    -- Marked rather than deleted on use: a link that was already spent must be distinguishable from
    -- one that never existed. Deleting it makes a replay look like a typo, and the person who
    -- reports "the link says invalid" gets told to try again forever.
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One live request per person. Asking twice replaces the first link rather than leaving two valid
-- ones lying in an inbox -- and it stops somebody filling a mailbox by pressing the button.
CREATE UNIQUE INDEX idx_password_resets_live ON password_resets (user_id) WHERE used_at IS NULL;

-- The purge reads this: expired requests are rubbish, and rubbish holding a token is a liability.
CREATE INDEX idx_password_resets_expiry ON password_resets (expires_at);

-- +goose Down
DROP TABLE IF EXISTS password_resets;
ALTER TABLE mail_users DROP COLUMN IF EXISTS recovery_email;
