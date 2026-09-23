-- +goose Up
-- Tag catalog per tenant (shared: events, contacts, later tasks) + tags/Meet on the event.

CREATE TABLE tags (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    color      TEXT NOT NULL DEFAULT '#116466',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

ALTER TABLE events ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE events ADD COLUMN meeting_room TEXT NOT NULL DEFAULT ''; -- linked Meet room (optional)

-- +goose Down
ALTER TABLE events DROP COLUMN meeting_room;
ALTER TABLE events DROP COLUMN tags;
DROP TABLE IF EXISTS tags;
