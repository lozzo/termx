# TermX Remote Rebuild Workflow

Status file for unattended remote rebuild work. Update this file before starting and after completing every todo.

## Current State

- Current phase: P2 identity and app certificate pairing
- Active todo: P2-A machine key generation/load/fingerprint
- Last updated: 2026-05-01T01:49:00+08:00
- Worktree goal before final response: clean after each completed todo commit

## Ordered Todos

| ID | Phase | Todo | Status | Commit |
| --- | --- | --- | --- | --- |
| R0 | workflow | Create and seed `docs/remote-rebuild/WORKFLOW.md` with full todo plan | completed | `8734d00` |
| P2-A | identity | Implement Ed25519 machine key generation, load, persistence permissions, and fingerprint helpers in `termx-core/internal/remote/identity` | in_progress | pending |
| P2-B | cert | Implement canonical app certificate payload, sign/verify helpers, and nonce/timestamp replay helper in `termx-core/internal/remote/cert` | pending |  |
| P2-C | pairing | Implement local pair session creation, TTL, single-use semantics, and app certificate issuance in `termx-core/internal/remote/pairing` | pending |  |
| P2-D | CLI | Keep `termx remote status` working and add a conservative `termx pair` CLI skeleton only after core primitives exist | pending |  |
| P3-A | rendezvous | Implement anonymous rendezvous interfaces/contracts with payload limit, TTL, channel secret verification, and no TURN credentials | pending |  |

## TDD Log

### R0 seed persistent workflow file

- Tests written before implementation: none; documentation/workflow-only todo.
- Expected failing test: not applicable.
- Focused tests: not run; documentation/workflow-only todo.
- Broader tests: not run; documentation/workflow-only todo.
- Result: completed. Commit: `8734d00`.

### P2-A machine key generation/load/fingerprint

- Tests written before implementation: `termx-core/internal/remote/identity/identity_test.go`
- Expected failing test: `go test ./internal/remote/identity` fails to build because `LoadOrCreateMachineKey`, `MachineKeyFilename`, and `MachinePublicKeyFingerprint` do not exist yet.
- Focused tests: failed as expected before implementation.
- Code review regression tests: added concurrent first-run and private-key JSON leak tests; `go test ./internal/remote/identity` fails as expected because `MachineKey.Sign` and hidden `privateKey` do not exist yet.
- Follow-up review regression tests: added formatting redaction test; `go test ./internal/remote/identity` failed as expected before `String`/`GoString` redaction.
- Final focused tests: `cd termx-core && go test ./internal/remote/identity` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: implementation complete; pending commit hash.

## Subagents

- `Galileo` (`019ddf61-2c8a-7cf0-8a00-991250f8b294`): explorer for termx-core P2 identity/runtime integration points. Result: preserve existing `DeviceIdentity` status baseline, add machine-named key/cert/pairing primitives, and avoid exposing machine private key.
- `Lovelace` (`019ddf61-2c9d-76b2-b547-7b4afd1f5cfe`): explorer for termx-cli remote command/test structure. Result: future `termx pair` should be top-level, use protocol/public core API, and leave `termx remote status` unchanged.
- `Fermat` (`019ddf63-ea27-7260-94ef-84bb9b078503`): P2-A code review. Findings: first-run concurrency race, exported private key boundary, stale workflow. Result: fixed with exclusive install/reload, unexported private key plus signer method, and workflow updates.
- `Jason` (`019ddf6a-ef0e-7e32-89da-880e1a2590f5`): P2-A follow-up review. Findings: Go formatting could expose unexported private key fields, stale workflow. Result: fixed with redacted `String`/`GoString` and formatting regression tests.

## Code Review Log

- P2-A review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, or transport boundary drift introduced.

## Deferred Human Decisions And Placeholders

- Public rendezvous deployment, DNS, TLS certificates, billing/subscription provider, mobile signing, and app store configuration remain deferred by policy.
- No mocks or placeholders added in code yet.

## Risks

- Existing baseline uses `DeviceID` terminology while remote rebuild docs require public `machine -> terminal` object language. The implementation should preserve compatibility where needed but introduce machine-key/certificate concepts without exposing workspace/tab/pane.
- Existing hub baseline may include relay fields. P3 anonymous paths must explicitly reject or omit TermX TURN relay credentials.

## Next Exact Action

1. Commit P2-A.
2. Update this workflow with the P2-A commit hash.
3. Start P2-B by writing failing certificate canonical/sign/verify/replay tests in `termx-core/internal/remote/cert`.
