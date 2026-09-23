-- +goose Up
-- The schema's last twelve Portuguese columns. Like `dominio` in 00129, each one dragged its
-- name into the generated Go and from there into the API JSON -- a naming oversight that ended
-- up as a public contract. See docs/engineering-standards.md §4.6.
-- Examples of the old generated names: `Nome`, `Plano`, `Tamanho`. lang:ok
--
-- None collides with an English column that already exists in the same table: this was checked
-- table by table before writing this, including the ADD/DROP COLUMN of later migrations.
--
-- RENAME preserves data, indexes and foreign keys, and only touches the catalog -- it is
-- instantaneous even on large tables (storage_files, usage), unlike create-copy-delete.
--
-- MIND THE DEPLOYMENT ORDER: this migration and the new code must ship together.
ALTER TABLE audit_log      RENAME COLUMN acao            TO action;
ALTER TABLE audit_log      RENAME COLUMN alvo            TO target;
ALTER TABLE file_versions  RENAME COLUMN tamanho         TO size;
ALTER TABLE file_versions  RENAME COLUMN versao          TO version;
ALTER TABLE storage_files  RENAME COLUMN nome            TO name;
ALTER TABLE storage_files  RENAME COLUMN tamanho         TO size;
ALTER TABLE storage_files  RENAME COLUMN versao          TO version;
ALTER TABLE storage_shares RENAME COLUMN nivel           TO level;
ALTER TABLE subscriptions  RENAME COLUMN faixa_de_caixas TO mailbox_range;
ALTER TABLE tenants        RENAME COLUMN nome            TO name;
ALTER TABLE tenants        RENAME COLUMN plano           TO plan;
ALTER TABLE usage          RENAME COLUMN periodo         TO period;

-- +goose Down
ALTER TABLE audit_log      RENAME COLUMN action        TO acao;
ALTER TABLE audit_log      RENAME COLUMN target        TO alvo;
ALTER TABLE file_versions  RENAME COLUMN size          TO tamanho;
ALTER TABLE file_versions  RENAME COLUMN version       TO versao;
ALTER TABLE storage_files  RENAME COLUMN name          TO nome;
ALTER TABLE storage_files  RENAME COLUMN size          TO tamanho;
ALTER TABLE storage_files  RENAME COLUMN version       TO versao;
ALTER TABLE storage_shares RENAME COLUMN level         TO nivel;
ALTER TABLE subscriptions  RENAME COLUMN mailbox_range TO faixa_de_caixas;
ALTER TABLE tenants        RENAME COLUMN name          TO nome;
ALTER TABLE tenants        RENAME COLUMN plan          TO plano;
ALTER TABLE usage          RENAME COLUMN period        TO periodo;
