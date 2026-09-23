# Beskos project laws

**Mandatory** rules for all code in this repository. They apply to every contributor.
They complement — not replace — the non-negotiable constraints in [project.md](../project.md) (§2)
and the [Stalwart divergences](../deploy/DIVERGENCES.md).

## 1. Architecture and simplicity

1.1. **Write simple code.** Prefer the obvious solution over the clever one. No speculative
     abstraction — introduce an interface/generics only once there are ≥2 real call sites. YAGNI.
1.2. **Separate concerns by responsibility.** Each package/module has one purpose. Don't mix
     HTTP, business logic, and data access in the same file/function.
1.3. **Backend layers:** `httpapi` (transport) → service/logic → `db` (sqlc) / `stalwart` (JMAP).
     Handlers don't build SQL or speak JMAP directly when there's real logic; extract a service.
1.4. **Small files.** **600 lines** maximum per file (aim for ≤300). Past that, split by
     responsibility. The one exception: **generated** code (`internal/db/*`).
1.5. **Short, clearly named functions.** One function does one thing. Avoid going past ~60 lines.

## 2. Go standards

2.1. Follow **Effective Go** and **Go Code Review Comments**. Clean `gofmt` and `go vet` are a
     merge condition. Nothing lands with dirty formatting.
2.2. **Errors:** return them, don't panic (except in `main`/impossible initialization). Enrich
     with `fmt.Errorf("context: %w", err)`. Handle or propagate — never swallow silently.
2.3. **`context.Context`** is the first parameter in every function that does I/O.
2.4. Idiomatic names: no stutter (`stalwart.Client`, not `stalwart.StalwartClient`); acronyms in
     uppercase (`ID`, `URL`, `HTTP`, `JMAP`, `DNS`).
2.5. **No unnecessary dependencies.** Prefer the stdlib. Every new library needs a justification.
2.6. Config and secrets **only via the environment** (`internal/config`). A secret in code is a bug.
2.7. **Tests live with the code** (brief §11). Every package with logic has a `_test.go`.
     Integration tests skip themselves without the required env vars (they don't break CI
     without a container).
2.8. **A loop that swallows an error has to ESCALATE.** A periodic worker doing
     `log.Printf(...); continue` is correct for a transient failure — and blind to a permanent
     one. Count **consecutive** failures and shout past a threshold (`consecutive >= N`), and
     also announce recovery. See [`watchdog.go`](../backend/cmd/worker/watchdog.go) as the reference.

     > Written on 2026-07-31 because the reputation sentinel failed **100% of its passes** for
     > days without anyone knowing: SES was returning `HTTP 400` every hour, the `continue`
     > moved on to the next domain, and the line got lost in the middle of the log. This is the
     > defense that stops a customer from burning the whole account's reputation — it was off
     > and looked on. An error that repeats forever isn't an error: it's an outage.

2.9. **"Not yet" has to be distinguishable from "never."** A step that waits on something
     external (DNS published, payment, file uploaded) returns the same "not yet" whether the
     wait is legitimate or the condition will never be met. If it's waiting for a target — a
     record, an identifier, a path — **log which one** the first time, and **shout** once the
     wait crosses a threshold. Without that, the screen says "waiting on the customer" and the
     customer already did their part.

     > Written on 2026-08-01 after a domain sat over an hour in `dns_pending` with every record
     > published and visible to the whole world. The checker was looking for a DKIM record whose
     > content an earlier bug had wiped, and a lookup that finds nothing looks, right there,
     > exactly like a customer who hasn't published yet. Nothing in the logs, nothing on screen:
     > just the customer staring at "waiting on DNS" and concluding the product doesn't work.

## 3. Frontend standards (React/TS)

3.1. TypeScript **strict**. No `any` without a comment justifying it.
3.2. Small, single-responsibility components (the same 600/300-line law). One component per
     file; a page goes in `pages/`, a reusable one in `components/`.
3.3. Server state via **TanStack Query**; don't reinvent caching/fetching.
3.4. Every call to our API goes through `src/api.ts`. Components don't do raw `fetch`.
3.5. **No Stalwart admin credential in the frontend** (project.md §2). The webmail speaks JMAP
     with the **end user's own token**; everything else speaks to our Go API.

3.6. **A new screen dresses like the others.** Every panel screen uses the same shell:
     `.pnl-top` (title `h1` + `.desc`) → `.pnl-content` → `.pnl-card` with `.pnl-card-h`
     (icon + `h3`) and `.pnl-card-b`. Don't invent layout classes: if you need a `pnl-*` class
     that doesn't exist, either use the one that does or define it in the CSS — never write it
     and move on. Checked by `scripts/check_laws.py` on new lines.

     And **a loading state doesn't replace the screen**. The title and description show up
     before the response arrives; only the body waits. A page that is just `…` looks no
     different from a broken one — you can't even tell where you are.

     > Written on 2026-08-03, after the import screen shipped with `pnl-section`, a class no one
     > had ever defined. It didn't fail or warn: React doesn't check whether a class exists. It
     > just rendered without the house shell — no title band, no content column, boxes with the
     > wrong spacing. Whoever wrote it was looking at the code, not the screen, and the product
     > ended up with a page that didn't look like the product. On the same screen, and from the
     > same oversight, the `retry: false` every other query has was missing too: the route
     > wasn't live in production yet, TanStack retried three times with exponential backoff, and
     > the user sat for thirty seconds looking at three dots.

## 4. Internationalization (i18n) — MANDATORY

4.1. **No user-visible string is hardcoded.** All UI text comes from the i18n system.
4.2. The frontend uses **`i18next`/`react-i18next`**. Keys in `src/i18n/locales/<lang>/*.json`.
     Default language **`pt-BR`**; **`en`** mandatory. Every key exists in every language.
4.3. Use semantic keys (`login.submit`, not `"Sign in"`). Plurals/interpolation via i18next.
4.4. **API error messages** come back with a **key** (`error_code`) alongside the text, for the
     frontend to translate. The backend never assumes the user's language in displayed text.
4.5. Dates/numbers/currency via `Intl`, matching the locale — never formatted by hand.
4.6. **Language of the code AND its documentation: EVERYTHING in English.** Names (variables,
     functions, types, fields, files), **comments**, database columns, JSON keys, CLI flags,
     logs, error messages, and doc prose (`.md` files, including this one). Applies to backend
     and frontend alike. The **only** exception is text the end user reads, which comes from
     i18n — and wherever it has to sit in code (e.g. SMS), mark the line with `lang:ok`.
     `error_code` is the stable contract; the **frontend translates** the code via i18n
     (`server.<code>`) and shows the user their own language. Never show raw backend error text
     in the UI.

     > Comments were in Portuguese until 2026-07-31, and documentation prose until 2026-09-15.
     > Existing text gets migrated over time; anything written from now on is in English. The
     > checker only looks at NEW lines, so the history doesn't get in the way.

4.7. **The law is machine-checked, not a matter of goodwill.** `scripts/check_laws.py` runs
     automatically on every file write (an editor hook) and validates laws 4.6
     and 1.4 on new lines — in code and in documentation prose alike, though 1.4's line ceiling
     only applies to source files, never to docs. It exists because these laws were already
     written down and got broken anyway — a document obligates no one; a command that fails does.
     This script, and the rest of `scripts/check_*.py`, are internal review tooling and are not
     part of this repository's public copy (see `scripts/export_public.py`'s `EXCLUDE`); a
     contributor doesn't need to run them, a reviewer checks by hand instead.

4.8. **All text the user reads is formal and corporate.** Address the customer with their
     language's formal register — `usted`/`su` (es), `Sie`/`Ihr` (de), `vous`/`votre` (fr),
     `lei`/`suo` (it), `u`/`uw` (nl), `Państwa` (pl), `Vy`/`Váš` (cs), third person (pt), -- lang:ok
     `您` (zh), です・ます (ja), formal Modern Standard Arabic (ar). **Never** the informal -- lang:ok
     register: `tu`, `tú`, `du`, `je`/`jij`, `tuo`, `Twój`, `tvůj`, `你` -- foreign pronouns quoted as data, lang:ok
     **One exception, by convention: Swedish and Norwegian address the reader as `du`.** That is
     the register of software in both languages; the old polite pronouns (`Ni`, `De`) read there as
     archaic rather than courteous. Prefer impersonal phrasing, and use `du` where a pronoun is
     needed. Decided 2026-09-23.
     No exclamation marks, emoji, slang, or jokes; declarative sentences. Applies to the landing
     page, the documentation, the UI, and
     transactional messages (email, SMS) — **every surface**, because the customer doesn't know
     they're different projects. Checked by `scripts/check_register.py` (internal tooling, §4.7).

     **Formal address isn't enough — tone is law too.** Write declaratively: say what the system
     does. Don't justify the design decision to the reader, don't editorialize, don't ask
     rhetorical questions. "Sending is suspended until there is a subscription" — not "sending
     stops, because a test that hides what it was evaluating answers the wrong question." This
     is **not** machine-checked: the pronouns are, the tone is a review item.

     > Written on 2026-08-03, after the register had already drifted between surfaces without
     > anyone noticing: the Polish documentation addressed the customer formally while the
     > Polish landing page used the informal pronoun; the Italian mail checker was entirely
     > informal while the rest of the Italian copy stayed formal. Someone who opens both pages
     > in the same session
     > doesn't see two style choices — they see two companies. And the mail checker is the
     > site's front door: it was exactly the page where informality cost the most.
     >
     > The tone paragraph was added the same day, a few hours later. The checker had just
     > declared everything clean, and the entire documentation set — seventeen articles across
     > eight languages — was still written in essay register, justifying every decision to the
     > reader. The pronouns were correct. A machine that only knows how to measure pronouns signs
     > off on a text no enterprise customer would read to the end, and the "clean" bill it prints
     > ends up worth less than it looks.

## 5. Security and multi-tenancy (reinforcing project.md)

5.1. **Every customer-data query filters by `tenant_id`.** Always use the authenticated user's
     tenant (from context), never one that came from the request.
5.2. Check **resource ownership** before any admin operation on Stalwart.
5.3. **Idempotency** in provisioning and webhooks — assume every network call can repeat.
5.4. Never modify Stalwart; only over the network (JMAP). See [DIVERGENCES.md](../deploy/DIVERGENCES.md).

## 6. Process

6.1. **One phase at a time** (project.md §9). Don't start Phase N+1 before N is tested.
6.2. Structural decisions (libraries, schema, auth): propose trade-offs and **ask** first.
6.3. Document decisions and setup in the README as you go.
6.4. Divergences between the brief and Stalwart's reality → trust reality and **record it in
     DIVERGENCES.md**.
