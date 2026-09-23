-- +goose Up
-- A SINGLE contact: company or person, all owned by the organization (decided on 2026-07-15, afternoon).
--
-- History: earlier the same day we separated the address book (private) from clients (shared) because
-- the model contradicted itself. The separation resolved the contradiction but created two places for
-- "people" — and the original complaint was never the single table, it was the privacy CEREMONY
-- (3-level selector, sharing tables). This step keeps the ceremony removed and undoes the separation:
--
--   `contacts` = everything. kind = 'company' | 'person'. `company_id` points at the company contact.
--
-- VISIBILITY by GROUP, not per contact: the contact belongs to its owner and is visible to members of
-- the TEAMS (user_groups) its CONTACT GROUPS were shared with. The decision is made once per
-- group — choosing contact by contact was the burden. An unshared group = only the owner sees it.
-- EDIT/DELETE remains owner-only (viewing ≠ changing).

ALTER TABLE contacts ADD COLUMN kind       TEXT NOT NULL DEFAULT 'person';
ALTER TABLE contacts ADD COLUMN company_id UUID REFERENCES contacts(id) ON DELETE SET NULL;
ALTER TABLE contacts ADD COLUMN domain     TEXT NOT NULL DEFAULT '';   -- only meaningful for kind='company'
ALTER TABLE contacts ADD COLUMN allow_phone BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE contacts ADD COLUMN allow_email BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE contacts ADD CONSTRAINT contacts_kind_ck CHECK (kind IN ('company', 'person'));
-- A company is subordinate to no one; a person points at a company (or at none).
ALTER TABLE contacts ADD CONSTRAINT contacts_company_ck CHECK (kind = 'person' OR company_id IS NULL);
CREATE INDEX idx_contacts_company ON contacts(company_id);
CREATE INDEX idx_contacts_tenant_kind ON contacts(tenant_id, kind, name);

-- Sharing of a CONTACT GROUP with a TEAM of users.
CREATE TABLE contact_group_shares (
    group_id      UUID NOT NULL REFERENCES contact_groups(id) ON DELETE CASCADE,
    user_group_id UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_group_id)
);

-- 1) clients → contacts, KEEPING the ids: notes, tasks and tickets keep pointing at the right row.
INSERT INTO contacts (id, tenant_id, owner_user_id, kind, name, email, phone, domain, notes, tags, created_at)
SELECT c.id, c.tenant_id, c.owner_user_id, c.kind, c.name, c.email, c.phone, c.domain, c.notes, c.tags, c.created_at
FROM clients c;

-- 2) client_contacts → contacts (people), linked to the company through company_id.
INSERT INTO contacts (tenant_id, owner_user_id, kind, name, email, phone, mobile, is_whatsapp,
                      allow_phone, allow_email, title, company_id, notes, tags, created_at)
SELECT p.tenant_id,
       (SELECT cl.owner_user_id FROM clients cl WHERE cl.id = p.client_id),
       'person', p.name, p.email, p.phone, p.mobile, p.is_whatsapp,
       p.allow_phone, p.allow_email, p.title, p.client_id, p.notes, '{}', p.created_at
FROM client_contacts p;

-- 3) The work points at the contact again (the id is the same as the old client's).
ALTER TABLE cards     RENAME COLUMN client_id TO contact_id;
ALTER TABLE tickets   RENAME COLUMN client_id TO contact_id;
ALTER TABLE crm_notes RENAME COLUMN client_id TO contact_id;

-- 4) The FKs pointed at `clients`; now they point at `contacts`.
ALTER TABLE cards     DROP CONSTRAINT IF EXISTS cards_client_id_fkey;
ALTER TABLE tickets   DROP CONSTRAINT IF EXISTS tickets_client_id_fkey;
ALTER TABLE crm_notes DROP CONSTRAINT IF EXISTS crm_notes_client_id_fkey;
ALTER TABLE cards     ADD CONSTRAINT cards_contact_id_fkey     FOREIGN KEY (contact_id) REFERENCES contacts(id) ON DELETE SET NULL;
ALTER TABLE tickets   ADD CONSTRAINT tickets_contact_id_fkey   FOREIGN KEY (contact_id) REFERENCES contacts(id) ON DELETE SET NULL;
ALTER TABLE crm_notes ADD CONSTRAINT crm_notes_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES contacts(id) ON DELETE CASCADE;

DROP TABLE client_contacts;
DROP TABLE clients;

-- Contacts that came from `clients` were visible to the whole tenant. Without a shared group they
-- would become owner-only — invisible to colleagues overnight. Put them in a NEW group, shared
-- with every team, preserving who saw what.
--
-- A dedicated name on purpose: reusing an existing group (e.g. a "Clientes" someone already
-- created) would share that person's PERSONAL group with everyone — changing their intent without
-- them asking. DISTINCT ON guarantees one group per TENANT, not one per owner.
INSERT INTO contact_groups (tenant_id, owner_user_id, name, color)
SELECT DISTINCT ON (c.tenant_id) c.tenant_id, c.owner_user_id, 'Contatos da empresa', '#116466'
FROM contacts c
WHERE (c.kind = 'company' OR c.company_id IS NOT NULL)
  AND NOT EXISTS (SELECT 1 FROM contact_groups g WHERE g.tenant_id = c.tenant_id AND g.name = 'Contatos da empresa')
ORDER BY c.tenant_id, c.created_at;

INSERT INTO contact_group_members (contact_id, group_id)
SELECT c.id, g.id FROM contacts c
JOIN contact_groups g ON g.tenant_id = c.tenant_id AND g.name = 'Contatos da empresa'
WHERE c.kind = 'company' OR c.company_id IS NOT NULL
ON CONFLICT DO NOTHING;

INSERT INTO contact_group_shares (group_id, user_group_id)
SELECT g.id, ug.id FROM contact_groups g
JOIN user_groups ug ON ug.tenant_id = g.tenant_id
WHERE g.name = 'Contatos da empresa'
ON CONFLICT DO NOTHING;

-- +goose Down
-- ASYMMETRIC (same reason as 00018 and 00022): Up merges populations and Down does not know who was
-- who. It restores the schema so goose can migrate down; restoring data = restoring a backup.
CREATE TABLE clients (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL DEFAULT 'company',
    name          TEXT NOT NULL,
    email         TEXT NOT NULL DEFAULT '',
    domain        TEXT NOT NULL DEFAULT '',
    phone         TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT '',
    tags          TEXT[] NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE client_contacts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    client_id   UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    email       TEXT NOT NULL DEFAULT '',
    phone       TEXT NOT NULL DEFAULT '',
    mobile      TEXT NOT NULL DEFAULT '',
    is_whatsapp BOOLEAN NOT NULL DEFAULT false,
    allow_phone BOOLEAN NOT NULL DEFAULT true,
    allow_email BOOLEAN NOT NULL DEFAULT true,
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE cards     DROP CONSTRAINT IF EXISTS cards_contact_id_fkey;
ALTER TABLE tickets   DROP CONSTRAINT IF EXISTS tickets_contact_id_fkey;
ALTER TABLE crm_notes DROP CONSTRAINT IF EXISTS crm_notes_contact_id_fkey;
ALTER TABLE cards     RENAME COLUMN contact_id TO client_id;
ALTER TABLE tickets   RENAME COLUMN contact_id TO client_id;
ALTER TABLE crm_notes RENAME COLUMN contact_id TO client_id;
DROP TABLE IF EXISTS contact_group_shares;
DELETE FROM contacts WHERE kind = 'company';
ALTER TABLE contacts DROP CONSTRAINT contacts_company_ck;
ALTER TABLE contacts DROP CONSTRAINT contacts_kind_ck;
ALTER TABLE contacts DROP COLUMN allow_email;
ALTER TABLE contacts DROP COLUMN allow_phone;
ALTER TABLE contacts DROP COLUMN domain;
ALTER TABLE contacts DROP COLUMN company_id;
ALTER TABLE contacts DROP COLUMN kind;
