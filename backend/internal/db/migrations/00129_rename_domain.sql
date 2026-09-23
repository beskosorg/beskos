-- +goose Up
-- The `domains` table is entirely in English except for one column. `dominio` was the only
-- Portuguese word in the schema and dragged its name into the generated Go (`Dominio`) and from
-- there into the API JSON, which is how a naming slip ends up as a public contract. See
-- docs/engineering-standards.md §4.6.
--
-- RENAME rather than a new column: it preserves the data, the UNIQUE index and the foreign key
-- without copying anything. It is instantaneous -- Postgres only changes the catalog -- and
-- reversible by the Down migration.
--
-- MIND THE DEPLOYMENT ORDER: this migration and the new code must ship together. The old code
-- queries `dominio` and stops finding the column as soon as this runs.
ALTER TABLE domains RENAME COLUMN dominio TO domain;

-- +goose Down
ALTER TABLE domains RENAME COLUMN domain TO dominio;
