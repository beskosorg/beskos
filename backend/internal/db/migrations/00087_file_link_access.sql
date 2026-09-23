-- +goose Up
-- The public link (file_links) is no longer just "download": it now OPENS the file (OnlyOffice,
-- anonymous session), in view or edit mode, with download as a separate option — the Google model.
--   permission: 'view' (read only) | 'edit' (edit anonymously). Default 'view' (the safest).
--   allow_download: whether whoever opens it can download. Default true so that existing links,
--                   which were exactly download links, keep working as before.
ALTER TABLE file_links ADD COLUMN permission TEXT NOT NULL DEFAULT 'view';
ALTER TABLE file_links ADD COLUMN allow_download BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE file_links DROP COLUMN permission;
ALTER TABLE file_links DROP COLUMN allow_download;
