# Changelog

All notable changes to this project are documented in this file. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

Work preparing the first public, source-available release.

### Added

- A Business Source License 1.1 core license with a four-year conversion to Apache License 2.0,
  plus dual licensing for anyone who wants to offer the software as a competing hosted service
  instead of staying within the license's use grant. See
  [ADR 0006](docs/adr/0006-busl-license.md) for the reasoning and the alternatives considered.
- `ee/LICENSE` (the Enterprise license covering Pro-only code), `TRADEMARK.md`, `SECURITY.md`,
  `CODE_OF_CONDUCT.md` (Contributor Covenant 2.1), and `CONTRIBUTING.md`.
- An `edition` package that determines whether an instance has unlocked Pro capabilities, by
  verifying a signed license key offline (Ed25519) with a 14-day grace period after expiry, and
  a public `GET /api/edition` endpoint so the frontend can gate Pro-only UI before a user is even
  logged in. See [docs/pro/README.md](docs/pro/README.md) for the full activation flow, including
  why a missing key and an invalid key are handled differently.
- A `backend/ee` seam: Pro-only backend routes register through it, gated by both a Go build tag
  and the relevant capability. Three features moved there so far -- organization AI agents,
  custom branding, and mandatory organization-wide MFA -- each with a Community build that
  refuses the capability unconditionally, independent of any license state. See
  [`backend/ee/README.md`](backend/ee/README.md) for the pattern the next feature follows.
- `cmd/bootstrap-owner`, the self-hosted equivalent of the hosted signup flow: creates the first
  tenant, domain, and owner mailbox on a fresh install (refuses if one already exists). Without
  it a fresh Community install had no way to create its first organization at all.
- `deploy/docker-compose.yml`, a self-hosted reference deployment: the same product
  without the hosted operator console, running Valkey instead of Redis for license compatibility.
- Public documentation: `docs/README-oss.md`, `docs/architecture.md`,
  `docs/configuration.md` (generated from the config package so it can't drift out of date),
  `docs/editions.md`, `docs/getting-started.md`, `docs/self-hosting.md`, `docs/FUNCTIONALITY.md`,
  and now `docs/adr/` (architecture decision records), `docs/api/README.md` (API orientation),
  and `docs/pro/README.md`.
- CodeQL and Dependabot scanning, `gitleaks` in CI with an allowlist for known test fixtures, and
  a `govulncheck` job for Go stdlib and dependency vulnerabilities.
- A first security threat model, and a `gosec` CI job (report-only while findings are triaged).
- A GoReleaser pipeline building `api`, `worker`, and `bootstrap` for Linux and macOS on amd64 and
  arm64, publishing releases as drafts; issue and PR templates; a `CODEOWNERS` file.
- GitHub Discussions enabled, with `good first issue` and `help wanted` labels for external
  contributors.
- `scripts/export_public.py`, which produces the public repository export with an exclusion list
  and a residue report so nothing internal ships by accident.

### Changed

- Renamed the Go module from its internal name to `github.com/beskosorg/beskos/backend`, across
  every import in the backend.
- Renamed the remaining internal identifiers to match: the Rust sync daemon crate
  (`mailin-sync` to `beskos-sync`) and the desktop app's Rust crate (`mailin` to `beskos`).
- Made `BRAND_NAME`, `BRAND_URL`, and `SALES_EMAIL` configurable through the environment instead
  of hardcoded, defaulting to the values already used in production so behavior doesn't change
  for the existing deployment.
- Split `internal/httpapi/server.go`'s `Server` type and constructor into their own file
  (`server_types.go`) to keep the router file under the project's 600-line limit.
- Fixed the CI workflow to actually run against this repository's `master` branch -- it had been
  configured for `main` and had never run on push.

### Fixed

- Three pre-existing CI failures uncovered the moment the workflow started running for real (it
  had targeted a branch that doesn't exist here): unformatted Rust code in the sync daemon and
  the desktop app's Tauri crate, a frontend CI job installing dependencies without the flag the
  project's own build already required, and a frontend test that assumed a Portuguese locale and
  failed under any other one.
- A malformed line in the `gitleaks` CI configuration that had been silently failing the entire
  workflow.
- A fresh Community install: the public `Dockerfile` built three Cloud-only binaries that don't
  exist in the export, Stalwart crash-looped because the database its config expects was never
  created, and self-hosted plan quotas of `0` were enforced as literal zero-mailbox/zero-storage
  caps instead of "unlimited" (only `MaxDailySend` had that convention before). All three meant a
  self-hosted instance could not boot, or booted into a mailbox that could not do anything, before
  this was tested end-to-end for the first time.
- All remaining hardcoded Portuguese strings and code comments across the backend and frontend
  translated to English (law 4.6) -- including a real bug, not just a translation gap: a public
  booking page's calendar-event title/description was always built in Portuguese even though the
  host's own language preference was already available at that call site.
- `sync-daemon`'s `cargo clippy -D warnings` gate had never actually been run clean: 2 formatting
  violations and 6 clippy errors, plus dead documentation comments left behind by an earlier
  refactor describing functions that had since moved elsewhere in the file.

### Security

- **A legacy, fully unauthenticated tenant-creation endpoint (`/auth/signup`) was mounted in
  every build, self-hosted and hosted, with no rate limit, no relation to the real signup flow,
  and no check for an already-provisioned instance -- any request could create a new organization
  and owner account for free, with no limit.** Gated behind an explicit, off-by-default flag that
  a production boot now refuses outright if enabled.
- The OnlyOffice save callback and meet-presentation PDF conversion could be pointed at an
  attacker-controlled URL if the Document Server's own response were ever compromised; both now
  share one host allowlist and a hardened (no-redirect, timeout) HTTP client.
- File uploads: the multipart upload path didn't sanitize its filename the way the sync-upload
  path already did; image thumbnailing had no cap on a file's declared pixel dimensions before
  allocating a buffer for them (a classic decode-time memory bomb); OOXML text extraction had no
  cap on a single zip member's decompression ratio.
- A meet-lobby endpoint let an authenticated host of one tenant look up another tenant's pending
  guest-request row by guessing its ID, before the subsequent ownership check rejected the request.
- Logging out on the native app only cleared the `wm_session` cookie's session row, never the
  `Authorization: Bearer` token the native app actually authenticates with -- a leaked native-app
  token stayed valid after "logging out" until it expired on its own (up to 7 days).
- SMS MFA enrollment (`/mfa/sms/start`) had no rate limiting at all, letting any authenticated
  account script SMS sends to an arbitrary phone number.
- Fuzz tests (`FuzzSNSVerify`, `FuzzValidCertURL`, `FuzzVerifyStripeSignature`,
  `FuzzParsePresToken`) added for the SNS, Stripe, and OnlyOffice-presentation-token signature/
  parsing boundaries; none found a crash.
- Two indirect dependencies patched for known advisories (`klauspost/compress`, `golang-jwt/jwt`);
  a third (`golang.org/x/crypto`) deliberately left unpatched for now since the fix requires a Go
  toolchain bump past what the published Docker image provides -- documented as an open item
  rather than silently deferred.
- A full pass over tenant isolation, sessions/MFA, uploads/OnlyOffice, license enforcement, and
  JMAP credential brokering found the existing design sound; see
  [`docs/security/threat-model.md`](docs/security/threat-model.md) for the complete, per-boundary
  writeup, including the residual risks above and one deliberately unfixed item (an
  organization's "MFA required" policy is enforced only as a UI hint today, not against a
  not-yet-enrolled user's session -- closing it needs a product decision, not a blind patch).
- The `check_tenant_scoping.py` heuristic's ~219 flagged queries were cross-checked by hand
  against their actual call sites (not just spot-checked): every one resolved to either
  self/owner-scoped access (the row's owner already implies its tenant) or a documented,
  intentional cross-tenant maintenance path (a CLI tool or background worker, never reachable
  from an authenticated tenant request) -- no real gap found.

### Security

- Upgraded Go from 1.25.7 to 1.25.13, resolving 21 standard-library vulnerabilities the newly
  added vulnerability scanner found with real call paths into this codebase (TLS handshake and
  certificate handling, `net/http`, `net/url`, `encoding/xml`, `encoding/asn1`).
- Triaged all 130 pre-existing `gosec` findings by hand; the ones in categories severe enough to
  matter (flagged possible SSRF) were confirmed as already-defended call sites (private-IP
  blocking and DNS-rebinding protection already in place) rather than real gaps.
- Added a heuristic checker (`scripts/check_tenant_scoping.py`) that cross-references every
  tenant-scoped table against the queries that touch it and flags any missing a `tenant_id`
  mention, to support review of the project's multi-tenancy rule. Remains report-only by design
  (a query can be safely scoped without mentioning the column, e.g. via ownership or a join), but
  every hit has since been triaged by hand -- see the tenant-isolation entry above.

[Unreleased]: https://github.com/beskosorg/beskos/commits/master
