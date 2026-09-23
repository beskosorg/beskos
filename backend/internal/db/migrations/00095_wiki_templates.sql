-- +goose Up
-- Page templates: a page marked as a template appears in its space's "new page from template"
-- picker. The content is copied at creation time (the frontend reads the template's body_json and
-- creates the new page with it -- no server-side magic).
ALTER TABLE wiki_pages ADD COLUMN is_template BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX idx_wiki_pages_templates ON wiki_pages(tenant_id, space_id) WHERE is_template;

-- +goose Down
DROP INDEX IF EXISTS idx_wiki_pages_templates;
ALTER TABLE wiki_pages DROP COLUMN IF EXISTS is_template;
