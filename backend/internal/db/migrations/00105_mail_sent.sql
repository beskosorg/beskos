-- +goose Up
-- Daily outbound recipient count for tenant sending limits.
-- Includes outbox, proxied JMAP submissions, and assistant messages.
CREATE TABLE tenant_mail_sent (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    day       DATE NOT NULL,
    sent      INT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, day)
);

-- +goose Down
DROP TABLE IF EXISTS tenant_mail_sent;
