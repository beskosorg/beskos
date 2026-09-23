-- +goose Up
-- JMAP brokering (audit H11): the mailbox credential (Basic auth) no longer lives in the browser.
-- We store the user's credential here, ENCRYPTED (AES-GCM, enc: prefix); the JMAP proxy injects it
-- server-to-server. The browser authenticates only with the session (cookie/bearer). Empty = no
-- mailbox, or a login from before the feature (forces re-login). See deploy/DIVERGENCES.md.
ALTER TABLE mail_users ADD COLUMN jmap_cred TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE mail_users DROP COLUMN jmap_cred;
