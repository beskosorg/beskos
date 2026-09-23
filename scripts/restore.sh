#!/usr/bin/env bash
# Restore of Beskos's production stack from a backup made by scripts/backup.sh.
#
# DESTRUCTIVE: overwrites Postgres, MinIO and Stalwart with the backup's content. Asks for
# confirmation (skip with FORCE=1). Stops api/worker during the restore to avoid concurrent writes.
#
# Usage: scripts/restore.sh <backup-dir>   (e.g. backups/20260719-031500)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-$ROOT/deploy/docker-compose.yml}"
ENV_FILE="${ENV_FILE:-$ROOT/deploy/.env}"
DC=(docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE")

SRC="${1:-}"
[[ -n "$SRC" && -d "$SRC" ]] || { echo "Usage: scripts/restore.sh <backup-dir>"; exit 1; }
[[ -f "$SRC/MANIFEST.txt" ]] || { echo "ERROR: $SRC does not look like a backup (no MANIFEST.txt)"; exit 1; }

PG_CONTAINER="mailin-postgres"
MINIO_VOL="mailin_minio_data"
STALWART_VOL="mailin_stalwart_data"
PG_USER="$(grep -E '^POSTGRES_USER=' "$ENV_FILE" | tail -1 | cut -d= -f2-)"
PG_DB="$(grep -E '^POSTGRES_DB=' "$ENV_FILE" | tail -1 | cut -d= -f2-)"
: "${PG_USER:?}" "${PG_DB:?}"

echo "== Beskos restore from $SRC =="
cat "$SRC/MANIFEST.txt"
if [[ "${FORCE:-0}" != "1" ]]; then
  read -r -p "This OVERWRITES the current data. Continue? (type 'sim'): " ok
  [[ "$ok" == "sim" ]] || { echo "Aborted."; exit 1; }
fi

# 1) Stops the data consumers (keeps the datastores up for the restore).
echo "[1/4] Stopping api and worker..."
"${DC[@]}" stop api worker >/dev/null 2>&1 || true

# 2) Postgres: applies the logical dump (the dump already has DROP/CREATE via --clean --if-exists).
echo "[2/4] Postgres..."
gunzip -c "$SRC/postgres.sql.gz" | docker exec -i "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -q

# 3) MinIO: stops the service, empties and re-extracts the volume, brings it back up.
echo "[3/4] MinIO..."
"${DC[@]}" stop minio >/dev/null 2>&1 || true
docker run --rm -v "$MINIO_VOL:/data" -v "$SRC:/backup:ro" alpine \
  sh -c 'rm -rf /data/* /data/..?* /data/.[!.]* 2>/dev/null; tar xzf /backup/minio_data.tar.gz -C /data'
"${DC[@]}" start minio >/dev/null

# 4) Stalwart: same.
echo "[4/4] Stalwart..."
"${DC[@]}" stop stalwart >/dev/null 2>&1 || true
docker run --rm -v "$STALWART_VOL:/data" -v "$SRC:/backup:ro" alpine \
  sh -c 'rm -rf /data/* /data/..?* /data/.[!.]* 2>/dev/null; tar xzf /backup/stalwart_data.tar.gz -C /data'
"${DC[@]}" start stalwart >/dev/null

echo "== Bringing api and worker back up =="
"${DC[@]}" start api worker >/dev/null
echo "OK. Confirm with:  ${DC[*]} ps  and  curl -fsS http://localhost:8000/healthz"
