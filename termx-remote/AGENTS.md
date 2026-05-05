# termx-remote Agent Notes

## Boundary

- `termx-remote` is the remote product domain.
- It owns hub, agent, pairing, signaling, registration, and remote session orchestration.
- It depends on `termx-core/clientapi` for shell-neutral daemon capability.
- It must not depend on concrete `termx-core` server internals.

## Migration Rules

- Follow the root `AGENTS.md` migration rules and root `workflow.md` task ledger.
- Use Go interfaces as the main boundary between `termx-remote` and `termx-core`; RPC may only be an adapter for those interfaces.
- Keep local, public_p2p, and managed paths on one hub/signaling/ICE/session flow.
- Do not introduce relay as a fourth client transport path.
- Do not implement native UI adapters here.
