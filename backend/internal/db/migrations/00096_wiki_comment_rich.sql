-- +goose Up
-- Rich comments: a comment becomes a small BlockNote document.
-- body_json is the source; body (text) and body_html are derived on save (same derive as pages).
-- Older comments (body only) keep rendering from the text.
ALTER TABLE wiki_comments ADD COLUMN body_json JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE wiki_comments ADD COLUMN body_html TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE wiki_comments DROP COLUMN IF EXISTS body_html;
ALTER TABLE wiki_comments DROP COLUMN IF EXISTS body_json;
