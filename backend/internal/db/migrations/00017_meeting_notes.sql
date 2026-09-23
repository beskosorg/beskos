-- +goose Up
-- Meeting notes: one shared document per meeting, edited live by whoever is in the room.
-- Last-write-wins with autosave (meeting notes rarely have two people typing at once);
-- a CRDT would be complexity with no real use here.

CREATE TABLE meeting_notes (
    meeting_id UUID PRIMARY KEY REFERENCES meetings(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    body       TEXT NOT NULL DEFAULT '',
    updated_by UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS meeting_notes;
