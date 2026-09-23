# Pro

How a self-hosted instance activates a Pro license, and exactly what that does today. Read
[`backend/internal/edition/edition.go`](../../backend/internal/edition/edition.go) and
[`backend/internal/httpapi/edition_handlers.go`](../../backend/internal/httpapi/edition_handlers.go)
for the code this describes.

## What Pro is

Pro unlocks a fixed set of capabilities on top of the free Community edition, at
€1/user/month -- see [docs/editions.md](../editions.md) for the current feature split and the
licensing terms. This document is about the activation mechanism, not the feature list.

## Configuration

Two environment variables, both required together:

- `EDITION_LICENSE_KEY` -- a license key of the form
  `<base64url payload>.<base64url Ed25519 signature>`, issued per organization.
- `EDITION_PUBLIC_KEY` -- the base64-standard-encoded Ed25519 public key used to verify it.

Verification is entirely **offline**: `edition.Verify` checks the signature against the
configured public key and decodes the payload (organization, seat count, expiry, and which
capability flags are granted) without the running process ever calling out to a licensing
server. The signing key itself never lives in this repository -- only the public key does, and
only in a deployment's own environment, not as a default in code.

## `GET /api/edition`

Unauthenticated on purpose (`handleGetEdition`), because the signup and login pages need to know
the edition before there is a session to check it with. It returns the `edition.Capabilities`
this instance unlocked at startup:

```json
{
  "edition": "community",
  "seats": 0,
  "recording": false,
  "branding": false,
  "audit_log": false,
  "org_policies": false,
  "agents": false
}
```

`edition` is `"community"` or `"pro"`; `seats` is `0` for unlimited (always the case in
Community); the boolean fields are the individual capability flags Pro can grant
(`Recording`, `Branding`, `AuditLog`, `OrgPolicies`, `Agents`). The frontend reads this once to
decide whether to show Pro-gated UI, without guessing from a feature flag of its own.

## Missing key vs. invalid key -- these are deliberately different outcomes

`edition.Load()` reads both environment variables at process startup (`cmd/api` and `cmd/worker`
both call it, and both `log.Fatalf` if it returns an error -- an edition problem is a startup
problem, never a runtime one that could silently change mid-process):

- **Both variables empty.** Not an error. The instance is Community: every capability `false`,
  unlimited seats. This is the ordinary self-hosted case, and it has to boot cleanly.
- **A key is configured but fails to verify** -- malformed, wrong public key, bad signature, or
  expired past the grace period below -- **`Load()` returns an error and the process refuses to
  start.** It does not fall back to Community.

That second behavior is intentional, not an oversight, and follows law §2.9 directly:
*"ainda não" tem de ser distinguível de "nunca"* -- "not yet" has to stay distinguishable from
"never." A customer who paid for Pro and has a key configured is in a different state than a
customer who never configured one at all. Silently downgrading the first case to Community would
look, from the operator's side, exactly like the second: the screen would say "Community" whether
the license was never set up or was set up and is broken, and there would be nothing in the
running instance to tell those apart. Refusing to boot, with the specific verification error in
the log (`edition: EDITION_LICENSE_KEY: ...`), is what keeps a broken license configuration from
being mistaken for the simple, correct absence of one.

## The 14-day grace period

An **expired** key still verifies successfully for 14 days past its `expires_at`
(`gracePeriod` in `edition.go`) before `Verify` starts returning an error. This is a narrower
case than "invalid key" above: the signature is genuinely valid, the license genuinely existed,
and it has simply lapsed -- most plausibly because a renewal is late, not because anything is
wrong. The grace period lets Pro capabilities keep working through that gap instead of an
instance's paid features disappearing the moment a renewal is a day late. Past the 14 days, an
expired key behaves like any other invalid key: `Load()` errors and the process won't start,
for the same reason spelled out above -- a customer whose renewal never happened is not the same
state as one who never had a license, and the instance shouldn't quietly relabel itself
Community and let the difference go unnoticed.

## What Pro actually gates today

Three capabilities, each with a real `-tags pro`/`Capabilities`-field gate and a matching
Community build that refuses unconditionally: **organization AI agents** (create/update/delete/
toggle, `backend/ee/agents_pro.go` vs. `agents_community.go`, gated on `Capabilities.Agents`),
**custom branding** (color/logo, `branding_pro.go` vs. `branding_community.go`, gated on
`Capabilities.Branding`), and **organization security policy** (mandatory MFA,
`security_policy_pro.go` vs. `security_policy_community.go`, gated on `Capabilities.OrgPolicies`).
`backend/ee`'s `Register` function stays a documented no-op in both builds -- these three don't
route through it. See [`backend/ee/README.md`](../../backend/ee/README.md) for the actual
mechanism (a small `handle<Feature>Gate` method on `*Server` checks `s.edition.<Field>` before
calling into the `ee`-exported function; the Community build of that function refuses regardless
of what any gate above it decided) and the same pattern to follow for the next feature that moves
here. `frontend/src/ee` is the equivalent seam on the frontend side, gated at render time by what
`GET /api/edition` reports.

Everything else in the table in [docs/editions.md](../editions.md) either reads data that stays
free either way (e.g. recording, gated differently -- see `meet_recording.go`) or is marked
`(planned)` there because the code for it does not exist yet, including audit log and bulk
migration. Do not describe a Pro feature as available in any other documentation unless you can
point at the code that actually gates it on the `Capabilities` struct.
