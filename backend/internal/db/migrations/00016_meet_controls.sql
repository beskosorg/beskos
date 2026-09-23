-- +goose Up
-- Organizer controls (Google Meet style): what participants may do in the room.
-- Actually enforced in the LiveKit token grants + UpdateParticipant (not just the UI).

ALTER TABLE meetings ADD COLUMN allow_mic   BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE meetings ADD COLUMN allow_cam   BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE meetings ADD COLUMN allow_share BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE meetings ADD COLUMN allow_chat  BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE meetings DROP COLUMN allow_mic;
ALTER TABLE meetings DROP COLUMN allow_cam;
ALTER TABLE meetings DROP COLUMN allow_share;
ALTER TABLE meetings DROP COLUMN allow_chat;
