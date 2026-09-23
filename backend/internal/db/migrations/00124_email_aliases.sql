-- +goose Up
-- Email aliases: an extra address that delivers to ANOTHER person's mailbox. Its own table, and
-- deliberately not a row in mail_users -- an alias is not a person: it does not join chat, the
-- member list, or groups, and that separation is guaranteed by the structure instead of depending
-- on a filter someone would forget to add to a new query.
-- Billing counts them as users (queries_usage.sql), which is the required business rule.
CREATE TABLE email_aliases (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    domain_id    UUID NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    -- Owner: the mailbox that receives mail for this address. Deleting the mailbox takes the alias
    -- with it.
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    address      TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- An address can exist only once, and can never clash with a real mailbox.
CREATE UNIQUE INDEX idx_email_aliases_address ON email_aliases (lower(address));
CREATE INDEX idx_email_aliases_user ON email_aliases (mail_user_id);
CREATE INDEX idx_email_aliases_tenant ON email_aliases (tenant_id);

-- +goose Down
DROP TABLE email_aliases;
