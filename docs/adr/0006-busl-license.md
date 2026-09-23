# 6. Business Source License 1.1 for the core, with dual licensing

## Status

Accepted.

## Context

The project needed a license for its public release that satisfies two goals the Open Source
Definition's own field-of-use clause (criterion 6) does not let coexist: publishing the code so
any company can install and run it internally, while legally prohibiting a third party from
reselling it as a competing hosted service. A license that restricts the field of use is not open
source by the OSI's own definition -- calling it that anyway would cost the project the
community's trust faster than anything else it could do, so the choice was framed from the start
as which honest, clearly-labeled alternative to pick, not how to describe a restricted license as
open source.

`docs/OPEN-SOURCE.md` §1.1 lays out every license considered, compared on three axes: whether it
carries the OSI seal, whether it stops a competing SaaS offering, and whether (and how) it
converts to a permissive license over time.

| License | OSI | Stops competing SaaS | Converts |
|---|---|---|---|
| FSL-1.1-ALv2 | no | yes ("Competing Use") | Apache 2.0 after 2 years, per version |
| **BUSL-1.1** | no | yes, via a custom *Additional Use Grant* | Chosen license after ≤4 years |
| ELv2 (Elastic License) | no | yes (managed service) | never |
| SSPL | no | indirectly | never |
| AGPL-3.0 | yes | not legally -- only a practical deterrent (must publish changes) | -- |
| Commons Clause / CC BY-NC | no | yes | never |

AGPL-3.0 was the nearest open-source-licensed option, but its network-copyleft clause is a
practical deterrent, not a legal one: a well-resourced competitor can comply with AGPL (publish
their modifications) and still resell the software as a service. FSL and ELv2 both solve the
resale problem, but FSL's conversion window (2 years) was judged too short for a young product
still establishing itself, and ELv2 never converts at all -- both weighed against the
"nothing here is ever permanently locked away" commitment the project wanted to make publicly.
SSPL and Commons Clause were excluded outright: SSPL is rejected by the OSI and by Debian/Fedora,
and Commons Clause's added restriction has a track record of confusing users about what they can
actually do with the code.

## Decision

License the core under **BUSL-1.1** (`LICENSE.md`), with an *Additional Use Grant* that permits
production use -- including internal business use and paid professional services around it -- for
an organization and its affiliates, and prohibits only a **Competing Offering**: making the
software (or a substantial part of its functionality) available to third parties as a hosted,
managed, or embedded service. The definition is written around *how the software is made
available*, deliberately not around "competes with what the vendor sells" -- the latter phrasing
is what cost HashiCorp's BSL change its community's trust and produced the OpenTofu fork; a
grant that doesn't depend on knowing what the licensor currently sells is harder to argue about
and doesn't shift meaning as the product line changes.

Every released version gets its own four-year conversion clock (`Change Date`) to Apache License
2.0, set from that version's first public release -- the maximum period BUSL permits, chosen over
a shorter window to give the project more runway before older versions become freely
redistributable by anyone, including a competitor. `ee/` (Pro-only code) carries a separate,
proprietary Enterprise license instead, usable only with a valid Pro license key -- see
[docs/pro/README.md](../pro/README.md).

**Dual licensing.** BUSL's standard text ends with a line reserving alternative licensing
arrangements; this project fills that in with `https://beskos.com/licensing`. A party that wants
to do exactly what the Additional Use Grant prohibits -- resell the software as a hosted service,
white-label it, embed it in another product -- isn't simply blocked: they can buy a **Beskos
Commercial License** instead, priced by revenue share or by active seat depending on the
arrangement (`docs/OPEN-SOURCE.md` §1.2.1). This is the intended use of BUSL's design, not a
workaround: three licenses cover the same code (BUSL for everyone, the Enterprise license for
Pro features, the Commercial license for competing offerings), and a Contributor License
Agreement is required from external contributors specifically so their contributions can be
included in what gets commercially licensed.

## Consequences

- The project must say "source-available, converts to Apache 2.0 after four years" and never
  "open source" -- in the README, in any announcement, anywhere the license is characterized.
  Misstating this is treated as a direct risk to community trust, not a marketing nuance.
- `LICENSE.md`'s `Change Date` has to be updated on every release from the release pipeline, not
  by hand -- a version that ships without an updated date silently inherits the previous version's
  clock, converting it earlier than intended. This is a known follow-up item, not yet automated.
- After four years, any given version becomes Apache 2.0 and forkable by a competitor with no
  further restriction -- accepted as the cost of the conversion promise; the project's trademark
  and pace of development are the intended defenses at that point, not the license.
- A CLA is mandatory infrastructure, not a formality: without assignable rights to every
  contribution, the project cannot honor the commercial-licensing side of the dual-license model
  for code it doesn't fully own.
- Registering the "Beskos" trademark becomes load-bearing: the license alone doesn't stop someone
  from operating a hosted "Beskos" without infringing the Additional Use Grant's wording in some
  edge case -- the trademark is what stops them from calling it Beskos while doing so.

## Alternatives considered

See the comparison table above; AGPL-3.0 (path B in `docs/OPEN-SOURCE.md` §0) was the most
seriously considered alternative, rejected specifically because its deterrent against a competing
SaaS offering is reputational and legal-cost-based rather than an actual prohibition, which did
not match the goal of legally preventing that specific outcome.
