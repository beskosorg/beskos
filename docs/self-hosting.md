# Self-hosting in production

This assumes you've already gone through [getting-started.md](getting-started.md) once, locally.

## Account access and usage

The public edition uses the `self_hosted` plan. It has no commercial limit on users, storage,
or daily sends, and access does not expire after a hosted trial. Infrastructure capacity and
configured service limits still apply. Usage statistics remain available in the administration
overview. Hosted checkout, payment-provider configuration, and commercial signup are absent.

The public worker does not run hosted daily usage aggregation or delete recordings according
to a hosted subscription plan. When object storage is configured, its initialization must
succeed before the worker starts its storage jobs.

## One machine

[deploy/docker-compose.host.yml](../deploy/docker-compose.host.yml) is the whole product on a
single host, with no orchestrator: the base compose file plus the built web client, Caddy for TLS
and routing, and the backup service.

```
docker compose -f docker-compose.yml -f docker-compose.host.yml up -d
```

The hostnames live in [deploy/Caddyfile.host](../deploy/Caddyfile.host) and are the ones
beskos.com runs on, so change them there first; DNS for every name has to point at the machine
before the containers start, since that is how the certificates are issued. An installation that
has no public site of its own drops the `landing` service and points the root domain at `app`.

This is the deployment this project runs for itself, which is the reason to prefer it over a
description: what is written here is what is running.

## TLS and the reverse proxy

Stalwart sends no CORS headers, so the webmail and the JMAP client must be served from the same
origin as the API -- there is no way around a same-origin reverse proxy in front of everything.
[deploy/Caddyfile](../deploy/Caddyfile) is a working example: it terminates TLS, serves the built
frontend, and forwards `/api`, `/jmap`, `/auth`, and `/.well-known` to the Go API. Edit the
domain and the `control-plane` upstream to match your deployment, or adapt the same routes to
nginx or another proxy -- the important part is that `/jmap` and `/auth` reach the Go API, never
Stalwart directly, since the API is what brokers the end user's session into a Stalwart
credential.

The certificate for the SMTP and IMAP ports is a separate problem, and one worth solving before it
becomes urgent. Stalwart obtains its own over ACME, which needs port 80 or 443 -- and on a single
host the proxy has both, so Stalwart silently stops being able to renew. An expired certificate
there does not degrade to plaintext: a sender holding an MTA-STS policy at `enforce` refuses to
deliver at all. [deploy/mailcert/](../deploy/mailcert/) copies the proxy's certificate for the
mail hostname into Stalwart whenever the proxy's is newer, and says how many days are left every
time it finds nothing to do. Stalwart serves the new certificate after a restart.

## Set `APP_ENV=production`

With `APP_ENV=production`, the backend refuses to start if a secret is empty, shorter than 16
characters, or matches one of the known development defaults (`dev-oo-jwt-secret-change-me`,
`devkey`, and similar) -- so a forgotten secret fails loudly at boot instead of quietly shipping
an insecure instance. It also requires `COOKIE_SECURE=true` and `NOTIFY_SMTP_INSECURE=false`.
Generate real secrets for `STALWART_ADMIN_SECRET`, `MFA_ENC_KEY`, `ONLYOFFICE_JWT_SECRET`,
`DOC_TOKEN_SECRET`, and `LIVEKIT_API_SECRET` (32 random bytes, base64-encoded, is a reasonable
default: `openssl rand -base64 32`).

## Outbound mail delivery

Test that your provider actually lets outbound port 25 through -- most cloud VMs block it by
default, and mail from a domain with no matching reverse DNS or SPF/DKIM/DMARC records will be
rejected or filed as spam regardless. Publishing correct DNS for every domain you add is
Stalwart's job once Beskos hands it the records at `GET /api/domains/{id}/dns/check`; getting
your provider to route port 25 is yours.

## AI assistant

The assistant and the editor's AI panel are off until `AI_API_KEY` is set. They speak the
OpenAI-compatible chat API, so any endpoint that implements it works; `AI_PROVIDER`, `AI_BASE_URL`
and `AI_MODEL` select which one. The full list of variables, with examples, is in the README and in
[deploy/.env.example](../deploy/.env.example).

The choice of endpoint decides where the content of mail, documents and chat is processed when
somebody uses the assistant. A vendor's cloud endpoint processes it under that vendor's terms and
jurisdiction. A local runtime -- Ollama, vLLM, llama.cpp, LM Studio -- on the same machine or the
same network processes it without it leaving the installation.

The startup log states the outcome: `AI enabled (provider=…)`, `AI disabled (no AI_API_KEY)`, or
`AI MISCONFIGURED` followed by the variable that is missing. The last one is the one to look for
after a change; the rest of the product starts normally with the assistant off.

## Backups

[deploy/backup/](../deploy/backup/) has scripts for a full backup and restore of Postgres, the
Stalwart data volume, and object storage together -- back these up as a set, not independently,
since a mailbox's metadata (Postgres), its actual mail (Stalwart), and its files (object storage)
have to stay consistent with each other. `deploy/backup/test-restore.sh` exercises a full
restore into a scratch environment; run it at least once before trusting a backup in an
emergency.

## Scaling

The `api` and `worker` binaries are stateless and can run more than one replica behind the
reverse proxy; `worker` additionally needs to be the only writer for any given background job
(it currently assumes a single instance for things like the provisioning state machine's polling
loop -- running more than one worker replica needs care that hasn't been built yet). Postgres and
Stalwart are the parts to size and back up carefully; object storage can be any S3-compatible
service once you outgrow a single MinIO container.

## Updating

Pull the new image or rebuild from a newer tag, then restart `api` -- it applies pending database
migrations itself on startup, in order, and idempotently. Read the release notes for the version
you're jumping to: a migration is never rolled back automatically, so going back to an older
version after upgrading needs a restore from backup, not a downgrade.
