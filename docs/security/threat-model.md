# Threat model

A first pass, scoped to the boundaries that matter most for a multi-tenant product built around
a mail engine it does not control the source of. Not exhaustive; extend it when a new trust
boundary is added, and revisit it before F9 (docs/OPEN-SOURCE.md).

## Trust boundaries

```
                        ┌─────────────────────────────────────────────┐
                        │                 Internet                     │
                        └───────────────────────┬───────────────────────┘
                                                 │ HTTPS only
                    ┌────────────────────────────┴────────────────────────────┐
                    │           reverse proxy (same-origin, required)          │
                    └───┬─────────────┬─────────────┬─────────────┬───────────┘
                        │             │             │             │
                  ┌─────▼─────┐ ┌─────▼─────┐ ┌─────▼──────┐ ┌────▼─────┐
                  │  Go API   │ │  Stalwart │ │ OnlyOffice │ │ LiveKit  │
                  │(this repo)│ │  (JMAP)   │ │  Document  │ │ (SFU +   │
                  │           │ │           │ │  Server    │ │ Egress)  │
                  └─────┬─────┘ └─────┬─────┘ └─────┬──────┘ └────┬─────┘
                        │             │             │             │
                 ┌──────▼──────┐      │       ┌─────▼──────┐      │
                 │  Postgres   │      │       │ blobgw (S3 │◄─────┘
                 │ (shared,    │      │       │  signing   │
                 │ tenant_id)  │      │       │  proxy)    │
                 └─────────────┘      │       └─────┬──────┘
                                       │             │
                                 ┌─────▼─────┐ ┌─────▼──────┐
                                 │ mail data │ │   object   │
                                 │(Stalwart) │ │  storage   │
                                 └───────────┘ └────────────┘
```

## 1. Tenant isolation (Postgres, shared schema)

**Boundary:** every organization's data lives in the same tables, distinguished only by
`tenant_id`. **Attacker:** an authenticated user of tenant A, trying to read or write tenant B's
data. **Control:** every handler resolves `tenant_id` from the authenticated session, never from
a request parameter, and every sqlc query touching customer data filters by it (law 5.1).
**Verification:** an internal heuristic script (report-only, not included in this repository --
flags a query against a tenant-scoped table that never mentions `tenant_id`, including via
ownership columns like `owner_user_id` it can't recognize as equivalent; every hit needs a human
to actually read the query). **Residual risk:** a new query that forgets the filter ships silently until the
checker or a manual review catches it -- there is no runtime enforcement (e.g. Postgres row-level
security) backing this up. Worth considering once the schema stabilizes.

## 2. JMAP credential brokering (webmail ↔ Stalwart)

**Boundary:** the browser never holds a Stalwart account credential. **Attacker:** a user trying
to read another user's mailbox, or exfiltrate the shared Stalwart admin credential.
**Control:** the webmail authenticates to *this* API with a session cookie; the API looks up the
user's own encrypted JMAP credential (`internal/secretbox`, AES-GCM) and injects it server-to-
server when proxying to Stalwart. The admin credential (`STALWART_ADMIN_SECRET`) is read only by
the Go processes and is never returned in any API response. **Verification:** `grep -rn
StalwartAdminPwd backend/internal/httpapi` should only ever show it flowing into the
`stalwart.Client` constructor, never into a response body -- worth scripting the same way as the
tenant-scoping check if this file grows large enough to drift. **Residual risk:** the encrypted
credential's key (`MFA_ENC_KEY`) is a single shared secret; its rotation story
(docs/OPEN-SOURCE.md notes this as an open architectural item) is not yet designed.

## 3. blobgw (S3-compatible signing proxy)

**Boundary:** blobgw sits between OnlyOffice/LiveKit Egress and object storage, and is reachable
only on the internal network -- but "internal network" is an infrastructure assumption, not a
property of the service, so it verifies AWS SigV4 signatures anyway (`cmd/blobgw/sigv4.go`):
access key, a 15-minute clock-skew window against replay, and an HMAC over the canonical request.
**Attacker:** anything that reaches blobgw's port without holding a valid signing key -- an
internal network misconfiguration, a container that shouldn't have this route, a future service
added carelessly to the same network. **Residual risk:** correct by inspection of the signature
verification logic; not covered by an automated test that forges a bad signature and asserts a
rejection -- worth adding (see F5's fuzzing item in docs/OPEN-SOURCE.md).

## 4. OnlyOffice Document Server callback

**Boundary:** the Document Server calls back into the API (save the edited document, fetch it for
editing) over a JWT shared secret (`ONLYOFFICE_JWT_SECRET`), separate from the token that signs
the download/callback URLs themselves (`DOC_TOKEN_SECRET`). **Attacker:** something other than
our own configured Document Server instance, claiming to be delivering a saved document.
**Control:** the JWT is verified before any callback is trusted; production refuses to boot
without both secrets set to a real (non-default, ≥16-char) value (`config.validateProduction`).
**Residual risk (confirmed, 2026-09-16 P5 audit):** the callback handler (`handleDocCallback`,
`internal/httpapi/onlyoffice_handlers.go`) verifies the JWT's signature but discards its decoded
payload -- `cb.URL`/`cb.Status` are read straight from the callback's unauthenticated JSON body,
never cross-checked against the verified JWT's own claims. Since `jwtSecret` also signs the
editor **config** handed to any user's browser on every `/edit-session` call, any user who can
open a document they have at least view access to walks away with a syntactically valid
`jwtSecret`-signed JWT they fully control. That JWT replays as `cb.Token` on a hand-crafted
`POST /doc/callback/{id}` with a forged body, and `jwtVerify` accepts it -- the signature check
only proves "some token signed with our secret," not "this token's claims describe THIS
callback." **Blast radius today is bounded**, not open: `fetchURLAllowed` still restricts the
fetched `url` to the internal Document Server's own host (closes SSRF to arbitrary hosts), and
`docAuth`'s separate HMAC token still ties the request to the caller's own `FileID`/`TenantID`
(closes cross-tenant/cross-file overwrite) -- so this cannot escalate into external SSRF or a
cross-tenant write by itself. The remaining exposure is narrower: fetching an arbitrary *path* on
the Document Server's own host and storing it as a new version of a file the caller already has
write access to, which would only matter if the DS serves other sessions' cached content at
guessable unauthenticated paths (not confirmed either way -- depends on the deployed DS version's
internals, not this codebase). **Fix path, deliberately not applied blind:** bind the callback to
the verified JWT payload instead of the outer body (either read `url`/`status`/`key` exclusively
from the JWT's own claims, or assert equality between the two and reject on mismatch). Doing this
requires knowing the exact claim shape the deployed Document Server actually sends
(`JwtInBody`-dependent: some configurations sign the whole callback body as the JWT's claims,
others nest it under a `payload` key) -- verify against a real callback from the running
self-hosted Document Server before changing the parsing, since guessing the shape wrong would
silently break legitimate document saving in production. Tracked for before F9.

## 5. Webhooks (Stripe, SES/SNS)

**Boundary:** external services push events into the API without an interactive session.
**Control:** Stripe's webhook signature is verified (`internal/billing/stripe.go`) and events are
deduplicated by `event_id` (idempotency, law 5.3); SES/SNS events go through
`internal/ses.SNSVerifier`. **Residual risk: closed (2026-09-16).** `FuzzVerifyStripeSignature`
(`internal/billing/stripe_test.go`) and `FuzzSNSVerify`/`FuzzValidCertURL`
(`internal/ses/sns_fuzz_test.go`) now fuzz both signature parsers directly against adversarial
input -- untrusted bytes, malformed headers, forged/tampered signatures -- and assert the parser
never panics and never reports a forged/unfetchable message as valid. Neither fuzz run found a
crash in 30s/millions of executions.

## 6. Encrypted file storage

**Boundary:** uploaded files are encrypted client-side (Go process, before the object leaves for
S3/MinIO) with AES-256-GCM in blocks; the object's key travels inside the object itself.
**Attacker:** anyone with read access to the object storage bucket directly (a misconfigured
bucket policy, a compromised storage credential) but not to the application or its database.
**Residual risk:** the encryption key material's own rotation and the blast radius of a leaked
`FILE_ENC_KEYS` value are not documented anywhere yet -- see
[docs/adr/0004-aes-gcm-block-file-encryption.md](../adr/0004-aes-gcm-block-file-encryption.md)
for the scheme and close this gap there, not here.

## 7. Rate limiting and abuse

**Present today (this section was stale before the 2026-09-16 P5 audit -- corrected):** login is
rate-limited per IP (`loginIP`, 30/5min) and per account (`loginUser`, 8/5min) before spending
bcrypt time or a round trip to Stalwart, with a fixed dummy-bcrypt comparison equalizing timing
between existing and nonexistent accounts (`auth_handlers.go`, audit finding H5). Signup and
password recovery (`/auth/forgot`, `/auth/reset`) share the same limiters. MFA verification is
capped at 5 attempts per challenge (`mfaMaxAttempts`) *and* 20 checks/5min per IP (`mfaIP`) --
the second limiter specifically closes the loophole of requesting a fresh challenge every 5
guesses to reset the per-challenge counter. SMS MFA *enrollment* (`/mfa/sms/start`) has its own
per-IP and per-account hourly limiters (`smsIP`/`smsUser`, added 2026-09-16 P5 audit) after the
audit found it was reachable by any authenticated account with an arbitrary phone number and no
throttle at all -- an SMS-bombing/cost-abuse vector, not an account-takeover one, but real.

## 8. Sessions and MFA policy enforcement

**Boundary:** the webmail session token (`auth.NewSessionToken`, 256-bit `crypto/rand`,
base64 URL-safe) authorizes every authenticated API call, via a `wm_session` cookie (web) or
`Authorization: Bearer` (native app). **Control:** login always mints a fresh token
(`issueSession`) -- no session-fixation path exists; password change and password reset both
revoke every other session for the user; the org turning on mandatory MFA revokes every session in
the tenant except the admin's own. TOTP is encrypted at rest (`secretbox`/AES-GCM,
`MFA_ENC_KEY` mandatory in production) with real anti-replay (a used time-step can never validate
again, enforced at the SQL layer too, not just in Go). Recovery codes are bcrypt-hashed and
single-use.

**Residual risk 1 (fixed, 2026-09-16 P5 audit):** `handleWebmailLogout` deleted only the
`wm_session` *cookie*'s row, never checking the `Authorization: Bearer` token the native app
actually authenticates with. A user logging out on the native app left their session valid in the
database for up to 7 days -- if that Bearer token had ever leaked (compromised device, a stolen
local-storage backup), "logging out" did nothing to it. Fixed by routing logout through the same
`sessionToken(r)` helper every other authenticated route already uses.

**Residual risk 2 (open, needs a product decision, not fixed):** an organization's "MFA required"
policy (`ee/security_policy_pro.go`) is enforced only as a UI hint (`mfaSetupRequired` in
`handleMe`) -- a user in such a tenant who has not yet enrolled TOTP/SMS still gets a fully valid
session from a plain password login (`handleLogin` only challenges a second factor when the
*user's own* `MfaTotpEnabled`/`MfaSmsEnabled` is set, never consulting the tenant's policy), and
that session works against every other endpoint exactly like a compliant one. This does not bypass
authentication -- the attacker still needs the victim's real password first, and a user who *has*
enrolled a second factor is correctly gated -- but it means "MFA required" does not actually
require MFA for anyone still mid-enrollment, which defeats the point of an admin turning the
policy on for a not-yet-compliant team. **Not fixed blind:** closing it means deciding what a
"must enroll before doing anything else" session looks like -- a restricted token that only
`/mfa/*` enrollment endpoints accept, an immediate hard lockout, or a grace period -- which is a
product/UX call (law 6.2), not a pure security patch. Tracked here until that decision is made.

## 9. License enforcement (Ed25519-signed Pro license)

**Boundary is different from every other section above: this is a piracy/business-honesty
boundary, not a data-confidentiality one.** The "attacker" here is a self-hosted operator of
*their own* deployment trying to unlock Pro capabilities without a valid license, not a party
trying to reach another tenant's or another customer's data. Read this section with that in mind
-- a finding here does not mean any customer's data or any other deployment is at risk.

**Control:** `internal/edition.Verify` checks a real Ed25519 signature over the license payload
(seats, expiry, capabilities); a build without `-tags pro` has no code path that can execute any
Enterprise logic regardless of what the license says -- confirmed for all three currently gated
capabilities (agents, branding, org security policy; see [docs/pro/README.md](../pro/README.md))
by checking each has a `_community.go` counterpart under `//go:build !pro` that refuses
unconditionally. `s.edition` is populated once from `Load()` at process boot and never
reassigned; nothing in `httpapi` accepts a client-supplied capability flag, and `GET /api/edition`
is read-only. Missing vs. invalid license is handled correctly (law 2.9): both env vars empty
boots as Community with no error; a configured-but-invalid or past-grace-period key refuses to
boot at all, rather than silently downgrading.

**Residual risk 1 (by design, not a bug):** `EDITION_PUBLIC_KEY` is read from the environment at
boot, not compiled into the binary. An operator with control of their own deployment's environment
could sign their own license with their own keypair and unlock Pro features without paying. This
is not a security bypass in the sense the rest of this document is concerned with: whoever
controls that environment already has root-equivalent access to that one deployment (database
credentials, `MFA_ENC_KEY`, everything), and the actual signing key -- the only secret this scheme
depends on -- never leaves the private issuer repository. Compiling the public key into the binary
instead would raise the bar for a self-hosted operator's own binary, but closes no cross-tenant or
cross-customer exposure, so it's a business decision, not a security fix.

**Residual risk 2 (open, low severity):** the license's `Seats` field is parsed but never compared
against a tenant's actual user count anywhere in the codebase -- a self-hosted operator can add
unlimited users on a seat-limited license and nothing will notice. Contract-compliance gap, not an
access-control one.

**Residual risk 3 (open, deliberate tradeoff):** expiry is checked once at boot; a long-running
process keeps granting Pro capabilities until its next restart even if the license is revoked or
expires mid-process (beyond the 14-day grace period). Consistent with the documented "no
phone-home, edition is a startup problem" design in `docs/pro/README.md`, but means there is no
same-day revocation channel for a canceled subscription.

## Out of scope for this document

Stalwart's and OnlyOffice's and LiveKit's own internal security are their upstream projects'
responsibility -- this document covers how *this* codebase talks to them, not their internals.
A vulnerability in how this project configures or calls one of them is in scope; report it per
[SECURITY.md](../../SECURITY.md).

## Open items (tracked, not yet done)

- Automated tests that forge a bad signature/JWT/webhook and assert rejection: done for the
  Stripe/SNS webhooks (fuzz tests, 2026-09-16); still open for blobgw and the OnlyOffice callback.
- ~~Global rate limiting on login, signup, and password recovery~~ -- done (see §7).
- A decision on how "MFA required" is enforced for a not-yet-enrolled user, not just hinted at in
  the UI (§8, residual risk 2).
- Binding the OnlyOffice callback's verified JWT payload to the callback body it authenticates,
  once the deployed Document Server's exact JWT-in-body claim shape is confirmed (§4).
- A designed rotation story for `MFA_ENC_KEY` and `FILE_ENC_KEYS`.
- Row-level security in Postgres as a second line of defense behind the query-level `tenant_id`
  filter, if the manual/heuristic checking described in §1 proves insufficient at scale.
- License `Seats` enforcement against actual user count, and a same-day revocation channel for a
  canceled subscription against an already-running process (§9, residual risks 2 and 3) -- both
  license-compliance/business-continuity gaps, not access-control ones.
