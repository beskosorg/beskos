#!/bin/bash
# Postgres backup to the destination (EU jurisdiction).
#
# Three things this script does that most don't:
#  1. VERIFIES the file before sending it (`pg_restore --list`). A dump that can't be read isn't
#     a backup, it's a file — and that's always discovered on the worst possible day.
#  2. SIGNALS when it runs fine. Pings a "push" monitor on the status page: if a backup doesn't
#     happen within the window, the monitor goes red and we know. A silent backup that stopped
#     running three weeks ago is the classic failure mode.
#  3. FAILS LOUD. Any error aborts with a message; no carrying on as if nothing happened.
#  4. ENCRYPTS before sending, with an ASYMMETRIC key (age). The server holds only the public
#     key: it can write new copies and CANNOT read old ones. With symmetric encryption, anyone
#     who compromised this container could read the entire history — which here includes the
#     mail messages, because Stalwart stores everything in Postgres. The private key lives
#     outside this infrastructure.

set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL is missing}"
: "${BACKUP_BUCKET:?BACKUP_BUCKET is missing}"
: "${RETENTION_DAYS:=30}"
# Two databases on the same server: the application's and MAIL's. Stalwart stores everything in
# Postgres — messages, attachments, accounts, and its own configuration — so mail goes through
# here like any other database, live and without stopping anything.
: "${BACKUP_DATABASES:=beskos stalwart}"
# age recipient (public key, "age1..."). Without it the backup ABORTS: sending unencrypted to a
# bucket at a US provider is exactly what this encryption exists to prevent, and a forgotten
# variable must not be able to silently degrade that guarantee.
: "${BACKUP_AGE_RECIPIENT:?BACKUP_AGE_RECIPIENT is missing (age public key of the recipient)}"

STAMP="$(date -u +%Y-%m-%dT%H%M%SZ)"
DAY="$(date -u +%Y/%m/%d)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) backup: $*"; }
fail() { log "FAILED: $*"; exit 1; }

# url_for <base> — swaps the database name in DATABASE_URL, preserving credentials and options.
# The user is the same for both (it's the one Stalwart uses), so there's no second credential.
url_for() {
  echo "$DATABASE_URL" | sed -E "s#(://[^/]+/)[^?]+#\1$1#"
}

for BASE in $BACKUP_DATABASES; do
  DUMP="$TMP/$BASE-$STAMP.dump"

  log "[$BASE] exporting"
  # -Fc: custom format, compressed and selectively restorable (table by table, if needed).
  # --no-owner/--no-privileges: the restore doesn't depend on the same roles existing at the
  # destination, which is what ruins half of all emergency restores.
  pg_dump --dbname="$(url_for "$BASE")" --format=custom --no-owner --no-privileges --file="$DUMP" \
    || fail "[$BASE] pg_dump"

  SIZE=$(stat -c %s "$DUMP" 2>/dev/null || stat -f %z "$DUMP")
  [ "$SIZE" -gt 1024 ] || fail "[$BASE] dump is $SIZE bytes — too small to be real"

  # Verification: reads the file's index. Catches truncation and corruption before we go to bed.
  # Done BEFORE encryption, deliberately: once encrypted you can no longer look inside, and a
  # broken dump stays broken once encrypted.
  TABLES=$(pg_restore --list "$DUMP" | grep -c "TABLE DATA" || true)
  [ "$TABLES" -gt 0 ] || fail "[$BASE] the dump has no table data at all"
  log "[$BASE] verified: $((SIZE / 1024)) KiB, $TABLES tables with data"

  # Encrypts for the recipient. What leaves here doesn't open without the private key, which
  # isn't on this server — not in the bucket, not in CapRover.
  ENC="$DUMP.age"
  age --encrypt --recipient "$BACKUP_AGE_RECIPIENT" --output "$ENC" "$DUMP" || fail "[$BASE] encryption"
  rm -f "$DUMP" # the plaintext dump doesn't survive this line
  ENC_SIZE=$(stat -c %s "$ENC" 2>/dev/null || stat -f %z "$ENC")
  [ "$ENC_SIZE" -gt 1024 ] || fail "[$BASE] encrypted file is $ENC_SIZE bytes — that can't be right"

  rclone copyto "$ENC" "copias:$BACKUP_BUCKET/$BASE/$DAY/$BASE-$STAMP.dump.age" \
    --s3-no-check-bucket --stats-one-line || fail "[$BASE] upload to destination"

  # Confirms on the other end: sending without confirming assumes the network never lies.
  REMOTE=$(rclone size "copias:$BACKUP_BUCKET/$BASE/$DAY/$BASE-$STAMP.dump.age" --json | sed 's/.*"bytes":\([0-9]*\).*/\1/')
  [ "$REMOTE" = "$ENC_SIZE" ] || fail "[$BASE] remote size ($REMOTE) differs from local ($ENC_SIZE)"
  log "[$BASE] confirmed at destination, encrypted"

  # Without --rmdirs: the destination has no directories, only keys with slashes. Asking it to
  # remove them fills the log with errors that aren't errors — and a log full of ignored errors
  # is where the real error hides.
  rclone delete "copias:$BACKUP_BUCKET/$BASE/" --min-age "${RETENTION_DAYS}d" || true
  rm -f "$ENC"
done

# ---------------------------------------------------------------------------------------------
# Mail blobs.
#
# Stalwart SPLITS every message: Postgres holds the parsed version (sender, subject, search
# terms, body text) and object storage holds the RAW message with attachments — which is what
# IMAP delivers when a client requests the original. Measured on 2026-07-30: 115 KiB in the
# database against 637 KiB in the bucket, with a single 540 KiB attachment that doesn't fit in a
# database whose largest row is 7.9 KiB.
#
# In other words: pg_dump alone does NOT restore mail. It brings back mailboxes, accounts, and
# searchable content, and loses the attachments. That's why the blobs go through here too.
if [ -n "${BLOB_BUCKET:-}" ]; then
  PREFIX="${BLOB_PREFIX:-mail}"
  TAR="$TMP/blobs-$STAMP.tar"
  DIR="$TMP/blobs"
  mkdir -p "$DIR"

  log "[blobs] fetching $PREFIX/ from storage"
  rclone copy "blobs:$BLOB_BUCKET/$PREFIX" "$DIR" --stats-one-line || fail "[blobs] download"

  # CAP. This copies the ENTIRE prefix on every run — cheap with hundreds of KiB, absurd with
  # tens of GiB. Failing loud once it exceeds the cap beats a silently growing bill: when this
  # triggers, the copy switches to incremental (`rclone sync` of the objects already encrypted by
  # the gateway, which then don't need age).
  BYTES=$(du -sk "$DIR" | cut -f1)
  [ "$BYTES" -le "${BLOB_MAX_KIB:-2097152}" ] \
    || fail "[blobs] $((BYTES / 1024)) MiB exceeds the cap — time for incremental copying"

  tar -cf "$TAR" -C "$DIR" . || fail "[blobs] archiving"
  FILES=$(tar -tf "$TAR" | grep -c . || true)
  [ "$FILES" -gt 0 ] || fail "[blobs] empty archive"
  log "[blobs] $FILES files, $((BYTES / 1024)) MiB"

  age --encrypt --recipient "$BACKUP_AGE_RECIPIENT" --output "$TAR.age" "$TAR" || fail "[blobs] encryption"
  rm -f "$TAR"
  ENC_SIZE=$(stat -c %s "$TAR.age" 2>/dev/null || stat -f %z "$TAR.age")

  rclone copyto "$TAR.age" "copias:$BACKUP_BUCKET/blobs/$DAY/blobs-$STAMP.tar.age" \
    --s3-no-check-bucket --stats-one-line || fail "[blobs] upload"
  REMOTE=$(rclone size "copias:$BACKUP_BUCKET/blobs/$DAY/blobs-$STAMP.tar.age" --json | sed 's/.*"bytes":\([0-9]*\).*/\1/')
  [ "$REMOTE" = "$ENC_SIZE" ] || fail "[blobs] remote size ($REMOTE) differs from local ($ENC_SIZE)"
  log "[blobs] confirmed at destination, encrypted"

  rclone delete "copias:$BACKUP_BUCKET/blobs/" --min-age "${RETENTION_DAYS}d" || true
  rm -rf "$DIR" "$TAR.age"
fi

if [ -n "${HEARTBEAT_URL:-}" ]; then
  curl -fsS --max-time 20 "$HEARTBEAT_URL" >/dev/null && log "status signaled" || log "status signal failed (the backup itself is done)"
fi

log "completed at $(date -u +%Y-%m-%dT%H:%M:%SZ)"
