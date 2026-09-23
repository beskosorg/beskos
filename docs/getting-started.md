# Getting started

## Requirements

Docker and Docker Compose (v2); ports 25/143/465/587/993 free on the host if you want to
send and receive real mail -- most cloud providers block outbound 25 by default, so mail sending
from a fresh VM often needs that unblocked first. Go 1.25+ locally for the one-time Stalwart
bootstrap step below (everything else runs in containers). Node 20+ only if you want to run the
frontend outside Docker with `npm run dev` (see "First login").

## Run it

```bash
git clone https://github.com/beskosorg/beskos.git
cd beskos
cp deploy/.env.example deploy/.env
```

Edit `deploy/.env`: at minimum, set real values for `POSTGRES_PASSWORD`,
`STALWART_ADMIN_SECRET`, and `MINIO_ROOT_PASSWORD`. Everything else has a working default for a
local trial. See [docs/configuration.md](configuration.md) for what every variable does.

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
```

The `api` container applies pending database migrations itself on startup -- there is nothing to
run by hand for that. Give everything a minute to become healthy, then run the one-time Stalwart
bootstrap (a fresh Stalwart refuses every administrative call until this runs once):

```bash
cd backend
STALWART_URL=http://localhost:8080 STALWART_ADMIN_SECRET=<STALWART_ADMIN_SECRET> \
  STALWART_HOSTNAME=mail.example.org STALWART_DEFAULT_DOMAIN=example.org \
  go run ./cmd/bootstrap
```

`bootstrap` prints a generated Stalwart admin address and secret once -- save it; the running
server needs a restart to pick the bootstrap up:

```bash
docker compose -f deploy/docker-compose.yml restart stalwart
```

## First login

The public edition has no self-service sign-up page -- that is a hosted-only feature. Create the
first organization and owner mailbox with `bootstrap-owner`, once, on a fresh database (it
refuses to run again once any organization already exists). Postgres has no host port published,
so run it as a one-off container on the same Docker network instead of from the host, reusing the
`api` service's own environment (`DATABASE_URL`, `STALWART_URL`, and the rest):

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env run --rm \
  -e OWNER_ORG_NAME="Acme" -e OWNER_DOMAIN=example.org -e OWNER_LOGIN=admin -e OWNER_NAME="Jane Doe" \
  api bootstrap-owner
```

Leave `OWNER_PASSWORD` unset to have one generated and printed once.

This compose runs the backend only; it does not serve the web app. For a local trial, run the
frontend's dev server, which proxies `/api` and `/jmap` to the `api` container on `127.0.0.1:8000`:

```bash
cd frontend && npm install --legacy-peer-deps && npm run dev
```

Open <http://localhost:5173> and log in with that mailbox address and password. For a real
deployment, a reverse proxy serves the built frontend and the API on one origin instead -- see
[docs/self-hosting.md](self-hosting.md) for the Caddy configuration.

## What's next

- [docs/architecture.md](architecture.md) for how the pieces fit together.
- [docs/self-hosting.md](self-hosting.md) for TLS, backups, and running this in production.
- [docs/configuration.md](configuration.md) for every environment variable, including the
  optional ones (AI assistant, video meetings, office editing, SMS for MFA).

## Something not working?

Check the container logs first: `docker compose -f deploy/docker-compose.yml logs -f
api worker stalwart`. A domain stuck in `dns_pending` almost always means the DNS records
`GET /api/domains/{id}/dns/check` lists haven't propagated yet -- for local testing without real
DNS, set `PROVISION_SKIP_DNS_VERIFY=true`. For anything else, open an issue with what you ran and
what you expected instead.
