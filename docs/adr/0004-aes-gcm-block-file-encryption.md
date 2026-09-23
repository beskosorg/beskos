# 4. Block-based AES-256-GCM encryption for stored files

## Status

Accepted.

## Context

Files uploaded to the Drive (object storage, MinIO in the reference deployment) need to be
encrypted at rest without trusting the storage backend with the key. Two constraints shaped the
design, both visible directly in `backend/internal/filecrypt/format.go`'s own documentation:

1. **Range reads have to keep working.** The product serves video with seek (`http.ServeContent`
   honoring `Range` headers) and the desktop sync daemon reads files in chunks to compare hashes
   against a remote copy. A cipher that requires decrypting from the start of a file to reach the
   middle would make both of those unusable on anything large.
2. **Writing a file has to be atomic.** If the encryption key lived in a separate database table,
   saving a file would be two writes -- the object and the key row -- with a failure window
   between them in either order: write the object first and a crash leaves ciphertext with no
   key (data loss); write the key row first and a crash leaves an orphaned key.

## Decision

Encrypt in fixed-size blocks (`DefaultChunkSize` = 1 MiB) with AES-256-GCM, and carry the
object's own encryption key **inside the object**, wrapped, rather than in a separate table:

```
header (32 bytes) | wrapped key (n bytes) | block 0 | block 1 | ... | block n
```

Each block is independently AES-256-GCM sealed with a nonce derived from a random 4-byte prefix
(stored once, in the header) plus the block's own index -- so a block can be decrypted on its own
once its offset is known, which is what makes range reads and chunked hashing work without
touching the rest of the file. The block index and a last-block marker are authenticated
associated data (AEAD) on every block: without that, whoever controls the storage backend could
reorder blocks or truncate the file's tail and the per-block seal alone would not catch it.

Wrapping the key inside the object makes the write atomic by construction: there is exactly one
write (the storage backend's `PUT`), and it either lands whole or not at all -- there is no
second write to order against the first. Whoever holds the stored object holds the wrapped key
but cannot open it: that needs the tenant's key (kept in this project's own database) and the KEK
(kept in this project's own environment), and the object store has neither.

A 4-byte magic (`BSK1`) at the start of the header distinguishes an encrypted object from
pre-encryption content, which is what lets existing, unencrypted objects keep being served as-is
without a migration cutover -- an object only gains the header once it is next written.

## Consequences

- Per-block overhead is small and fixed: 16 bytes of GCM tag per 1 MiB block, about 0.0015%.
- A seek decrypts at most one extra block's worth of data beyond what was requested, bounded by
  the chunk size -- a deliberate trade-off between per-block overhead and seek granularity.
- The format is versioned (`buf[4]`) so a future incompatible change can coexist with old objects
  during a rollout, the same way the encrypted/unencrypted split does today.
- Losing the tenant key or the KEK loses access to every object encrypted under it -- there is no
  secondary key-escrow path in this design; that risk is accepted in exchange for having no
  separate key-management round trip on every read and write.

## Alternatives considered

- **A single encryption pass over the whole file, key in a separate table.** Simpler to write,
  but breaks range reads on large files and reintroduces the two-write atomicity problem this
  design exists to avoid.
- **Server-side encryption provided by the object storage backend (e.g. SSE-S3/SSE-KMS).**
  Rejected as the sole mechanism: it ties confidentiality to trusting the storage backend's own
  key management, and a self-hosted deployment may run storage that doesn't offer it at all
  (see `internal/storage` for the pluggable backend). Application-level encryption keeps the
  guarantee independent of which storage backend is configured.
