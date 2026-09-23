-- +goose Up
-- Control-plane schema (brief §5). Every table holding customer data carries tenant_id
-- and EVERY query must filter by it (multi-tenant on a shared database, brief §5/§10).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE tenants (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    nome              TEXT NOT NULL,
    plano             TEXT NOT NULL DEFAULT 'trial',
    status            TEXT NOT NULL DEFAULT 'active',
    stalwart_instance TEXT NOT NULL DEFAULT 'default',
    outbound_ip_pool  TEXT NOT NULL DEFAULT 'default',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Users of OUR panel (SaaS): internal operators and per-customer admins.
-- Distinct from mail_users (mailboxes in Stalwart). Panel auth is ours (brief §8).
CREATE TABLE panel_users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID REFERENCES tenants(id) ON DELETE CASCADE,  -- NULL = internal operator (our global admin)
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'tenant_admin',           -- 'internal_admin' | 'tenant_admin'
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token       TEXT PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES panel_users(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE domains (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    dominio         TEXT NOT NULL UNIQUE,
    stalwart_id     TEXT,                       -- link to Stalwart (x:Domain id)
    dkim_status     TEXT NOT NULL DEFAULT 'pending',
    dns_status      TEXT NOT NULL DEFAULT 'pending',
    provision_state TEXT NOT NULL DEFAULT 'pending',  -- state machine §7
    dns_zone        TEXT,                       -- dnsZoneFile captured from Stalwart
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE mail_users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email               TEXT NOT NULL UNIQUE,
    name                TEXT NOT NULL DEFAULT '',
    role                TEXT NOT NULL DEFAULT 'user',
    stalwart_account_id TEXT,                   -- link to Stalwart (x:Account id)
    quota_bytes         BIGINT NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE storage_files (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    nome          TEXT NOT NULL,
    blob_key      TEXT NOT NULL,
    hash          TEXT NOT NULL,
    tamanho       BIGINT NOT NULL DEFAULT 0,
    versao        INT NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE storage_shares (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id   UUID NOT NULL REFERENCES storage_files(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    principal TEXT NOT NULL,
    nivel     TEXT NOT NULL          -- owner | editor | leitor | link
);

CREATE TABLE usage (
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    storage_bytes BIGINT NOT NULL DEFAULT 0,
    sent_today    BIGINT NOT NULL DEFAULT 0,
    periodo       DATE NOT NULL,
    PRIMARY KEY (tenant_id, periodo)
);

CREATE TABLE subscriptions (
    tenant_id      UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    stripe_id      TEXT,
    faixa_de_caixas TEXT NOT NULL DEFAULT '0-10',
    status         TEXT NOT NULL DEFAULT 'trialing'
);

CREATE TABLE audit_log (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID REFERENCES tenants(id) ON DELETE SET NULL,
    actor     TEXT NOT NULL,
    acao      TEXT NOT NULL,
    alvo      TEXT NOT NULL DEFAULT '',
    timestamp TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_domains_tenant   ON domains(tenant_id);
CREATE INDEX idx_mail_users_tenant ON mail_users(tenant_id);
CREATE INDEX idx_storage_files_tenant ON storage_files(tenant_id);
CREATE INDEX idx_audit_tenant     ON audit_log(tenant_id);
CREATE INDEX idx_domains_provision ON domains(provision_state);

-- +goose Down
DROP TABLE IF EXISTS audit_log, subscriptions, usage, storage_shares, storage_files,
    mail_users, domains, sessions, panel_users, tenants CASCADE;
