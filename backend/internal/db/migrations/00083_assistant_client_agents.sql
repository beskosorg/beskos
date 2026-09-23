-- +goose Up
-- Agents created by the CUSTOMER (§3.2). The built-in ones are NOT here: they live in code
-- (`internal/assistant/agent.go`) because they are part of the product and nobody changes them. A
-- row in a table is editable by definition — a single UPDATE would change an agent's tools, which
-- is exactly the security boundary that has to stay frozen.
--
-- A customer agent's key is its own `id`: a UUID never collides with 'assistant', 'inbox' or
-- 'docs', so the same request field serves both kinds without prefixes.
CREATE TABLE assistant_agents (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    purpose      TEXT NOT NULL DEFAULT '',   -- one line; it is what routing reads (§3.2)
    instructions TEXT NOT NULL DEFAULT '',
    -- Allow list. Validated ON THE SERVER against the tool ceiling on every save: the interface
    -- is not the defense (§3.2, §6.1 defense 2).
    tools        TEXT[] NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Names are unique per organization, regardless of case: two agents "Sales" and "sales" in the
-- same list would be indistinguishable to whoever is choosing.
CREATE UNIQUE INDEX idx_assistant_agents_name ON assistant_agents (tenant_id, lower(name));

-- Availability of the BUILT-IN agents per organization. Administrators control whether each one
-- appears — not what it does (§3.2). Only what differs from the default is stored: absent = enabled.
CREATE TABLE assistant_builtin_settings (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_key TEXT NOT NULL,
    enabled   BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (tenant_id, agent_key)
);

-- +goose Down
DROP TABLE assistant_builtin_settings;
DROP TABLE assistant_agents;
