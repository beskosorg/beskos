# Beskos

A self-hosted email, calendar, files, chat, and knowledge-base platform for a team that wants to
stop paying for five different SaaS subscriptions and run one thing instead. Email runs on
[Stalwart](https://stalw.art) underneath, used over the network only, never modified; everything
else -- webmail, Drive, wiki, Kanban, video meetings, an AI assistant -- is built around it.

## What's in the box

- **Email**: full IMAP/JMAP/SMTP via Stalwart, a modern webmail, per-domain provisioning, DKIM/SPF/DMARC handled for you.
- **Calendar & meetings**: month/week/day views, shared calendars, conflict warnings, and video calls (LiveKit) with scheduling and RSVPs.
- **Files**: a Drive with folders, sharing, and in-browser editing of Word/Excel/PowerPoint documents (OnlyOffice).
- **Chat & Kanban**: real-time channels and a lightweight board, in the same app as your email.
- **Wiki**: a Confluence-style knowledge base with access by group or person, and AI-powered search.
- **AI assistant**: summarize, draft, and act on your mail and documents. It runs on any
  OpenAI-compatible endpoint -- a vendor's API, or a local runtime such as Ollama or vLLM when
  content should stay on your own infrastructure ([configuration](docs/self-hosting.md#ai-assistant)).
- **Security**: RBAC with per-member permission overrides, invites, MFA (TOTP and SMS), audit-friendly logging.
- **A native app**: desktop and mobile builds (Tauri) with two-way folder sync to the Drive.

See [docs/FUNCTIONALITY.md](docs/FUNCTIONALITY.md) for the full, current list.

## Roadmap

- **Calendar sync via JMAP Calendars**, replacing today's own-layer calendar with real
  Stalwart sync, plus meeting invitations over iMIP/ICS.
- **Resumable uploads** for large files (multipart/tus), beyond today's single-request upload.
- **Global rate limiting** on login and API traffic, beyond the per-challenge limit MFA already has.
- **Observability**: structured logs, metrics, and alerting out of the box.
- **Sending reputation controls**: per-tenant abuse detection and an outbound IP pool.
- **Data protection tooling**: export/deletion workflows for regulations like the GDPR/LGPD.
- **Horizontal scaling**: routing tenants across multiple Stalwart instances.

Have a feature you'd like to see prioritized? Open an issue.

## Community and Pro

Beskos is source-available under the [Business Source License 1.1](LICENSE.md): free to run,
for your own organization, on your own infrastructure, with no seat limit. What it does not
allow is reselling it as a hosted service to third parties -- that needs a
[commercial license](LICENSE.md#what-this-means-in-practice).
Every BUSL-licensed version converts to Apache 2.0 four years after its release, so nothing here
is ever permanently locked away.

A handful of features that a business pays for without hesitation -- meeting recording,
white-label branding, organization-wide security policies, AI agents -- are Pro, at **€1 per
user per month**. Everything else, including every security feature, is Community. See
[docs/editions.md](docs/editions.md) for the full split, including what's planned but not shipped yet.

## Quickstart

```bash
git clone https://github.com/beskosorg/beskos.git
cd beskos
cp deploy/.env.example deploy/.env   # fill in the values you need
docker compose -f deploy/docker-compose.yml up -d
```

Then open the app at the address `PUBLIC_URL` points to, and follow
[docs/getting-started.md](docs/getting-started.md) to add your first domain. Production deployment,
backups, and scaling live in [docs/self-hosting.md](docs/self-hosting.md).

## Architecture, in one paragraph

A Go control plane (`backend/cmd/api`, `backend/cmd/worker`) talks to Stalwart over JMAP for
everything mail-related, and to its own Postgres for everything else -- domains, users, files,
chat, wiki, calendar. The React frontend (`frontend/`) never holds a Stalwart admin credential;
it authenticates as the end user and reaches Stalwart only through a same-origin proxy. See
[docs/architecture.md](docs/architecture.md) for the full picture, including why JMAP and not the
REST API the Stalwart docs describe (short version: the pinned Stalwart version doesn't ship
one -- see [docs/adr/0001-jmap-not-rest.md](docs/adr/0001-jmap-not-rest.md)).

## Contributing

Bug reports, features, and pull requests are welcome -- see [CONTRIBUTING.md](CONTRIBUTING.md)
for the development setup and the rules the codebase follows. Security issues go to
[SECURITY.md](SECURITY.md)'s address, not a public issue.

## License

[Business Source License 1.1](LICENSE.md) for the core, converting to Apache 2.0 four years
after each release. The `ee/` directories carry the [Beskos Enterprise License](ee/LICENSE)
instead. "Beskos" and its logo are trademarks -- see [TRADEMARK.md](TRADEMARK.md).
