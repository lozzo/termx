# Layered Backing Grid Workflow

This file is the active driver and running record for the current autonomous development line.

## Scope

- Active scope:
  - `termx-core/`
  - `termx-vterm/`
  - `tuiv2/`
  - required contract glue in `internal/protocol/`, `termx-proto/`, and `termx-cli/`
- Deferred until core+tui are stable:
  - `remote-ui/`
  - `termx-app/`
  - wider remote/App transport and product-layer integration

## Goal

Move TermX terminal history toward a layered backing-grid architecture:

- hot mutable backing grid for live screen + recent history
- cold file-backed structured logical store for deep history
- canonical row identity / generation across paging, copy-mode, retention, and resize

## Current Design Source

- Main design document:
  - `docs/terminal-history-layered-backing-grid-design.md`
- Historical archive:
  - `docs/workflows/archive/grid-history-rebase-workflow.md`

## Current Priorities

1. Define and harden canonical row identity / generation at core history boundary.
2. Specify hot-grid responsibilities versus cold-store responsibilities in code-facing terms.
3. Replace remaining resize/history bridge inference with explicit row-movement ownership in core+tui paths.
4. Keep committed-row paging contract stable while internal models evolve.

## Current Rules

- Do not change observer-width semantics:
  - one real PTY size
  - observers project canonical content only
- Do not reintroduce raw PTY journal as UI history truth.
- Do not expand this phase into `remote-ui` or app product work.
- Every completed module slice must leave:
  - focused tests
  - if relevant, tmux smoke evidence
  - a compressed workflow update

## Current Risks

- Canonical row identity may drift if hot/cold boundary semantics are not defined before implementation.
- Flush/compact logic can easily split an incomplete logical line unless the contract is explicit.
- Resize migration can replace content-matching bugs with identity-mapping bugs if generation rules stay fuzzy.

## Current Validation Standard

- Focused tests first:
  - `termx-core`
  - `termx-vterm/vterm`
  - `tuiv2/runtime`
  - `tuiv2/app`
  - `internal/protocol` when contracts move
- Periodic full verification:
  - `go test ./internal/... ./termx-cli/... ./termx-core/... ./termx-proto/... ./termx-vterm/... ./tuiv2/runtime/... ./tuiv2/app/... ./tuiv2/render/...`
- tmux smoke when terminal history semantics change materially:
  - copy-mode top reaches oldest retained content
  - resize does not collapse retained history
  - attach/re-entry does not regress loaded depth semantics

## Next Slice

- Convert the design document into an implementation-ready contract:
  - canonical row identity vocabulary
  - hot-grid invariants
  - cold-store invariants
  - flush boundary rules
  - resize authority rules

## Compression Rule

When updating this file:

- keep only the active goal state, latest decisions, latest evidence, open risks, and next actions;
- compress completed slice notes into 3-6 line summaries;
- move verbose historical detail into `docs/workflows/archive/` if needed.
