# Functionality

A high-level map of what's in Beskos, by area. This intentionally doesn't carry a
per-item ✅/🚧/📋 status table -- that kind of table drifts out of sync with the code between
updates, and a stale one is worse than none. For what a specific feature actually does, read the
code it's named after, or open an issue asking.

## Email

Full IMAP/SMTP/JMAP through Stalwart: per-domain provisioning with automatic DKIM, a DNS records
screen the customer publishes, and automatic verification once they have. A modern webmail --
folders, read/unread, full-text search, compose with rich text, attachments -- talking JMAP
directly to Stalwart through a same-origin proxy, never through the Go API.

## Calendar and meetings

Month/week/day views, shared calendars, conflict warnings when double-booking, and video calls
through a self-hosted LiveKit: scheduling, RSVPs, and (with a Pro license) recording via LiveKit
Egress with an offline compose step.

## Files (Drive)

Upload and download over S3-compatible object storage, content-addressed deduplication, sharing
at owner/editor/reader levels or by link, and in-browser editing of Word/Excel/PowerPoint
documents through OnlyOffice Document Server (with AI wired into its assistant panel).

## Chat and Kanban

Real-time channels (Postgres + Redis/Valkey pub-sub) and a lightweight board for turning an
email or a message into a task.

## Wiki

A Confluence-style knowledge base: access by group or by person, and semantic search over its
content when an embeddings provider is configured.

## AI assistant

An agentic engine that can summarize a thread, draft a reply in your tone, extract tasks, and act
on tools it has been granted access to -- always with the API key held server-side, never in the
browser. It speaks the OpenAI-compatible chat API, so it runs on any endpoint that implements it,
including a local runtime such as Ollama or vLLM when content should not leave the installation.

## Security

Role-based access control with per-member permission overrides, invitations with anti-escalation
checks, MFA via TOTP or SMS with one-time recovery codes, and an audit log on sensitive
operations. Every query for tenant data filters by the authenticated session's tenant --
[docs/architecture.md](architecture.md) has the detail.

## Desktop and sync

A native desktop and mobile app (Tauri) with two-way folder synchronization to the Drive, built
around a standalone Rust sync engine (`sync-daemon/`).

## Internationalization

Every user-facing string ships in Portuguese (Brazil) and English; adding a language means adding
a locale file and translating its keys.

## Community vs. Pro

Meeting recording, white-label branding, an audit-log UI, organization-wide security policies,
and AI agents are Pro features -- see [docs/editions.md](editions.md) for the full split and why
those specific features.
