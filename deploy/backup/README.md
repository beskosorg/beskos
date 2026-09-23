# Backups

Three times a day — **02:00, 10:00, and 18:00 UTC** — plus one run on every service startup,
so a configuration error shows up the minute it deploys, not at two in the morning.

Destination: `r2://beskos-backup/<base>/YYYY/MM/DD/<base>-<timestamp>.dump.age`, in **EU
jurisdiction**. 30-day retention. **Encrypted** — see [Encryption](#encryption) right below.

## What's covered, and what isn't

| | Status |
|---|---|
| Application database (`beskos`) — accounts, files, calendar, chat, wiki, billing | **Covered** |
| Mail database (`stalwart`) — messages, attachments, mailboxes, and server configuration | **Covered** |
| Drive files and large attachments (R2) | Live in R2, with the provider's own durability. **We keep no second copy** — an accidental bucket deletion is not recoverable |

Stalwart stores everything in **PostgreSQL**: messages, attachments, accounts, and its own
configuration. That's why mail goes through here like any other database — **live, without
stopping anything**. If it ever moves back to an
embedded store, this stops being true and backing up mail will require a stop: that's a decision
to make with that trade-off in mind.

## What this script does that most don't

1. **Verifies the file before sending it** (`pg_restore --list`, and counts the tables with
   data). A dump that can't be read isn't a backup, it's a file — and that's always discovered on
   the worst possible day.
2. **Confirms on the other end.** Compares the size in R2 with the local size. Sending without
   confirming assumes the network never lies.
3. **Signals when it runs fine.** If `HEARTBEAT_URL` is set, it pings a push monitor on the
   status page; if a backup doesn't happen within the window, the monitor goes red and we know.
   The classic failure mode for backups isn't failing — it's having stopped running three weeks
   ago with no one noticing.
4. **Fails loud.** Any error aborts with a message. No carrying on as if nothing happened.
5. **Encrypts before sending**, with an asymmetric key. Next section.

## Encryption

Dumps leave here encrypted with [`age`](https://age-encryption.org), for an **asymmetric** key.
The server holds only the **public key**: it can write new copies and **cannot read old ones**.

This isn't a configuration detail — it's the difference between two scenarios. With symmetric
encryption (or no encryption), anyone who compromised this container, or the R2 key, could read
the entire history. And that history **includes the mail messages**, because Stalwart stores
everything in Postgres. With an asymmetric key, the same attacker can at most overwrite future
copies with garbage.

The **private key lives outside this infrastructure** — it isn't in the container, the bucket, or
CapRover. It only appears at restore time, passed in via `AGE_IDENTITY_FILE`.

> **Losing the private key makes every copy useless.** That's the price, and it's a deliberate
> one: without it, the backup is a readable copy of every customer's data sitting in a bucket at
> a US provider. Keep it in two places independent of everything else (password vault + paper,
> for example).

Generate the pair:

```sh
age-keygen -o beskos-backup.key     # the private key; store it OUTSIDE of here
# the public key is printed to stderr and in the file's comment: "age1..."
```

The public key goes into `BACKUP_AGE_RECIPIENT`. **Without it the backup aborts** — sending
unencrypted is exactly what this encryption exists to prevent, and a forgotten variable must not
be able to silently degrade that guarantee.

## Restoring

```sh
# 1. Pick the copy. --files-only is MANDATORY: without it rclone also lists FOLDERS, and an
#    absent-minded `tail -1` returns "2026/07/29/" instead of the file. (Happened during the
#    first test.)
rclone lsf r2:beskos-backup/beskos --recursive --files-only | sort | tail -5

# 2. Restore to a NEW database (never over the one currently serving traffic, without looking at
#    it first). restore.sh takes care of downloading, decrypting, and verifying before it writes
#    anything at all.
createdb beskos_restored
AGE_IDENTITY_FILE=/path/to/beskos-backup.key BACKUP_BUCKET=beskos-backup \
  restore.sh beskos beskos/2026/07/29/beskos-<timestamp>.dump.age \
  --to "postgres://.../beskos_restored"

# 3. Check before switching anything over
psql "postgres://.../beskos_restored" -c "select count(*) from tenants;"
```

Without `--to`, `restore.sh` decrypts, verifies, and leaves the plaintext dump on disk — which is
what you want when the destination doesn't exist yet, or when you just want to peek inside.

The `--no-owner --no-privileges` on both the dump and the restore is deliberate: the restore no
longer depends on the same roles existing at the destination, which is what ruins half of all
emergency restores.

### Rehearsal

```sh
deploy/backup/test-restore.sh
```

Spins up a disposable Postgres, runs the **real `backup.sh` and `restore.sh`** (not copies
adapted for testing — a rehearsal that exercises a similar script proves nothing about what runs
at two in the morning), and compares the content at the end. R2 is replaced with a rclone `local`
remote: the same command, the same code path, writing to a directory instead.

Checks, in this order: that the uploaded file starts with the `age` header; that `pg_restore`
**cannot** read it; that there's no content text inside it; that no plaintext dump was left on
disk; that a **different private key is rejected**; and that restoring with the right key returns
rows with identical content (compared by content hash, not row count — a count would let through
a restore that brought back the right rows with swapped content).

Run this every time `backup.sh`, `restore.sh`, or the image changes. The asymmetric encryption
means an error on the key side only shows up on restore day — the worst possible day to discover
it.

## Configuration

App `backup` on CapRover, image `beskos-backup`. Variables:

| Variable | Purpose |
|---|---|
| `DATABASE_URL` | Same as the API's |
| `R2_ENDPOINT` | `<account>.eu.r2.cloudflarestorage.com` — the **EU jurisdiction** endpoint |
| `R2_ACCESS_KEY` / `R2_SECRET_KEY` | R2 credentials |
| `BACKUP_BUCKET` | `beskos-backup` |
| `BACKUP_AGE_RECIPIENT` | **Public** key `age1…`. **Required** — without it the backup aborts |
| `BACKUP_DATABASES` | `beskos stalwart` (default) |
| `RETENTION_DAYS` | 30 |
| `HEARTBEAT_URL` | Push monitor on the status page (optional, but it's what alerts us) |

## To improve

- **Dedicated credential.** Today the service uses the same R2 key as the application, which also
  reads customer files. A token with write-only access to the backups bucket would limit the
  damage of a leak. Less critical since encryption was added — what's stored there is already
  unreadable — but it still allows deletion.
- **Copy outside of R2.** Everything with a single provider is an account risk, not a disk risk.
- **Periodic restore rehearsal.** `test-restore.sh` proves the round trip on any machine, but it
  runs by hand. Scheduling it would catch a silent degradation — that's the difference between
  "it was proven once" and "we know it works today."
