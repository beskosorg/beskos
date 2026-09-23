-- +goose Up
-- One person, one permission per file. Without this constraint, sharing again with the same
-- address created a SECOND row and the effective permission depended on the read order -- which
-- is what prevented changing "reader" to "editor" without deleting and sharing again.
-- Existing duplicates: the most permissive row is kept (owner > editor > reader > link).
DELETE FROM storage_shares s USING storage_shares keep
WHERE s.file_id = keep.file_id AND s.principal = keep.principal AND s.id <> keep.id
  AND CASE keep.nivel WHEN 'owner' THEN 3 WHEN 'editor' THEN 2 WHEN 'leitor' THEN 1 ELSE 0 END
    > CASE s.nivel  WHEN 'owner' THEN 3 WHEN 'editor' THEN 2 WHEN 'leitor' THEN 1 ELSE 0 END;
-- Ties (same level repeated): the one with the lowest id is kept.
DELETE FROM storage_shares s USING storage_shares keep
WHERE s.file_id = keep.file_id AND s.principal = keep.principal AND s.nivel = keep.nivel AND s.id > keep.id;

CREATE UNIQUE INDEX storage_shares_file_principal_key ON storage_shares (file_id, principal);

-- +goose Down
DROP INDEX IF EXISTS storage_shares_file_principal_key;
