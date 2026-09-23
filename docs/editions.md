# Community and Pro

## License

The core is [Business Source License 1.1](../LICENSE.md): free to run in production for your own
organization, including through an integrator you pay for setup and support, with no seat limit.
What it doesn't allow is offering it to third parties as a hosted or embedded service -- that
needs a commercial license (see [LICENSE.md](../LICENSE.md#what-this-means-in-practice) for a
plain-language table of what's allowed). Every released version converts to Apache License 2.0
four years after its release, so nothing here is ever permanently locked away. `ee/` directories
carry a separate [Enterprise license](../ee/LICENSE) instead, usable only with a valid Pro key.

## The three editions

- **Community** -- this repository, built and run by you, with no license key. Free, no seat
  limit.
- **Pro** -- the same self-hosted installation with a license key configured
  (`EDITION_LICENSE_KEY`, see [docs/pro/README.md](pro/README.md)). **€1 per user per month.**
- **Hosted** -- the service operated by Beskos, Lda. at [beskos.com](https://beskos.com): Pro
  features plus billing, plans, and operations that only make sense when someone else runs the
  servers. Its operator-side code lives in a private repository, not here.

## What differs

Everything not listed below is identical in all three editions. Nothing that affects basic
security is ever Pro or Hosted-only.

| | Community | Pro | Hosted | Enforced by |
|---|---|---|---|---|
| Email, calendar, contacts, Drive, chat, wiki, Kanban, office editing | yes | yes | yes | -- |
| Video meetings | yes | yes | yes | -- |
| Meeting recording (LiveKit Egress, offline compose) | no | yes | yes | license key, at runtime (`Recording`) |
| AI assistant with your own provider key | yes | yes | yes | -- |
| AI agents (organization-wide automations) | no | yes | yes | `backend/ee`, compile-time (`Agents`) |
| White-label branding: logo, colors, domain, no "Powered by" | no | yes | yes | `backend/ee`, compile-time (`Branding`) |
| Organization-wide security policies (mandatory MFA, session limits) | no | yes | yes | `backend/ee`, compile-time (`OrgPolicies`) |
| Audit log and retention policy UI *(planned)* | no | yes | yes | license key (`AuditLog`) |
| RBAC, groups, invitations, MFA (TOTP/SMS), audit logging | yes | yes | yes | -- |
| IMAP and file migration | yes | yes | yes | -- |
| Bulk-assisted migration across several domains *(planned)* | no | yes | yes | -- |
| Desktop app and folder sync; pt-BR and en | yes | yes | yes | -- |
| Seats | unlimited | per key (`Seats`) | per plan | license key / plan |
| Mailbox, storage, and daily-send quotas | none (`self_hosted` plan) | none | per plan | plan catalog |
| Self-service signup, checkout, billing profile and VAT, subscription portal | no | no | yes | `backend/ee`, compile-time; active only with a payment provider configured |
| Operator console, usage metering, outbound relay and sending reputation | no | no | yes | private repository |
| Support | community, GitHub issues | priority support, SLA | included per plan | -- |
| Updates and backups | yours | yours | operated by Beskos | -- |
| License | BUSL 1.1 | BUSL 1.1 + [Enterprise license](../ee/LICENSE) for `ee/` | terms of service | -- |

*(planned)* marks rows that are not implemented yet. The rule of thumb for the split: a feature
is Pro when a business pays for it without hesitation and a small team or an individual does not
miss it.

**How the boundary is enforced.** "Compile-time" rows live in `backend/ee/*_pro.go` behind the
`pro` build tag: a Community build does not link that code at all, and the routes do not exist.
Meeting recording predates that seam -- its code ships in every build and is gated at runtime by
the license key's `Recording` capability (`internal/edition`); see
[architecture.md](architecture.md#the-ee-seam). Hosted-only billing code also lives in
`backend/ee`, so it ships in source form, but it only becomes active with a payment provider
configured (`BILLING_PROVIDER=stripe`): without one, no checkout, signup, or billing route is
mounted and no trial or subscription state is ever evaluated -- a self-hosted installation, Pro
or Community, is never locked out for not paying. The published release binaries are built with
`-tags pro`: Community is that same binary with no key.

## Offering Beskos as a service

The Business Source License's [Additional Use Grant](../LICENSE.md) exists precisely to prevent
this without a separate agreement -- reselling it as a hosted or embedded service needs a
commercial license, priced by revenue share or by active seat depending on the arrangement. Get
in touch at <https://beskos.com/licensing>.
