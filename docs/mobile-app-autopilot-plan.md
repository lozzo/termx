# termx Mobile App Autopilot Plan

Last updated: 2026-04-28

## Goal

Build a minimal mobile `termx` app and its supporting services around the existing flat terminal pool model:

- login or configure token
- device/server list
- terminal list
- connect to a terminal
- view and operate the terminal

Hard constraints:

- remote transport is WebRTC DataChannel only
- server-side model remains flat terminals, not tmux-style session/window/pane
- reuse existing `termx` protocol, attach, snapshot, resize, input, events semantics whenever possible
- maximize reuse from `tgent`, but delete complexity aggressively

## Current Survey

### termx today

- `termx` already has the right server core: terminal lifecycle, attach, snapshot, events, stream fan-out, resize, binary screen update path.
- `protocol/` already defines a stable multiplexed wire protocol with control-plane JSON plus binary stream frames.
- `transport/unix` already gives a `transport.Transport` abstraction we can proxy against.
- `cmd/termx` already owns CLI, config path resolution, daemon startup, and tests around config wiring.
- `tuiv2/shared/config_file.go` is the current config entry point and already has tests for file generation and parsing.

### tgent reusable source

- `tgent-go/internal/agent` and `tgent-go/internal/hub*` already contain working WebRTC, TURN, signaling, and agent registry code.
- `tgent-go/cmd/tgent-hub/main.go` shows the minimal runtime needed to boot hub HTTP, gRPC, and TURN together.
- `tgent-web/src/app/api/auth/*`, `api/agents/*`, `lib/auth.ts`, and `lib/queries.ts` contain a reusable login plus token plus agent-list control plane.
- `tgent-app` already has a working Capacitor plus React shell, WebRTC connect flow, reconnect handling, and xterm.js terminal component.

## Intentional Simplifications

Compared with `tgent`, this project will not carry over:

- QR pairing and pair-code encrypted private-key download
- file transfer
- tmux session or pane abstractions
- dashboard, billing, admin, promo, release center, scanner flows
- multi-channel terminal protocol designed around pane IDs

Instead:

- `termx-agent` will bridge a WebRTC DataChannel directly to a local `termx` daemon connection.
- the mobile app will speak the existing `termx` protocol over DataChannel.
- the control plane will only manage users, tokens, devices, terminal listing, and connect tickets.

## Proposed Directory Layout

- `cmd/termx`
  - keep local CLI and add auth or token subcommands
- `cmd/termx-agent`
  - new remote bridge process
- `cmd/termx-hub`
  - new hub or signaling or TURN entrypoint migrated from `tgent-go/cmd/tgent-hub`
- `remote/agent`
  - agent bridge logic, daemon dialing, WebRTC session lifecycle
- `remote/hub`
  - registry, signaling, TURN bootstrap, auth helpers
- `remote/controlplane`
  - shared Go types for tickets, hub tokens, device records when needed
- `web/control`
  - minimal Next.js control plane migrated from `tgent-web`
- `mobile/app`
  - minimal Capacitor plus React app migrated from `tgent-app`

## Architecture Decisions

### D1. WebRTC carries the existing termx protocol

This is the main reduction step.

- One reliable ordered DataChannel will carry full `termx` frames.
- `termx-agent` will proxy frames between the DataChannel and a local Unix-socket transport connection to the daemon.
- The mobile app will implement a small TypeScript `termx` protocol client instead of reusing `tgent`'s pane-specific terminal channel protocol.

Why:

- preserves `attach`, `snapshot`, `input`, `resize`, `events`, `sync_lost`, `screen_update` as-is
- avoids inventing a second remote terminal business protocol
- keeps the server abstraction flat and unchanged
- makes reconnect and snapshot recovery line up with existing tests and behavior

### D2. Token-first auth replaces pairing

- `termx` config gains a first-class auth section for control-plane URL, access token, refresh token, and active device identity.
- `termx login/logout/whoami` or equivalent commands will read and write that config.
- `termx-agent` will use the configured token to register itself with the control plane and hub.

### D3. Control plane stays minimal

Required API surface:

- `POST /api/auth/login`
- `POST /api/auth/refresh`
- `POST /api/auth/logout`
- `GET /api/auth/me`
- `GET /api/devices`
- `GET /api/devices/:id/terminals`
- `POST /api/devices/:id/connect`

The connect endpoint returns:

- hub URL
- short-lived hub token
- target device ID
- optional ICE config hints

### D4. termx daemon remains clean

- no app-side navigation or device concepts leak into the daemon
- no tmux-style hierarchy is added server-side
- remote ownership, signaling, and token logic stay in `termx-agent`, hub, and control plane layers

## Migration Map

### Server auth and token config

- termx destination:
  - `cmd/termx`
  - `tuiv2/shared/config.go`
  - `tuiv2/shared/config_file.go`
- tgent references:
  - `tgent-web/src/lib/auth.ts`
  - `tgent-web/src/app/api/auth/login/route.ts`

### WebRTC bridge

- termx destination:
  - `cmd/termx-agent`
  - `remote/agent`
- tgent references:
  - `tgent-go/internal/agent/webrtc.go`
  - `tgent-go/internal/hub/agent_registry.go`
  - `tgent-go/internal/hubserver/handlers.go`

### Hub and TURN

- termx destination:
  - `cmd/termx-hub`
  - `remote/hub`
- tgent references:
  - `tgent-go/cmd/tgent-hub/main.go`
  - `tgent-go/internal/hub`
  - `tgent-go/internal/hubgrpc`
  - `tgent-go/internal/hubserver`

### Control plane

- termx destination:
  - `web/control`
- tgent references:
  - `tgent-web/src/app/api/auth/*`
  - `tgent-web/src/app/api/agents/*`
  - `tgent-web/src/lib/auth.ts`
  - `tgent-web/src/lib/queries.ts`

### Mobile app

- termx destination:
  - `mobile/app`
- tgent references:
  - `tgent-app/src/components/Terminal.tsx`
  - `tgent-app/src/api/webrtc.ts`
  - `tgent-app/src/state/connectors/p2pConnector.ts`
  - `tgent-app/src/api/terminalClient.ts`

## Milestone TODO

### M1. Survey and plan

- [x] inspect `termx` server, protocol, config, and tests
- [x] inspect reusable `tgent` backend, hub, TURN, and app code
- [x] decide the primary bridge strategy: existing `termx` protocol over DataChannel
- [x] keep this document updated as milestones land

### M2. termx auth and token config

- [x] add characterization tests for config parsing and CLI auth commands
- [x] extend config schema with auth section
- [x] add `login`, `logout`, and `whoami` or equivalent token-management commands
- [x] document config and CLI usage
- [x] commit after tests pass

### M3. termx-agent minimal bridge

- [x] add failing tests for DataChannel-to-local-transport proxy behavior
- [x] implement minimal `termx-agent`
- [x] proxy full `termx` protocol frames over a single DataChannel
- [x] verify attach, input, resize, snapshot happy path
- [x] commit after tests pass

### M4. hub, signaling, TURN

- [x] characterize the smallest reusable hub and TURN slices from `tgent`
- [ ] migrate minimal registry, auth, rtc config, and offer endpoints
- [ ] wire TURN startup for local or test runs
- [ ] add local smoke coverage
- [ ] commit after tests pass

### M5. control plane

- [ ] scaffold minimal Next.js control plane
- [ ] migrate login plus refresh plus logout plus me
- [ ] migrate device list and connect ticket endpoints
- [ ] add terminal listing endpoint backed by device metadata or agent proxy
- [ ] commit after tests pass

### M6. mobile scaffold

- [ ] scaffold `mobile/app` from `tgent-app`
- [ ] delete non-essential pages and plugins
- [ ] keep only login, device list, terminal list, terminal view, and basic settings
- [ ] add tests for routing and auth state
- [ ] commit after tests pass

### M7. mobile termx protocol client

- [ ] add characterization tests for frame encoding and decoding in TypeScript
- [ ] implement request or response plus stream client over DataChannel
- [ ] render terminal list from remote `list` or `events`
- [ ] connect `attach` plus xterm.js view
- [ ] commit after tests pass

### M8. reconnect and recovery

- [ ] add tests for reconnect, `sync_lost`, and snapshot recovery behavior
- [ ] implement reconnect flow in app and agent if needed
- [ ] add basic observability and user-facing errors
- [ ] commit after tests pass

### M9. regression and docs

- [ ] run focused Go, web, and app regression suites
- [ ] fill in operator docs for local dev and deployment
- [ ] summarize remaining risks and known gaps

## Immediate Next Slice

1. add a WebRTC-backed `transport.Transport` adapter with frame fragmentation tests
2. migrate the minimal hub registry and HTTP offer flow on top of the current local-offer bridge
3. reuse `tgent` TURN startup and ephemeral credential generation without pulling in tmux APIs
4. add a local or hermetic smoke path from hub offer to `termx-agent`

## Progress Log

- 2026-04-28: M1 completed. Surveyed `termx` and `tgent`, chose the native `termx` protocol-over-DataChannel architecture, and wrote the milestone plan.
- 2026-04-28: M2 completed. Added `auth` config support, `termx login/logout/whoami`, a minimal control-plane auth client, docs, and focused tests.
- 2026-04-28: M3 completed. Added a WebRTC transport adapter with frame fragmentation, a bidirectional frame proxy, a minimal local-offer WebRTC handler, and the first `cmd/termx-agent` direct bridge entrypoint.

## Risks To Track

- the current config parser is a small YAML subset, so new auth fields should stay flat and simple unless a real parser is introduced
- TypeScript needs a fresh `termx` protocol client because the current reusable implementation is Go
- `tgent` WebRTC code assumes pane-oriented channels in some paths; only the offer/answer, ICE, and connection lifecycle should be reused directly
- control-plane data model can easily bloat if we blindly carry over `tgent-web` tables and endpoints
