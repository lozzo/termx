# `termx-hub/` Agent Notes

Current project root: `termx-hub/`

## Boundary

- `termx-hub` is the standalone Hub executable and configuration adapter.
- Use Go.
- Hub product-domain implementation belongs in `termx-remote/hub`; keep agent registry, agent register/heartbeat/signaling poll-answer, app/browser cloud signaling, ICE config, STUN/TURN, relay session accounting, rate limiting, and usage heartbeat implementation there.
- `termx-hub/cmd` may parse environment/configuration, construct `termx-remote/hub` services, and start loops, but must not own hub product logic.
- Hub must not be a terminal/file/api/events HTTP or WebSocket runtime proxy. Runtime data remains WebRTC DataChannel.
- Hub stores only short-lived control/signaling/relay state. Web Control is the source of truth for users, ownership, subscription, quota, and policy.
- Hub must remain stateless in the tgent sense: no database, no migrations, no durable local source of truth. If a state must survive a Hub restart, it belongs in Web Control.
- Allowed Hub memory is bounded TTL state only: online agent sessions, pending offers/answers, waiters, relay counters, temporary TURN credential maps, and rate-limit/policy snapshots. Every map must have cleanup and size/backpressure behavior.

## Remote Build Rules

- Follow root `AGENTS.md` and root `workflow.md` for unattended execution, TDD, subagent review, and stable todo numbering.
- Every slice must update `workflow.md` before and after tests, implementation, review, fixes, and commits.
- Tests must be written or revised before implementation. Record the expected failing result in `workflow.md`.
- Each completed slice must run focused tests, relevant broader tests, subagent review, review fixes, and then mark workflow state accurately.

## Policy And Transport

- Client-visible connection paths are only `local`, `public_p2p`, and `managed`.
- Relay is not a fourth client transport. It may appear only as `relayInUse`, relay policy, quota, session lease, throttling, accounting, or telemetry.
- `public_p2p` must remain STUN/rendezvous-only and must not receive TermX TURN credentials.
- `managed` may include TURN credentials only as managed ICE/capability/session information; do not surface it as path `relay`.
- HTTP long-poll, WebSocket, or gRPC may be used for signaling/control, but not for terminal/file/api/events runtime data.

## External Dependencies

- Public DNS, TLS certificates, production TURN ports/firewall, cloud accounts, deployment approvals, and real billing integration are external dependencies.
- Do not block on them. Use local config, fake Web Control clients, and test TURN/relay policy locally where possible; record `deferred_external` items in root `workflow.md`.

## Review Focus

- Check ticket verification, machine ownership boundaries, relay policy enforcement, temporary credential expiry, registry TTL cleanup, session correlation, quota/session limit behavior, and absence of HTTP runtime proxy behavior.
- Also check that Hub has no DB dependency, migration, durable local business state, or unbounded in-memory registry/signaling/relay map.
