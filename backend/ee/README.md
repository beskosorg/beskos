# backend/ee

Enterprise routes, gated by `-tags pro` and by `internal/edition.Capabilities`. See
[../../ee/README.md](../../ee/README.md) for the license and the overall shape, and
[../../docs/editions.md](../../docs/editions.md) for which features this is meant to hold.

`Register` is a no-op in both builds; nothing depends on it mounting anything yet. Moving a
feature is, per feature: extract its handler(s) here, add the matching field to `Deps`, and prove
with a test that the route answers `403 edition_required` (or similar) without the capability. Do
this one feature at a time (project law §6.1), not as one large change.

**Where the capability check actually lives.** Most Pro routes are already RBAC-gated by an
httpapi middleware (`s.requirePerm(...)`) that needs `*Server` (a DB call for per-member
permission overrides) -- `ee` cannot depend on `httpapi` without an import cycle, since `httpapi`
is what calls `ee.Register`. Rather than have `Register` mount routes conditionally, the working
pattern (see `branding_pro.go`/`branding_community.go`) is:

1. httpapi keeps the route mounted, behind its normal RBAC middleware, pointing at a small
   `handle<Feature>Gate` method on `*Server`.
2. That gate method checks `s.edition.<Field>` and answers `403 edition_required` itself when it's
   off; otherwise it calls into the `ee`-exported function for the real logic.
3. The real logic (validation, database writes) lives in `<feature>_pro.go`, built only with
   `-tags pro`. `<feature>_community.go` (`//go:build !pro`) defines the same exported function
   name refusing the same way, so a build without the tag never links the Enterprise logic at
   all -- the httpapi-side gate is defense in depth, not the only line drawn.
4. The authenticated user reaches `ee` via `internal/reqauth` (`reqauth.MailUserFrom(ctx)`),
   the same context key httpapi's session/JMAP-proxy middleware already populates -- a shared
   leaf package exists specifically so `ee` never needs to import `httpapi` for it. Generic JSON
   response helpers live the same way, in `internal/httpx`.
