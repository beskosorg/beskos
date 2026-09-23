# Contributing to Beskos

Thanks for considering it. This document covers how to get a development environment running,
what a change needs before it merges, and the house rules the codebase follows -- summarized in
English here; the canonical, longer version lives in [docs/engineering-standards.md](docs/engineering-standards.md).

## Before you start

- **One phase at a time.** For anything beyond a small fix, open an issue or a draft PR
  describing the change before writing a lot of code. A structural decision (a new dependency, a
  schema change, an authentication change) gets discussed there first.
- **Contributor License Agreement.** A CLA will be required on first contribution once the
  process is finalized and wired up as an automated check -- it is not active yet, so no bot
  comments on a PR today. Beskos ships under a dual-license model (see
  [docs/editions.md](docs/editions.md)): the CLA lets us offer commercial licenses that include
  your contribution, and lets us relicense the project if that ever becomes necessary (e.g. once
  a Business Source License version converts to Apache 2.0). Signing it will not transfer
  copyright; you keep it.

## Development setup

```bash
git clone https://github.com/beskosorg/beskos.git
cd beskos
cp deploy/.env.example deploy/.env   # fill in the values you need
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d stalwart postgres minio
cd backend && go run ./cmd/bootstrap   # first-time Stalwart setup
go run ./cmd/api &
go run ./cmd/worker &
# First login needs an organization + owner account -- there is no public sign-up page
# (that's hosted-only). Create one:
OWNER_ORG_NAME="Dev" OWNER_DOMAIN="dev.local" OWNER_LOGIN="admin" go run ./cmd/bootstrap-owner
cd ../frontend && npm install --legacy-peer-deps && npm run dev
```

See [docs/getting-started.md](docs/getting-started.md) for the full walkthrough, and
[docs/configuration.md](docs/configuration.md) for every environment variable.

## Before opening a PR

```bash
cd backend && gofmt -l . && go vet ./... && go build ./... && go test ./...
cd ../frontend && npm run build && npm test
cd ../sync-daemon && cargo fmt --all --check && cargo test --locked
```

CI runs the same checks, plus `govulncheck`, `npm audit`, `cargo audit`, gitleaks, and CodeQL.
A red CI check is not optional to fix before review. The house rules below (file size, English,
formal address) are reviewed by hand against internal tooling that isn't part of this repository
-- don't worry about matching them exactly on a first pass; a reviewer will point out anything
that needs adjusting.

## House rules

The full text is [docs/engineering-standards.md](docs/engineering-standards.md); the parts that matter for a first contribution:

- **Simple over clever.** No speculative abstraction -- introduce an interface or generics only
  once there are two real call sites.
- **Layers stay separate.** `internal/httpapi` is transport only; business logic and database
  access live in their own packages. A handler that starts building SQL or talking JMAP directly
  is a sign the logic belongs in a service.
- **Files stay small.** 600 lines is the hard ceiling, 300 is the target. Generated code
  (`internal/db/*`) is the only exception.
- **Errors are returned, never swallowed.** Wrap with `fmt.Errorf("context: %w", err)`. A
  background loop that retries on failure must count *consecutive* failures and escalate past a
  threshold -- a failure that repeats forever is not a blip, it is an outage nobody is watching.
  See [`watchdog.go`](backend/cmd/worker/watchdog.go).
- **Tests live next to the code.** Every package with logic has a `_test.go`. Integration tests
  that need a live container skip themselves when the environment isn't there, instead of
  failing CI.
- **Every tenant-scoped query filters by `tenant_id`**, taken from the authenticated session's
  context -- never from a request parameter.
- **User-facing text goes through i18n**, never hardcoded. `pt-BR` and `en` both ship every key.
  API errors return a stable `error_code`; the frontend translates it, never showing raw backend
  text.
- **Comments say why, not what.** If code needs a comment to explain what it does, that is
  usually a sign to rename something or extract a function instead. No comment should reference
  a date, a ticket, or an internal planning document -- that context belongs in the commit
  message and the PR description, not in code that outlives both.
- **New code is in English** -- identifiers, comments, log lines, error strings, commit
  messages. The only exception is text a user reads, which comes from the i18n files.

The language and file-size rules are enforced automatically on every changed line before a merge;
they do not check history, so existing text in another language is not something you need to fix
in an unrelated PR.

## Commit messages and PRs

Keep commits scoped to one change. Explain *why* in the body when the diff alone doesn't make it
obvious. Reference the issue a PR closes. A PR description that says what changed and why is
worth more than a long one that narrates the process of getting there.

## Reporting a bug

Open an issue with: what you expected, what happened, and how to reproduce it (a `docker compose
up` from a clean checkout is the easiest baseline to describe against). For a security
vulnerability, follow [SECURITY.md](SECURITY.md) instead -- do not open a public issue.

## License

By contributing, you agree that your contribution is licensed under the same terms as the file
or directory you changed (see [LICENSE.md](LICENSE.md) and [ee/LICENSE](ee/LICENSE)). Once the
Contributor License Agreement described above is active, signing it will be asked for on your
first contribution; until then there is nothing to sign.
