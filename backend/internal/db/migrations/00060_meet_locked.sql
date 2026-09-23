-- +goose Up
-- Lock the room to new entries: distinct from lock_until_host ("wait for the organizer"). When the
-- host locks it, NO ONE new gets in (internal or external); only the host can rejoin. The approval
-- lobby only applies to external guests; this lock is hard and applies to everyone.
ALTER TABLE meetings ADD COLUMN locked BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE meetings DROP COLUMN locked;
