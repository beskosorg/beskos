-- +goose Up
-- End-user contacts: contacts, groups (N:N) and tags (array). Per owner (logged-in mailbox).

CREATE TABLE contacts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    email         TEXT NOT NULL DEFAULT '',
    phone         TEXT NOT NULL DEFAULT '',
    company       TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT '',
    tags          TEXT[] NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE contact_groups (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    color         TEXT NOT NULL DEFAULT '#116466',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- N:N between contacts and groups.
CREATE TABLE contact_group_members (
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES contact_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (contact_id, group_id)
);

CREATE INDEX idx_contacts_owner ON contacts(owner_user_id);
CREATE INDEX idx_cgroups_owner ON contact_groups(owner_user_id);

-- +goose Down
DROP TABLE IF EXISTS contact_group_members, contact_groups, contacts CASCADE;
