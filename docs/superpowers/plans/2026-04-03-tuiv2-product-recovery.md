# TUIV2 Product Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore `tuiv2` to a product-truthful baseline by fixing the highest-risk lifecycle and interaction gaps first, with test-first execution and expanded e2e coverage.

**Architecture:** Work in three phases. First repair shared-terminal lifecycle truth and broken interaction loops. Then close floating/display gaps that directly damage terminal behavior. Finally upgrade Terminal Pool fidelity without regressing the now-truthful baseline. Preserve `workbench -> runtime -> render`, and keep `workbench.PaneState.TerminalID` as the only structural binding truth.

**Tech Stack:** Go, Bubble Tea, Lip Gloss, termx protocol client, `tuiv2` app/orchestrator/runtime/workbench/render packages, Go test/e2e suite

---

## File Ownership

- `tuiv2/app/*`
  Responsibility: real user workflow closure, mode transitions, prompt/page state, e2e orchestration.
- `tuiv2/input/*`
  Responsibility: truthful root keymap and user-reachable actions.
- `tuiv2/orchestrator/*`
  Responsibility: semantic workflow application across workbench/runtime.
- `tuiv2/runtime/*`
  Responsibility: attachment, cleanup, stream/recovery, derived ownership/cache.
- `tuiv2/workbench/*`
  Responsibility: structural pane/tab/workspace/floating truth.
- `tuiv2/render/*`
  Responsibility: page/body/status/help/product-visible state projection.
- `tuiv2/app/*_test.go`, `tuiv2/*_test.go`
  Responsibility: failing tests first, then verification.

## Phase 1: Interaction And Lifecycle Truth

### Task 1: Shared Terminal Cleanup On Pane Close/Rebind

**Files:**
- Modify: `tuiv2/runtime/*`
- Modify: `tuiv2/orchestrator/orchestrator.go`
- Modify: `tuiv2/app/update.go`
- Test: `tuiv2/runtime/runtime_test.go`
- Test: `tuiv2/app/feature_test.go`

- [ ] **Step 1: Write failing runtime tests for binding cleanup**

Cover:
- closing a pane removes its runtime binding
- rebinding a pane removes it from the previous terminal's bound/owner cache
- owner cache is reassigned or cleared consistently after detach/close

- [ ] **Step 2: Run targeted runtime tests and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/runtime -run 'Binding|Owner|Detach|Close' -count=1`
Expected: FAIL because bindings are append-only today.

- [ ] **Step 3: Write failing app feature test for close-pane lifecycle truth**

Cover:
- close pane does not kill terminal
- close pane does remove stale runtime binding/owner state

- [ ] **Step 4: Run targeted app test and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'ClosePane.*Lifecycle|RuntimeBinding' -count=1`
Expected: FAIL with stale runtime state.

- [ ] **Step 5: Implement minimal runtime cleanup**

Implementation notes:
- add explicit runtime unbind/cleanup helpers
- call them on pane close and pane rebind
- do not introduce a second writable truth outside `workbench.PaneState.TerminalID`

- [ ] **Step 6: Re-run targeted runtime and app tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/runtime ./tuiv2/app -run 'Binding|Owner|Detach|Close|RuntimeBinding' -count=1`
Expected: PASS

### Task 2: Implement Reachable Pane Lifecycle Actions

**Files:**
- Modify: `tuiv2/input/keymap.go`
- Modify: `tuiv2/app/update.go`
- Modify: `tuiv2/orchestrator/orchestrator.go`
- Modify: `tuiv2/runtime/*`
- Test: `tuiv2/app/feature_test.go`
- Test: `tuiv2/orchestrator/orchestrator_test.go`

- [ ] **Step 1: Write failing tests for `detach-pane`**

Cover:
- action is reachable from pane workflow
- detach leaves pane unconnected
- terminal remains alive and attachable

- [ ] **Step 2: Run focused tests and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/orchestrator -run 'DetachPane' -count=1`
Expected: FAIL

- [ ] **Step 3: Write failing tests for `reconnect-pane`**

Cover:
- reconnect path opens picker from current pane
- selecting another terminal rebinds the pane cleanly

- [ ] **Step 4: Run focused tests and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/orchestrator -run 'ReconnectPane' -count=1`
Expected: FAIL

- [ ] **Step 5: Write failing tests for `close-pane-kill`**

Cover:
- pane closes
- target terminal is killed
- exited/unconnected behavior matches intended contract

- [ ] **Step 6: Run focused tests and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/orchestrator -run 'CloseAndKill|ClosePaneKill' -count=1`
Expected: FAIL

- [ ] **Step 7: Implement minimal reachable lifecycle actions**

Implementation notes:
- prefer pane-mode bindings, not normal-mode shortcuts
- wire actual handlers, not test-only semantic entry points

- [ ] **Step 8: Re-run focused lifecycle tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/orchestrator -run 'DetachPane|ReconnectPane|CloseAndKill|ClosePaneKill' -count=1`
Expected: PASS

### Task 3: Fix Interaction Truth Regressions

**Files:**
- Modify: `tuiv2/input/keymap.go`
- Modify: `tuiv2/modal/help.go`
- Modify: `tuiv2/render/frame.go`
- Modify: `tuiv2/app/update.go`
- Test: `tuiv2/input/router_test.go`
- Test: `tuiv2/app/app_test.go`
- Test: `tuiv2/app/feature_test.go`

- [ ] **Step 1: Write failing test proving `?` should pass through in normal mode**

- [ ] **Step 2: Run targeted input/app tests and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/input ./tuiv2/app -run 'QuestionMark|Help|NormalPassthrough' -count=1`
Expected: FAIL because `?` opens help today.

- [ ] **Step 3: Write failing test for picker empty-result Enter**

Cover:
- empty filtered picker + Enter is safe no-op
- no attach request with empty terminal id

- [ ] **Step 4: Run targeted picker tests and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'Picker.*Empty|Picker.*Noop' -count=1`
Expected: FAIL

- [ ] **Step 5: Write failing test for Terminal Pool edit-return flow**

Cover:
- from Terminal Pool edit prompt cancel returns to Terminal Pool mode
- save also returns to Terminal Pool mode

- [ ] **Step 6: Run targeted page/prompt tests and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'TerminalPool.*Edit|TerminalManager.*Edit' -count=1`
Expected: FAIL

- [ ] **Step 7: Implement minimal fixes and truthy help/status updates**

Implementation notes:
- remove normal-mode `?` interception; keep help reachable from an explicit documented surface
- make empty picker submit a safe no-op
- restore page-local mode after Terminal Pool edit prompt
- keep help/status truthful to the remaining real bindings

- [ ] **Step 8: Re-run focused tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/input ./tuiv2/app -run 'QuestionMark|Help|Picker|TerminalPool|TerminalManager' -count=1`
Expected: PASS

### Task 4: Add Phase 1 End-To-End Coverage

**Files:**
- Modify: `tuiv2/app/e2e_test.go`
- Test: `tuiv2/app`

- [ ] **Step 1: Write a failing e2e for detach/reconnect**

Cover:
- create and attach terminal
- detach pane
- reconnect to same or another terminal
- verify rendered output and binding state

- [ ] **Step 2: Run the single e2e and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'E2E.*Detach|E2E.*Reconnect' -count=1`
Expected: FAIL

- [ ] **Step 3: Write a failing e2e for close-pane lifecycle truth**

Cover:
- close pane does not kill terminal by default
- terminal still appears in attachable/global surfaces after pane closure

- [ ] **Step 4: Run the single e2e and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'E2E.*ClosePane' -count=1`
Expected: FAIL

- [ ] **Step 5: Implement the minimal support needed for green**

- [ ] **Step 6: Re-run targeted e2e**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'E2E.*Detach|E2E.*Reconnect|E2E.*ClosePane' -count=1`
Expected: PASS

## Phase 2: Floating And Display Closure

### Task 5: Fix Floating Create/Attach Targeting

**Files:**
- Modify: `tuiv2/workbench/*`
- Modify: `tuiv2/app/update.go`
- Modify: `tuiv2/orchestrator/orchestrator.go`
- Test: `tuiv2/app/feature_test.go`

- [ ] **Step 1: Write failing feature test proving new floating attaches to the new pane**
- [ ] **Step 2: Run it and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'Floating.*Attach.*NewPane' -count=1`
Expected: FAIL

- [ ] **Step 3: Implement the minimal fix**
- [ ] **Step 4: Re-run the targeted test**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'Floating.*Attach.*NewPane' -count=1`
Expected: PASS

### Task 6: Make Floating Resize Real

**Files:**
- Modify: `tuiv2/app/update.go`
- Modify: `tuiv2/runtime/resize.go`
- Test: `tuiv2/app/feature_test.go`
- Test: `tuiv2/runtime/runtime_test.go`

- [ ] **Step 1: Write failing tests showing floating resize updates PTY size**
- [ ] **Step 2: Run them and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/runtime -run 'Floating.*Resize|Resize.*Floating' -count=1`
Expected: FAIL

- [ ] **Step 3: Implement minimal floating resize propagation**
- [ ] **Step 4: Re-run targeted tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/runtime -run 'Floating.*Resize|Resize.*Floating' -count=1`
Expected: PASS

### Task 7: Complete Minimal Floating Lifecycle Actions

**Files:**
- Modify: `tuiv2/input/keymap.go`
- Modify: `tuiv2/app/update.go`
- Modify: `tuiv2/orchestrator/orchestrator.go`
- Modify: `tuiv2/workbench/*`
- Test: `tuiv2/app/feature_test.go`

- [ ] **Step 1: Write failing tests for floating close / visibility / recall**
- [ ] **Step 2: Run them and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'Floating.*Close|Floating.*Visibility|Floating.*Recall' -count=1`
Expected: FAIL

- [ ] **Step 3: Implement minimal real actions**
- [ ] **Step 4: Re-run targeted tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'Floating.*Close|Floating.*Visibility|Floating.*Recall' -count=1`
Expected: PASS

## Phase 3: Terminal Pool Fidelity

### Task 8: Repair Terminal Pool Mode Stack

**Files:**
- Modify: `tuiv2/app/update.go`
- Test: `tuiv2/app/feature_test.go`

- [ ] **Step 1: Write/keep failing tests for edit cancel/save return to page mode**
- [ ] **Step 2: Run and verify red if not already red**
- [ ] **Step 3: Implement minimal mode restoration**
- [ ] **Step 4: Re-run targeted tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'TerminalPool.*Edit|TerminalManager.*Edit' -count=1`
Expected: PASS

### Task 9: Expand Terminal Pool Data And Page Shape

**Files:**
- Modify: `tuiv2/render/*`
- Modify: `tuiv2/app/update.go`
- Modify: `tuiv2/runtime/*`
- Test: `tuiv2/render/*_test.go`
- Test: `tuiv2/app/feature_test.go`

- [ ] **Step 1: Write failing render tests for grouped list + live/detail columns**
- [ ] **Step 2: Run and verify red**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/render -run 'TerminalPool|TerminalManager' -count=1`
Expected: FAIL

- [ ] **Step 3: Implement minimal three-area page contract**
- [ ] **Step 4: Re-run targeted render/app tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/render ./tuiv2/app -run 'TerminalPool|TerminalManager' -count=1`
Expected: PASS

## Final Verification

- [ ] **Step 1: Run focused phase suites**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/input ./tuiv2/orchestrator ./tuiv2/runtime ./tuiv2/workbench ./tuiv2/render -count=1`
Expected: PASS

- [ ] **Step 2: Run `tuiv2` full suite**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/...`
Expected: PASS

- [ ] **Step 3: Summarize what is now truly complete vs still deferred**

- [ ] **Step 4: Commit phase-by-phase or as user directs**
