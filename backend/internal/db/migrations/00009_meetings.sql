-- +goose Up
-- Meet: meetings (scheduled/instant) + participants with RSVP. Integrates with the calendar
-- (event_id) and with LiveKit (room_name + actual times via webhook).

CREATE TABLE meetings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id   UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    room_name       TEXT NOT NULL UNIQUE,
    scheduled_start TIMESTAMPTZ,          -- NULL = instant meeting
    scheduled_end   TIMESTAMPTZ,
    actual_start    TIMESTAMPTZ,          -- via the room_started webhook
    actual_end      TIMESTAMPTZ,          -- via the room_finished webhook (→ duration)
    status          TEXT NOT NULL DEFAULT 'scheduled', -- scheduled | live | ended | canceled
    event_id        UUID REFERENCES events(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE meeting_participants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    meeting_id  UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,            -- invitee (may be external)
    user_id     UUID REFERENCES mail_users(id) ON DELETE SET NULL, -- if they belong to the tenant
    role        TEXT NOT NULL DEFAULT 'guest',   -- host | guest
    rsvp        TEXT NOT NULL DEFAULT 'pending', -- pending | accepted | declined | tentative
    notified_at TIMESTAMPTZ,
    joined_at   TIMESTAMPTZ,
    left_at     TIMESTAMPTZ,
    UNIQUE(meeting_id, email)
);

CREATE INDEX idx_meetings_tenant ON meetings(tenant_id, scheduled_start);
CREATE INDEX idx_meetings_room ON meetings(room_name);
CREATE INDEX idx_mparticipants_meeting ON meeting_participants(meeting_id);
CREATE INDEX idx_mparticipants_email ON meeting_participants(tenant_id, email);

-- +goose Down
DROP TABLE IF EXISTS meeting_participants;
DROP TABLE IF EXISTS meetings;
