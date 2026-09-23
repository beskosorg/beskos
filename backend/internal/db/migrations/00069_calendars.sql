-- +goose Up
-- Multiple calendars per user: each event belongs to a calendar (NULL = uncategorized).
-- Free-busy uses the events themselves; no new column is needed for that.
CREATE TABLE calendars (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    color         TEXT NOT NULL DEFAULT '#116466',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_calendars_owner ON calendars(owner_user_id);
ALTER TABLE events ADD COLUMN calendar_id UUID REFERENCES calendars(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE events DROP COLUMN calendar_id;
DROP TABLE calendars;
