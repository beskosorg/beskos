# 3. LiveKit as the video-call engine behind Meet

## Status

Accepted.

## Context

Meet, the product's video-calling feature, needs real-time audio/video routing between
participants (an SFU), room-token issuing, and -- for the Pro edition -- server-side recording
of a call. Building a WebRTC SFU from scratch is not a reasonable scope for this project; the
choice is which existing one to embed.

LiveKit was chosen: it is Apache 2.0 licensed (no copyleft obligation, unlike Stalwart and
OnlyOffice), self-hostable as a single Go binary plus a Redis-compatible coordinator, and ships
Egress, a companion service that records a room by compositing it with headless Chrome and
uploading the result to object storage -- exactly the shape needed for meeting recording without
writing a media pipeline.

## Decision

Run `livekit-server` and `livekit/egress` as their own containers
(`deploy/docker-compose.yml`), unmodified, reached only over the network -- the same
rule as every other embedded service in this project. `internal/config.Config` carries:

- `LiveKitURL` -- the WebSocket address the **browser** connects to for the call itself.
- `LiveKitHTTPURL` -- the Twirp API address the **Go backend** calls to manage rooms and trigger
  Egress recordings.
- `LiveKitAPIKey` / `LiveKitAPISecret` -- sign the room-join tokens the backend issues to
  participants. The backend never hands out LiveKit's own admin credentials; it mints a scoped
  JWT per participant per room, the same brokering pattern used for the Stalwart JMAP proxy
  (nobody in the browser ever holds infrastructure credentials, only a token scoped to what they
  are doing).

Egress needs a Redis-protocol store to coordinate with `livekit-server`; the project uses
**Valkey** there rather than Redis itself -- see `docs/OPEN-SOURCE.md` §1.3: Redis moved to the
RSALv2/SSPL license (and full AGPL from 8.x) which this project's dependency policy excludes,
and Valkey is a permissively-licensed, protocol-compatible fork with no code changes required on
either LiveKit's or this project's side.

Recording (Egress) is gated behind the Pro edition capability `Recording`
(`backend/internal/edition`) -- the call infrastructure itself (`LiveKitURL` et al.) is
Community; only turning a call into a stored recording is a paid feature. See
[docs/pro/README.md](../pro/README.md) for the current state of that gate, which is a capability
flag with nothing wired to it yet.

## Consequences

- Video calling has zero dependency on a third-party calling service (Zoom, Google Meet, Twilio
  Video); everything runs on infrastructure the operator controls, consistent with the project's
  self-hosted positioning.
- Recording specifically depends on Egress's headless-Chrome compositing, which needs `SYS_ADMIN`
  and a sizable `/dev/shm` -- a heavier operational footprint than the base call path, which is
  why the compose file keeps it as a separate, clearly commented service rather than folding it
  into `livekit-server`.
- LiveKit and Egress being Apache 2.0 means this project carries no source-offer obligation for
  them (unlike Stalwart and OnlyOffice), simplifying the license story for this one piece of the
  stack.

## Alternatives considered

- **mediasoup / Jitsi Meet** -- both viable self-hosted SFUs. LiveKit was preferred for its Go
  server (matching the rest of the backend's language) and Egress shipping as a first-party,
  already-integrated recording solution rather than something to build separately.
- **A hosted WebRTC platform (Twilio, Daily, Vonage)** -- rejected for the same reason as a
  hosted document-editing API: it would make a core feature depend on a third party by default.
