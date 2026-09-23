# API

This is an orientation to the HTTP surface the Go backend exposes, not a full reference. The
full, current route list is the router itself:
[`backend/internal/httpapi/server.go`](../../backend/internal/httpapi/server.go)'s `Router()`
function. It is grouped by concern with inline comments explaining why each group needs the
authentication and permissions it has -- reading it is faster and more reliable than a generated
spec would be at this stage, and it cannot drift out of sync with itself the way a hand-maintained
OpenAPI document could. Generating a full OpenAPI spec from the route table is a reasonable
future addition; it is out of scope for this document.

## Base URL

There is one HTTP server (`backend/cmd/api`) behind everything: the panel API, the webmail's
session and mail-adjacent endpoints, and a same-origin JMAP proxy to Stalwart. In the reference
deployment (`deploy/docker-compose.yml`) it listens on `:8000` behind whatever reverse
proxy terminates TLS; see [docs/self-hosting.md](../self-hosting.md) for a production Caddy
config. Every application route under `/api/*` also answers `GET /api/healthz` and
`GET /api/readyz`; the bare `/healthz` and `/readyz` (no `/api` prefix) exist for a load balancer
or orchestrator that reaches the container directly.

## Authentication

Two different authentication schemes cover two different things this server does, and they are
not interchangeable:

- **Session cookie, for the panel and everything that isn't mail transport itself.** Login
  (`POST /api/auth/login`, with an MFA step at `POST /api/auth/mfa/verify` when enabled) sets a
  session cookie; `requireWebmailSession` (`backend/internal/httpapi/webmail_auth.go`) validates
  it on every route inside the authenticated group and loads the requesting mail user into the
  request context. A user whose account is suspended has their session torn down and rejected
  immediately, not merely blocked at the next login (`auth.account_suspended`) -- suspension has
  to take effect for an already-open session, or it doesn't do what it claims to.

  State-changing requests (`POST`/`PUT`/`PATCH`/`DELETE`) inside that session-authenticated group
  also pass through `csrfGuard`: the request must carry either an `Authorization` header (the
  native desktop app's non-cookie bearer token) or the `X-Mailin-Client` header the web frontend
  always sends. A cross-site form submission can't set a custom header without a CORS preflight,
  and that preflight is checked against the configured origin allow-list -- so a plain
  cross-origin `<form>` POST is rejected before it reaches a handler.

  Routes that need a specific permission, not just a valid session, are wrapped with
  `s.requirePerm(rbac.Perm...)` -- domain management, billing, member/role changes, and similar
  administrative actions. `internal/rbac` is the source of truth for what each permission gates.

- **JMAP directly to Stalwart, for mail itself.** The webmail never holds a Stalwart credential.
  It authenticates to *this* API with its normal session, and reaches Stalwart only through a
  same-origin reverse proxy mounted at `/jmap/*`
  (`backend/internal/httpapi/jmap_proxy.go`). The proxy validates the caller's session and then
  **injects** that mailbox's own JMAP credential (stored encrypted in `mail_users.jmap_cred`) into
  the request on its way to Stalwart -- credential brokering, so the mailbox password never
  travels to or lives in the browser. This is also why `/jmap/*` is mounted unconditionally for
  both the web app and the native (Tauri) app: the brokering has to sit in the path either way. See
  [ADR 0001](../adr/0001-jmap-not-rest.md) for why administrative operations on Stalwart go over
  this same JMAP protocol rather than a REST API.

A handful of routes use neither scheme, each for a specific reason visible in `server.go`'s
comments: `POST /billing/webhook` authenticates by the billing provider's own request signature;
`/sync/*` (the desktop sync daemon) authenticates by a device token, not a session, with access
scoped to just the file-sync endpoints; `/doc/*` (the OnlyOffice callback) and the AI proxy
endpoints (`/ai/openai/v1/*`, `/ai/proxy`, `/v1/chat/completions`) authenticate by an HMAC token
or Bearer token in the request itself, because the caller is another service (the Document
Server, LiveKit) rather than a logged-in browser.

## Errors

Every error response is a stable `error_code` plus an English fallback message derived from it
mechanically (`writeErr`/`humanizeCode` in `backend/internal/httpapi/middleware.go`), for
example:

```json
{ "error_code": "auth.invalid_credentials", "error": "Invalid credentials" }
```

The `error_code` is the contract: the frontend translates it through i18n
(`server.<error_code>`) and shows the user their own language's message, never the English
fallback text. This follows directly from law 4.4/4.6 -- the backend never assumes or
embeds the caller's language in an error, and the frontend never displays raw backend text. If
you add a new error path, give it a stable dotted code (`<area>.<reason>`, matching the existing
codes) rather than a free-form message, and add its translation to every locale under
`frontend/src/i18n/locales/`.

## Finding a specific route

Search `backend/internal/httpapi/server.go`'s `Router()` for the path or feature area -- it is
organized in commented blocks by feature (mail, domains, billing, the AI assistant, Kanban,
calendar, contacts, wiki, files, chat, meetings) in roughly the order a user would encounter them
in the product. Each handler function lives in a file named after that feature area in the same
package.
