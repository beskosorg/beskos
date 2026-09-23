-- +goose Up
-- EXTERNAL event guests: people outside the organization, by email.
-- They receive the invitation by email (best-effort via notify) -- no account and no internal RSVP.
CREATE TABLE event_externals (
    event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, email)
);

-- +goose Down
DROP TABLE IF EXISTS event_externals;
