-- +goose Up
-- Booking (Calendly-style public scheduling): each member can have ONE public page
-- (/b/<org>/<member>) where external people book meetings in the free slots.
-- windows: JSONB [{"weekday":1,"start":540,"end":1080}] -- minutes since 00:00, weekday 0=Sunday.

CREATE TABLE booking_pages (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    org_slug      TEXT NOT NULL,
    user_slug     TEXT NOT NULL,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    title         TEXT NOT NULL DEFAULT '',
    duration_min  INT NOT NULL DEFAULT 30,
    phone_field   TEXT NOT NULL DEFAULT 'optional',   -- off | optional | required
    company_field TEXT NOT NULL DEFAULT 'optional',   -- off | optional | required
    windows       JSONB NOT NULL DEFAULT '[]',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id),
    UNIQUE (org_slug, user_slug)
);

CREATE TABLE bookings (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    page_id    UUID NOT NULL REFERENCES booking_pages(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    email      TEXT NOT NULL,
    phone      TEXT NOT NULL DEFAULT '',
    company    TEXT NOT NULL DEFAULT '',
    note       TEXT NOT NULL DEFAULT '',
    starts_at  TIMESTAMPTZ NOT NULL,
    ends_at    TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_bookings_user ON bookings(user_id, starts_at);

-- An event created by a booking is flagged -- the calendar highlights it.
ALTER TABLE events ADD COLUMN booking_id UUID REFERENCES bookings(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE events DROP COLUMN booking_id;
DROP TABLE bookings;
DROP TABLE booking_pages;
