# `web-control/` Agent Notes

Current project root: `web-control/`

## Boundary

- `web-control` is the TermX Web Control Plane.
- Use Go for the backend, Vite + React for the frontend, and SQLite for development/test persistence.
- Keep web product business here: users, sessions, plans, subscriptions, machine ownership, app certificate metadata, hub registry, connect tickets, relay leases, quota, usage aggregation, and policy.
- Do not put terminal/file/api/events runtime transport here. Runtime data remains WebRTC DataChannel between app/browser and `termx daemon`.
- Do not import or depend on `tuiv2` workspace/tab/pane/tmux models.

## Remote Build Rules

- Follow root `AGENTS.md` and `docs/remote-rebuild/WORKFLOW.md` for unattended execution, TDD, subagent review, and stable todo numbering.
- Every slice must update `docs/remote-rebuild/WORKFLOW.md` before and after tests, implementation, review, fixes, and commits.
- Tests must be written or revised before implementation. Record the expected failing result in `WORKFLOW.md`.
- Each completed slice must run focused tests, relevant broader tests, subagent review, review fixes, and then commit only related files.

## External Dependencies

- Real payment, subscription billing, invoices, tax, email, OAuth, object storage, SMS, analytics, risk providers, DNS, TLS, cloud accounts, and manual approvals are external dependencies.
- Do not block on them. Define provider interfaces, implement mock/fake/local providers, test complete state transitions, and record `deferred_external` items in `WORKFLOW.md`.
- Mock providers must stay behind interfaces and must not become hard-coded core business behavior.

## Security And Policy

- Web Control may store machine public keys and app certificate metadata. It must never store machine private keys or app private keys.
- Ownership, session/token auth, app certificate revocation, connect ticket TTL, relay lease TTL, quota, active session limit, and SQLite transactions must be real behavior covered by tests.
- Registered free `public_p2p` may receive rendezvous/signaling and STUN only. It must not receive TermX TURN credentials.
- Paid `managed` may receive relay policy/TURN credentials only through capability/policy/lease rules.
- Client-visible connection paths are only `local`, `public_p2p`, and `managed`; relay is not a fourth path.

## Frontend

- Use TailwindCSS for styling. Do not create a broad custom CSS system.
- The frontend shell should consume backend policy/capability results; do not encode plan names or relay as connection path taxonomy.
