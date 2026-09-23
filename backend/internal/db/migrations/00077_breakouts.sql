-- +goose Up
-- Breakout rooms: parallel rooms inside a meeting. Each breakout is its own LiveKit room
-- (room_name derived from the main one). The host creates/opens/closes them; participants are
-- assigned to one.
CREATE TABLE meeting_breakouts (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    meeting_id UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    idx        INT  NOT NULL,           -- 1..N, order and room_name derivation
    name       TEXT NOT NULL,
    room_name  TEXT NOT NULL,           -- <main_room>-b<idx>
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_breakouts_meeting ON meeting_breakouts(meeting_id);

-- Assignment: each participant is in at most one breakout per meeting.
CREATE TABLE breakout_assignments (
    meeting_id  UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL,
    breakout_id UUID NOT NULL REFERENCES meeting_breakouts(id) ON DELETE CASCADE,
    PRIMARY KEY (meeting_id, user_id)
);
CREATE INDEX idx_breakout_assign_breakout ON breakout_assignments(breakout_id);

-- +goose Down
DROP TABLE breakout_assignments;
DROP TABLE meeting_breakouts;
