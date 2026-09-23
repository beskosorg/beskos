-- +goose Up
-- One prefix, one board, per tenant.
--
-- WHY IT MATTERS: the task key is derived (prefix + number) and the number restarts at 1 on every
-- board. Two boards sharing a prefix therefore both produce "QUA-1", for different tasks. Anyone
-- pasting a key into a conversation would be naming two things at once, and the reader has no way
-- to tell which. Names collide easily -- "Marketing" and "Marcas" both start MAR.
--
-- PER TENANT, not globally: two organisations never see each other's boards, so making one wait for
-- a prefix the other took would be a leak of information as well as an annoyance.

-- Resolve whatever is already duplicated, oldest board keeps the plain prefix. Production had none
-- when this was written, but a development database easily does, and CREATE UNIQUE INDEX on a table
-- with duplicates fails and leaves the migration half-applied.
WITH ranked AS (
    SELECT id, prefix, tenant_id,
           row_number() OVER (PARTITION BY tenant_id, prefix ORDER BY created_at, id) AS n
    FROM boards
)
UPDATE boards b SET prefix = r.prefix || r.n::text
FROM ranked r
WHERE b.id = r.id AND r.n > 1;

-- The constraint is the guarantee. Everything above it in the stack -- the SELECT that picks a free
-- prefix, the check in the update handler -- narrows the window; only this closes it, because two
-- requests can both read "free" before either writes.
CREATE UNIQUE INDEX boards_tenant_prefix_uq ON boards (tenant_id, prefix);

-- +goose Down
DROP INDEX boards_tenant_prefix_uq;
