# TUIV2 Product Recovery Design

Date: 2026-04-03
Status: Approved for implementation

## Goal

Recover `tuiv2` from a "green but misleading" MVP into a product-truthful baseline where:

- primary workflows match the product docs,
- missing workflows are either implemented or no longer implied,
- tests and e2e cover real user paths instead of direct action injection shortcuts.

## Scope

This recovery work is intentionally phased. It does not try to polish everything at once.

### Phase 1: Interaction And Lifecycle Truth

Focus on the highest-risk gaps that break the product model:

- shared terminal lifecycle:
  - `detach pane`
  - `reconnect pane`
  - `close pane + kill terminal`
  - runtime cleanup when a pane closes or rebinds
- interaction truth:
  - remove normal-mode `?` interception
  - fix picker empty-result submit behavior
  - fix Terminal Pool edit-return mode mismatch
- tests:
  - add red tests for the above
  - add e2e coverage for detach/reconnect and lifecycle correctness

Success criteria:

- closing or rebinding a pane no longer leaves stale runtime ownership/binding state
- lifecycle actions are reachable through the real keymap or documented surface
- the new tests fail before implementation and pass after implementation

### Phase 2: Floating And Display Closure

Focus on the second-largest source of broken experience:

- floating pane creation/attach target correctness
- floating resize must update terminal PTY size
- floating close / visibility / recall closure
- floating keyboard focus coverage
- display model correctness:
  - stop treating display as a tab-global shortcut bucket
  - add missing tests around fullscreen/floating size-sensitive behavior

Success criteria:

- floating behaves like a real pane for attach, resize, focus, and recovery-critical paths
- size-sensitive terminal programs in floating panes are not visually desynced from PTY size

### Phase 3: Terminal Pool Fidelity

Focus on turning Terminal Pool from a page shell into the product-defined page:

- repair page-local mode stack and edit flow
- complete manager data contract
- move toward the three-column product shape:
  - grouped list
  - live terminal content
  - metadata/relationship detail
- add tests and e2e around page navigation and attach/edit flows

Success criteria:

- Terminal Pool is not just a renamed picker/manager surface
- users can inspect, act on, and return from Terminal Pool without state confusion

## Architectural Rules

- `workbench.PaneState.TerminalID` remains the only structural binding source of truth.
- `runtime` may cache ownership/binding state, but must not become a second writable source of truth.
- Tests must prefer real key routing and real surface transitions over direct `SemanticAction` injection when validating user workflows.
- If a behavior is still intentionally deferred, help/status/tests must not imply that it is complete.

## Testing Strategy

Every product fix follows red-green-refactor:

1. write the smallest failing unit/feature test,
2. run it and confirm it fails for the expected reason,
3. implement the minimal fix,
4. rerun targeted tests,
5. add or extend e2e if the behavior is user-critical and cross-module.

Testing layers:

- unit / package tests for state transitions and pure behavior,
- app feature tests for user-visible workflow contracts,
- e2e tests for terminal lifecycle and attach/rebind flows.

## Non-Goals For This Recovery Pass

- broad visual redesign,
- speculative new shortcuts,
- advanced display policy (`fit/fixed/pin/auto-acquire`),
- unrelated refactors outside the touched workflows.
