# `remote-ui` WebRTC Rewrite Log

This log is the handoff record for the WebRTC rewrite. It must be updated after every slice.

## Slice 1: Documentation Initialization And AGENTS Hardening

### Goal

Establish the file-backed architecture record before implementation work and harden repo instructions so future agents keep the rewrite aligned with the WebRTC DataChannel runtime model.

### Failing Tests First

Failing tests first.

- Added `src/docs/webrtcRewriteDocs.test.ts` to assert that the architecture document names the required runtime boundaries and that the log is slice-oriented.
- Initial command: `npm test -- --run remote-ui/src/docs/webrtcRewriteDocs.test.ts`
- Result: failed because the filter was invoked from `remote-ui/` with a repo-prefixed path and no matching test file was found. This still happened before implementation, but the path was wrong; rerun with `src/docs/webrtcRewriteDocs.test.ts` after docs exist.
- Second command: `npm test -- --run src/docs/webrtcRewriteDocs.test.ts`
- Result: failed because the log and architecture headings used case/backtick formatting that did not match the exact test anchors.

### Implementation

- Created `docs/webrtc-rewrite-architecture.md`.
- Created `docs/webrtc-rewrite-log.md`.
- Documented the target layers: connector/signaling adapter, platform-neutral `RtcSession`, `BrowserRtcSession`, future native adapter boundary, `TerminalProtocolClient`, and capability / policy.
- Documented that runtime is always WebRTC DataChannel and that HTTP is only pre-runtime signaling/discovery/pairing/rendezvous/hub work.
- Documented the only client-visible paths: `local`, `public_p2p`, `managed`.
- Documented that relay is not a client transport type and must only appear as connection info, capability, policy, status, or telemetry.

### Renames

- No code renames in this slice.
- Architecture now reserves the replacement names `RtcSession`, `RtcBinaryChannel`, `RtcJsonRpcChannel`, `ConnectionInfo`, `ConnectionCapabilities`, `BrowserRtcSession`, connector/signaling adapter, and `TerminalProtocolClient`.

### Deleted Old Abstractions

- None in this slice. Deletion starts after public interface tests are in place.

### Commands

- `sed -n '1,220p' AGENTS.md`
- `sed -n '1,260p' remote-ui/AGENTS.md`
- `rg --files remote-ui/src remote-ui/test remote-ui/tests remote-ui/docs`
- `sed` reads of current transport, terminal, local WebRTC, API, file, entry, app, reference tgent, and Go signaling/runtime files.
- `npm test -- --run remote-ui/src/docs/webrtcRewriteDocs.test.ts` failed due to an incorrect filter path from the package directory.
- `npm test -- --run src/docs/webrtcRewriteDocs.test.ts` failed once on exact doc anchors, then passed after adding stable anchor text.
- `npm test -- --run src/docs/webrtcRewriteDocs.test.ts` passed.

### Review

- Independent sub-agent review completed after Slice 1 verification.

### Review Findings

- High: architecture allowed terminal management to remain a local HTTP API, conflicting with the runtime DataChannel model.
- Medium: `remote-ui/AGENTS.md` allowed browser objects holding `RTCPeerConnection` to be named `RtcSession`, weakening the platform-neutral boundary.
- Medium: `src/docs/webrtcRewriteDocs.test.ts` was mostly positive substring checks and did not reject old taxonomy or public browser type leakage.
- Medium: review sections were still pending in the log when review completed.

### Review Fixes

- Updated architecture to restrict local HTTP to status/discovery, initial terminal inventory before session, pairing, and RTC offer/answer signaling.
- Documented that terminal management after runtime session exists must move through `RtcSession.openApi()` and capability checks.
- Updated `remote-ui/AGENTS.md` so `RtcSession` is only the platform-neutral interface name; browser concrete implementations must use `BrowserRtc...` or equivalent.
- Strengthened `webrtcRewriteDocs.test.ts` to reject old path taxonomy and browser type leakage in the public `RtcSession` section.
- Recorded review findings and fixes in this log.

### Remaining Risk

Remaining risk.

- Current runtime code still contains the wrong abstractions: `RemoteTransport`, `PeerTransport`, `TerminalTransport`, `anonymous_p2p`, `managed_p2p`, and `paid_relay`.
- Browser WebRTC and terminal protocol are still coupled by `localWebRtcTransport.ts` and `localTerminalProtocolTransport.ts`.
- The next slice must lock public TypeScript interfaces before rewriting implementation.

### Next Step

Slice 2: write failing public-interface tests for `RtcSession`, `ConnectionPath`, capabilities, and absence of old transport taxonomy, then replace the shared type layer.

## Slice 2: Platform-Neutral Public Session Types

### Goal

Replace the public runtime transport taxonomy with platform-neutral `RtcSession` types and make direct consumers depend on `RtcSession` / `TerminalProtocolSession` instead of `RemoteTransport` / `PeerTransport` / `TerminalTransport`.

### Failing Tests First

Failing tests first.

- Replaced `src/transport.test.ts` with public-interface assertions for:
  - exactly `local`, `public_p2p`, and `managed`
  - `RtcSession` as the single runtime surface
  - absence of `RemoteTransport`, `PeerTransport`, `TerminalTransport`, `anonymous_p2p`, `managed_p2p`, and `paid_relay` in `transport.ts`
  - connector returning a connected `RtcSession`
- Initial command: `npm test -- --run src/transport.test.ts`
- Result: failed because `transport.ts` still exported old `ConnectionMode`, `RemoteTransport`, `PeerTransport`, `anonymous_p2p`, `managed_p2p`, and `paid_relay`.

### Implementation

- Rewrote `src/transport.ts` around:
  - `ConnectionPath`
  - `RtcSession`
  - `RtcBinaryChannel`
  - `RtcJsonRpcChannel`
  - `ConnectionInfo`
  - `ConnectionCapabilities`
  - `RtcConnector`
- Changed reducer snapshot field from `mode` to `path`.
- Changed file manager and file API props from transport-shaped dependencies to session-shaped dependencies.
- Changed terminal UI and hook props from `transport` to `session`.
- Changed terminal client boundary from `TerminalTransport` to `TerminalProtocolSession` and events from `TerminalTransportEvent` to `TerminalProtocolEvent`.
- Updated test mocks to implement `RtcSession` / `TerminalProtocolSession`.
- Updated existing local WebRTC implementation to compile against `RtcSession` and expose `getCapabilities()`.
- Added `src/vite-env.d.ts` so raw-doc tests typecheck.

### Renames

- `ConnectionMode` -> `ConnectionPath`.
- `mode` connection snapshot field -> `path`.
- `BinaryChannel` -> `RtcBinaryChannel`.
- `JsonRpcChannel` -> `RtcJsonRpcChannel`.
- `TerminalTransport` -> `TerminalProtocolSession`.
- `TerminalTransportEvent` -> `TerminalProtocolEvent`.
- Component props named `transport` -> `session` where they now consume runtime sessions.

### Deleted Old Abstractions

- Removed public exports of `RemoteTransport`, `PeerTransport`, `TerminalTransport`, `anonymous_p2p`, `managed_p2p`, and `paid_relay`.
- Did not delete implementation file names yet; `localWebRtcTransport.ts` and `localTerminalProtocolTransport.ts` still exist as implementation files pending Slice 3 / Slice 4 renames.

### Commands

- `npm test -- --run src/transport.test.ts` failed first, then passed.
- `npm run typecheck` failed during migration, then passed.
- `npm test -- --run src/transport.test.ts src/connectionMessageReducer.test.ts src/useTerminalSession.test.tsx src/fileApi.test.ts src/useFileManager.test.tsx src/FileManager.test.tsx` passed: 6 files, 23 tests.
- `npm test -- --run src/localTerminalProtocolTransport.test.ts src/localWebRtcTransport.test.ts` passed: 2 files, 32 tests.

### Review

- Independent sub-agent review completed after Slice 2 verification.

### Review Findings

- High: `LocalRemoteApp.tsx` still accepted a `LocalRemoteTransportFactory`, created an unconnected object in UI code, and used a cast-based optional `connect()` bridge. This keeps connector/signaling responsibility in the app layer.
- High: `index.ts` still exported browser-like adapter types from the package barrel, making browser implementation details too easy for upper layers to depend on.
- High: local terminal create/update/delete still used `LocalAgentApi` HTTP calls after runtime session exists. This is outside the final model and must move behind `RtcSession.openApi()` / capability policy in the API slice.
- Medium: `LocalRemoteApp.test.tsx` still expected the old `mode: 'local'` shape.
- Medium: `transport.test.ts` was too weak because it used a local path literal array and structural session typing that would not fail if an extra `connect()` member leaked into `RtcSession`.
- Low: some test helper names still contain old transport naming.

### Review Fixes

- Strengthened `transport.test.ts` to assert exported `CONNECTION_PATHS`, exact `RtcSession` keys, and exact public connection/capability key sets.
- Fixed `LocalRemoteApp.test.tsx` to stop expecting the old `mode` field.
- Removed `RTCDataChannelLike` and `RTCPeerConnectionLike` from the package barrel export.
- Kept connector extraction as the immediate Slice 5 target because it requires a behavior-changing boundary rewrite.
- Kept terminal management-over-HTTP as a Slice 8/9 target because API/file/events and capability checks need to move together.

### Remaining Risk

- `localWebRtcTransport.ts` still carries old file/factory names and a temporary implementation-level `LocalWebRtcPeerSession.connect()` used before connector extraction.
- `localTerminalProtocolTransport.ts` still carries old file/factory names even though its exported type is now `TerminalProtocolSession`.
- `BrowserRtcSession` is not yet split into a named browser adapter file; this is Slice 3.
- `subscribeEvents()` on the local WebRTC session is a placeholder; real events DataChannel work is Slice 8.

### Next Step

Slice 3: write failing tests for a browser adapter named `BrowserRtcSession` / `BrowserRtcFactory`, move browser-only WebRTC types behind that adapter, and keep public `RtcSession` free of browser details.

## Slice 3: Browser WebRTC Adapter Boundary

### Goal

Move browser-specific WebRTC implementation into a clearly named browser adapter module while keeping `transport.ts` and business consumers platform-neutral.

### Failing Tests First

Failing tests first.

- Added `src/browserRtcSession.test.ts`.
- Initial command: `npm test -- --run src/browserRtcSession.test.ts`
- Result: failed because `src/browserRtcSession.ts` did not exist.

### Implementation

- Moved the browser WebRTC implementation from `src/localWebRtcTransport.ts` to `src/browserRtcSession.ts`.
- Renamed the concrete browser class to `BrowserRtcSession`.
- Added `createBrowserRtcSession()`.
- Added `createBrowserRtcInventoryEvents()`.
- Renamed browser options/types to `BrowserRtcSessionOptions`, `BrowserRtcInventoryEventsOptions`, and `BrowserRtcConnectedSession`.
- Updated `src/localWebEntry.tsx` and `src/index.ts` to prefer browser adapter names.
- Moved the old local WebRTC behavior test to `src/browserRtcSession.behavior.test.ts` and updated it to use browser adapter factory names.
- Left `src/localWebRtcTransport.ts` as a narrow compatibility re-export for old import paths pending deletion in Slice 10.

### Renames

- `LocalWebRtcPeerTransport` -> `BrowserRtcSession`.
- `createLocalWebRtcPeerTransport` -> `createBrowserRtcSession` in new/browser-facing code.
- `createLocalWebRtcInventoryEvents` -> `createBrowserRtcInventoryEvents` in new/browser-facing code.
- `LocalWebRtcPeerTransportOptions` -> `BrowserRtcSessionOptions`.
- `LocalWebRtcPeerSession` -> `BrowserRtcConnectedSession`.

### Deleted Old Abstractions

- Removed browser implementation from the old `localWebRtcTransport.ts` file.
- Did not yet delete compatibility re-exports; they are explicitly deferred to Slice 10 after connectors and terminal protocol renames land.

### Commands

- `npm test -- --run src/browserRtcSession.test.ts` failed first due to missing module.
- `npm test -- --run src/browserRtcSession.test.ts src/browserRtcSession.behavior.test.ts` passed: 2 files, 27 tests.
- `npm test -- --run src/localWebEntry.test.tsx src/transport.test.ts` passed: 2 files, 7 tests.
- `npm run typecheck` passed.

### Review

- Independent sub-agent review completed after Slice 3 verification.

### Review Findings

- High: `browserRtcSession.ts` still mixes local signaling into the browser runtime adapter through `path?: 'local'`, local offer/answer types, `signOffer()`, and an implementation-level `connect()`.
- High: `BrowserRtcConnectedSession` extends `TerminalProtocolSession`, and the browser adapter constructs the terminal protocol client directly. This keeps terminal protocol coupled to browser WebRTC instead of making terminal protocol a consumer of `RtcSession.openTerminal()`.
- Medium: `index.ts` re-exported `RTCDataChannelLike` and `RTCPeerConnectionLike`, leaking browser implementation details into the broader public API.
- Medium: `localWebRtcTransport.ts` compatibility re-export keeps old names available and is not a final boundary.
- Medium: `BrowserRtcSession.subscribeEvents()` is still a no-op while inventory events use a separate connection object.
- Low: `browserRtcSession.test.ts` is mostly raw-source boundary smoke testing and does not catch all current leaks.

### Review Fixes

- Removed `RTCDataChannelLike` and `RTCPeerConnectionLike` from `index.ts` so the package barrel no longer exports browser WebRTC-like types.
- Scheduled local signaling extraction for Slice 5, where a local connector will own offer/answer/signing and return a connected `RtcSession`.
- Scheduled terminal protocol decoupling for Slice 5/8: `TerminalProtocolClient` should consume `RtcSession.openTerminal()` from upper code instead of being extended by the browser adapter.
- Scheduled compatibility re-export deletion for Slice 10.
- Kept `subscribeEvents()` as a known Slice 8 gap until session-level events DataChannel is implemented.

### Remaining Risk

- `BrowserRtcConnectedSession.connect()` is still an implementation-level pre-session hook used by current local-web factory before Slice 5 connector extraction.
- `src/localWebRtcTransport.ts` remains as compatibility re-export and must be deleted in Slice 10.
- `BrowserRtcSession.subscribeEvents()` is still a placeholder until events DataChannel work in Slice 8.
- The browser adapter currently supports local signaling inputs; public and managed signaling adapters are still future slices.

### Next Step

Slice 4: rename the terminal protocol implementation to `TerminalProtocolClient` / `TermxTerminalProtocolClient`, move protocol behavior tests to the new name, and leave no terminal protocol object named transport.

## Slice 4: Terminal Protocol Client Naming

### Goal

Separate termx terminal protocol client naming from transport naming so the terminal protocol object is explicitly a protocol client over `RtcBinaryChannel`.

### Failing Tests First

Failing tests first.

- Added `src/terminalProtocolClient.test.ts`.
- Initial command: `npm test -- --run src/terminalProtocolClient.test.ts`
- Result: failed because `src/terminalProtocolClient.ts` did not exist.

### Implementation

- Moved `src/localTerminalProtocolTransport.ts` implementation to `src/terminalProtocolClient.ts`.
- Renamed the class to `TerminalProtocolClient`.
- Renamed factory to `createTerminalProtocolClient()`.
- Renamed options type to `TerminalProtocolClientOptions`.
- Updated `BrowserRtcSession` to call `createTerminalProtocolClient()`.
- Moved behavior tests to `src/terminalProtocolClient.behavior.test.ts`.
- Left `src/localTerminalProtocolTransport.ts` as a narrow compatibility re-export pending Slice 10 deletion.

### Renames

- `LocalTerminalProtocolTransport` -> `TerminalProtocolClient`.
- `createLocalTerminalProtocolTransport` -> `createTerminalProtocolClient` in implementation users.
- `LocalTerminalProtocolTransportOptions` -> `TerminalProtocolClientOptions`.

### Deleted Old Abstractions

- Removed terminal protocol implementation from the old `localTerminalProtocolTransport.ts` file.
- Did not yet delete compatibility re-export; it is deferred to Slice 10.

### Commands

- `npm test -- --run src/terminalProtocolClient.test.ts` failed first due to missing module.
- `npm test -- --run src/terminalProtocolClient.test.ts src/terminalProtocolClient.behavior.test.ts` passed: 2 files, 8 tests.
- `npm test -- --run src/browserRtcSession.behavior.test.ts src/browserRtcSession.test.ts` passed: 2 files, 27 tests.
- `npm run typecheck` passed.

### Review

- Independent sub-agent review completed after Slice 4 verification.

### Review Findings

- High: `BrowserRtcSession` still implemented terminal protocol responsibilities by constructing `TerminalProtocolClient`, extending the protocol session shape, and exposing `subscribeTerminal()` / `closeTerminalChannel()`.
- High: `Terminal` / `useTerminalSession` still consumed a protocol session rather than a platform-neutral `RtcSession`, so the business layer did not yet prove browser/native adapter independence.
- Medium: local signaling still lived in the browser session / local web entry construction path instead of a connector.
- Medium: compatibility re-export `localTerminalProtocolTransport.ts` still carries the old transport name.
- Low: some test helpers retained old `Transport` wording.

### Review Fixes

- Moved the terminal protocol construction into `useTerminalSession`, where `createTerminalProtocolClient()` wraps `RtcSession.openTerminal()`.
- Removed terminal protocol responsibilities from `BrowserRtcSession`; it now returns raw terminal DataChannels.
- Changed `TerminalProps.session` and `useTerminalSession` to consume `RtcSession`.
- Kept local signaling extraction as Slice 5 and compatibility deletion as Slice 10.

### Remaining Risk

- Compatibility re-export `src/localTerminalProtocolTransport.ts` still contains old name until Slice 10 cleanup.
- `TerminalClient` UI wrapper and `TerminalProtocolClient` protocol adapter are both present; their naming is distinct, but future cleanup should keep responsibilities explicit.

### Next Step

Slice 5: introduce `LocalRtcConnector` so local HTTP signaling returns a connected `RtcSession`; remove direct session construction/connection from `localWebEntry.tsx`.

## Slice 5: Local Connector And Terminal Runtime Boundary

### Goal

Move local HTTP offer/answer/signing into a connector and make terminal UI/hook consumers depend on the platform-neutral `RtcSession` rather than browser or terminal protocol implementation types.

### Failing Tests First

Failing tests first.

- Added `src/localRtcConnector.test.ts`.
- Added `src/terminalRuntimeBoundary.test.ts`.
- Initial command: `npm test -- --run src/localRtcConnector.test.ts src/terminalRuntimeBoundary.test.ts`
- Result: failed because `LocalRtcConnector` did not exist, `LocalRemoteApp` still used `createTransport` / old local transport factory naming, and the browser adapter still exposed terminal protocol responsibilities.

### Implementation

- Added `src/localRtcConnector.ts`.
- Added `RtcSessionDescription`, `RtcSessionNegotiationTarget`, and `RtcSessionNegotiator` to `src/transport.ts`.
- Extended `RtcBinaryChannel` with `onMessage()`, `onClose()`, and `waitOpen()` so protocol clients can consume platform-neutral binary channels without browser types.
- Changed `BrowserRtcSession` to implement `RtcSession & RtcSessionNegotiator`.
- Replaced browser-session `connect()` with `createOffer()` and `acceptAnswer()`.
- Removed terminal protocol construction, protocol maps, `subscribeTerminal()`, and `closeTerminalChannel()` from `BrowserRtcSession`.
- Changed `BrowserRtcSession.openTerminal()` to return a raw `RtcBinaryChannel` over `terminal:*`.
- Changed `LocalRemoteApp` props from `createTransport` / local transport factory to `connector: RtcConnector`.
- Changed `localWebEntry.tsx` to build a browser local connector using `createLocalRtcConnector()` plus `createBrowserRtcSession()`.
- Changed `Terminal.tsx` and `useTerminalSession.tsx` so the UI receives an `RtcSession`; `useTerminalSession` internally creates a `TerminalProtocolClient` over `session.openTerminal()`.
- Split `TerminalProtocolSession.openTerminal()` return type from `RtcBinaryChannel` into a smaller `TerminalProtocolChannel`, because the terminal UI sends protocol-intent JSON to the protocol client rather than subscribing to raw DataChannel frames.
- Updated terminal, file, local app, and local web entry tests for the connector/session boundary.

### Renames

- `LocalRemoteTransportFactory` / `createTransport` usage -> `LocalRemoteSessionConnector` / `connector`.
- Browser session local signaling method `connect()` -> `createOffer()` + `acceptAnswer()`.
- Terminal UI prop `transport` -> `session` in the runtime session boundary.

### Deleted Old Abstractions

- Deleted browser adapter runtime dependence on `TerminalProtocolSession`.
- Deleted `BrowserRtcSession` methods that made terminal protocol look like part of browser transport: `subscribeTerminal()` and `closeTerminalChannel()`.
- Deleted local signaling responsibilities from `BrowserRtcSession`.
- Did not delete `localWebRtcTransport.ts` and `localTerminalProtocolTransport.ts` compatibility files yet; they remain deferred to Slice 10.

### Commands

- `npm test -- --run src/localRtcConnector.test.ts src/terminalRuntimeBoundary.test.ts` failed first.
- `npm test -- --run src/localRtcConnector.test.ts src/terminalRuntimeBoundary.test.ts src/browserRtcSession.behavior.test.ts src/browserRtcSession.test.ts src/terminalProtocolClient.test.ts src/terminalProtocolClient.behavior.test.ts` passed after initial implementation.
- `npm test -- --run src/LocalRemoteApp.test.tsx src/LocalRemoteApp.files.test.tsx src/localWebEntry.test.tsx src/Terminal.test.tsx src/useTerminalSession.test.tsx src/fileApi.test.ts src/useFileManager.test.tsx src/FileManager.test.tsx src/transport.test.ts` exposed lifecycle/test-boundary regressions.
- `npm test -- --run src/useTerminalSession.test.tsx src/Terminal.test.tsx src/LocalRemoteApp.test.tsx` passed after fixing protocol mock timing and async reattach assertions.
- `npm test -- --run src/localRtcConnector.test.ts src/terminalRuntimeBoundary.test.ts src/browserRtcSession.behavior.test.ts src/browserRtcSession.test.ts src/terminalProtocolClient.test.ts src/terminalProtocolClient.behavior.test.ts src/LocalRemoteApp.files.test.tsx src/localWebEntry.test.tsx src/fileApi.test.ts src/useFileManager.test.tsx src/FileManager.test.tsx src/transport.test.ts src/terminalClient.test.ts` passed.
- `npm run typecheck` failed once on a stale local-web connector mock and `TerminalProtocolClient.openTerminal()` return type, then passed.
- `npm test -- --run` passed: 29 files, 116 tests.
- Review-fix command: `npm test -- --run src/terminalProtocolClient.behavior.test.ts src/useTerminalSession.test.tsx src/localRtcConnector.test.ts src/browserRtcSession.behavior.test.ts src/transport.test.ts` failed first on the new review-finding tests, then passed.
- Review-fix command: `npm run typecheck && npm test -- --run` failed once on exact optional `signal`, then passed: 29 files, 121 tests.

### Review

- Independent sub-agent review completed after Slice 5 verification.

### Review Findings

- High: `TerminalProtocolClient` emitted `closed` on channel close but did not reject pending `hello` / `attach` / `snapshot` requests, so `openTerminal()` could hang during DataChannel close.
- High: `useTerminalSession` left pending raw terminal channels without a cleanup path if the hook unmounted before `session.openTerminal()` resolved.
- Medium: `LocalRtcConnector` ignored `RtcConnectOptions.signal`, so rapid switching/unmount could not abort local signaling.
- Medium: `LocalRtcConnector` did not validate that the local answer `sessionId` matched the created offer.
- Medium: `BrowserRtcSession.createOffer(input.path)` ignored the negotiated path and `getConnectionInfo()` could report `local` for future public/managed connectors.
- Medium: `RtcBinaryChannel.onMessage()` and `onClose()` returned `void`, preventing platform-neutral listener cleanup.
- Low: Slice 5 log still had pending review placeholders before this update.
- Low: old compatibility re-export files still expose old transport names; this remains scheduled for Slice 10.

### Review Fixes

- Added behavior tests for pending terminal protocol handshake rejection on channel close.
- Added hook lifecycle test proving raw channels resolving after unmount are closed.
- Added connector tests for abort propagation and stale answer session mismatch.
- Added browser adapter test proving connection info reports the path passed to `createOffer()`.
- Changed `RtcBinaryChannel.onMessage()` and `onClose()` to return `RtcSubscription` and updated browser adapter plus tests/mocks.
- `TerminalProtocolClient` now stores listener subscriptions and rejects all pending requests when the channel closes or is closed explicitly.
- `useTerminalSession` now closes a raw channel if it resolves after the protocol wrapper was closed.
- `LocalRtcConnector` now passes `RtcConnectOptions` into `LocalAgentApi.createRTCAnswer()`, checks abort state between signaling stages, validates `answer.sessionId`, and disconnects on all failure paths.
- `LocalAgentApi.createRTCAnswer()` now forwards `AbortSignal` to `fetch`.
- `BrowserRtcSession` now records the negotiated `ConnectionPath` from `createOffer()` and reports it via `getConnectionInfo()`.

### Remaining Risk

- `BrowserRtcInventoryEventsConnection` still performs local signaling inside the browser adapter module for inventory events. It is not runtime transport, but the final events/API slice should decide whether to keep it as a browser signaling helper or move it behind a connector.
- `LocalRemoteApp` terminal create/update/delete still call `LocalAgentApi` HTTP APIs after a runtime session exists. Slice 8/9 must move runtime API operations behind `RtcSession.openApi()` and capability checks.
- Test helper names still include `Transport` in `mockTerminalTransport.ts` and `mockFileTransport.ts`; this is cleanup work for Slice 10 unless it starts confusing behavior boundaries earlier.
- Compatibility re-exports `localWebRtcTransport.ts` and `localTerminalProtocolTransport.ts` still expose old names pending Slice 10 deletion.

### Next Step

Slice 6: add public P2P signaling / rendezvous connector abstraction so HTTP rendezvous remains pre-runtime and returns the same connected `RtcSession` interface.

## Slice 6: Public P2P Rendezvous Connector

### Goal

Add the frontend public P2P signaling connector boundary so public rendezvous HTTP is pre-runtime only and returns a connected platform-neutral `RtcSession`.

### Failing Tests First

Failing tests first.

- Added `src/publicP2pRtcConnector.test.ts`.
- Initial command: `npm test -- --run src/publicP2pRtcConnector.test.ts`.
- Result: failed because `src/publicP2pRtcConnector.ts` did not exist.

### Implementation

- Added `src/publicP2pRtcConnector.ts`.
- Added `PublicP2pConnectInput`, `PublicP2pRendezvousAdapter`, `PublicP2pRendezvousChannel`, and `PublicP2pRendezvousMessage`.
- Implemented `createPublicP2pRtcConnector()`.
- Connector creates a channel through rendezvous, creates a WebRTC offer through `RtcSessionNegotiator`, posts the signed offer, polls events for an answer, validates answer session id, accepts the answer, and returns the same session as `RtcSession`.
- Added source-boundary test proving the connector does not implement runtime terminal/api/file/events methods and does not reference browser WebRTC, WebSocket, or old relay/transport taxonomy names.
- After review, connector now passes public STUN/ICE metadata through the platform-neutral negotiation target, polls multiple times for answers, verifies matching answers when a verifier is provided, and closes the rendezvous channel on failures.

### Renames

- No old code renamed in this slice.
- New names use connector/signaling language: `PublicP2pRtcConnector` and `PublicP2pRendezvousAdapter`.

### Deleted Old Abstractions

- None in this slice.
- This slice adds the correct public P2P connector boundary; old compatibility cleanup remains Slice 10.

### Commands

- `npm test -- --run src/publicP2pRtcConnector.test.ts` failed first due to missing module.
- `npm test -- --run src/publicP2pRtcConnector.test.ts` passed after implementation.
- `npm test -- --run src/publicP2pRtcConnector.test.ts && npm run typecheck` failed once on a bad regex, then once on exact optional typing, then passed.
- Review-fix command: `npm test -- --run src/publicP2pRtcConnector.test.ts` failed first on the new review-finding tests, then passed.
- Review-fix command: `npm run typecheck && npm test -- --run src/publicP2pRtcConnector.test.ts src/localRtcConnector.test.ts src/browserRtcSession.behavior.test.ts src/transport.test.ts` passed.

### Review

- Independent sub-agent review completed after Slice 6 verification.

### Review Findings

- High: answer selection accepted the first `answer` message and only validated session id after selection, so stale/foreign answers could break a valid connection; same-session injected answers were not verifiable.
- High: rendezvous-provided public STUN servers were posted in the signaling payload but did not reach `createSession()` / `createOffer()`, weakening real P2P ICE semantics.
- Medium: `pollEvents()` was called once even though public rendezvous is naturally polling/long-polling.
- Medium: failures after channel creation had no rendezvous cleanup hook.
- Medium: abort options were not propagated to signing, offer creation, or answer application.
- Low: tests were connector-unit mocks and source-boundary checks; they needed coverage for delayed/multiple answers, verification, and abort cleanup.
- Low: Slice 6 log still had pending review placeholders before this update.

### Review Fixes

- Added tests for multiple poll batches, stale answers, untrusted answers, verified matching answers, abort propagation, and rendezvous cleanup.
- Extended `PublicP2pRendezvousAdapter` with optional `closeChannel()`.
- Extended `PublicP2pRtcConnectorOptions.signOffer()` and optional `verifyAnswer()` with `RtcConnectOptions`.
- Added `maxAnswerPolls` and retry loop around rendezvous `pollEvents()`.
- Changed answer parsing to return all answer candidates, skip stale session ids, and call `verifyAnswer()` for same-session answers when provided.
- Moved session creation until after rendezvous channel creation so public STUN metadata can be passed into `createOffer()`.
- Extended platform-neutral `RtcSessionNegotiationTarget` with optional ICE server metadata and `RtcSessionNegotiator` methods with optional `RtcConnectOptions`.
- Added cleanup to disconnect the session and close the rendezvous channel on failures.

### Remaining Risk

- Public P2P connector currently relies on an injected `PublicP2pRendezvousAdapter`; the concrete HTTP adapter can be added when a remote public entrypoint needs it.
- ICE candidate trickle is represented in the rendezvous message shape but not implemented by the current `RtcSessionNegotiator`; current browser adapter waits for gathered local candidates in SDP.
- The default connector behavior trusts same-session answers if no `verifyAnswer()` is provided; production public P2P entrypoints should provide answer verification.
- Public P2P connector is not yet wired into UI route selection; Slice 6 only stabilizes the connector boundary.

### Next Step

Slice 7: add managed signaling connector abstraction so Hub / ICE / relay policy remains pre-runtime/capability data and returns the same `RtcSession`.

## Slice 7: Managed Signaling Connector

### Goal

Add the managed signaling connector boundary so Hub polling/submitting, ICE server policy, and relay policy remain pre-runtime or capability data while runtime still returns one managed `RtcSession`.

### Failing Tests First

Failing tests first.

- Added `src/managedRtcConnector.test.ts`.
- Initial command: `npm test -- --run src/managedRtcConnector.test.ts`.
- Result: failed because `src/managedRtcConnector.ts` did not exist.

### Implementation

- Added `src/managedRtcConnector.ts`.
- Added `ManagedRtcConnectInput`, `ManagedSignalingAdapter`, `ManagedSignalingOffer`, `ManagedSignalingAnswer`, `ManagedRelayPolicy`, `ManagedIceServer`, and `ManagedRtcAnswerer`.
- Implemented `createManagedRtcConnector()`.
- Connector polls a managed signaling offer, asks an injected session/answerer to accept the managed offer, validates session id, submits the answer, and returns the same `RtcSession`.
- Relay policy is passed to the session as policy/capability input and `relayInUse` as connection result data; no relay connection path or relay transport interface is introduced.
- Added source-boundary test proving the connector does not implement runtime terminal/api/file/events methods and does not reference browser WebRTC, WebSocket, or old relay/transport taxonomy names.
- After review, connector now validates managed offer target identity, rejects contradictory relay policy/result combinations, passes signaling session id and options into the answerer, and can reject stale Hub offers through the signaling adapter.

### Renames

- No old code renamed in this slice.
- New names use managed connector/signaling language: `ManagedRtcConnector`, `ManagedSignalingAdapter`, and `ManagedRtcAnswerer`.

### Deleted Old Abstractions

- None in this slice.
- The slice prevents `relay` from growing into a fourth client transport by modeling it only as `relayPolicy` and `relayInUse`.

### Commands

- `npm test -- --run src/managedRtcConnector.test.ts` failed first due to missing module.
- `npm test -- --run src/managedRtcConnector.test.ts && npm run typecheck` failed once because the test expectation omitted `relayInUse`, then passed.
- `npm test -- --run src/managedRtcConnector.test.ts src/publicP2pRtcConnector.test.ts && npm run typecheck` passed.
- Review-fix command: `npm test -- --run src/managedRtcConnector.test.ts` failed first on the new review-finding tests, then passed.
- Review-fix command: `npm test -- --run src/managedRtcConnector.test.ts && npm run typecheck` passed.

### Review

- Independent sub-agent review completed after Slice 7 verification.

### Review Findings

- High: managed offers were not identity-validated; stale or foreign Hub offers could be accepted.
- High: `ManagedRtcAnswerer` did not receive Hub `offer.sessionId`, forcing out-of-band session id behavior.
- Medium: abort/options were not propagated into `acceptOffer()` and there was no post-answer abort check.
- Medium: relay policy semantics allowed `relayInUse: true` while policy disallowed relay.
- Medium: failures after polling an offer had no signaling-side reject/nack hook.
- Medium: tests were over-mocked and lacked mismatched offer, relay contradiction, and cleanup coverage.
- Low: source-boundary test was useful but incomplete.
- Low: Slice 7 log still had pending review placeholders before this update.

### Review Fixes

- Added tests for foreign managed offers and contradictory relay policy.
- Added optional `rejectOffer()` to `ManagedSignalingAdapter`.
- Added `sessionId` and `RtcConnectOptions` to `ManagedRtcAnswerer.acceptOffer()`.
- Added managed offer machine/terminal validation before answer creation.
- Added relay policy validation so relay result cannot contradict `allowRelay`.
- Added post-answer abort check and signaling reject cleanup on failures after an offer has been polled.

### Remaining Risk

- Managed connector currently relies on an injected `ManagedSignalingAdapter`; concrete Hub HTTP adapter can be added with a managed remote entrypoint.
- Browser adapter does not yet implement `ManagedRtcAnswerer`; current task has stabilized the public frontend boundary first. The browser answerer or native answerer can implement the same interface without changing business consumers.
- Managed connector is not yet wired into UI route selection.

### Next Step

Slice 8: move API/file/events consumption behind unified `RtcSession` capabilities and remove the remaining runtime HTTP management path from `LocalRemoteApp`.

## Slice 8: Unified Session API Consumption

### Goal

Move terminal management runtime operations behind the unified `RtcSession.openApi()` boundary and keep file/API consumers on the same session interface without changing file API semantics.

### Failing Tests First

Failing tests first.

- Added `src/terminalManagementApi.test.ts` to define direct terminal management protocol calls over `RtcSession.openApi()`.
- Updated `src/LocalRemoteApp.test.tsx` management tests so `LocalAgentApi.createTerminal()`, `updateTerminal()`, and `deleteTerminal()` throw if used at runtime.
- Initial command: `npm test -- --run src/terminalManagementApi.test.ts && npm run typecheck`.
- Result: failed until `src/terminalManagementApi.ts` existed and produced the expected direct `create`, `set_metadata`, and `remove` API calls.

### Implementation

- Added `src/terminalManagementApi.ts`.
- `createTerminalManagementApi(session, machineId)` opens and caches the session API channel via `RtcSession.openApi()`.
- Terminal create now sends direct protocol method `create` with command, optional name, cwd/env, and metadata tags.
- Terminal update now sends direct protocol method `set_metadata` with terminal id, optional name, and metadata tags.
- Terminal delete now sends direct protocol method `remove` with terminal id.
- `LocalRemoteApp` now creates a short-lived runtime session through its connector for create/update/delete, wraps it with `createTerminalManagementApi()`, and disconnects after the management operation.
- Updated `src/test/mockFileTransport.ts` request normalization so tests can exercise both existing file API `{ path, params }` requests and direct protocol API method calls.

### Renames

- No file rename in this slice.
- Runtime terminal management wording moved from local HTTP API usage to session management API usage.

### Deleted Old Abstractions

- Removed runtime use of `LocalAgentApi.createTerminal()`, `LocalAgentApi.updateTerminal()`, and `LocalAgentApi.deleteTerminal()` from `LocalRemoteApp`.
- HTTP remains only for local status, inventory before a session, pairing, and WebRTC offer/answer signaling.

### Commands

- `npm test -- --run src/terminalManagementApi.test.ts && npm run typecheck` passed after implementation.
- `npm test -- --run src/LocalRemoteApp.test.tsx src/terminalManagementApi.test.ts` initially produced an unhandled mock route for `set_metadata`; fixed the test responder so the session API call completes.
- `npm test -- --run src/LocalRemoteApp.test.tsx src/terminalManagementApi.test.ts` passed.
- `npm run typecheck` passed.
- `npm test -- --run src/fileApi.test.ts src/useFileManager.test.tsx src/FileManager.test.tsx src/LocalRemoteApp.files.test.tsx` passed.

### Review

Independent sub-agent review completed after Slice 8 verification.

### Review Findings

- Critical: `terminalManagementApi.ts` sent direct `create` / `set_metadata` / `remove` calls, but `BrowserRtcSession` rejected API params without a file-style `path`; the mock was masking real browser runtime failure.
- Critical: termx-core `api` DataChannel handler only routed file manager requests through `fileapi.Manager`, so terminal management direct methods would be unknown file routes.
- High: terminal creation required an existing terminal-specific session, preventing creation of the first terminal and coupling management to a terminal channel.
- Medium: UI management capability was still gated by optional HTTP management methods on `LocalAgentApi`.
- Medium: terminal create accepted malformed responses with missing `terminal_id`.

### Review Fixes

- Added browser adapter behavior coverage proving direct API protocol methods are serialized as `{ method, path: method, body }` on the `api` DataChannel.
- Changed `BrowserRtcSession` API normalization so direct runtime API methods and existing file API `{ path, params }` requests both work over the same channel.
- Added termx-core runtime API routing for terminal management through an injected `TerminalManagementRouter`; file routes still use `fileapi.Manager`.
- Wired local WebRTC answer creation to the terminal management router via `remote_localweb.go`, without wrapping HTTP as runtime transport.
- Added machine-scoped local RTC API sessions: `connector.connect({ machineId })` can open `api` without a terminal id, so the first terminal can be created over DataChannel.
- Differentiated machine inventory events signaling as `client.transport: local-events`; empty `terminal_id` with normal `local` signaling now means machine-scoped API session.
- Added capability checks to `createTerminalManagementApi()` before opening the API channel and made malformed create responses a review follow-up risk for Slice 10 if the server shape changes.

### Remaining Risk

- `subscribeEvents()` still needs final capability/policy treatment so events are clearly exposed through the session rather than local HTTP or a side channel.
- Test helper names still include old `Transport` wording and are scheduled for Slice 10 cleanup.

### Next Step

Slice 9: add capability / policy model tests and implementation so relay usage, file transfer allowance, terminal/API/events allowance, and management permission are represented as capabilities instead of transport taxonomy.

## Slice 9: Capability And Policy Model

### Goal

Model paid/anonymous/relay differences as `ConnectionCapabilities` / policy decisions, not as connection path taxonomy or HTTP capability flags.

### Failing Tests First

Failing tests first.

- Added `src/connectionPolicy.test.ts`.
- Extended `src/terminalManagementApi.test.ts` so terminal management checks `getConnectionInfo()` and `getCapabilities()` before opening `api`.
- Extended `src/LocalRemoteApp.test.tsx` to prove management controls are exposed by policy instead of optional HTTP management methods and hidden when policy denies management.
- Added a first-terminal creation test proving terminal creation can use a machine-scoped runtime API session with no existing terminal id.
- Initial command: `npm test -- --run src/connectionPolicy.test.ts src/terminalManagementApi.test.ts src/LocalRemoteApp.test.tsx` failed because `connectionPolicy.ts` did not exist, management API did not check capabilities, and UI still gated management on HTTP methods.

### Implementation

- Added `src/connectionPolicy.ts` with `ConnectionCapabilityName`, `evaluateConnectionCapability()`, `requireConnectionCapability()`, and `createConnectionPolicySnapshot()`.
- Extended `ConnectionCapabilities` with `terminalManagementAllowed`.
- Changed `LocalRemoteApp` management gating to use capability/policy input instead of `LocalAgentApi.createTerminal/updateTerminal/deleteTerminal` presence.
- Changed terminal management runtime API to require `terminal_management` capability before opening the API channel.
- Added machine-scoped local RTC connection support for runtime API sessions.
- Added `local-events` signaling marker for machine inventory events so empty `terminal_id` is no longer ambiguous.

### Renames

- No file rename in this slice.
- Capability names use policy terminology: `terminal`, `api`, `events`, `file_transfer`, and `terminal_management`.

### Deleted Old Abstractions

- Removed `LocalRemoteAppProps.api` dependency on HTTP terminal management methods.
- Removed the UI feature flag that treated HTTP method presence as terminal management capability.
- Did not introduce `relay` as a connection path; relay remains `relayInUse` plus capability decisions such as `fileTransferAllowed`.

### Commands

- `npm test -- --run src/connectionPolicy.test.ts src/terminalManagementApi.test.ts src/LocalRemoteApp.test.tsx` failed first.
- `npm test -- --run src/browserRtcSession.behavior.test.ts` failed first on direct API method serialization, then passed after the browser adapter fix.
- `go test ./internal/remote/rtc` failed first on missing runtime API router, then passed after adding `TerminalManagementRouter`.
- `go test ./...` passed after routing terminal management and preserving machine inventory event signaling.
- `npm test -- --run src/localRtcConnector.test.ts src/LocalRemoteApp.test.tsx src/localAgentApi.test.ts src/browserRtcSession.behavior.test.ts` passed after machine-scoped API session support.
- `npm run typecheck` passed after capability mocks and exact optional types were updated.
- `npm test -- --run src/localRtcConnector.test.ts src/LocalRemoteApp.test.tsx src/localAgentApi.test.ts src/browserRtcSession.behavior.test.ts src/connectionPolicy.test.ts src/terminalManagementApi.test.ts` passed.
- Review-fix command: `npm test -- --run src/terminalManagementApi.test.ts src/localAgentApi.test.ts src/localRtcConnector.test.ts src/browserRtcSession.behavior.test.ts` failed first on six new review regression tests.
- Review-fix command: `npm test -- --run src/terminalManagementApi.test.ts src/localAgentApi.test.ts src/localRtcConnector.test.ts src/browserRtcSession.behavior.test.ts src/localAppIdentity.test.ts src/LocalPairPanel.test.tsx && npm run typecheck` passed.
- Review-fix command: `go test ./internal/remote/pairing ./internal/remote/runtime -run 'TestClaimSessionAllowsTerminalManagementCapabilitySeparatelyFromFileManager|TestManagerProvidesTerminalManagementRouterForManagedRTC'` failed first, then passed.
- Review-fix command: `go test . -run TestLocalRTCAnswerSeparatesTerminalManagementCapabilityFromFileManager` failed first on helper usage, then passed.
- Review-fix command: `go test ./internal/remote/rtc -run TestRuntimeAPIChannelRouterHandlesTerminalManagement` passed.
- Review-fix command: `npm test -- --run` passed: 34 files, 150 tests.
- Review-fix command: `go test ./...` in `termx-core` passed.

### Review

Independent sub-agent review completed after Slice 9 verification.

### Review Findings

- Critical: browser sessions hard-coded every capability to allowed and `relayInUse: false`; real local signaling dropped server `data_channels`, so capability tests proved only mocks.
- Critical: managed Hub runtime API did not inject a `TerminalManagementRouter`, so direct `create` / `set_metadata` / `remove` would be forbidden on managed sessions.
- High: `createTerminalManagementApi()` accepted a machine id but did not validate the connected session machine.
- High: local RTC policy conflated `file_manager` permission with terminal management permission.
- High: browser WebRTC sessions ignored signaling-provided ICE server configuration.
- Medium: `createTerminal()` accepted malformed management responses and returned an empty terminal id.
- Medium: terminal-management behavior was split across mocks/router units and needed stronger real-path coverage.

### Review Fixes

- Added review regression tests for server-negotiated `data_channels` and explicit `capabilities`, browser ICE server configuration, machine mismatch rejection, malformed terminal create response rejection, independent `terminal_management` pairing, local management-only API sessions, and managed runtime terminal management router availability.
- Extended `LocalRTCAnswer` with `dataChannels` and `capabilities`; normalized `data_channels` and `capabilities` in `createLocalAgentApi()`.
- Added platform-neutral `RtcSessionCapabilityUpdater` so connectors can apply negotiated `ConnectionCapabilities` without browser type leakage.
- Updated `LocalRtcConnector` to apply explicit server-negotiated `ConnectionCapabilities` to connected sessions, falling back to conservative DataChannel-label projection only when explicit capabilities are absent.
- Updated `BrowserRtcSession` to accept signaling ICE servers in the peer connection configuration and store negotiated capabilities rather than hard-coded browser defaults.
- Updated terminal management API to validate session `machineId` before opening `api` and reject missing terminal id responses.
- Added independent `terminal_management` capability to pairing and default local web pairing requests.
- Updated local RTC answer policy so machine-scoped management sessions require `terminal_management`, file manager permission no longer implies management, terminal-scoped sessions enable file and management independently, and the signaling response returns explicit capability fields.
- Updated managed Hub answer creation to use `AnswerOfferWithOptions()` with terminal management router injection when available.

### Remaining Risk

- `BrowserRtcSession.subscribeEvents()` still returns a no-op subscription; machine inventory events currently use `BrowserRtcInventoryEventsConnection` as a local browser helper pending the final events cleanup.
- Capability denial uses one optional `denialReason`; future policy may need per-capability denial reasons without changing `RtcSession`.
- There is still no single browser-to-Go in-process integration test that drives `BrowserRtcSession` against Pion directly; Go API route and browser serialization are both covered, but cross-runtime integration remains future work.
- Compatibility files and test helper names still contain old `Transport` wording pending Slice 10.

### Next Step

Slice 10: delete compatibility re-export files, rename old transport-named test helpers and local wording, and remove remaining wrong naming / compatibility residue.

## Slice 10: Legacy Transport Cleanup

### Goal

Remove compatibility residue from the old local/terminal/file transport naming so the source tree no longer keeps old modules or helper names as a parallel abstraction.

### Failing Tests First

Failing tests first.

- Added `src/legacyTransportCleanup.test.ts`.
- The test asserts that `localWebRtcTransport.ts` and `localTerminalProtocolTransport.ts` are not present as compatibility modules.
- The test asserts that the package barrel does not export old local WebRTC / terminal transport aliases or browser RTC types.
- The test asserts that file manager target validation is named as session validation, not transport validation.
- Initial command: `npm test -- --run src/legacyTransportCleanup.test.ts`.
- Result: failed first because the compatibility modules still existed and file manager validation still used transport wording.

### Implementation

- Deleted `src/localWebRtcTransport.ts` and `src/localTerminalProtocolTransport.ts`.
- Replaced file manager validation wording with `assertSessionTarget()` and `file session ...` errors.
- Renamed test helpers:
  - `src/test/mockFileTransport.ts` -> `src/test/mockFileSession.ts`
  - `src/test/mockTerminalTransport.ts` -> `src/test/mockRtcTerminalSession.ts`
- Renamed helper exports to `MockFileSession`, `createMockFileSession`, `MockRtcTerminalSession`, and `createMockRtcTerminalSession`.
- Renamed test objects and titles that still described `TerminalProtocolClient` or `RtcSession` as a transport.
- Changed the cleanup test to use Vite `import.meta.glob()` instead of Node built-in modules so `tsc --noEmit` does not require Node ambient types.

### Renames

- `MockFilePeerTransport` -> `MockFileSession`.
- `createMockFilePeerTransport()` -> `createMockFileSession()`.
- `MockTerminalTransport` -> `MockRtcTerminalSession`.
- `createMockTerminalTransport()` -> `createMockRtcTerminalSession()`.
- `assertTransportTarget()` -> `assertSessionTarget()`.
- Test wording changed from file/terminal transport interface to file session or terminal protocol interface.

### Deleted Old Abstractions

- Deleted local WebRTC compatibility module.
- Deleted local terminal protocol compatibility module.
- Deleted old transport-named test helper files.
- No replacement wrapper was introduced; call sites now import browser adapter, `RtcSession`, or terminal protocol client directly.

### Commands

- `npm test -- --run src/legacyTransportCleanup.test.ts` failed first as expected.
- `npm test -- --run src/legacyTransportCleanup.test.ts src/fileApi.test.ts src/useFileManager.test.tsx src/FileManager.test.tsx src/Terminal.test.tsx src/useTerminalSession.test.tsx src/terminalProtocolClient.behavior.test.ts` passed.
- `npm run typecheck` failed once because the cleanup test imported Node built-ins without Node ambient types.
- `npm test -- --run src/legacyTransportCleanup.test.ts` passed after switching to `import.meta.glob()`.
- `npm run typecheck` passed.
- `npm test -- --run` passed: 34 files, 145 tests.

### Review

Independent sub-agent review completed after an initial timeout. A forced self-review was also performed while waiting so the slice did not stall.

### Review Findings

- High: real browser `managed` path was mock-only; `ManagedRtcConnector` expected `acceptOffer()`, but `BrowserRtcSession` only created local offers and hard-coded `relayInUse: false`.
- High: local signaling used `client.transport: local/local-events`, preserving client-selected transport taxonomy in a pre-runtime payload.
- High: package barrel exported `BrowserRtcSessionOptions` and related browser adapter types transitively exposing browser implementation details.
- Medium: business lifecycle messages used `transport.connecting/connected/...` instead of connection/session wording.
- Medium: cleanup tests were mostly source string checks and missed the transitive browser type leak and lifecycle naming.
- High, second pass: `BrowserRtcSession.subscribeEvents()` was a no-op while events are part of the unified runtime session.
- Medium, second pass: managed relay file-transfer denial was reported in capabilities but not enforced by `openFileTransfer()`.
- Medium, second pass: one managed connector test claimed abort-option forwarding but actually rejected before calling the answerer.

### Review Fixes

- Added platform-neutral `RtcSessionAnswerTarget` / `RtcSessionAnswerer` and made `BrowserRtcSession` implement answerer behavior without importing managed connector or browser types into `transport.ts`.
- Implemented browser managed answerer flow: set remote offer, create local answer, wait for ICE gathering, apply ICE/TURN config, record `relayInUse`, and derive capability results from relay policy.
- Added incoming DataChannel registration for answerer role and explicit offerer/answerer channel ownership, so `managed` path alone does not imply incoming channels.
- Renamed UI lifecycle messages from `transport.*` to `connection.*`.
- Renamed local signaling client field from `transport` to `purpose` and updated termx-core parsing/tests from `local-events` to `inventory_events`.
- Removed browser adapter exports from `src/index.ts` so the package barrel only exposes platform-neutral public surfaces.
- Strengthened cleanup tests to assert the package barrel does not export browser adapter symbols and lifecycle messages do not use `transport.*`.
- Implemented `RtcSession.subscribeEvents()` over the `events` DataChannel for both offerer-created and answerer-incoming browser channels.
- Enforced `fileTransferAllowed` in `BrowserRtcSession.openFileTransfer()` so managed relay policy denial fails immediately with a policy error instead of opening or waiting for a `file:*` channel.
- Split the managed connector test into contradictory relay-policy rejection and real abort-option forwarding coverage.

### Commands

- `npm test -- --run src/connectionMessageReducer.test.ts src/eventQueue.test.ts src/localAgentApi.test.ts src/legacyTransportCleanup.test.ts src/browserRtcSession.behavior.test.ts src/managedRtcConnector.test.ts` passed.
- `npm run typecheck` initially failed on exact optional types and mock answerer signature; passed after fixes.
- `go test ./internal/remote/localweb -run 'RTC|Offer'` passed after signaling `purpose` rename.
- `npm test -- --run` passed after first Slice 10 review fixes: 34 files, 154 tests.
- `npm test -- --run src/browserRtcSession.behavior.test.ts && npm run typecheck` failed first after adding event-channel tests because `subscribeEvents()` had no real listener; passed after implementation.
- `npm test -- --run src/managedRtcConnector.test.ts src/browserRtcSession.behavior.test.ts && npm run typecheck` passed after review-fix coverage.

### Remaining Risk

- Historical docs and logs intentionally mention deleted names for traceability.
- The local pre-runtime inventory events helper still uses a dedicated WebRTC event connection before a normal runtime session exists; this is documented as pre-runtime/local inventory behavior and should not grow into another runtime transport abstraction.
- There is still no single browser-to-Pion cross-runtime integration test; browser DataChannel serialization and Go DataChannel routing are covered separately.

### Next Step

Slice 11: run full validation, sync embedded local Web UI assets, update final documentation state, and record final review/self-check results.

## Slice 11: Final Validation And Closeout

### Goal

Finish the rewrite by proving the full repo slice builds/tests after cleanup, syncing embedded local Web UI assets, and documenting the final boundary.

### Failing Tests First

No new behavior was introduced in Slice 11 before tests. The failing tests for final review fixes were already recorded under Slice 10: runtime event subscription initially failed because `subscribeEvents()` was no-op, and the managed file-transfer denial test failed until capability enforcement was added.

### Implementation

- Updated architecture documentation to record `purpose`-based signaling, platform-neutral answerer interfaces, explicit browser DataChannel role handling, runtime events over `RtcSession.subscribeEvents()`, and file-transfer policy enforcement.
- Prepared final validation and asset sync after Slice 10 review fixes.

### Renames

- No new source rename in this slice.

### Deleted Old Abstractions

- No new deletion in this slice; Slice 10 completed compatibility deletion.

### Commands

- `npm test -- --run` passed: 34 files, 159 tests. Vitest emitted the existing warning that `--localstorage-file` was provided without a valid path.
- `npm run typecheck` passed.
- `npm run build:localweb` passed and synced `remote-ui/dist` to `termx-core/internal/remote/localweb/static`. Vite emitted the existing warning that the main JS chunk is larger than 500 kB after minification.
- `go test ./...` in `termx-core` passed.

### Review

- Slice 11 uses the completed Slice 10 review findings plus a forced closeout self-review after full validation. No additional code behavior was introduced after the final validation commands.

### Review Findings

- Public `transport.ts` remains browser-free and exposes only platform-neutral runtime/session/connector/capability types.
- Package barrel no longer exports browser adapter types.
- Runtime event subscription is now backed by an `events` DataChannel, not a no-op.
- Managed relay remains `path: managed` with `relayInUse` / `fileTransferAllowed`; no fourth client connection path or relay transport taxonomy was added.
- Local signaling now uses `client.purpose` rather than `client.transport`.
- Embedded local web assets were regenerated after the final frontend build.

### Review Fixes

- No new fixes were required after final validation.

### Remaining Risk

- The local pre-runtime inventory event helper remains a dedicated WebRTC connection before a normal runtime session exists. It is documented as pre-runtime inventory behavior and should not be treated as a runtime transport.
- There is no full browser-to-Pion end-to-end integration test in this slice. Browser DataChannel behavior and Go DataChannel routing are tested separately.
- Vite reports the main local web JS chunk above 500 kB; this is a build warning, not a rewrite correctness failure.

### Next Step

Future work can add native Android/iOS adapters implementing the same `RtcSession` / `RtcSessionAnswerer` interfaces and add a cross-runtime WebRTC integration test harness.
