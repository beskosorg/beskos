# Architecture Decision Records

Structural decisions for this project, in the standard ADR shape (Status, Context, Decision,
Consequences). Each is grounded in the code and configuration as they exist today; where a
decision diverges from an earlier plan, the ADR says so and points at the source that settled it.

A new structural decision -- a new dependency, a schema change, an authentication change -- gets
one of these before the code, per [CONTRIBUTING.md](../../CONTRIBUTING.md).

| ADR | Decision |
|---|---|
| [0001](0001-jmap-not-rest.md) | Administer Stalwart over JMAP, not the REST API the brief assumed -- it doesn't exist in the pinned version. |
| [0002](0002-onlyoffice-document-editing.md) | In-browser Office editing runs on OnlyOffice Document Server, network-only, with the AGPL §13 source offer enforced at boot. |
| [0003](0003-livekit-for-meet.md) | Video calls and meeting recording run on self-hosted LiveKit + Egress. |
| [0004](0004-aes-gcm-block-file-encryption.md) | Files are encrypted at rest in fixed-size AES-256-GCM blocks, with the key wrapped inside the object for atomic writes and seekable reads. |
| [0005](0005-pgvector-for-wiki-search.md) | Wiki semantic search uses the `pgvector` Postgres extension instead of a separate vector database. |
| [0006](0006-busl-license.md) | The core ships under Business Source License 1.1, converting to Apache 2.0 after four years, with dual licensing for competing offerings. |
