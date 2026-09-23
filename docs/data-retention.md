# Data retention: what we keep, for how long, and why

This is the document a customer's lawyer reads, and the one we have to be able to defend. Every
number in it corresponds to a constant in the code; if one changes, the other has to change in the
same commit. `TestRecoveryWindowIsThirtyDays` exists to make that failure loud.

We are a **processor**. The customer is the controller: their employee's right to erasure is
exercised against *them*, not against us, and our job is to make it possible for them to comply —
Article 28(3)(e) — and to delete or return the data when the service ends — Article 28(3)(g).

## A mailbox

| State | Who can read it | Does mail still arrive | Data |
|---|---|---|---|
| **Active** | the owner | yes | intact |
| **Suspended** | nobody | **yes**, it collects | intact |
| **Deleted** | nobody | **no** — the address is released | intact, for 30 days |
| **Purged** | — | — | gone |

**Suspended** removes the account's credentials, so IMAP, SMTP, JMAP and the panel all refuse the
password at once. Mail keeps being delivered into the mailbox, which is the point: somebody on
suspension has not left. Reactivating requires setting a **new** password — the old one no longer
exists.

**Deleted** is not destruction. The mailbox is suspended, and its **address is taken away from it**:
the account is renamed, so senders to the old address are told the recipient does not exist, or —
only if an admin named a successor — the address is given to that colleague as an alias. Forwarding
never happens by default, because silently redirecting somebody's mail to a colleague is what
European authorities have penalised employers for.

**The address stays reserved** for the whole 30 days. Nobody can be given it in the meantime. Mail
meant for the person who left must never land in the inbox of whoever inherited their address —
that turns a resignation into a data breach. (Google reserves for 20 days; we chose 30 because the
people who discover that a mailbox mattered are usually the ones who were on holiday.)

**Purged** happens in the worker, ten minutes at a time, and is the only thing in the system that
actually destroys a mailbox. It re-checks the state at the moment of deletion, so a mailbox restored
seconds earlier is left alone.

### Getting the data out

During the 30 days, an admin with the `mailboxes.delete` permission can download the whole mailbox
as a single **mbox** file: every message exactly as it arrived, from every folder, in a format any
mail client can import and a human can read with `less` in ten years.

This is the "**or return**" half of Article 28(3)(g). A product that only implements deletion is
not offering the choice the article requires.

It is deliberately **not** possible for a live mailbox. Reading somebody's mail needs their
credentials; minting one for a mailbox still in use would lock the owner out to satisfy an export,
and an administrator quietly copying a working mailbox is not administration.

## A domain

A domain cannot be removed while any mailbox on it still holds data — including mailboxes already
deleted whose window has not closed. The API says **which** of the two is blocking, and when the
second one frees up. ("You still have mailboxes" told at least one person to go and delete
mailboxes that were already deleted.)

## Backups

Deleted data leaves the live systems immediately at purge. It leaves the **backups** as those
backups age out: copies are kept for **30 days** (`RETENTION_DAYS` in
[`deploy/backup/backup.sh`](../deploy/backup/backup.sh)).

The two windows stack, and saying otherwise would be the convenient lie. A mailbox deleted on day 0
is purged on day 30, and the last backup that still contains it was taken just before that — so it
can survive in backups until roughly **day 60**. That is the number to quote, not thirty.

Backups are never selectively edited. Restoring one restores the state of that day, and a restore
that would bring back purged mailboxes is followed by re-running the purge before the system is
returned to service.

This is written down because "we delete it eventually" is exactly the answer the EDPB's February
2026 coordinated enforcement report found unacceptable across 764 organisations: what is required
is an approach that is documented, reasoned and proportionate — and stated honestly to the people
whose data it is.

## What we do NOT delete, and cannot

**Invoices and accounting records.** Tax law requires us to keep them, and Article 28(3)(g) has the
carve-out in the same sentence: *unless Union or Member State law requires storage*. They contain a
company name, a billing address, a tax number and what was paid — not correspondence, not files, not
calendars.

Nothing else survives a purge.

## Where this lives in the code

| | |
|---|---|
| The window | `RecoveryWindow`, `internal/httpapi/mailbox_lifecycle.go` |
| States and the reserved address | migration `00134_mailbox_lifecycle.sql` |
| Releasing the address / the successor | `internal/httpapi/mailbox_mail_routing.go` |
| The export | `internal/mailbox/export.go` |
| The purge | `backend/cmd/worker/purge.go` |
| Suspending in Stalwart | `SuspendAccount`, `internal/stalwart/provisioning.go` |
