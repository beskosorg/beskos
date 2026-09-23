-- +goose Up
-- Meeting recordings (LiveKit Egress → MinIO). Optional: only when the user turns recording on.

CREATE TABLE recordings (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    room_name        TEXT NOT NULL,
    meeting_id       UUID REFERENCES meetings(id) ON DELETE SET NULL,
    egress_id        TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'recording', -- recording | ready | failed
    blob_key         TEXT NOT NULL DEFAULT '',
    started_by       TEXT NOT NULL DEFAULT '',
    started_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at         TIMESTAMPTZ,
    duration_seconds INTEGER,
    size_bytes       BIGINT
);

CREATE INDEX idx_recordings_tenant ON recordings(tenant_id, started_at DESC);
CREATE INDEX idx_recordings_egress ON recordings(egress_id);

-- +goose Down
DROP TABLE IF EXISTS recordings;
