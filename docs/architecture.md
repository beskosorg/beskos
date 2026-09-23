# Architecture

## Layers

```
frontend/      React + TypeScript webmail and admin panel
   ↓ REST (session cookie)         ↓ JMAP (end-user credential, same-origin proxy)
backend/cmd/api      HTTP control plane (chi router)
backend/cmd/worker   background jobs: provisioning, reminders, metering, push
   ↓
backend/internal/*   business logic, one package per domain (billing, rbac, wiki, ...)
   ↓
internal/db (sqlc)        internal/stalwart (JMAP client)
   ↓                            ↓
PostgreSQL                 Stalwart (email engine)
```

A handler in `internal/httpapi` never builds SQL or talks JMAP directly when there is real logic
involved -- that lives in a service package underneath it. `internal/httpapi` itself holds only
transport concerns: routing, sessions, request/response shaping.

The frontend never receives a Stalwart admin credential. The webmail authenticates as the end
user and reaches Stalwart only through a same-origin reverse proxy (Stalwart has no CORS
support); everything else -- domains, billing, files, chat -- goes through the Go API instead.

## Multi-tenancy

Every organization is a `tenant_id` in a shared Postgres database. Every query that touches
customer data filters by the tenant of the authenticated session -- never by a tenant id taken
from the request. `internal/rbac` layers per-role and per-member permission overrides on top of
that isolation.

## Why JMAP, not the REST API you might expect

Stalwart historically had a Management REST API. As of the version this project pins
(`stalwartlabs/stalwart:v0.16.4`), that API is gone: `/api/*` returns 404, and there is no
`openapi.yml`. Administration is instead JMAP-only, using a Stalwart-specific extension
(`urn:stalwart:jmap`) with methods in an `x:` namespace (`x:Domain/set`, `x:Account/set`,
`x:DkimSignature/set`, and others). `internal/stalwart` is a hand-written, typed Go client for
that protocol -- there was nothing to generate a client from.

A freshly booted Stalwart instance also starts in "bootstrap mode": every `x:` method except
`x:Bootstrap` refuses to run until a one-time `x:Bootstrap/set` call names the server's hostname
and default domain and the process restarts. `backend/cmd/bootstrap` does this once, idempotently,
against a new instance.

If you're integrating against a different Stalwart version and find the protocol has moved again,
trust what the running server actually does over what any document (including this one) claims,
and send a pull request updating this file.

## Provisioning state machine

Adding a domain moves it through an explicit, resumable state machine
(`backend/internal/provisioning`):

```
pending → domain_created → dkim_created → dns_pending → dns_verified → active
                                                  ↘ error
```

The worker (`cmd/worker`) polls non-terminal states and advances them; a crash mid-provisioning
resumes from the last saved state instead of leaving a domain half-configured. `dns_pending`
waits for the customer to publish the DNS records the previous step generated -- a step that
waits on something external logs *what* it's waiting for and escalates past a time threshold,
so "not yet" (the customer hasn't published DNS) stays distinguishable from "never" (a bug
means it's waiting for something that will never appear).

## Storage encryption

Files uploaded to object storage are encrypted client-side (AES-256-GCM, in blocks) before they
leave the Go process; the object's key travels inside the object itself in a single write, so
there is no separate key-management round trip per file. See `internal/filecrypt`.

## The `ee/` seam

Enterprise-only features live in `backend/ee` (Go, gated by the `pro` build tag) and
`frontend/src/ee` (React, gated at render time by what `GET /api/edition` reports). See
[docs/editions.md](editions.md) for which features that is and why, and
[backend/ee/README.md](../backend/ee/README.md) for the mechanism.

Meeting recording is the one Pro feature that predates this seam and hasn't been moved into it
yet: its code lives in ordinary, always-compiled `internal/httpapi` files, gated only at runtime
by the license key's `Recording` capability -- a Community build links it, just refuses to turn
it on without a key. Every other non-planned row in [docs/editions.md](editions.md)'s Pro column
does go through the compile-time boundary above.

## Further reading

- [docs/adr/0001-jmap-not-rest.md](adr/0001-jmap-not-rest.md) -- why this project talks to
  Stalwart over JMAP instead of the REST API a brief might assume.
- [docs/configuration.md](configuration.md) -- every environment variable.
- [CONTRIBUTING.md](../CONTRIBUTING.md) -- the coding rules referenced above, in full.
