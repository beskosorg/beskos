#!/usr/bin/env bash
# Backup of Beskos's production stack (compose in deploy/).
#
# Makes a consistent backup of the three stateful datastores:
#   - Postgres  -> logical pg_dump (predictable restore, independent of the binary's version)
#   - MinIO     -> tar of the object volume (dedup by hash: immutable files, safe hot copy)
#   - Stalwart  -> tar of the volume (mailboxes + config)
# Saves everything under backups/<timestamp>/ and applies retention (RETENTION_DAYS, default 7).
#
# Usage: scripts/backup.sh [destination]
#   destination      root directory for backups (default: ./backups)
#   RETENTION_DAYS   days to keep (default 7); 0 = delete nothing
#   COMPOSE_FILE     compose file to use (default: deploy/docker-compose.yml)
#
# Restore:  scripts/restore.sh <destination>/<timestamp>
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-$ROOT/deploy/docker-compose.yml}"
ENV_FILE="${ENV_FILE:-$ROOT/deploy/.env}"
DEST_ROOT="${1:-$ROOT/backups}"
RETENTION_DAYS="${RETENTION_DAYS:-7}"

# Real names of the compose resources (project "mailin" -> prefix on the volumes).
PG_CONTAINER="mailin-postgres"
PG_VOL="mailin_postgres_data"   # unused (Postgres goes through pg_dump), kept for reference
MINIO_VOL="mailin_minio_data"
STALWART_VOL="mailin_stalwart_data"

[[ -f "$ENV_FILE" ]] || { echo "ERROR: $ENV_FILE not found (copy it from deploy/.env.example)"; exit 1; }
# Reads POSTGRES_USER/POSTGRES_DB from .env without exporting the rest.
PG_USER="$(grep -E '^POSTGRES_USER=' "$ENV_FILE" | tail -1 | cut -d= -f2-)"
PG_DB="$(grep -E '^POSTGRES_DB=' "$ENV_FILE" | tail -1 | cut -d= -f2-)"
: "${PG_USER:?POSTGRES_USER missing in .env}" "${PG_DB:?POSTGRES_DB missing in .env}"

TS="$(date +%Y%m%d-%H%M%S)"
OUT="$DEST_ROOT/$TS"
mkdir -p "$OUT"
echo "== Beskos backup -> $OUT =="

# 1) Postgres: compressed logical dump. --clean so the restore can run over an existing database.
echo "[1/3] Postgres (pg_dump $PG_DB)..."
docker exec "$PG_CONTAINER" pg_dump -U "$PG_USER" --clean --if-exists "$PG_DB" | gzip > "$OUT/postgres.sql.gz"

# 2) MinIO: tar of the object volume, via an ephemeral alpine container that mounts the volume.
echo "[2/3] MinIO (volume $MINIO_VOL)..."
docker run --rm -v "$MINIO_VOL:/data:ro" -v "$OUT:/backup" alpine \
  tar czf /backup/minio_data.tar.gz -C /data .

# 3) Stalwart: tar of the volume (mailboxes + config).
echo "[3/3] Stalwart (volume $STALWART_VOL)..."
docker run --rm -v "$STALWART_VOL:/data:ro" -v "$OUT:/backup" alpine \
  tar czf /backup/stalwart_data.tar.gz -C /data .

# Manifest: what was saved and with which tools (restore reads this as a sanity check).
cat > "$OUT/MANIFEST.txt" <<EOF
mailin-backup
timestamp=$TS
postgres_db=$PG_DB
postgres=postgres.sql.gz (pg_dump --clean --if-exists | gzip)
minio=minio_data.tar.gz (tar of volume $MINIO_VOL)
stalwart=stalwart_data.tar.gz (tar of volume $STALWART_VOL)
EOF

echo "== Done. Sizes: =="
du -h "$OUT"/* 2>/dev/null || true

# Retention: deletes backup directories older than RETENTION_DAYS.
if [[ "$RETENTION_DAYS" -gt 0 ]]; then
  echo "== Retention: removing backups older than ${RETENTION_DAYS} days in $DEST_ROOT =="
  find "$DEST_ROOT" -mindepth 1 -maxdepth 1 -type d -mtime "+$RETENTION_DAYS" -print -exec rm -rf {} +
fi

echo "OK. Restore with:  scripts/restore.sh $OUT"
