-- +goose Up
-- Meet presentations (BigBlueButton style): one PDF per room session, with the current slide
-- shared. PPTX/PPT/ODP are converted to PDF (OnlyOffice) before being stored here; object_key points
-- to the final PDF in MinIO. Annotation (pen/laser) travels in real time over the LiveKit data
-- channel and is not persisted.
CREATE TABLE meet_presentations (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    room          text NOT NULL,
    tenant_id     uuid NOT NULL,
    name          text NOT NULL,
    object_key    text NOT NULL,               -- PDF key in MinIO
    current_slide integer NOT NULL DEFAULT 0,
    active        boolean NOT NULL DEFAULT true,
    created_by    uuid NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- One active presentation per room (the most recently started). The index speeds up lookup by room.
CREATE INDEX meet_presentations_room_active ON meet_presentations (room) WHERE active;

-- +goose Down
DROP TABLE meet_presentations;
