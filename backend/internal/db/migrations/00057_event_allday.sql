-- +goose Up
-- All-day event: no time, occupies the whole day(s). Multi-day is starts_at/ends_at on different
-- days (already supported by the schema); all_day only changes presentation (top row, no time).
ALTER TABLE events ADD COLUMN all_day BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE events DROP COLUMN all_day;
