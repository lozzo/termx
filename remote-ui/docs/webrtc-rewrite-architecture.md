# `remote-ui` WebRTC Rewrite Architecture

## Non-Negotiable Model

Runtime is always WebRTC DataChannel. Every terminal, API, file, and event operation must run over a connected WebRTC DataChannel.

HTTP is not transport. HTTP is only allowed before the runtime session exists, for signaling, discovery, pairing, rendezvous, hub polling, and answer submission.

The client-visible connection paths are exactly:

- `local`: local-machine WebRTC, including ICE TCP / WebRTC over TCP when available.
- `public_p2p`: WebRTC P2P established through public ICE/STUN and rendezvous infrastructure.
- `managed`: WebRTC established through self-managed Hub / ICE infrastructure.

`relay` is not a client transport type. Stated without formatting for test and handoff clarity: relay is not a client transport type. In `managed`, relay usage is decided by Hub / TURN / ICE policy and is reported only through connection info, capabilities, policy, status, or telemetry such as `relayInUse` and `fileTransferAllowed`.

Paid, anonymous, and managed differences are capability / policy results. They are not runtime transport taxonomy.

## Why The Old Boundary Is Wrong

The existing `RemoteTransport` / `PeerTransport` / `TerminalTransport` split mixes unrelated concepts:

- It models `anonymous_p2p`, `managed_p2p`, and `paid_relay` as transport modes even though runtime transport is always WebRTC DataChannel.
- It makes terminal protocol look like a transport, even though it is a protocol client running over a binary DataChannel.
- It lets local WebRTC implementation details become the shared abstraction shape.
- It gives HTTP-era names too much authority after signaling has finished.

The rewrite removes these boundaries instead of wrapping them.

## Target Layers

### Connector / Signaling Adapter

Connectors are pre-runtime objects. They gather inputs, perform signaling, and return an already connected `RtcSession`.

Expected connector families:

- `LocalRtcConnector`: pairs/signs/offers through local HTTP signaling and returns a local WebRTC session.
- `PublicP2pRtcConnector`: creates or joins a rendezvous channel, exchanges offers/answers/candidates through public signaling, and returns a public P2P WebRTC session.
- `ManagedRtcConnector`: uses Hub discovery/poll-answer/ICE config and returns a managed WebRTC session.

Connectors may use HTTP. They must not expose HTTP as runtime transport.

Signaling payloads must describe purpose or requested capabilities, not client-selected transport. For example, local signaling uses `purpose: "runtime"` for normal WebRTC sessions and `purpose: "inventory_events"` for the pre-runtime inventory event connection; it must not send `client.transport`.

### `RtcSession`

`RtcSession` is the only runtime transport abstraction and is platform neutral. It must not reference browser-only WebRTC classes, browser binary message classes, browser events, native plugin objects, or Hub HTTP request details.

Required public surface:

- `openTerminal(terminalId): Promise<RtcBinaryChannel>`
- `openApi(): Promise<RtcJsonRpcChannel>`
- `openFileTransfer(transferId): Promise<RtcBinaryChannel>`
- `subscribeEvents(handler): RtcSubscription`
- `getConnectionInfo(): Promise<ConnectionInfo>`
- `getCapabilities(): Promise<ConnectionCapabilities>`
- `disconnect(): Promise<void>`

Negotiation helpers remain platform-neutral:

- `RtcSessionNegotiator` creates browser/native offers and accepts answers.
- `RtcSessionAnswerer` accepts a remote offer and returns an answer.
- `RtcSessionCapabilityUpdater` lets connectors apply server-negotiated capability results without exposing browser or Hub implementation details.

Common types:

- `ConnectionPath = 'local' | 'public_p2p' | 'managed'`
- `ConnectionInfo` contains path, connection id, machine id, optional terminal id, `relayInUse`, and negotiated metadata.
- `ConnectionCapabilities` contains policy results such as `fileTransferAllowed`, `terminalAllowed`, `eventsAllowed`, and optional denial reasons.
- `RtcBinaryChannel` is a minimal binary channel contract with label, ready state, send, close, message subscription, close subscription, and wait-open support.
- `RtcJsonRpcChannel` provides request/response semantics over a DataChannel.

### Browser Adapter

`BrowserRtcSession` is the first implementation of `RtcSession`.

It owns browser-only objects:

- `RTCPeerConnection`
- `RTCDataChannel`
- browser ICE gathering / connection state events
- browser `Blob` and `ArrayBufferView` message normalization

It creates and manages these DataChannel labels:

- `terminal:${terminalId}`
- `api`
- `events`
- `file:${transferId}`

The browser adapter supports both offerer and answerer roles. When it creates the offer, it creates local DataChannels. When it accepts a remote offer, it waits for incoming DataChannels from the offerer. The role is explicit; it must not infer DataChannel ownership from `managed` alone.

Server-negotiated relay and capability results are stored as `ConnectionInfo` / `ConnectionCapabilities`. If managed relay policy denies file transfer, `openFileTransfer()` fails through capability policy rather than creating a `relay` transport or hanging on a `file:*` channel.

Browser types may appear only in this layer and direct browser adapter helpers/tests.

### Native Adapter Boundary

The current task does not implement native adapters, but the public interface must make them natural.

Future Android(Java/Kotlin) and iOS(Swift) WebView hosts should be able to implement the same `RtcSession` surface through native WebRTC and expose it to the shared TypeScript business layer without changing terminal, file, API, event, capability, or UI logic.

This means shared code must depend only on `RtcSession`, `RtcBinaryChannel`, `RtcJsonRpcChannel`, `ConnectionInfo`, and `ConnectionCapabilities`.

### Terminal Protocol Client

`TerminalProtocolClient` runs the termx terminal protocol over a terminal binary channel. It is not a transport.

Responsibilities:

- Send `hello`.
- Attach to a terminal stream.
- Request and normalize snapshots.
- Forward output.
- Encode input and resize control frames.
- Buffer early stream frames until attach names the stream channel.
- Emit terminal lifecycle events.

It does not care whether the session came from `local`, `public_p2p`, or `managed`, and it does not care whether the session is browser WebRTC or future native WebRTC.

### Capability / Policy

Capabilities are runtime facts and policy results attached to the session:

- `relayInUse`
- `fileTransferAllowed`
- `terminalAllowed`
- `eventsAllowed`
- anonymous or paid denial reasons
- negotiated channel availability

Relay never creates another client transport interface. If `managed` uses TURN relay, the UI and consumers see the same `RtcSession` methods plus different `ConnectionInfo` / `ConnectionCapabilities`.

Runtime events use `RtcSession.subscribeEvents()` over the `events` DataChannel. Pre-runtime inventory can still establish its own WebRTC event connection before a normal runtime session exists, but it is not an HTTP runtime transport and must not introduce another client transport taxonomy.

## Migration Rules

- Delete `RemoteTransport`, `PeerTransport`, `TerminalTransport`, `anonymous_p2p`, `managed_p2p`, and `paid_relay` as public runtime concepts.
- Replace them with `RtcSession` and `ConnectionPath`.
- Rename `LocalTerminalProtocolTransport` to `TerminalProtocolClient` or equivalent.
- Rename browser runtime implementation to `BrowserRtcSession` or equivalent.
- Rename pre-runtime local creation into a connector/signaling adapter.
- Keep local HTTP APIs only for status/discovery, initial terminal inventory needed before a session exists, pairing, and RTC offer/answer signaling.
- Terminal management after a runtime session exists must move through `RtcSession.openApi()` and capability checks instead of remaining an HTTP runtime path.

## Test Requirements

Behavior tests must prove:

- `local`, `public_p2p`, and `managed` are distinct connection paths but share one runtime interface.
- Terminal protocol logic depends on `RtcSession` / `RtcBinaryChannel`, not browser WebRTC.
- Browser adapter implements the public interface without leaking browser types into upper layers.
- Terminal, API, file, and events all go through one session after signaling.
- `managed` relay is represented only as connection info or capability/policy.
- Existing terminal lifecycle behavior remains intact: close, reattach, early frame buffering, snapshot, output, resize control.
- API and file semantics do not drift.

## Handoff Contract

`remote-ui/docs/webrtc-rewrite-log.md` is the source of truth for progress after context compaction. Each slice must record goals, failing tests, implementation, renames, deletions, commands, review findings, review fixes, remaining risk, and next step.
