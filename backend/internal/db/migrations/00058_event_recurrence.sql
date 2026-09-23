-- +goose Up
-- Recurring events: recur = frequency (daily|weekly|monthly|yearly; '' = single), recur_until =
-- optional end date. The stored event is the MASTER (1st occurrence); the backend expands the
-- occurrences within the requested range when listing. Editing/deleting affects the whole series (MVP).
ALTER TABLE events ADD COLUMN recur TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN recur_until TIMESTAMPTZ;

-- +goose Down
ALTER TABLE events DROP COLUMN recur_until;
ALTER TABLE events DROP COLUMN recur;
