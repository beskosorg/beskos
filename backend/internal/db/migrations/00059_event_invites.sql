-- +goose Up
-- Guests / RSVP: the owner invites tenant colleagues; the event appears on each guest's calendar and
-- they respond (pending|accepted|declined|maybe). Deleted by cascade with the event or the user.
CREATE TABLE event_invites (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id, user_id)
);
CREATE INDEX idx_event_invites_user ON event_invites (user_id);

-- +goose Down
DROP TABLE event_invites;
