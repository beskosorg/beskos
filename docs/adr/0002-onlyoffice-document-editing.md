# 2. In-browser Office editing via OnlyOffice Document Server

## Status

Accepted.

## Context

The Drive needs to let a user open and edit a `.docx`/`.xlsx`/`.pptx` file in the browser, the
way the SaaS products this project replaces (Google Workspace, Microsoft 365) do. Building an
office-document renderer and editor from scratch is a multi-year effort on its own and out of
scope for an email-first platform; the realistic choice is which existing document server to run
alongside the product.

OnlyOffice Document Server was chosen: it round-trips the actual OOXML formats without lossy
conversion, exposes an editor that embeds in an iframe, and its Community Edition is
self-hostable. It is licensed AGPL-3.0 (`docs/OPEN-SOURCE.md` §1.3), which matters because
Beskos already runs it, unmodified in principle but with two startup scripts applied at
container entry (`deploy/onlyoffice/onlyoffice-ai-init.sh` wires the AI proxy, an optional
branding patch removes "Powered by"); modifying an AGPL service and serving it over the network
triggers AGPL §13: the operator must offer the modified source to anyone using the service.

## Decision

Run OnlyOffice Document Server as its own container (`deploy/docker-compose.yml`,
service `onlyoffice`), reached over the network only -- the same rule the project applies to
Stalwart (docs/engineering-standards.md §5.4): never patch the vendored image's binary or embedded code, only its
startup configuration and an external init script.

`internal/config.Config` carries the wiring as environment-only values (docs/engineering-standards.md §2.6), with a
comment explaining why there are two URLs and two secrets, because it is easy to point one at the
other by mistake:

- `OnlyOfficeURL` -- what the **browser** loads (the editor's `api.js` and the editor iframe
  itself); reachable from outside the Docker network.
- `OnlyOfficeInternalURL` / `FilesHostURL` -- how the **Document Server** reaches this API back,
  for downloading the file to edit and posting the edited result to the save callback; these stay
  inside the Docker network and never need to be internet-reachable.
- `OnlyOfficeJWTSecret` -- shared with the Document Server so it can verify the editor config
  Beskos hands it is genuine (JWT-signed, per OnlyOffice's own integration contract).
- `DocTokenSecret` -- signs the download/callback URLs Beskos hands to the Document Server, so
  those URLs can't be replayed or guessed by an unrelated party.
- `OnlyOfficeSourceURL` -- the public address of this deployment's modified Document Server
  source, satisfying AGPL §13. It ships a default
  (`https://github.com/denerFernandes/beskos-docs-server`, pointing at the reference branding
  patch) and `config.validateProduction` refuses to boot in production when `OnlyOfficeURL` is
  set but `OnlyOfficeSourceURL` is empty -- the AGPL obligation is enforced at startup, not left
  as a step someone might forget in a deployment checklist.

## Consequences

- Office editing is optional at the infrastructure level: leaving `ONLYOFFICE_URL` unset disables
  the feature cleanly rather than half-booting it (`checkSecret` in `config.go` only requires the
  JWT and doc-token secrets when a Document Server URL is actually configured).
- Anyone who changes the branding patch or otherwise modifies the Document Server image inherits
  the AGPL §13 obligation personally -- the code comment on `OnlyOfficeSourceURL` says so
  explicitly, because the consequence of forgetting is a production boot failure at a moment that
  has nothing else to do with licensing.
- The editor runs in a cross-origin iframe (the Document Server has its own origin), so its AI
  assistant plugin can't rely on the app's session cookie; it authenticates over a separate
  Bearer-token proxy instead (`/ai/openai/v1/*`, `/ai/proxy` in `backend/internal/httpapi`).

## Alternatives considered

- **Collabora Online** -- also AGPL, LibreOffice-based. Not chosen; OnlyOffice's OOXML fidelity
  was judged better for files coming from Microsoft Office, which is the common case for a
  business migrating off Microsoft 365.
- **A commercial, closed-source document API** (e.g. a cloud conversion/editing service) --
  rejected outright: it would mean routing customer documents to a third party by default, which
  conflicts with the project's self-hosted, no-mandatory-external-dependency stance.
