-- +goose Up
-- Separates ADDRESS BOOK from CLIENT (decided on 2026-07-15: the model was mixing concepts).
--
-- Before: `contacts` served three owners with opposite rules — address book (mine, private), client
-- (the company's, shared) and support. Hence the 3-level visibility selector, the sharing tables,
-- and the symptom "Ticket #12 from —" (shared ticket + private contact = null name).
--
-- Now:
--   `contacts`        = personal address book. Always private (owner only). Feeds the email "To:".
--   `clients`         = the company's client (kind = company | person). Always visible to the tenant.
--   `client_contacts` = a client's people (job title, phone, LGPD consent).
--
-- The same person can be on both sides (in my address book and as a client's contact). That is
-- correct, not duplication: they are records of different owners — like Google Contacts vs a CRM.

-- 1) companies → clients. RENAME preserves the ids, so crm_notes/cards/tickets are not lost.
ALTER TABLE companies RENAME TO clients;
ALTER TABLE clients ADD COLUMN kind  TEXT NOT NULL DEFAULT 'company';
ALTER TABLE clients ADD COLUMN email TEXT NOT NULL DEFAULT '';   -- used when kind = 'person'
ALTER TABLE clients ADD CONSTRAINT clients_kind_ck CHECK (kind IN ('company', 'person'));
-- A client is an asset of the company, not of whoever registered it: visibility goes away.
ALTER TABLE clients DROP COLUMN visibility;
DROP TABLE company_shares;

-- 2) A client's people. Consent (LGPD) lives here — it is the company that makes contact.
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
CREATE INDEX idx_client_contacts ON client_contacts(client_id);

-- 3) Contact WITH a company → becomes a client's person.
INSERT INTO client_contacts (tenant_id, client_id, name, title, email, phone, mobile,
                             is_whatsapp, allow_phone, allow_email, notes, created_at)
SELECT c.tenant_id, c.company_id, c.name, c.title, c.email, c.phone, c.mobile,
       c.is_whatsapp, c.allow_phone, c.allow_email, c.notes, c.created_at
FROM contacts c WHERE c.company_id IS NOT NULL;

-- 4) Shared contact WITHOUT a company → becomes an individual client.
--    Keeps the contact id so that notes/tasks/tickets point at the same UUID.
INSERT INTO clients (id, tenant_id, owner_user_id, name, domain, phone, notes, tags, created_at, kind, email)
SELECT c.id, c.tenant_id, c.owner_user_id, c.name, '', c.phone, c.notes, c.tags, c.created_at, 'person', c.email
FROM contacts c WHERE c.company_id IS NULL AND c.visibility <> 'private';

-- 5) Remap the work: contact_id → client_id.
--    Those with a company now point at the company; those that became individuals kept their id.
ALTER TABLE cards   ADD COLUMN client_id UUID REFERENCES clients(id) ON DELETE SET NULL;
ALTER TABLE tickets ADD COLUMN client_id UUID REFERENCES clients(id) ON DELETE SET NULL;
UPDATE cards k SET client_id = COALESCE(
    (SELECT c.company_id FROM contacts c WHERE c.id = k.contact_id),
    (SELECT cl.id FROM clients cl WHERE cl.id = k.contact_id))
WHERE k.contact_id IS NOT NULL;
UPDATE tickets t SET client_id = COALESCE(
    (SELECT c.company_id FROM contacts c WHERE c.id = t.contact_id),
    (SELECT cl.id FROM clients cl WHERE cl.id = t.contact_id))
WHERE t.contact_id IS NOT NULL;
ALTER TABLE cards   DROP COLUMN contact_id;
ALTER TABLE tickets DROP COLUMN contact_id;
CREATE INDEX idx_cards_client   ON cards(client_id);
CREATE INDEX idx_tickets_client ON tickets(client_id);

-- 6) CRM notes now belong to the client (contact XOR company no longer makes sense).
ALTER TABLE crm_notes ADD COLUMN client_id UUID REFERENCES clients(id) ON DELETE CASCADE;
UPDATE crm_notes n SET client_id = COALESCE(
    n.company_id,
    (SELECT c.company_id FROM contacts c WHERE c.id = n.contact_id),
    (SELECT cl.id FROM clients cl WHERE cl.id = n.contact_id));
DELETE FROM crm_notes WHERE client_id IS NULL;  -- contact note that stayed in the address book: no client
ALTER TABLE crm_notes DROP CONSTRAINT IF EXISTS crm_notes_check;
ALTER TABLE crm_notes DROP COLUMN contact_id;
ALTER TABLE crm_notes DROP COLUMN company_id;
ALTER TABLE crm_notes ALTER COLUMN client_id SET NOT NULL;
CREATE INDEX idx_crm_notes_client ON crm_notes(client_id, created_at DESC);

-- 7) The address book slims down: visibility, sharing and the CRM fields go away.
--    What is lost here was already copied into client_contacts in step 3.
DELETE FROM contacts WHERE company_id IS NULL AND visibility <> 'private'; -- became an individual client
DELETE FROM contacts WHERE company_id IS NOT NULL;                          -- became a client_contact
DROP TABLE contact_shares;
ALTER TABLE contacts DROP COLUMN visibility;
ALTER TABLE contacts DROP COLUMN company_id;
ALTER TABLE contacts DROP COLUMN allow_phone;   -- consent is a client concept, not an address-book one
ALTER TABLE contacts DROP COLUMN allow_email;

-- +goose Down
-- ASYMMETRIC ON PURPOSE. Up merges and splits populations; undoing it exactly would require knowing
-- who was who before, and guessing would get it wrong — worse than failing. Down restores the old SCHEMA
-- (so goose can migrate down), not the data in its original layout. Restoring data = restoring a
-- backup. See the same decision in 00018_unify_users.sql.
ALTER TABLE contacts ADD COLUMN allow_email BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE contacts ADD COLUMN allow_phone BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE contacts ADD COLUMN company_id UUID;
ALTER TABLE contacts ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private';
CREATE TABLE contact_shares (
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (contact_id, group_id)
);
ALTER TABLE crm_notes ADD COLUMN contact_id UUID REFERENCES contacts(id) ON DELETE CASCADE;
ALTER TABLE crm_notes ADD COLUMN company_id UUID;
ALTER TABLE crm_notes DROP COLUMN client_id;
ALTER TABLE tickets ADD COLUMN contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL;
ALTER TABLE cards   ADD COLUMN contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL;
ALTER TABLE tickets DROP COLUMN client_id;
ALTER TABLE cards   DROP COLUMN client_id;
DROP TABLE IF EXISTS client_contacts;
ALTER TABLE clients ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private';
ALTER TABLE clients DROP CONSTRAINT clients_kind_ck;
ALTER TABLE clients DROP COLUMN email;
ALTER TABLE clients DROP COLUMN kind;
ALTER TABLE clients RENAME TO companies;
CREATE TABLE company_shares (
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (company_id, group_id)
);
