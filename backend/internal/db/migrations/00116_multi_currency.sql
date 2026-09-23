-- +goose Up
-- Currency per country: each plan can have a price per currency ({"USD":1900,"BRL":9900,...});
-- price_cents_per_user remains the price in the BASE currency (fallback). Each tenant has a
-- currency ('' = the global BILLING_CURRENCY); checkout/subscription use that currency's price via
-- the Price's currency_options in Stripe.
ALTER TABLE plans   ADD COLUMN prices   JSONB NOT NULL DEFAULT '{}';
ALTER TABLE tenants ADD COLUMN currency TEXT  NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE tenants DROP COLUMN currency;
ALTER TABLE plans   DROP COLUMN prices;
