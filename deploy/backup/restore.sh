#!/bin/bash
# Restore of an encrypted backup copy.
#
# This file exists because a backup that was never restored isn't a backup — it's a hope.
# Asymmetric encryption makes that worse: the server that writes the copies CANNOT read them, so
# an error on the private key side only shows up on the day it's actually needed. Hence the
# ability to rehearse the whole path, with test-restore.sh, without touching production.
#
# THE PRIVATE KEY DOES NOT LIVE HERE. It's passed in at restore time (AGE_IDENTITY_FILE), from
# wherever it's kept — vault, paper, hardware. If it lived in the container, the asymmetric
# encryption would be worthless: anyone who got into the server could read the entire history.
#
#   restore.sh <base> <object-in-r2> [--to <destination-url>]
#
# Without --to, it writes the decrypted dump to disk and stops there — which is what you want
# when the destination doesn't exist yet, or when you just want to inspect the content.

set -euo pipefail

: "${AGE_IDENTITY_FILE:?AGE_IDENTITY_FILE is missing (file with the age private key)}"
[ -r "$AGE_IDENTITY_FILE" ] || { echo "restore: cannot read $AGE_IDENTITY_FILE"; exit 1; }

BASE="${1:?usage: restore.sh <base> <object-in-r2> [--to <url>]}"
OBJETO="${2:?usage: restore.sh <base> <object-in-r2> [--to <url>]}"
DESTINO=""
if [ "${3:-}" = "--to" ]; then DESTINO="${4:?--to requires a URL}"; fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) restore: $*"; }
fail() { log "FAILED: $*"; exit 1; }

ENC="$TMP/$(basename "$OBJETO")"
DUMP="${ENC%.age}"

# 1. Fetch. The object may come from R2 or already be on disk (that's how the rehearsal runs
# without a network).
if [ -f "$OBJETO" ]; then
  cp "$OBJETO" "$ENC"
else
  log "downloading $OBJETO"
  rclone copyto "copias:${BACKUP_BUCKET:?BACKUP_BUCKET is missing}/$OBJETO" "$ENC" \
    --s3-no-check-bucket --stats-one-line || fail "download"
fi

# 2. Decrypt.
age --decrypt --identity "$AGE_IDENTITY_FILE" --output "$DUMP" "$ENC" \
  || fail "decryption — does the private key match the public key it was encrypted with?"
rm -f "$ENC"

# 3. VERIFY BEFORE WRITING. The same test as in the backup, now on the other side: a dump that
# pg_restore can't index has no business touching a production database.
TABLES=$(pg_restore --list "$DUMP" | grep -c "TABLE DATA" || true)
[ "$TABLES" -gt 0 ] || fail "the decrypted dump has no table data at all"
SIZE=$(stat -c %s "$DUMP" 2>/dev/null || stat -f %z "$DUMP")
log "[$BASE] decrypted and verified: $((SIZE / 1024)) KiB, $TABLES tables with data"

if [ -z "$DESTINO" ]; then
  GUARDA="${RESTORE_OUTPUT_DIR:-$PWD}/$(basename "$DUMP")"
  cp "$DUMP" "$GUARDA"
  log "plaintext dump at $GUARDA — DELETE IT when done"
  exit 0
fi

# 4. Restore. --clean --if-exists leaves the destination in the dump's state instead of mixing it
# with whatever was already there: a restore over partial data is how you find out, months later,
# that a table ended up with rows from two different eras.
log "[$BASE] restoring to destination"
pg_restore --dbname="$DESTINO" --no-owner --no-privileges --clean --if-exists \
  --exit-on-error "$DUMP" || fail "pg_restore"
log "[$BASE] restored"
