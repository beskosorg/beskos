-- +goose Up
-- Favorites/stars: marks a file as a favorite of its owner (filter/sort in Drive).
ALTER TABLE storage_files ADD COLUMN starred BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE storage_files DROP COLUMN starred;
