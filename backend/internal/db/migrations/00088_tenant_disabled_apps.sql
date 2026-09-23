-- +goose Up
-- Apps disabled per organization: the admin turns apps on/off and they disappear from the menu for
-- everyone, the integration between them is cut, and the assistant loses that app's tools and
-- agent. Empty = everything enabled. The list of DISABLED apps is stored (not the enabled ones) so
-- that new apps start enabled.
ALTER TABLE tenants ADD COLUMN disabled_apps TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE tenants DROP COLUMN disabled_apps;
