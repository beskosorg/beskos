-- +goose Up
-- Track egress records participant tracks for offline assembly by the worker.
-- Live room composite recording remains available.
ALTER TABLE recordings ADD COLUMN mode TEXT NOT NULL DEFAULT 'composite'
    CHECK (mode IN ('composite', 'tracks'));
-- Tracks-mode lifecycle: '' (composite) | pending (stopped, waiting for assembly) |
-- processing (worker assembling) | done | failed. The overall recording status stays in
-- recordings.status ('recording' -> 'ready'/'failed').
ALTER TABLE recordings ADD COLUMN assembly_status TEXT NOT NULL DEFAULT ''
    CHECK (assembly_status IN ('', 'pending', 'processing', 'done', 'failed'));
-- Assembly queue priority: offline_priority plans are assembled first.
ALTER TABLE recordings ADD COLUMN priority INT NOT NULL DEFAULT 0;
-- Worker claim timestamp: a processing row older than 1h goes back to pending (the worker died).
ALTER TABLE recordings ADD COLUMN assembly_claimed_at TIMESTAMPTZ;

-- One track egress per track published in the room during the recording.
CREATE TABLE recording_tracks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recording_id UUID NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
    egress_id    TEXT NOT NULL,
    track_id     TEXT NOT NULL,
    participant  TEXT NOT NULL DEFAULT '',
    kind         TEXT NOT NULL CHECK (kind IN ('audio', 'video')),
    -- The final blob_key comes from the egress_ended webhook (LiveKit picks the extension by codec).
    blob_key     TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'recording' CHECK (status IN ('recording', 'done', 'failed')),
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at     TIMESTAMPTZ
);
CREATE INDEX idx_recording_tracks_rec ON recording_tracks(recording_id);
CREATE INDEX idx_recording_tracks_egress ON recording_tracks(egress_id);

-- +goose Down
DROP TABLE IF EXISTS recording_tracks;
ALTER TABLE recordings DROP COLUMN IF EXISTS assembly_claimed_at;
ALTER TABLE recordings DROP COLUMN IF EXISTS priority;
ALTER TABLE recordings DROP COLUMN IF EXISTS assembly_status;
ALTER TABLE recordings DROP COLUMN IF EXISTS mode;
