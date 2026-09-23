-- +goose Up
-- Due-date notification: records which `due` value has already been notified. The worker notifies
-- when due is approaching/past and due_notified_for does not yet match the current due; it then stores
-- due here. If the deadline changes (UpdateCard stores another due), due_notified_for stops matching
-- and the notification is re-armed.
ALTER TABLE cards ADD COLUMN due_notified_for TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE cards DROP COLUMN due_notified_for;
