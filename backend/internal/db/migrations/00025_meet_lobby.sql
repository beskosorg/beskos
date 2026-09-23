-- +goose Up
-- Meet: "organizer joins first" (the room stays locked until the host arrives).
--
-- Default false: turning the lock on is a deliberate choice by the owner. On by default, every ad-hoc room
-- (e.g. a call started from chat) would block anyone arriving before the inviter — the opposite of useful.
ALTER TABLE meetings ADD COLUMN lock_until_host BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE meetings DROP COLUMN lock_until_host;
