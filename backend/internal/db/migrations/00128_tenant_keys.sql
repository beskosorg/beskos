-- +goose Up
-- Envelope encryption for files (docs/file-encryption.md). R2 now stores only ciphertext; the
-- keys stay on our side.
--
-- HIERARCHY:  KEK (environment, versioned) -> tenant key -> object key -> object
--
-- Only the TENANT key lives here. Each object's key travels inside the object itself, wrapped by
-- the tenant key -- and that is what makes writing a file ATOMIC: a single write, which storage
-- either performs in full or not at all. Storing the object key in a table would require two
-- writes, and either order would have a failure window: object first leaves ciphertext with no key
-- (data lost); row first leaves orphan rows. Whoever holds the object has the wrapped key and cannot
-- open it -- that would take this table and the KEK, and they have neither.
--
-- Why one key PER TENANT: deleting this row makes all of the tenant's files unreadable
-- immediately, which is a much stronger deletion guarantee than "we deleted the rows" -- and it is
-- auditable under the data processing agreement. It is the same law that requires every query to
-- filter by tenant_id (docs/engineering-standards.md §5.1), applied to keys.

CREATE TABLE tenant_keys (
    tenant_id   UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    -- Tenant key, wrapped by the environment's KEK. Never in plaintext, anywhere.
    wrapped_key BYTEA       NOT NULL,
    -- Version of the KEK that wrapped it. Rotating the KEK re-wraps THIS table and nothing else: the
    -- objects are not touched, because the key inside them was wrapped by the tenant key, which does
    -- not change. That is the envelope's reason to exist.
    kek_version INT         NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at  TIMESTAMPTZ
);

-- +goose Down
DROP TABLE tenant_keys;
