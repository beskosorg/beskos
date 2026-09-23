# 1. Administer Stalwart over JMAP, not REST

## Status

Accepted.

## Context

The original integration plan assumed Stalwart exposed a REST Management API: endpoints like
`POST /api/principal` and `POST /api/dkim`, documented with an `openapi.yml` a client could be
generated from. That assumption came from Stalwart's own historical documentation, not from the
pinned version.

Verified empirically against `stalwartlabs/stalwart:v0.16.4` (booted locally, cross-checked
against the source at that tag): the REST Management API is gone. Every `/api/*` path returns
404, and no `openapi.yml` is served or embedded. Administration -- creating domains, mailboxes,
DKIM keys, reading the DNS zone a customer needs to publish -- is available only through JMAP,
using a Stalwart-specific extension capability (`urn:stalwart:jmap`) with methods in an `x:`
namespace: `x:Domain/set`, `x:Account/set`, `x:DkimSignature/set`, `x:Bootstrap/set`, and others.

This is a case the project's own rules anticipate directly: when the brief and the running
software disagree, trust the software and record the divergence rather than build against a
document. Every payload shape referenced below was actually tested against the server (aliases
as a map of objects, not a list of address strings; accounts suspended by clearing
`credentials` rather than by a status field; DKIM records that don't appear in the DNS zone
synchronously with domain creation).

## Decision

Write a hand-typed Go JMAP client (`backend/internal/stalwart`) against the `x:` namespace
instead of generating a REST client. Every admin call goes through a single envelope:

```json
{ "using": ["urn:ietf:params:jmap:core", "urn:stalwart:jmap"],
  "methodCalls": [ ["x:Domain/set", { "create": { "d1": { "name": "example.com" } } }, "0"] ] }
```

sent to `POST /jmap/` with Basic auth as the recovery admin (`STALWART_RECOVERY_ADMIN`). A freshly
booted Stalwart instance also starts in bootstrap mode, refusing every `x:` method except
`x:Bootstrap` until a one-time `x:Bootstrap/set` call and a restart; `backend/cmd/bootstrap` runs
that once, idempotently, against a new instance (see
[docs/architecture.md](../architecture.md#why-jmap-not-the-rest-api-you-might-expect)).

## Consequences

- No client-generation step (`oapi-codegen` or similar) and nothing to keep in sync with an
  OpenAPI spec that doesn't exist for this surface -- the client is maintained by hand against
  the JMAP methods actually used.
- The client encodes Stalwart-specific quirks that a generated client would have hidden or
  gotten wrong: aliases and credentials are Stalwart's "indexed map" encoding of a JSON-Schema
  `List<T>`, not arrays; a credential patch has to replace the whole map or a single field, never
  a partial subset, or it destroys the login it was meant to update.
- Tying to a version-specific admin protocol means a Stalwart upgrade can move the ground again.
  This project pins the Stalwart image version deliberately (`docker-compose.yml`) rather than
  tracking `:latest`, so an upgrade is a deliberate, tested step rather than something that
  changes the API surface under the client's feet.
- Anyone integrating against a different Stalwart version should trust what the running server
  does over what this ADR or any other document claims, and send a pull request updating this
  one if something has moved.

## Alternatives considered

- **Generate a client from Stalwart's published OpenAPI spec.** Not viable: no such spec exists
  for the pinned version, and the REST surface it would have described has been removed.
- **Vendor an older Stalwart release that still has the REST API.** Rejected -- pins the project
  to an unmaintained version for a convenience that JMAP already provides, and loses whatever
  fixes and features shipped since.
