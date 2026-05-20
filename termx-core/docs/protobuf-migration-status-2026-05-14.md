# Protobuf Migration Status - 2026-05-14

This note records the protocol migration state before the next storage
refactor so the context is not lost.

## Current State

- Core control envelopes are protobuf encoded:
  - `Hello`
  - `RequestEnvelope`
  - `ResponseEnvelope`
  - `ErrorEnvelope`
  - `Event`
- Method params/results under the core protocol are now encoded through
  `termx-core/protocol/wirepb/terminal.proto`.
- Binary terminal payloads stay binary:
  - snapshots
  - grid viewports
  - screen updates
  - stream frames
- `termx-remote` runtime messages have a separate protobuf contract in
  `termx-remote/protocol/runtimepb/runtime.proto`.
- JSON compatibility was removed from the program-to-program message path.
  JSON should remain only for human-authored config or explicitly human-readable
  metadata, not for daemon/client protocol messages.

## Boundary

- Third-party clients should be able to implement the daemon wire contract from
  protobuf definitions and the frame format, without importing daemon internals.
- `termx-core` must stay shell-neutral. Product-specific app/client state should
  not become hard-coded daemon fields.
- `tuiv2` is the TUI app name. Avoid the old `ti` shorthand in new protocol and
  storage names.

## Remaining Protocol Work

- Move any remaining program-to-program JSON payloads to protobuf.
- Keep mobile/app migration separate until the Go daemon protocol is stable.
- Add generic app storage as a protobuf-backed core daemon capability so client
  state can be stored and observed without adding app-specific daemon fields.
