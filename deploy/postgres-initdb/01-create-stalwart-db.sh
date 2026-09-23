#!/bin/sh
# Runs once, automatically, only when the Postgres data volume is first initialized (the
# official image's docker-entrypoint-initdb.d convention). POSTGRES_DB creates the control
# plane's own database; Stalwart's config.json points at a second, separate database that
# nothing else creates -- without this, Stalwart crash-loops on every boot with "database
# \"stalwart\" does not exist".
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    CREATE DATABASE stalwart OWNER "$POSTGRES_USER";
EOSQL
