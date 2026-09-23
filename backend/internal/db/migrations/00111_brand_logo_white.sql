-- +goose Up
-- Branding: option to force the logo to WHITE in the dark theme -- dark logos disappeared
-- against the dark background; with the flag, the UI applies a filter that turns it into a white
-- silhouette.
ALTER TABLE tenants ADD COLUMN brand_logo_white_dark BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE tenants DROP COLUMN IF EXISTS brand_logo_white_dark;
