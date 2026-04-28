# termx Remote Refactor TODO

Status legend:

- `[x]` done
- `[>]` in progress
- `[ ]` pending
- `[!]` blocked or needs decision

## M0. Spec Freeze

- `[>]` Write and review the target remote architecture spec.
- `[>]` Record milestone plan in-repo before further implementation.
- `[ ]` Commit the spec-only slice after review.

Acceptance:

- Spec documents the target process model, identity model, hub/relay discovery model, and terminal-only app scope.
- The spec explicitly preserves `termx` flat terminal semantics and binary recovery payloads.

## M1. Config and Identity Foundation

- `[ ]` Add failing tests for remote config parsing and persistence in `termx.yaml`.
- `[ ]` Add failing tests proving daemon startup still works through CLI flags and auto-start after config changes.
- `[ ]` Extend config model with control-plane and remote-runtime sections.
- `[ ]` Add failing tests for device identity store creation, reload, and file permissions.
- `[ ]` Implement local device key generation and state persistence.
- `[ ]` Add CLI entry points for `login/logout/whoami/remote status`.
- `[ ]` Document config and local identity layout.

Acceptance:

- A daemon can be configured for remote mode without starting the remote stack yet.
- Device identity is generated once, persisted safely, and reloadable.

## M2. Embedded Remote Runtime

- `[ ]` Write failing tests for a shared runtime that can be started and stopped by `termx daemon`.
- `[ ]` Decide and test the first safe integration seam: existing client/protocol path, in-memory transport, or an extracted public transport session hook.
- `[ ]` Extract runtime responsibilities out of any standalone-agent-shaped code introduced later.
- `[ ]` Make `termx daemon` supervise the embedded runtime lifecycle.
- `[ ]` Add fail-fast validation for incomplete remote config.
- `[ ]` Keep `termx daemon` healthy when remote mode is disabled or discovery is unavailable.

Acceptance:

- Remote runtime can be enabled from daemon startup without a separate user-facing process.
- Local daemon behavior remains unchanged when remote mode is off.

## M3. Control-Plane Discovery and Device Registration Contract

- `[ ]` Write failing tests for hub catalog discovery and registration contract types.
- `[ ]` Add minimal control-plane auth/token config path used by the daemon.
- `[ ]` Define device registration API using public-key identity, not private-key escrow.
- `[ ]` Require proof-of-possession during device registration; do not rely on a stored `paired`-style flag.
- `[ ]` Define control-plane responses for hub candidates and relay catalog.
- `[ ]` Add ownership and subscription guard tests if subscription logic is retained.

Acceptance:

- A daemon can authenticate, fetch hub candidates, and register its public identity contract.

## M4. Hub Runtime and Signaling Core

- `[ ]` Write failing tests for hub registration, liveness, and offer/answer routing.
- `[ ]` Port the minimal reusable hub runtime from `tgent`.
- `[ ]` Add heartbeat/reporting hooks from hub to control plane.
- `[ ]` Add support for multiple relay/TURN entries in returned ICE config.
- `[ ]` Replace any transitional single-shared-secret internal auth with a replay-resistant hub/control-plane auth shape.
- `[ ]` Keep the runtime terminal-agnostic except for routing requests to the owning daemon.

Acceptance:

- A registered daemon can be found by hub ID and receive signaling for a target device.

## M5. Terminal List and Ticket Flow

- `[ ]` Write failing tests for `devices`, `terminals`, and `connect ticket` APIs.
- `[ ]` Return terminal lists from the daemon via the hub without introducing session trees.
- `[ ]` Issue short-lived tickets bound to device ownership and hub selection.
- `[ ]` Record device online/offline state from hub reports.

Acceptance:

- A logged-in client can discover devices, list terminals, and obtain a connect ticket for one terminal.

## M6. Mobile App Scaffold

- `[ ]` Write failing UI/data tests for login, device list, terminal list, and terminal route state.
- `[ ]` Scaffold a minimal Capacitor + React + WebView app.
- `[ ]` Add only the four primary screens.
- `[ ]` Add session persistence for account auth only.
- `[ ]` Add client-key import placeholder UX without blocking other screens.

Acceptance:

- The app can log in and navigate to a target terminal page without legacy `tgent` product clutter.

## M7. WebRTC Terminal Path

- `[ ]` Write failing tests for browser-side frame transport, reconnect boundaries, and protocol attach flow.
- `[ ]` Add browser-side WebRTC DataChannel transport carrying raw `termx` frames.
- `[ ]` Add browser/mobile protocol client for `attach`, `input`, and `resize`.
- `[ ]` Preserve current attach bootstrap ordering and attach-to-stream buffering semantics in remote clients.
- `[ ]` Bridge binary bootstrap/screen-update payloads into the terminal view.
- `[ ]` Render the live terminal in xterm.js.

Acceptance:

- The app can enter a live terminal and interact with it over WebRTC only.

## M8. Recovery and Resilience

- `[ ]` Write failing tests for reconnect, `SyncLost`, and snapshot recovery.
- `[ ]` Add reconnect flow with fresh ticket acquisition.
- `[ ]` Add binary resnapshot/bootstrap recovery on desync.
- `[ ]` Add basic runtime observability and diagnostics.

Acceptance:

- Lossy or interrupted sessions recover without inventing a separate terminal state protocol.

## M9. Cleanup, Docs, and Deletion of Transitional Paths

- `[ ]` Remove or demote any transitional standalone-agent startup path if it was used during migration.
- `[ ]` Update README and config docs.
- `[ ]` Add local end-to-end smoke instructions.
- `[ ]` Run full regression tests for touched packages.

Acceptance:

- The default documented architecture is `termx daemon` with embedded remote runtime.

## Working Rules For Every Milestone

- `[ ]` Start each implementation slice with a failing test.
- `[ ]` Add characterization tests before reshaping any migrated `tgent` logic.
- `[ ]` Run a findings-first subagent review before each milestone commit.
- `[ ]` Fix blocking findings before committing.
- `[ ]` Keep commits small and milestone-scoped.
