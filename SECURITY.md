# Security policy

## Supported versions

The latest tagged release and the one before it receive security fixes. Older versions do not;
upgrade before reporting an issue against one.

## Reporting a vulnerability

Email **security@beskos.com** with a description, the affected version or commit, and steps to
reproduce. Do not open a public issue for a vulnerability.

- We acknowledge a report within **72 hours**.
- We aim to confirm, fix, and coordinate disclosure within **90 days** of the report. If a fix
  needs longer, we say so and keep you updated rather than going quiet.
- Once a fix ships, we credit the reporter (unless you ask us not to) and publish a GitHub
  Security Advisory with a CVE where one applies.

## Scope

In scope: the code in this repository (`backend/`, `frontend/`, `sync-daemon/`) and its own
configuration. Out of scope: Stalwart, OnlyOffice Document Server, LiveKit, PostgreSQL, and other
upstream services this project runs against unmodified -- report those to their own maintainers.
A vulnerability in how *this project configures or calls* one of them is in scope.

## What we consider a vulnerability

Anything that lets one tenant read or affect another tenant's data, bypasses authentication or
the RBAC checks in `internal/rbac`, forges or replays a signed token (session, JMAP, license
key, presentation link), or executes code the server did not intend to run. Missing rate limits,
verbose error messages, and other hardening gaps are welcome as regular issues -- flag them as
security-sensitive in the report if you are unsure which this is.
