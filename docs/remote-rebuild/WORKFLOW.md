# TermX Remote Rebuild Workflow

Status file for unattended remote rebuild work. Update this file before starting and after completing every todo.

## Current State

- Current phase: P2 identity and app certificate pairing
- Active todo: R0 seed persistent workflow file
- Last updated: 2026-05-01T01:25:00+08:00
- Worktree goal before final response: clean after each completed todo commit

## Ordered Todos

| ID | Phase | Todo | Status | Commit |
| --- | --- | --- | --- | --- |
| R0 | workflow | Create and seed `docs/remote-rebuild/WORKFLOW.md` with full todo plan | in_progress | pending |
| P2-A | identity | Implement Ed25519 machine key generation, load, persistence permissions, and fingerprint helpers in `termx-core/internal/remote/identity` | pending |  |
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
- Result: in progress.

## Subagents

- `Galileo` (`019ddf61-2c8a-7cf0-8a00-991250f8b294`): explorer for termx-core P2 identity/runtime integration points. Status: running.
- `Lovelace` (`019ddf61-2c9d-76b2-b547-7b4afd1f5cfe`): explorer for termx-cli remote command/test structure. Status: running.

## Code Review Log

- No completed development todo yet. R0 is workflow documentation only.

## Deferred Human Decisions And Placeholders

- Public rendezvous deployment, DNS, TLS certificates, billing/subscription provider, mobile signing, and app store configuration remain deferred by policy.
- No mocks or placeholders added in code yet.

## Risks

- Existing baseline uses `DeviceID` terminology while remote rebuild docs require public `machine -> terminal` object language. The implementation should preserve compatibility where needed but introduce machine-key/certificate concepts without exposing workspace/tab/pane.
- Existing hub baseline may include relay fields. P3 anonymous paths must explicitly reject or omit TermX TURN relay credentials.

## Next Exact Action

1. Complete R0 by committing this workflow file.
2. Start P2-A by updating this file, then write failing machine key tests in `termx-core/internal/remote/identity`.
