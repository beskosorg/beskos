-- +goose Up
-- Customizable home of the Wiki app: one "page" per tenant, edited by org admins
-- (PermOrgSettings) with the same BlockNote editor, shown on the Wiki start screen in place of the
-- "select a wiki" prompt. body_json (BlockNote blocks) is the source; text/html are derived as in
-- pages.
CREATE TABLE wiki_home (
    tenant_id  UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    body_json  JSONB NOT NULL DEFAULT '[]'::jsonb,
    body_text  TEXT NOT NULL DEFAULT '',
    body_html  TEXT NOT NULL DEFAULT '',
    updated_by UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS wiki_home;
