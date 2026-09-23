#!/bin/bash
# Restore rehearsal. Spins up a disposable Postgres, runs the real backup and restore, and
# compares the content at the end.
#
#   deploy/backup/test-restore.sh
#
# Touches neither production nor the network: neither the database nor the "bucket" leave this
# machine. Run it every time backup.sh, restore.sh, or the image changes — the asymmetric
# encryption means an error on the key side only shows up on restore day, the worst possible day
# to discover it.

set -euo pipefail
cd "$(dirname "$0")"

NETWORK=mailin-backup-test
PG=mailin-backup-test-pg
IMG=mailin-backup:test

cleanup() { docker rm -f "$PG" >/dev/null 2>&1 || true; docker network rm "$NETWORK" >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup

echo "→ building the backup image"
docker build -q -t "$IMG" . >/dev/null

echo "→ starting a disposable Postgres"
docker network create "$NETWORK" >/dev/null
docker run -d --name "$PG" --network "$NETWORK" --network-alias pg \
  -e POSTGRES_PASSWORD=testpw postgres:16-alpine >/dev/null

for _ in $(seq 30); do
  docker exec "$PG" pg_isready -q && break
  sleep 1
done
docker exec "$PG" pg_isready -q || { echo "Postgres did not start"; exit 1; }

echo "→ round trip"
echo
docker run --rm --network "$NETWORK" -e PGPASSWORD=testpw \
  -v "$PWD/round-trip.sh:/round-trip.sh:ro" \
  --entrypoint bash "$IMG" /round-trip.sh
