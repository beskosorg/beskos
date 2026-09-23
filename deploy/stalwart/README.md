# Stalwart configuration

This file **replaced a `config.toml`** that was never read by anything — not in production, not
in development. While it existed, it led me to plan a storage migration that had already been
done months earlier.

## What this file is — and what it isn't

In 0.16, `--config` defines **only where the data lives**. Everything else — listeners, TLS, ACME,
domains, DKIM, accounts, queues, outbound relay — moved into a **registry** stored inside that
storage, and is edited through the admin interface or the API.

In other words: **you don't tune Stalwart by editing here.** If you're looking for where to change
the outbound relay or a listener, it's in the registry, not in this file.

## Format

It was reconstructed by trial and error against the official image, because the schema isn't
documented. Two pitfalls, both confirmed against real startups:

- `@type` is **`PostgreSql`** (capitalized exactly like that). The accepted variants are `RocksDb`,
  `Sqlite`, `FoundationDb`, `PostgreSql`, `MySql`.
- The secret is a tagged enum. Here we use `EnvironmentVariable` + **`variableName`** — the names
  `name`, `value`, `key` and `secret` all produce *"Variable name cannot be empty"*. With the
  `Value` variant, the field is called **`secret`**, and writing `value` **doesn't error**: it
  produces an empty password and an obscure authentication failure on the Postgres side.

The `EnvironmentVariable` variant is the one we use on purpose — that way the secret doesn't live
in the file (law 2.6).

## Production

In production this file lives at `/opt/beskos/stalwart/config.json` on VPS-1, mounted into the
`stalwart` swarm service as `/etc/stalwart/config.json`. **Production doesn't run via
`docker compose`.**
