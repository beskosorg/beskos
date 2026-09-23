-- +goose Up
-- CRM: tickets and support interactions.
-- Decisions: a complete system (status, priority, assignee, thread, SLA);
-- SLA = a FIXED deadline PER PRIORITY, configurable by the admin; a ticket belongs to a contact.
-- QUEUE: there is no "queue" entity — the group_id field points at the existing `user_groups`
-- (optional, null by default). Recorded recommendation: in a small company what works is an
-- "assignee", not queue routing. The field is ready at no cost and with no data to migrate.

-- Per-tenant numbering: UPDATE ... RETURNING locks the tenant's row, so two tickets
-- created at the same time never get the same number (MAX(number)+1 would tie).
ALTER TABLE tenants ADD COLUMN ticket_seq BIGINT NOT NULL DEFAULT 0;

CREATE TABLE tickets (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    number       BIGINT NOT NULL,               -- #1, #2… per tenant
    subject      TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open',  -- open | pending | resolved | closed
    priority     TEXT NOT NULL DEFAULT 'normal',-- low | normal | high | urgent
    contact_id   UUID REFERENCES contacts(id) ON DELETE SET NULL,
    -- Who asked, by email. Always stored: the ticket must survive the contact being
    -- deleted, and an outside email can open a ticket before any contact exists.
    requester_email TEXT NOT NULL DEFAULT '',
    assignee_id  UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    group_id     UUID REFERENCES user_groups(id) ON DELETE SET NULL,
    created_by   UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    source       TEXT NOT NULL DEFAULT 'ui',    -- ui | email
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- SLA: deadlines are FROZEN at creation from the policy in force. Changing the policy
    -- later must not rewrite the clock of a ticket that was already running.
    due_first_response_at TIMESTAMPTZ,
    due_resolution_at     TIMESTAMPTZ,
    first_response_at     TIMESTAMPTZ,
    resolved_at           TIMESTAMPTZ,
    closed_at             TIMESTAMPTZ,
    CONSTRAINT tickets_number_uq UNIQUE (tenant_id, number),
    CONSTRAINT tickets_status_ck CHECK (status IN ('open', 'pending', 'resolved', 'closed')),
    CONSTRAINT tickets_priority_ck CHECK (priority IN ('low', 'normal', 'high', 'urgent'))
);
CREATE INDEX idx_tickets_tenant  ON tickets(tenant_id, status, created_at DESC);
CREATE INDEX idx_tickets_contact ON tickets(contact_id);

-- Reply thread. `internal` = a note between colleagues: NEVER goes out to the customer.
CREATE TABLE ticket_messages (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    ticket_id  UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    author_id  UUID REFERENCES mail_users(id) ON DELETE SET NULL, -- null = came from the customer
    from_email TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL,
    internal   BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ticket_messages ON ticket_messages(ticket_id, created_at);

-- SLA policy per priority, per tenant. Minutes until the 1st response and until resolution.
CREATE TABLE sla_policies (
    tenant_id              UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    priority               TEXT NOT NULL,
    first_response_minutes INT NOT NULL,
    resolution_minutes     INT NOT NULL,
    PRIMARY KEY (tenant_id, priority),
    CONSTRAINT sla_priority_ck CHECK (priority IN ('low', 'normal', 'high', 'urgent'))
);

-- Default for existing tenants (and the same one the API applies to a new tenant).
INSERT INTO sla_policies (tenant_id, priority, first_response_minutes, resolution_minutes)
SELECT t.id, p.priority, p.frm, p.res
FROM tenants t
CROSS JOIN (VALUES
    ('urgent',  60,   480),   -- 1h / 8h
    ('high',    240,  1440),  -- 4h / 24h
    ('normal',  480,  4320),  -- 8h / 3d
    ('low',     1440, 10080)  -- 24h / 7d
) AS p(priority, frm, res);

-- +goose Down
DROP TABLE IF EXISTS sla_policies;
DROP TABLE IF EXISTS ticket_messages;
DROP TABLE IF EXISTS tickets;
ALTER TABLE tenants DROP COLUMN ticket_seq;
