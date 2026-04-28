# termx Remote Architecture Spec

Status: draft

This document defines the target remote-access architecture for `termx` after the reset. It is the spec that future implementation slices must follow.

## 1. Problem Statement

`termx` already has the right core server model: a flat terminal pool with stable `terminal_id`, PTY lifecycle management, attach semantics, snapshot/recovery, and a binary screen-update stream. What it does not have yet is a production-shaped remote architecture for mobile access.

The previous `tgent` stack proved several ideas:

- mobile terminal access over WebRTC DataChannel is viable
- signaling + TURN should be treated as first-class infrastructure
- hub discovery and heartbeat matter in real networks

But `tgent` also carries product and architecture baggage that `termx` must not inherit:

- app/session/window/pane business structure in the remote stack
- standalone long-lived `agent` as the final user-facing process model
- web-side private-key escrow
- dashboard/billing/file-manager/pairing flows unrelated to terminal access

This spec defines a smaller target:

- `termx` remains a flat terminal server
- remote access is terminal-only
- `termx daemon` embeds the remote runtime
- mobile connects over WebRTC DataChannel only
- control plane tracks users, devices, hubs, and tickets
- users keep their own client private keys

## 2. Hard Constraints

The following are not optional:

- Server-side entity model remains `terminal` only. No tmux-style remote `session/window/pane` hierarchy is introduced.
- The remote data plane uses WebRTC DataChannel only.
- The remote stack reuses existing `termx` terminal semantics: `list`, `attach`, `snapshot`, `input`, `resize`, `events`, `SyncLost`, `Closed`.
- The terminal stream and recovery chain stay binary. Do not replace `screen_update`, `snapshot/bootstrap`, or related transport payloads with JSON.
- The app UI remains minimal: `login -> devices -> terminals -> terminal`.
- The long-term process model is `termx daemon` with embedded remote runtime, not `termx + separate agent`.
- The web/control plane does not escrow user private keys.

## 3. Goals

- Let a user log into a mobile app, see their devices, list terminals on a device, and enter a live terminal.
- Keep the server abstraction clean: remote infra must wrap `termx`, not distort its core model.
- Reuse `tgent` hub/TURN/discovery ideas where they fit, but aggressively drop unrelated product scope.
- Support multiple hub/TURN nodes so different network environments can choose better relay paths.
- Make identity closer to SSH:
  - device private key stays on the device
  - client private key stays on the client
  - control plane stores public material and issues short-lived tickets

## 4. Non-Goals

- No remote session tree, workspace tree, pane graph, or synchronized layout model.
- No file manager, pairing QR flow, promo/billing/admin product work in the first remote path.
- No second terminal business protocol parallel to the existing `termx` protocol.
- No requirement that the first implementation ship full relay optimization or perfect hub scoring on day one.

## 5. Existing termx Anchors To Preserve

The remote architecture must build on these existing anchors:

- Core server and daemon entry:
  - `termx.go`
  - `cmd/termx/main.go`
- Config file loading:
  - `tuiv2/shared/config.go`
  - `tuiv2/shared/config_file.go`
- Wire protocol and binary stream message types:
  - `protocol/frame.go`
  - `protocol/messages.go`
- Existing attach stream pump and screen-update coalescing:
  - `attachment_stream.go`
  - `stream_screen_state.go`

Important consequence:

- Remote access should carry existing `termx` frames and binary screen-update payloads over a different transport.
- The mobile stack is a new transport/runtime wrapper, not a new terminal server.
- The first embedded-runtime implementation should not assume there is already a public generic server-side transport hook; today the safe seams are the existing client/protocol path and `tuiv2/bridge.Client`-shaped abstractions.

## 6. Target Components

### 6.1 termx daemon

`termx daemon` remains the only long-lived user process on the device.

It owns:

- terminal pool
- PTY lifecycle
- local Unix socket API
- embedded remote runtime
- device identity material
- remote registration state

It does not own:

- account database
- hub catalog truth
- global user/device directory

### 6.2 Embedded Remote Runtime

The embedded runtime is the code currently conceptually called “agent”, but it becomes an internal `termx` subsystem, not a separate required process.

It owns:

- control-plane bootstrap/login usage
- hub selection and registration
- signaling sessions with hubs
- WebRTC peer setup
- bridging WebRTC DataChannel frames to the local `termx` server API/protocol
- device liveness, metadata, and terminal-list reporting

### 6.3 Control Plane

The control plane is a remote web/backend service.

It owns:

- user auth and account session tokens
- device metadata and ownership
- hub catalog
- relay catalog
- public-key registry and fingerprints
- short-lived connect tickets

It does not own:

- terminal state
- private-key escrow
- direct terminal I/O

### 6.4 Hub

A hub is the remote signaling ingress for registered devices.

It owns:

- device registration and liveness for currently connected daemons
- offer/answer forwarding
- ICE server config for a connect attempt
- policy enforcement attached to a short-lived ticket

At minimum, a hub may also run TURN. In larger deployments, TURN may be split into dedicated relay nodes while the hub remains the signaling endpoint.

### 6.5 Relay / TURN Nodes

Relay nodes provide ICE fallback and performance options across different network environments.

The architecture must allow:

- one hub with embedded TURN in dev/minimal deployment
- multiple relay endpoints in production
- control-plane managed relay catalogs and ranking metadata

### 6.6 Mobile App

The app owns only the minimum terminal UX:

- control-plane login
- device list
- terminal list
- terminal view
- reconnect and recovery

The app does not own any server-side terminal organization model.

## 7. Identity and Trust Model

This architecture separates device identity from client identity.

### 7.1 Device Identity

Each `termx daemon` has a long-lived device keypair.

- Generated locally on first remote enable
- Private key stored only on the device
- Public key and fingerprint registered with the control plane

This key identifies the device/runtime to hubs and control-plane verification endpoints.

Registration rule:

- device registration must require proof of possession for the device private key
- matching `{device_id, public_key}` in a database is not sufficient on its own

### 7.2 Client Identity

Each mobile client uses a user-held client keypair.

- Private key generated/imported on the client
- Private key never uploaded to the control plane
- Public key or fingerprint is registered for the account

Import/export should be explicit and manual, closer to SSH authorized-key workflows than to cloud key escrow.

### 7.3 User Login Tokens

The app still logs into the control plane with normal account auth. These account tokens are for:

- device discovery
- ticket requests
- account-scoped metadata APIs

They are not a replacement for client key possession.

### 7.4 Connect Tickets

The control plane issues short-lived connect tickets bound to:

- user ID
- target device ID
- target terminal ID or allowed terminal scope
- selected hub ID
- expiry
- optional client key fingerprint

The ticket authorizes the connection attempt. It does not replace proof of client key possession when client keys are enabled.

Explicit non-goal:

- a database boolean such as `paired=true` is not a trust anchor
- possession-based proof is the trust anchor

## 8. Discovery and Node Selection

The system distinguishes two catalogs:

- hub catalog: signaling endpoints that devices can register to
- relay catalog: STUN/TURN endpoints that a connection may use during ICE negotiation

### 8.1 Hub Catalog

The control plane maintains the online hub catalog from hub heartbeats.

Each catalog entry includes at least:

- hub ID
- public signaling URL
- region
- online status
- load/capacity hints
- version/capability hints

### 8.2 Relay Catalog

The control plane maintains relay inventory, which may be:

- co-located with a hub
- separate relay-only nodes

Each relay entry includes at least:

- relay ID
- ICE URL set
- region
- protocol/transport support
- capacity/health metadata

### 8.3 Device-Side Registration Selection

On startup, the embedded remote runtime:

1. authenticates to the control plane
2. fetches candidate hubs
3. probes or scores them locally
4. chooses a primary hub
5. registers with that hub

Scoring may use:

- reachability
- observed RTT
- static region hints
- load/capacity hints

The first implementation may start with simpler ranking, but the architecture must not hardcode a single hub.

Important note:

- keep the split between control-plane inventory publication and device-side final selection
- do not copy `tgent`'s exact hardcoded weighted scoring formula unless `termx` gains trustworthy resource telemetry for it

### 8.4 Connect-Time Relay Selection

For a mobile connect attempt, the ticket response includes:

- the selected signaling hub for the target device
- an ordered ICE server list

The ICE list may include multiple TURN nodes so the connection can perform better in different network conditions.

## 9. End-to-End Flows

### 9.1 Device Bootstrap

1. User logs `termx` into the control plane.
2. `termx daemon` remote mode is enabled in config.
3. On first enable, daemon creates a device keypair.
4. Daemon fetches hub candidates from the control plane.
5. Daemon selects a hub and registers using device key proof of possession.
6. Hub reports online device state back to the control plane.

### 9.2 Mobile Login and Device Discovery

1. User logs into the mobile app.
2. App calls control-plane `devices` API.
3. Control plane returns devices owned by that account.
4. App selects a device and requests `terminals`.
5. Control plane proxies or routes terminal-list requests through the device’s active hub/runtime.

### 9.3 Terminal Connect

1. App requests a connect ticket for `{device_id, terminal_id}`.
2. Control plane validates ownership and returns:
   - hub signaling URL
   - short-lived connect ticket
   - ICE server list
   - device key fingerprint
3. App opens signaling with the selected hub.
4. Hub forwards the offer to the device’s registered runtime.
5. Device runtime bridges the WebRTC DataChannel to the local `termx` terminal attach path.
6. App enters the live terminal using existing `termx` attach semantics.

### 9.4 Reconnect

On disconnect:

1. app reacquires a fresh connect ticket
2. app reconnects through the selected hub
3. runtime re-attaches to the terminal
4. app restores state via binary bootstrap/snapshot and ongoing screen updates

Recovery must prefer existing `SyncLost` and snapshot semantics over inventing custom app-only recovery behavior.

## 10. Protocol and Transport Rules

### 10.1 Control Plane

Control-plane APIs may remain JSON/HTTP.

Examples:

- login
- hub catalog discovery
- devices list
- connect ticket issue

Internal service-to-service auth is allowed to be different from user auth, but it should not stop at a single static shared secret in the final design. Replay-resistant hub/control-plane authentication is preferred.

### 10.2 Data Plane

The WebRTC DataChannel carries existing `termx` protocol frames.

Rules:

- use a reliable ordered DataChannel for the primary terminal stream
- preserve frame structure from `protocol/frame.go`
- preserve message types such as `Output`, `Input`, `Resize`, `ScreenUpdate`, `SyncLost`, `Closed`, `BootstrapDone`
- do not wrap terminal frames in a second JSON protocol

### 10.3 Recovery Chain

The remote recovery chain must preserve binary payloads:

- screen updates stay in existing TSU binary payload form
- bootstrap/snapshot handoff stays binary on the streaming path
- no JSON fallback is introduced for production transport

### 10.4 Terminal Semantics To Preserve

The remote stack must preserve these existing behaviors:

- `list` remains a flat terminal list
- `attach` remains the I/O entry point
- `observer` vs `collaborator` attach modes stay meaningful
- `resize` applies to the attached terminal
- `SyncLost` triggers snapshot-based recovery
- `Closed` carries terminal exit state
- attach bootstrap ordering remains meaningful: initial recovery is `Resize -> full ScreenUpdate -> BootstrapDone`, with `Closed` following for exited terminals
- clients that open stream consumers after attach still need the current no-loss buffering/replay semantics provided by the existing protocol client path

### 10.5 Existing Session APIs

`termx` already carries `session.*` methods for local/shared-workbench clients.

Rules:

- the new mobile remote product does not build on `session` as a user-facing remote entity model
- the remote architecture must not introduce a second remote session tree
- existing `session.*` protocol support may remain for current TUI/local clients and should not be broken accidentally during the remote refactor

## 11. Config and CLI Surface

The implementation should extend the existing `termx.yaml` and CLI rather than inventing separate config files.

### 11.1 Config Direction

`termx.yaml` should grow dedicated remote sections such as:

- `control`
  - control-plane base URL
  - optional environment/profile name
- `remote`
  - enabled
  - auto-register
  - preferred regions or pinned hub IDs
- `identity`
  - device key path
  - known client public keys or keyring path

The exact field names are implementation details, but the separation of concerns should remain:

- account/login config
- remote runtime behavior
- identity material paths

Important note:

- this is a new daemon-facing config surface; the current `termx.yaml` is TUI-oriented and does not yet configure daemon startup

### 11.2 CLI Direction

Minimum CLI additions:

- `termx login`
- `termx logout`
- `termx whoami`
- `termx remote status`

Likely useful follow-ups:

- `termx remote enable`
- `termx remote disable`
- `termx remote doctor`
- `termx keys export-client-pub`

The first code slices should not overbuild CLI surface before the core runtime path exists.

## 12. Migration From tgent

### 12.1 Reuse

The following ideas or implementations are worth migrating or adapting:

- hub heartbeat and catalog reporting
- multi-hub discovery shape
- TURN/STUN server implementation
- signaling flow around offer/answer relay
- separation between long-lived device enrollment auth and short-lived runtime attach auth
- terminal-capable mobile shell using Capacitor + React + WebView + xterm.js
- browser/mobile-side WebRTC frame transport patterns

### 12.2 Drop

The following should not be migrated as-is:

- remote session/tree semantics
- file management and transfer UI
- dashboard-heavy information architecture
- pairing flows as a required happy path
- any trust model that depends on a DB-side `paired` flag instead of proof of key possession
- web-side encrypted private-key storage
- product/admin/billing code unrelated to terminal access
- REST proxy planes, file/event/session relay features, and other non-terminal remote surfaces

### 12.3 Adapt

The following need adaptation rather than direct copy:

- device registration must become `termx daemon` embedded runtime registration
- terminal listing must map to `termx list`, not a remote session structure
- connect flow must target a terminal directly, not a broader workspace/session abstraction

## 13. Deployment Shapes

### 13.1 Minimal Dev Shape

- 1 control-plane service
- 1 hub service
- hub may include TURN
- 1 local `termx daemon`
- 1 mobile app

### 13.2 Production Shape

- 1 control-plane service or small control-plane cluster
- multiple hubs across regions
- multiple relay nodes across regions and network environments
- each device registered to one active hub at a time, with failover candidates

## 14. First Refactor Direction

The first implementation slices should move in this order:

1. establish config + identity foundations
2. extract/shape a shared embedded remote runtime inside `termx`
3. add control-plane discovery and device registration contracts
4. add hub runtime and signaling path
5. add minimal mobile terminal-only client

This order is intentional:

- it aligns the process model first
- it avoids reintroducing a standalone agent as the default architecture
- it keeps each milestone independently testable

## 15. Open Decisions

These are allowed to remain open for the first implementation slices, but they must be explicit:

- exact on-disk client-key import/export format
- exact hub-selection scoring algorithm
- whether relay-only nodes are a separate service type from hubs in the first production rollout
- how much connect-ticket scope is bound to a single terminal versus a device-level session

None of these open points justify drifting away from the constraints in sections 2 and 10.
