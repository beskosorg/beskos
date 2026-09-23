-- +goose Up
-- Assistant tools turned off by the organization. Finer granularity on top of what already
-- existed (turning an entire assistant or a rail app on/off): here ONE capability is cut --
-- "the AI never sends email", for example -- and the cut applies to ALL assistants, built-in or
-- created by the customer. It is a ceiling, never an extension: it cannot grant what the assistant
-- lacks. Only what differs from the default is stored; absence = on.
CREATE TABLE assistant_tool_settings (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    tool      TEXT NOT NULL,
    enabled   BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (tenant_id, tool)
);

-- +goose Down
DROP TABLE assistant_tool_settings;
