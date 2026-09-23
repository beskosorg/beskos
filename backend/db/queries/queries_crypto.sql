-- Envelope-encryption keys for files (doc filename not yet renamed: docs/file-encryption.md) lang:ok
--
-- None of these queries ever return a key in the clear: what comes out of here is always wrapped
-- material, and only the `filecrypt` package opens it, with the environment's KEK.

-- name: GetTenantKey :one
SELECT wrapped_key, kek_version FROM tenant_keys WHERE tenant_id = @tenant_id;

-- name: CreateTenantKey :one
-- ON CONFLICT DO NOTHING + RETURNING returns no row when one already exists; that's why the
-- caller handles the race by re-reading. Two simultaneous requests for the same tenant can never
-- produce two keys -- the second would make unreadable whatever the first one encrypted.
INSERT INTO tenant_keys (tenant_id, wrapped_key, kek_version)
VALUES (@tenant_id, @wrapped_key, @kek_version)
ON CONFLICT (tenant_id) DO NOTHING
RETURNING wrapped_key, kek_version;

-- name: ListTenantKeysByKekVersion :many
-- For KEK rotation: re-wraps only this table, without touching a single object.
SELECT tenant_id, wrapped_key, kek_version FROM tenant_keys WHERE kek_version = @kek_version;

-- name: UpdateTenantKeyWrap :exec
UPDATE tenant_keys SET wrapped_key = @wrapped_key, kek_version = @kek_version, rotated_at = now()
WHERE tenant_id = @tenant_id;
