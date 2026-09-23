-- +goose Up
-- Customer billing details (who subscribes, where the invoice goes). Previously checkout went to
-- Stripe with nothing: the invoice went out without a company name, address or tax ID, which does
-- not work for a company that needs to deduct VAT. We store it here because the invoice is OUR
-- legal obligation -- Stripe is only the payment channel, and switching providers must not take
-- the tax data with it.
CREATE TABLE billing_profiles (
    tenant_id      UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    -- company | individual: changes the required fields (tax ID/company name) and the VAT rule.
    kind           TEXT NOT NULL DEFAULT 'company',
    legal_name     TEXT NOT NULL DEFAULT '',
    -- Tax ID/VAT number with the country prefix (PL1234567890). Empty for individuals and for
    -- companies without a number.
    tax_id         TEXT NOT NULL DEFAULT '',
    country        TEXT NOT NULL DEFAULT '',  -- ISO 3166-1 alpha-2, uppercase
    city           TEXT NOT NULL DEFAULT '',
    postal_code    TEXT NOT NULL DEFAULT '',
    address_line1  TEXT NOT NULL DEFAULT '',
    address_line2  TEXT NOT NULL DEFAULT '',
    -- Billing contact: the invoice goes here, not to whoever happened to subscribe.
    invoice_email  TEXT NOT NULL DEFAULT '',
    invoice_name   TEXT NOT NULL DEFAULT '',
    phone          TEXT NOT NULL DEFAULT '',
    -- Customer mirrored in Stripe (cus_...): reused on every checkout so customers are not duplicated.
    stripe_customer TEXT NOT NULL DEFAULT '',
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE billing_profiles;
