-- +goose Up
-- Personal GIF library (decided on 2026-07-16: no Giphy/Tenor).
--
-- There is no new file table: the GIF is a `storage_files` row like any other, already in MinIO.
-- This is only the marker "this file is in my GIF drawer" — anyone who has attached a GIF
-- can save it and reuse it without uploading it again.
--
-- Personal on purpose: it is each person's taste, not a company asset. If sharing ever makes
-- sense, the path is the same as for contacts — a group, not a per-GIF field.
CREATE TABLE gif_library (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    -- ON DELETE CASCADE: deleting the file in Drive removes it from the drawer. An entry pointing at
    -- a file that no longer exists would be a broken GIF in front of whoever picked it.
    file_id    UUID NOT NULL REFERENCES storage_files(id) ON DELETE CASCADE,
    name       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT gif_library_uq UNIQUE (owner_user_id, file_id)
);
CREATE INDEX idx_gif_library ON gif_library(owner_user_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS gif_library;
