-- +goose Up
-- Mirror of the catalog in Stripe: each plan has a Product and a recurring Price there.
-- Creating/changing a plan in the manager syncs it; if the price changed -> create a new Price
-- (they are immutable) and deactivate the old one. Checkout/switching use the price_id -- financial
-- management lives entirely in Stripe.
ALTER TABLE plans ADD COLUMN stripe_product_id TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN stripe_price_id   TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE plans DROP COLUMN stripe_price_id;
ALTER TABLE plans DROP COLUMN stripe_product_id;
