-- +goose Up
-- Per-organization branding: primary color (hex, e.g. "#2563EB") and logo (data URI, stored inline:
-- logos are small, and this avoids a public image endpoint). Empty = use the default Beskos
-- branding. Only admin/owner can change it (RBAC org.settings).
ALTER TABLE tenants
    ADD COLUMN brand_color TEXT NOT NULL DEFAULT '',
    ADD COLUMN brand_logo  TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE tenants DROP COLUMN brand_color, DROP COLUMN brand_logo;
