# `termx-hub/` Agent Notes

Current project root: `termx-hub/`

## Boundary

- `termx-hub` is the TermX Hub / Signaling / Relay service.
- Use Go.
- Responsibilities include agent registry, agent register/heartbeat/signaling poll-answer, app/browser cloud signaling, ICE config, STUN/TURN, relay session accounting, rate limiting, and usage heartbeat to Web Control.
- Hub must not be a terminal/file/api/events HTTP or WebSocket runtime proxy. Runtime data remains WebRTC DataChannel.
- Hub stores only short-lived control/signaling/relay state. Web Control is the source of truth for users, ownership, subscription, quota, and policy.
- Hub must remain stateless in the tgent sense: no database, no migrations, no durable local source of truth. If a state must survive a Hub restart, it belongs in Web Control.
- Allowed Hub memory is bounded TTL state only: online agent sessions, pending offers/answers, waiters, relay counters, temporary TURN credential maps, and rate-limit/policy snapshots. Every map must have cleanup and size/backpressure behavior.
- After a Hub restart, agents must re-register and apps must request fresh tickets/sessions; old pending signaling may expire rather than be recovered from local persistence.
- Product direction is APP-first: Hub serves APP/remote-ui connection establishment and daemon agent signaling; it is not a UI surface and must not copy tgent workspace/session/window/pane product semantics.

## Remote Build Rules

- Follow root `AGENTS.md` and `docs/remote-rebuild/WORKFLOW.md` for unattended execution, TDD, subagent review, and stable todo numbering.
- Every slice must update `WORKFLOW.md` before and after tests, implementation, review, fixes, and commits.
- Tests must be written or revised before implementation. Record the expected failing result in `WORKFLOW.md`.
- Each completed slice must run focused tests, relevant broader tests, subagent review, review fixes, and then commit only related files.

## Policy And Transport

- Client-visible connection paths are only `local`, `public_p2p`, and `cloud`.
- Relay is not a fourth client transport. It may appear only as `relayInUse`, relay policy, quota, session lease, throttling, accounting, or telemetry.
- During development, Web Control policy may allow registered/dev users to use cloud relay for free so the full flow can be tested before billing gates exist.
- `public_p2p` must remain STUN/rendezvous-only and must not receive TermX TURN credentials, even while `cloud` relay is dev-free.
- `cloud` may include TURN credentials only as cloud ICE/capability/session information from Web Control policy/relay lease. Do not surface it as path `relay`.
- HTTP long-poll, WebSocket, or gRPC may be used for agent signaling/control, but not for terminal/file/api/events runtime data.
- Do not copy tgent HTTP proxy/WebSocket runtime behavior. Only the stateless registry, heartbeat, policy, relay metering, temporary credential, and reconnect patterns are valid references.

## External Dependencies

- Public DNS, TLS certificates, production TURN ports/firewall, cloud accounts, deployment approvals, and real billing integration are external dependencies.
- Do not block on them. Use local config, fake Web Control clients, and test TURN/relay policy locally where possible; record `deferred_external` items in `WORKFLOW.md`.

## Review Focus

- Check ticket verification, machine ownership boundaries, relay policy enforcement, temporary credential expiry, registry TTL cleanup, session correlation, quota/session limit behavior, and absence of HTTP runtime proxy behavior.
- Also check that Hub has no DB dependency, migration, durable local business state, or unbounded in-memory registry/signaling/relay map.
