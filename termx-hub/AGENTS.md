# `termx-hub/` Agent Notes

Current project root: `termx-hub/`

## Boundary

- `termx-hub` is the TermX Hub / Signaling / Relay service.
- Use Go.
- Responsibilities include agent registry, agent register/heartbeat/signaling poll-answer, app/browser managed signaling, ICE config, STUN/TURN, relay session accounting, rate limiting, and usage heartbeat to Web Control.
- Hub must not be a terminal/file/api/events HTTP or WebSocket runtime proxy. Runtime data remains WebRTC DataChannel.
- Hub stores only short-lived control/signaling/relay state. Web Control is the source of truth for users, ownership, subscription, quota, and policy.

## Remote Build Rules

- Follow root `AGENTS.md` and `docs/remote-rebuild/WORKFLOW.md` for unattended execution, TDD, subagent review, and stable todo numbering.
- Every slice must update `WORKFLOW.md` before and after tests, implementation, review, fixes, and commits.
- Tests must be written or revised before implementation. Record the expected failing result in `WORKFLOW.md`.
- Each completed slice must run focused tests, relevant broader tests, subagent review, review fixes, and then commit only related files.

## Policy And Transport

- Client-visible connection paths are only `local`, `public_p2p`, and `managed`.
- Relay is not a fourth client transport. It may appear only as `relayInUse`, relay policy, quota, session lease, throttling, accounting, or telemetry.
- Registered free `public_p2p` must not receive TermX TURN credentials.
- `managed` may include TURN credentials only when Web Control policy/relay lease allows it.
- HTTP long-poll, WebSocket, or gRPC may be used for agent signaling/control, but not for terminal/file/api/events runtime data.

## External Dependencies

- Public DNS, TLS certificates, production TURN ports/firewall, cloud accounts, deployment approvals, and real billing integration are external dependencies.
- Do not block on them. Use local config, fake Web Control clients, and test TURN/relay policy locally where possible; record `deferred_external` items in `WORKFLOW.md`.

## Review Focus

- Check ticket verification, machine ownership boundaries, relay policy enforcement, temporary credential expiry, registry TTL cleanup, session correlation, quota/session limit behavior, and absence of HTTP runtime proxy behavior.
