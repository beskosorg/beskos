#!/bin/bash
# Schedule: 02:00, 10:00, and 18:00 UTC. Also runs ONCE on startup — so a configuration error
# shows up the minute it deploys, not at two in the morning, and a freshly installed service
# already leaves a backup behind it.

set -euo pipefail

# rclone reads its configuration from the environment (RCLONE_CONFIG_<REMOTE>_*), which avoids
# having a file with credentials inside the image.
# Destination for the copies. The provider is configurable on purpose: it has already been
# Cloudflare, is now Bunny, and the next one shouldn't require touching code — just a variable.
export RCLONE_CONFIG_COPIAS_TYPE=s3
export RCLONE_CONFIG_COPIAS_PROVIDER="${BACKUP_PROVIDER:-Other}"
export RCLONE_CONFIG_COPIAS_ENDPOINT="${BACKUP_ENDPOINT:?BACKUP_ENDPOINT is missing}"
export RCLONE_CONFIG_COPIAS_ACCESS_KEY_ID="${BACKUP_ACCESS_KEY:?BACKUP_ACCESS_KEY is missing}"
export RCLONE_CONFIG_COPIAS_SECRET_ACCESS_KEY="${BACKUP_SECRET_KEY:?BACKUP_SECRET_KEY is missing}"
export RCLONE_CONFIG_COPIAS_REGION="${BACKUP_REGION:-auto}"
export RCLONE_CONFIG_COPIAS_NO_CHECK_BUCKET=true

# Second remote: the APPLICATION's bucket, from which the mail prefix is read. It's read-only for
# this service — the backup never writes to the production bucket, and separate credentials make
# that verifiable instead of merely promised.
if [ -n "${BLOB_BUCKET:-}" ]; then
  export RCLONE_CONFIG_BLOBS_TYPE=s3
  export RCLONE_CONFIG_BLOBS_PROVIDER=Cloudflare
  export RCLONE_CONFIG_BLOBS_ENDPOINT="${BLOB_ENDPOINT:?BLOB_ENDPOINT is missing}"
  export RCLONE_CONFIG_BLOBS_ACCESS_KEY_ID="${BLOB_ACCESS_KEY:?BLOB_ACCESS_KEY is missing}"
  export RCLONE_CONFIG_BLOBS_SECRET_ACCESS_KEY="${BLOB_SECRET_KEY:?BLOB_SECRET_KEY is missing}"
  export RCLONE_CONFIG_BLOBS_REGION=auto
  export RCLONE_CONFIG_BLOBS_NO_CHECK_BUCKET=true
fi

echo "backup: service started — 02:00, 10:00, and 18:00 UTC (+ one run now)"

/usr/local/bin/backup.sh || echo "backup: initial run failed — see above"

# busybox's crond does not inherit the environment, so it's saved to a file that the cron job
# loads.
env | grep -E '^(DATABASE_URL|BACKUP_BUCKET|BACKUP_DATABASES|BACKUP_AGE_RECIPIENT|BACKUP_PROVIDER|BACKUP_ENDPOINT|BACKUP_ACCESS_KEY|BACKUP_SECRET_KEY|BACKUP_REGION|BLOB_BUCKET|BLOB_PREFIX|BLOB_MAX_KIB|RETENTION_DAYS|HEARTBEAT_URL|RCLONE_CONFIG_)' \
  | sed 's/^/export /; s/=/="/; s/$/"/' > /etc/backup.env

mkdir -p /etc/crontabs
cat > /etc/crontabs/root <<'CRON'
0 2,10,18 * * * . /etc/backup.env && /usr/local/bin/backup.sh
CRON

exec crond -f -l 8
