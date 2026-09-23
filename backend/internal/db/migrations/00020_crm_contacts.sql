-- +goose Up
-- CRM — part 1: companies, subcontacts with a job title, phones with consent and privacy.
-- Decisions: `companies` is a separate table; a contact belongs to a company and has a
-- job title; a contact can be private, shared with groups, or visible to the whole organization.

CREATE TABLE companies (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    domain        TEXT NOT NULL DEFAULT '',   -- website/domain (helps match emails to the company)
    phone         TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT '',
    tags          TEXT[] NOT NULL DEFAULT '{}',
    -- Privacy: private = owner only; groups = owner + groups in company_shares; tenant = everyone.
    -- Fail-closed default: the creator decides to open it up; nothing leaks by omission.
    visibility    TEXT NOT NULL DEFAULT 'private',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_companies_tenant ON companies(tenant_id);

-- Contacts gain a company, job title, consent and privacy.
ALTER TABLE contacts ADD COLUMN company_id UUID REFERENCES companies(id) ON DELETE SET NULL;
ALTER TABLE contacts ADD COLUMN title      TEXT NOT NULL DEFAULT '';   -- job title
ALTER TABLE contacts ADD COLUMN mobile     TEXT NOT NULL DEFAULT '';
-- Per-channel consent (LGPD): whether we MAY contact, and through which channel.
ALTER TABLE contacts ADD COLUMN is_whatsapp  BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE contacts ADD COLUMN allow_phone  BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE contacts ADD COLUMN allow_email  BOOLEAN NOT NULL DEFAULT true;
-- WARNING: today contacts are effectively private (the queries filter only by owner_user_id).
-- Default 'private' preserves exactly that behavior — 'tenant' would expose, all at once,
-- everyone's entire address book to the organization.
ALTER TABLE contacts ADD COLUMN visibility   TEXT NOT NULL DEFAULT 'private';
CREATE INDEX idx_contacts_company ON contacts(company_id);

-- Sharing with groups (only applies when visibility = 'groups').
CREATE TABLE contact_shares (
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (contact_id, group_id)
);
CREATE TABLE company_shares (
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (company_id, group_id)
);

-- CRM notes (timeline per contact/company) — separate from the free-form `notes` field.
CREATE TABLE crm_notes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    contact_id UUID REFERENCES contacts(id) ON DELETE CASCADE,
    company_id UUID REFERENCES companies(id) ON DELETE CASCADE,
    author_id  UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A note belongs to a contact OR a company, never both and never neither.
    CHECK ((contact_id IS NULL) <> (company_id IS NULL))
);
CREATE INDEX idx_crm_notes_contact ON crm_notes(contact_id, created_at DESC);
CREATE INDEX idx_crm_notes_company ON crm_notes(company_id, created_at DESC);

-- Customer task = Kanban card linked to the contact (decision: reuse the Kanban).
ALTER TABLE cards ADD COLUMN contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL;
CREATE INDEX idx_cards_contact ON cards(contact_id);

-- +goose Down
ALTER TABLE cards DROP COLUMN contact_id;
DROP TABLE IF EXISTS crm_notes;
DROP TABLE IF EXISTS company_shares;
DROP TABLE IF EXISTS contact_shares;
ALTER TABLE contacts DROP COLUMN company_id;
ALTER TABLE contacts DROP COLUMN title;
ALTER TABLE contacts DROP COLUMN mobile;
ALTER TABLE contacts DROP COLUMN is_whatsapp;
ALTER TABLE contacts DROP COLUMN allow_phone;
ALTER TABLE contacts DROP COLUMN allow_email;
ALTER TABLE contacts DROP COLUMN visibility;
DROP TABLE IF EXISTS companies;
