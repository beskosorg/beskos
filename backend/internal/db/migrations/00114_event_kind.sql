-- +goose Up
-- Absences in the calendar: day off and vacation are all-day events with their own "kind" --
-- booking already respects them (they block the day) and the UI shows them with a distinct style.
ALTER TABLE events ADD COLUMN kind TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE events DROP COLUMN kind;
