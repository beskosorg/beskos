#!/bin/bash
# Full round trip, run INSIDE the backup image. Called by test-restore.sh.
#
# Runs the real backup.sh and restore.sh, not copies adapted for testing: a rehearsal that
# exercises a similar version of the script proves nothing about what runs at two in the morning.
#
# R2 is replaced with an rclone `local` remote — the same command, the same code path, writing to
# a directory instead of to Cloudflare. What this needs to prove is the encryption and the
# restore, not Cloudflare's HTTP.

set -euo pipefail

PG="postgres://postgres:testpw@pg:5432"
BUCKET=/tmp/bucket
ok() { echo "  ✓ $*"; }
fail() { echo "  ✗ FAILED: $*"; exit 1; }

echo "1. seeding both databases"
for BASE in beskos stalwart; do
  psql "$PG/postgres" -qtc "CREATE DATABASE $BASE" >/dev/null
  psql "$PG/$BASE" -qc "
    CREATE TABLE messages (id INT PRIMARY KEY, body TEXT NOT NULL);
    INSERT INTO messages SELECT i, 'secret from $BASE #' || i FROM generate_series(1, 5000) i;
  " >/dev/null
done
psql "$PG/postgres" -qtc "CREATE DATABASE beskos_restaurado" >/dev/null
# The content fingerprint. This is what gets compared at the end — comparing row counts would let
# through a restore that brought back the right rows with swapped content.
FINGERPRINT=$(psql "$PG/beskos" -qtAc "SELECT md5(string_agg(body, ',' ORDER BY id)) FROM messages")
ok "5000 rows in each database, fingerprint ${FINGERPRINT:0:12}…"

echo "2. generating the age key pair"
age-keygen -o /tmp/key.txt 2>/dev/null
PUBKEY=$(grep -o 'age1[a-z0-9]*' /tmp/key.txt | head -1)
[ -n "$PUBKEY" ] || fail "age-keygen didn't produce a public key"
ok "public key $PUBKEY"

echo "3. seeding mail blobs in storage"
# Stalwart stores the raw message and the attachments here, outside the database. Without these
# files in the copy, a restore returns mailboxes with no attachments — that was the gap measured
# on 2026-07-30.
BLOBS=/tmp/blobs-origem/mail
mkdir -p "$BLOBS"
printf 'raw message with attachment' > "$BLOBS/0abc"
head -c 100000 /dev/urandom > "$BLOBS/1def"
ok "2 blobs"

echo "4. running backup.sh (the real one)"
export DATABASE_URL="$PG/beskos"
export BACKUP_BUCKET="$BUCKET"
export BACKUP_DATABASES="beskos stalwart"
export BACKUP_AGE_RECIPIENT="$PUBKEY"
export RCLONE_CONFIG_COPIAS_TYPE=local
export BLOB_BUCKET=/tmp/blobs-origem
export RCLONE_CONFIG_BLOBS_TYPE=local
/usr/local/bin/backup.sh || fail "backup.sh"

ARCHIVE=$(find "$BUCKET/blobs" -name '*.tar.age' | head -1)
[ -n "$ARCHIVE" ] || fail "the mail blobs didn't make it into the copy"
head -c 21 "$ARCHIVE" | grep -q "age-encryption.org" || fail "the blob archive isn't encrypted"
grep -qa "raw message with attachment" "$ARCHIVE" && fail "plaintext mail content inside the archive"
age --decrypt --identity /tmp/key.txt "$ARCHIVE" | tar -tf - | grep -q "0abc" \
  || fail "the blob isn't inside the archive"
ok "mail blobs encrypted and recoverable"

ENCRYPTED=$(find "$BUCKET/beskos" -name '*.dump.age' | head -1)
[ -n "$ENCRYPTED" ] || fail "no encrypted file showed up in the bucket"
ok "$(basename "$ENCRYPTED")"

echo "5. checking that what was uploaded CANNOT be read"
# This is the assertion that matters. Without it, the rehearsal would pass just the same if
# backup.sh had sent the plaintext dump by mistake — and no one would notice until the bucket got
# copied.
head -c 21 "$ENCRYPTED" | grep -q "age-encryption.org" \
  || fail "the file in the bucket doesn't start with the age header — was it sent unencrypted?"
ok "age header present"

pg_restore --list "$ENCRYPTED" >/dev/null 2>&1 \
  && fail "pg_restore was able to read the encrypted file — it isn't encrypted"
ok "pg_restore cannot read it"

grep -qa "secret from beskos" "$ENCRYPTED" \
  && fail "found content text inside the encrypted file"
ok "no plaintext content inside"

[ -z "$(find /tmp -maxdepth 2 -name '*.dump' 2>/dev/null)" ] \
  || fail "a plaintext dump was left on disk after the backup"
ok "no plaintext dump left on disk"

echo "6. checking that the WRONG key doesn't open it"
age-keygen -o /tmp/outra.txt 2>/dev/null
if AGE_IDENTITY_FILE=/tmp/outra.txt BACKUP_BUCKET="$BUCKET" \
   /usr/local/bin/restore.sh beskos "${ENCRYPTED#"$BUCKET"/}" --to "$PG/beskos_restaurado" >/dev/null 2>&1; then
  fail "a different private key was able to restore"
fi
ok "wrong key rejected"

echo "7. restoring with the right key (the real restore.sh)"
AGE_IDENTITY_FILE=/tmp/key.txt BACKUP_BUCKET="$BUCKET" \
  /usr/local/bin/restore.sh beskos "${ENCRYPTED#"$BUCKET"/}" --to "$PG/beskos_restaurado" \
  || fail "restore.sh"

echo "8. comparing the restored content with the original"
RESTORED=$(psql "$PG/beskos_restaurado" -qtAc "SELECT md5(string_agg(body, ',' ORDER BY id)) FROM messages")
[ "$RESTORED" = "$FINGERPRINT" ] || fail "fingerprint mismatch: $RESTORED vs $FINGERPRINT"
ROWS=$(psql "$PG/beskos_restaurado" -qtAc "SELECT count(*) FROM messages")
[ "$ROWS" = "5000" ] || fail "$ROWS rows restored, expected 5000"
ok "5000 rows, byte-for-byte identical content"

echo
echo "full round trip: dump → encrypt → bucket → download → decrypt → restore → identical content"
