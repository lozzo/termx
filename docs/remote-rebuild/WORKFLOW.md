# TermX Remote Rebuild Workflow

Status file for unattended remote rebuild work. Update this file before starting and after completing every todo.

## Current State

- Current phase: P3 embedded local web first
- Active todo: P3-B embedded local web foundation
- Last updated: 2026-05-01T09:42:56+08:00
- Worktree goal before final response: clean after each completed todo commit

## Ordered Todos

| ID | Phase | Todo | Status | Commit |
| --- | --- | --- | --- | --- |
| R0 | workflow | Create and seed `docs/remote-rebuild/WORKFLOW.md` with full todo plan | completed | `8734d00` |
| R1 | planning | Revise remote rebuild plan so the early path builds `termx` embedded local web, shared remote UI components, and local WebRTC-over-TCP before migrating the same UI to mobile app | completed | `6a657be` |
| R2 | planning | Record that remote UI page code, architecture, and component boundaries should stay as synchronized with `../tgent` as practical while TermX message handling should emulate native app behavior where tgent interactions feel too web-like | completed | `0ce0023` |
| P2-A | identity | Implement Ed25519 machine key generation, load, persistence permissions, and fingerprint helpers in `termx-core/internal/remote/identity` | completed | `5aef5b8` |
| P2-B | cert | Implement canonical app certificate payload, sign/verify helpers, and nonce/timestamp replay helper in `termx-core/internal/remote/cert` | completed | `62d1f70` |
| P2-C | pairing | Implement local pair session creation, TTL, single-use semantics, and app certificate issuance in `termx-core/internal/remote/pairing` | completed | `12067cb` |
| P2-D | CLI | Keep `termx remote status` working and add a conservative `termx pair` CLI skeleton only after core primitives exist | completed | `4b24258` |
| P3-A | rendezvous | Implement anonymous rendezvous interfaces/contracts with payload limit, TTL, channel secret verification, and no TURN credentials | completed | `4012a1b` |
| P3-B | localweb | Implement embedded local web foundation served from `termx` binary with local status, terminal list, and pair API contracts | pending |  |
| P3-C | rtc | Implement local WebRTC signaling and ICE TCP mux/over-TCP support for browser-to-daemon local connections | pending |  |
| P3-D | remote-ui | Create shared remote UI package and adapt `Terminal.tsx`, `TerminalList.tsx`, and `FileManager.tsx` from `../tgent` for machine/terminal-only semantics | pending |  |
| P3-E | local/e2e | Wire embedded local web to terminal and file manager over local WebRTC DataChannels and validate in browser before mobile migration | pending |  |
| P3-F | rendezvous | Implement anonymous rendezvous HTTP adapter/service after local embedded web path is stable | pending |  |
| P4-A | mobile | Recreate mobile app shell around the shared remote UI components and replace browser adapters with native/mobile adapters | pending |  |

## TDD Log

### R0 seed persistent workflow file

- Tests written before implementation: none; documentation/workflow-only todo.
- Expected failing test: not applicable.
- Focused tests: not run; documentation/workflow-only todo.
- Broader tests: not run; documentation/workflow-only todo.
- Result: completed. Commit: `8734d00`.

### R1 embedded local web first plan revision

- Tests written before implementation: none; documentation/workflow-only todo.
- Expected failing test: not applicable.
- Focused tests: not run; documentation/workflow-only todo.
- Broader tests: `git diff --check` passed.
- Result: completed. Commit: `6a657be`.

### R2 tgent-aligned UI and native-like message handling constraint

- Tests written before implementation: none; documentation/workflow-only todo.
- Expected failing test: not applicable.
- Focused tests: pending.
- Broader tests: `git diff --check` passed.
- Result: completed. Commit: `0ce0023`.

### P2-A machine key generation/load/fingerprint

- Tests written before implementation: `termx-core/internal/remote/identity/identity_test.go`
- Expected failing test: `go test ./internal/remote/identity` fails to build because `LoadOrCreateMachineKey`, `MachineKeyFilename`, and `MachinePublicKeyFingerprint` do not exist yet.
- Focused tests: failed as expected before implementation.
- Code review regression tests: added concurrent first-run and private-key JSON leak tests; `go test ./internal/remote/identity` fails as expected because `MachineKey.Sign` and hidden `privateKey` do not exist yet.
- Follow-up review regression tests: added formatting redaction test; `go test ./internal/remote/identity` failed as expected before `String`/`GoString` redaction.
- Final focused tests: `cd termx-core && go test ./internal/remote/identity` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `5aef5b8`.

### P2-B app certificate canonical/sign/verify/replay

- Tests written before implementation: `termx-core/internal/remote/cert/cert_test.go`
- Expected failing test: `go test ./internal/remote/cert` fails to build because `CanonicalPayload`, `AppCertificatePayload`, `SignAppCertificate`, `VerifyAppCertificate`, and `NewReplayWindow` do not exist yet.
- Focused tests: failed as expected before implementation.
- Hardening regression tests: app public key length and duplicate capabilities initially accepted; `go test ./internal/remote/cert` failed before validation was tightened.
- Code review regression tests: signer initially accepted caller-supplied machine fingerprint and could issue a certificate it would later reject; `go test ./internal/remote/cert` failed before signer stamped the fingerprint from `machineKey.PublicKey`.
- Final focused tests: `cd termx-core && go test ./internal/remote/cert` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `62d1f70`.

### P2-C local pair session primitives

- Tests written before implementation: `termx-core/internal/remote/pairing/session_test.go`
- Expected failing test: `go test ./internal/remote/pairing` fails to build because `NewManager`, `Config`, `Manager`, and `ClaimRequest` do not exist yet.
- Focused tests: failed as expected before implementation.
- Hardening regression tests: invalid app public key initially consumed the one-time secret; `go test ./internal/remote/pairing` failed before claim consumption moved after successful certificate issuance.
- Code review regression tests: unsupported requested capabilities initially received machine-signed certificates; `go test ./internal/remote/pairing` failed before capabilities were restricted to `terminal` and `file_manager`.
- Final focused tests: `cd termx-core && go test ./internal/remote/pairing` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `12067cb`.

### P2-D CLI pair skeleton and remote status regression

- Tests written before implementation: `termx-core/protocol/client_test.go`, `termx-core/remote_test.go`, and `termx-cli/cmd/termx/main_test.go`
- Expected failing test: `go test ./protocol -run TestClientRemotePairStart` fails to build because `PairStartParams`, `PairStartResult`, and `Client.RemotePairStart` do not exist yet.
- Additional expected failing test: `go test ./cmd/termx -run 'TestRootCmdHasRemoteStatusAndPairCommands|TestPairCmdEmitsJSONPairSession'` fails to build because `pairStartClient` and the top-level `pair` command do not exist yet.
- Focused tests: failed as expected before implementation at protocol/server/CLI layers.
- Hardening regression tests: repeated pair starts with a new `--local-url` initially reused the stale URL; `go test . -run TestE2ERemotePairStartUsesLatestLocalPairURL` failed before server pairing config updates were added.
- Code review regression/fix: review found that replacing the server pairing manager on config changes could invalidate unexpired sessions. Fixed by adding `pairing.Manager.UpdateConfig`, storing session issuer config per session, and updating future-session config without discarding existing sessions.
- Final focused tests: `cd termx-core && go test ./protocol -run TestClientRemotePairStart` passed; `cd termx-core && go test . -run 'TestE2ERemote(PairStart|Status)'` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRootCmdHasRemoteStatusAndPairCommands|TestPairCmdEmitsJSONPairSession'` passed.
- Broader tests: `cd termx-core && go test ./...` passed; `cd termx-cli && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `4b24258`.

### P3-A anonymous rendezvous interfaces/contracts

- Tests written before implementation: `termx-core/internal/remote/rendezvous/channel_test.go`
- Expected failing test: `go test ./internal/remote/rendezvous` fails to build because `NewMemoryStore`, `Config`, `CreateChannelRequest`, `Message`, and message type constants do not exist yet.
- Focused tests: failed as expected before implementation.
- Hardening regression tests: unsupported message types were initially accepted; `go test ./internal/remote/rendezvous` failed before message types were restricted to offer/answer/candidate.
- Code review regression tests: excessive TTL, non-JSON or non-signaling payloads, different app public keys after claim, and unbounded per-channel messages initially passed or failed to build; `go test ./internal/remote/rendezvous` failed before max TTL, structured signaling payload validation, app public key binding, and message count limits were added.
- Final focused tests: `cd termx-core && go test ./internal/remote/rendezvous` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `4012a1b`.

## Subagents

- `Galileo` (`019ddf61-2c8a-7cf0-8a00-991250f8b294`): explorer for termx-core P2 identity/runtime integration points. Result: preserve existing `DeviceIdentity` status baseline, add machine-named key/cert/pairing primitives, and avoid exposing machine private key.
- `Lovelace` (`019ddf61-2c9d-76b2-b547-7b4afd1f5cfe`): explorer for termx-cli remote command/test structure. Result: future `termx pair` should be top-level, use protocol/public core API, and leave `termx remote status` unchanged.
- `Fermat` (`019ddf63-ea27-7260-94ef-84bb9b078503`): P2-A code review. Findings: first-run concurrency race, exported private key boundary, stale workflow. Result: fixed with exclusive install/reload, unexported private key plus signer method, and workflow updates.
- `Jason` (`019ddf6a-ef0e-7e32-89da-880e1a2590f5`): P2-A follow-up review. Findings: Go formatting could expose unexported private key fields, stale workflow. Result: fixed with redacted `String`/`GoString` and formatting regression tests.
- `Socrates` (`019ddf76-3981-7193-b7c7-2a9ba59d4941`): P2-B code review. Findings: signer could issue self-inconsistent machine fingerprint, workflow stale. Result: fixed by stamping `MachinePublicKeyFingerprint` from `machineKey.PublicKey` before canonical signing and updating workflow.
- `Dirac` (`019ddf7d-2bf6-78d0-89d0-e886099b6d01`): P2-C code review. Findings: arbitrary requested capabilities could be machine-signed, local pair request struct lacked snake_case JSON tags, workflow stale. Result: fixed with capability allowlist, JSON tags, and workflow updates.
- `Averroes` (`019ddf8c-a240-7063-8d97-e582d128011f`): P2-D code review. Findings: changing pair config replaced the manager and invalidated active sessions, workflow stale. Result: fixed with manager config updates that preserve existing sessions and workflow updates.
- `Epicurus` (`019ddf9b-6e36-7c32-9433-71c59a027d69`): P3-A code review. Findings: unbounded TTL, arbitrary data under signaling message types, missing app binding after claim, unbounded message retention, workflow stale. Result: fixed with max TTL, structured signaling payload validation, app public key binding, per-channel message limits, and workflow updates.
- R1: no subagent launched; documentation-only planning adjustment requested by the user and no implementation correctness review was required.

## Code Review Log

- P2-A review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, or transport boundary drift introduced.
- P2-B review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, or transport boundary drift introduced.
- P2-C review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, or transport boundary drift introduced.
- P2-D review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, CLI internal-package import, or transport boundary drift introduced.
- P3-A review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay credentials, terminal/file data relay, app machine-private-key exposure, or transport boundary drift introduced.
- R1 planning review: self-checked updated docs for scope drift. Plan now prioritizes embedded local web, shared `remote-ui/` components, and local WebRTC-over-TCP before mobile app migration; no new workspace/tab/pane public model and no anonymous/free TURN relay entitlement were introduced.
- R2 planning review: self-checked updated docs for scope drift. Plan now requires tgent-aligned page/component structure where practical, plus TermX-owned native-like message reducer/event queue behavior; no new workspace/tab/pane public model and no anonymous/free TURN relay entitlement were introduced.

## Deferred Human Decisions And Placeholders

- Public rendezvous deployment, DNS, TLS certificates, billing/subscription provider, mobile signing, and app store configuration remain deferred by policy.
- No mocks or placeholders added in code yet.
- Whether local HTTP and ICE TCP share one port via cmux or use adjacent/independent ports is intentionally deferred to P3-C implementation; current contract only requires the browser adapter to discover the ICE TCP endpoint.

## Risks

- Existing baseline uses `DeviceID` terminology while remote rebuild docs require public `machine -> terminal` object language. The implementation should preserve compatibility where needed but introduce machine-key/certificate concepts without exposing workspace/tab/pane.
- Existing hub baseline may include relay fields. P3 anonymous paths must explicitly reject or omit TermX TURN relay credentials.
- New `remote-ui/` package must avoid carrying over tgent pane/session public concepts when copying `Terminal.tsx`, `SessionList.tsx`, and file manager code.
- Keeping TermX UI close enough to tgent for future synchronization conflicts with replacing tgent's web-like interaction state. The boundary is explicit: copy structure/components/adapters where possible, but normalize messages and lifecycle through TermX reducers/queues.

## Next Exact Action

1. Start P3-B by updating this workflow before writing tests for local embedded web foundation.
2. Write failing tests for local embedded web status, terminal list, pair API contracts, and embedded asset serving.
