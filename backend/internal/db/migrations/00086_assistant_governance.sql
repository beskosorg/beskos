-- +goose Up
-- Assistant governance (§9 and §6.6): usage quota and folder exclusion.

-- Usage recorded per turn, for the 6-hour sliding-window quota (§9.2). One row per execution; the
-- quota sums the rows of the last 6 hours. It is the COMMERCIAL limit — distinct from the
-- per-execution step ceiling (§9.1), which is a safety measure and has lived in code from the start.
CREATE TABLE assistant_usage (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    tokens       INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- The window sum is always (user, recent time): the index serves exactly that query.
CREATE INDEX idx_assistant_usage_window ON assistant_usage (mail_user_id, created_at);

-- Folders out of the assistant's reach (§6.6). Choosing broad reach by default (decision §12.1,
-- point 1) increases what an injection could reach; this is the valve that lets the administrator
-- take sensitive folders out of the assistant's view. It is per FOLDER, and recursive in the
-- application.
CREATE TABLE assistant_excluded_folders (
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    folder_id    UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, folder_id)
);

-- +goose Down
DROP TABLE assistant_excluded_folders;
DROP TABLE assistant_usage;
