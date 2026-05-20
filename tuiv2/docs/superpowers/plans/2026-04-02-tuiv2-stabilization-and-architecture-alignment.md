# TUIV2 Stabilization And Architecture Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stabilize `tuiv2` so that its tests/documents are truthful, its visible workflows match the canonical product docs, and the implementation converges toward the intended V2 architecture instead of accumulating more MVP shortcuts.

**Architecture:** Treat this as a convergence project, not a feature sprint. First restore a truthful and green baseline, then close fake or partial workflow contracts, then repair data-model gaps (`floating`, metadata, manager data), and only after that reshape higher-level UI surfaces such as Terminal Pool page form. Preserve the existing `workbench -> runtime -> render` layering and keep `workbench.PaneState.TerminalID` as the only structural binding source of truth.

**Tech Stack:** Go, Bubble Tea, Lip Gloss, termx protocol client, existing `tuiv2` modules (`app`, `input`, `orchestrator`, `workbench`, `runtime`, `render`, `persist`, `modal`)

---

## File Structure And Ownership

This plan assumes the following ownership boundaries during implementation:

- `tuiv2/input/*`
  Responsibility: canonical keymap, mode routing, semantic action vocabulary, overlay text-input routing.
- `tuiv2/app/*`
  Responsibility: Bubble Tea integration, modal-local state handling, effect application, prompt submission, status/error lifecycle.
- `tuiv2/orchestrator/*`
  Responsibility: multi-owner semantic workflows and cross-module contracts.
- `tuiv2/workbench/*`
  Responsibility: structural truth for workspaces/tabs/panes/floating layout.
- `tuiv2/runtime/*`
  Responsibility: terminal-centric runtime state, attach/stream/recovery, derived ownership/cache only.
- `tuiv2/render/*`
  Responsibility: pure visible-state projection and frame rendering.
- `tuiv2/persist/*`, `tuiv2/bootstrap/*`
  Responsibility: schema, save/load/restore contract.
- `docs/*.md`
  Responsibility: product truth, migration truth, current-status truth.

Implementation guardrails:

- Do not add new direct shortcuts in normal mode without first updating the canonical keybinding doc.
- Do not make `runtime` the writable owner of pane-terminal intent.
- Do not redesign Terminal Pool page shape before the manager data contract and actions are real.
- Do not keep UI hints for actions that are not actually wired.
- Prefer deleting fake behavior over preserving misleading polish.

---

### Task 1: Restore A Truthful Green Baseline

**Files:**
- Modify: `tuiv2/input/actions.go`
- Modify: `tuiv2/app/feature_test.go`
- Modify: `tuiv2-current-status.md`
- Test: `tuiv2/app`

- [ ] **Step 1: Inventory compile blockers and classify them as either “planned-but-not-implemented” or “obsolete test references”**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/...`
Expected: FAIL in `tuiv2/app` with undefined action symbols.

- [ ] **Step 2: Decide the honest baseline for pending actions**

Rule:
- If an action is part of the intended V2 semantic vocabulary and will be implemented in later tasks, define it in `tuiv2/input/actions.go`.
- If an action is not actually part of the approved near-term plan, remove or rewrite the test so the suite no longer pretends the action exists.

- [ ] **Step 3: Make the test package compile without inventing user-visible behavior**

Implementation notes:
- Avoid binding unfinished actions in `input/keymap.go`.
- Avoid adding status/help/footer mentions for unfinished actions in this task.
- If a test remains skipped, it must still compile cleanly.

- [ ] **Step 4: Run the full `tuiv2` suite**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/...`
Expected: PASS

- [ ] **Step 5: Update the current-status doc so it reflects the actual baseline**

Update:
- what currently passes
- what remains pending
- remove stale claims if any behavior is still intentionally deferred

- [ ] **Step 6: Re-run targeted verification**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add tuiv2/input/actions.go tuiv2/app/feature_test.go tuiv2-current-status.md
git commit -m "chore: 修正 tuiv2 基线与状态文档"
```

---

### Task 2: Remove Fake Capabilities And Make Overlay Input Real

**Files:**
- Modify: `tuiv2/input/translate.go`
- Modify: `tuiv2/input/keymap.go`
- Modify: `tuiv2/app/update.go`
- Modify: `tuiv2/modal/picker.go`
- Modify: `tuiv2/modal/terminal_manager.go`
- Modify: `tuiv2/modal/workspace_picker.go`
- Modify: `tuiv2/modal/help.go`
- Modify: `tuiv2/render/frame.go`
- Modify: `tuiv2/render/overlays.go`
- Test: `tuiv2/input/router_test.go`
- Test: `tuiv2/app/app_test.go`
- Test: `tuiv2/app/feature_test.go`

- [ ] **Step 1: Write/extend failing tests for overlay text input**

Cover:
- picker query accepts runes and backspace
- workspace picker query accepts runes and backspace
- terminal manager query accepts runes and backspace
- prompt input continues to work unchanged

- [ ] **Step 2: Run only the new overlay-input tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'Picker|WorkspacePicker|TerminalManager|Prompt' -count=1`
Expected: FAIL for missing query mutation behavior

- [ ] **Step 3: Implement a dedicated modal text-input path instead of abusing semantic actions**

Implementation notes:
- Keep `input.TranslateKeyMsg()` semantic for bound control keys.
- Add modal-local rune/backspace handling in `app` for picker/workspace-picker/terminal-manager sessions.
- After query mutation, call `ApplyFilter()` and normalize selection.

- [ ] **Step 4: Delete or wire every fake capability in picker/manager/help/status/footer**

Rule:
- If `ActionPickerAttachSplit`, `ActionAttachTab`, `ActionAttachFloating`, `ActionEditTerminal` are not wired by the end of this task, remove them from help/status/footer.
- Prefer consistency over aspirational hints.

- [ ] **Step 5: Run focused tests for routing and overlay behavior**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/input ./tuiv2/app -run 'Route|Picker|WorkspacePicker|TerminalManager|Help|Status' -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add tuiv2/input/translate.go tuiv2/input/keymap.go tuiv2/app/update.go tuiv2/modal/picker.go tuiv2/modal/terminal_manager.go tuiv2/modal/workspace_picker.go tuiv2/modal/help.go tuiv2/render/frame.go tuiv2/render/overlays.go tuiv2/input/router_test.go tuiv2/app/app_test.go tuiv2/app/feature_test.go
git commit -m "fix: 收口 overlay 输入与假能力提示"
```

---

### Task 3: Complete The Workspace Picker Contract

**Files:**
- Modify: `tuiv2/app/update.go`
- Modify: `tuiv2/orchestrator/orchestrator.go`
- Modify: `tuiv2/modal/workspace_picker.go`
- Test: `tuiv2/app/feature_test.go`
- Test: `tuiv2/orchestrator/orchestrator_test.go`

- [ ] **Step 1: Write a failing test for selecting `new workspace` from the workspace picker**

Expected behavior:
- selecting the synthetic create row should create a workspace or open a rename/create prompt, according to the chosen design
- session should not silently no-op

- [ ] **Step 2: Run the focused test**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app -run 'WorkspacePicker' -count=1`
Expected: FAIL because create-row selection is a no-op today

- [ ] **Step 3: Implement the chosen create-row flow**

Recommended approach:
- keep simple first: create a new workspace with generated name, switch to it, then return to normal mode
- only open a naming prompt if the user experience clearly improves without complicating the contract

- [ ] **Step 4: Keep orchestration rules clean**

Rule:
- workspace switching/creation side effects belong in `orchestrator`
- modal close + input-mode reset must remain explicit

- [ ] **Step 5: Re-run workspace-specific tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/orchestrator -run 'Workspace' -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add tuiv2/app/update.go tuiv2/orchestrator/orchestrator.go tuiv2/modal/workspace_picker.go tuiv2/app/feature_test.go tuiv2/orchestrator/orchestrator_test.go
git commit -m "feat: 补齐 workspace picker 创建工作流"
```

---

### Task 4: Repair Floating Pane Persistence Before UI Expansion

**Files:**
- Modify: `tuiv2/persist/schema_v2.go`
- Modify: `tuiv2/persist/workspace_state.go`
- Modify: `tuiv2/bootstrap/restore.go`
- Modify: `tuiv2/workbench/types.go`
- Modify: `tuiv2/workbench/workbench.go`
- Test: `tuiv2/persist/workspace_state_test.go`
- Test: `tuiv2/bootstrap/bootstrap_test.go`
- Test: `tuiv2/workbench/workbench_test.go`

- [ ] **Step 1: Write failing persistence tests for floating pane rect and z-order**

Cover:
- save preserves floating pane identity and rect
- restore rebuilds `tab.Floating`
- floating panes remain distinct from tiled panes after restore

- [ ] **Step 2: Run the focused persistence tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/persist ./tuiv2/bootstrap ./tuiv2/workbench -run 'Floating|Persist|Restore' -count=1`
Expected: FAIL because schema/restore do not round-trip floating state

- [ ] **Step 3: Extend V2 schema minimally but completely**

Implementation notes:
- Add explicit floating entries to `TabEntryV2`; do not infer them indirectly from pane order.
- Persist `PaneID`, `Rect`, and z-order data required to rebuild behavior.
- Keep tiled layout schema unchanged.

- [ ] **Step 4: Implement save/restore round-trip**

Rule:
- `tab.Panes` remains the pane record store
- `tab.Floating` remains the placement/index structure
- restore must rebuild both consistently

- [ ] **Step 5: Run persistence and bootstrap suites**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/persist ./tuiv2/bootstrap ./tuiv2/workbench -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add tuiv2/persist/schema_v2.go tuiv2/persist/workspace_state.go tuiv2/bootstrap/restore.go tuiv2/workbench/types.go tuiv2/workbench/workbench.go tuiv2/persist/workspace_state_test.go tuiv2/bootstrap/bootstrap_test.go tuiv2/workbench/workbench_test.go
git commit -m "fix: 持久化并恢复 floating pane 状态"
```

---

### Task 5: Rebuild The Terminal Metadata Contract

**Files:**
- Modify: `tuiv2/persist/workspace_state.go`
- Modify: `tuiv2/bootstrap/restore.go`
- Modify: `tuiv2/runtime/terminal_registry.go`
- Modify: `tuiv2/app/update.go`
- Modify: `tuiv2/modal/prompt.go`
- Modify: `tuiv2/render/overlays.go`
- Test: `tuiv2/bootstrap/bootstrap_test.go`
- Test: `tuiv2/app/app_test.go`
- Test: `tuiv2/app/feature_test.go`

- [ ] **Step 1: Write failing tests for metadata round-trip and two-step edit flow**

Cover:
- save writes top-level `terminal_metadata`
- restore hydrates runtime metadata cache
- edit-terminal prompt goes `name -> tags`
- saving metadata updates runtime state without rebinding panes

- [ ] **Step 2: Run metadata-focused tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/bootstrap ./tuiv2/app -run 'Metadata|EditTerminal|Prompt' -count=1`
Expected: FAIL

- [ ] **Step 3: Save and restore top-level metadata as the canonical persisted owner**

Rule:
- metadata lives at `WorkspaceStateFileV2.Metadata`
- pane entries continue to store only structural `TerminalID`

- [ ] **Step 4: Upgrade edit flow to match create flow shape**

Implementation notes:
- `edit-terminal-name` advances to `edit-terminal-tags`
- preserve existing tags as editable seed text
- update runtime registry and any visible pane titles after successful save

- [ ] **Step 5: Make prompt rendering informative enough for real workflows**

Add:
- step indicator
- terminal id
- command summary
- short explanatory line when editing terminal metadata

- [ ] **Step 6: Run focused tests, then full `tuiv2/app`**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/bootstrap ./tuiv2/app -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add tuiv2/persist/workspace_state.go tuiv2/bootstrap/restore.go tuiv2/runtime/terminal_registry.go tuiv2/app/update.go tuiv2/modal/prompt.go tuiv2/render/overlays.go tuiv2/bootstrap/bootstrap_test.go tuiv2/app/app_test.go tuiv2/app/feature_test.go
git commit -m "feat: 重建 terminal metadata 持久化与编辑流"
```

---

### Task 6: Complete The Terminal Manager Data Contract Before Changing Its Shape

**Files:**
- Modify: `tuiv2/modal/terminal_manager.go`
- Modify: `tuiv2/app/update.go`
- Modify: `tuiv2/runtime/visible.go`
- Modify: `tuiv2/runtime/runtime.go`
- Modify: `tuiv2/workbench/workbench.go`
- Modify: `tuiv2/render/overlays.go`
- Test: `tuiv2/app/feature_test.go`
- Test: `tuiv2/render/overlays_test.go`

- [ ] **Step 1: Write failing tests for manager item richness**

Cover:
- visible/parked/exited grouping input data
- command/location visibility
- selected item detail sufficiency for attach/edit/kill decisions

- [ ] **Step 2: Run focused manager tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/render -run 'TerminalManager' -count=1`
Expected: FAIL because manager items only expose `ID/Name/State`

- [ ] **Step 3: Enrich manager data projection**

Implementation notes:
- compute whether a terminal is visible or parked from current pane bindings/visible workbench
- include command, location, and observed/bound counts in manager items
- keep the manager state model read-only from the render layer’s perspective

- [ ] **Step 4: Implement real manager actions or strip them**

Preferred minimum complete set:
- Enter: bring here / replace current pane
- Ctrl-T: open in new tab
- Ctrl-O: open in floating pane
- Ctrl-E: edit metadata
- Ctrl-K: kill terminal

- [ ] **Step 5: Re-run manager-focused tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/render -run 'TerminalManager' -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add tuiv2/modal/terminal_manager.go tuiv2/app/update.go tuiv2/runtime/visible.go tuiv2/runtime/runtime.go tuiv2/workbench/workbench.go tuiv2/render/overlays.go tuiv2/app/feature_test.go tuiv2/render/overlays_test.go
git commit -m "feat: 补齐 terminal manager 数据与动作契约"
```

---

### Task 7: Finish Canonical Floating Mode Instead Of Relying On Mouse-Only Behavior

**Files:**
- Modify: `tuiv2/input/actions.go`
- Modify: `tuiv2/input/keymap.go`
- Modify: `tuiv2/orchestrator/orchestrator.go`
- Modify: `tuiv2/workbench/mutate.go`
- Modify: `tuiv2/render/frame.go`
- Test: `tuiv2/app/feature_test.go`
- Test: `tuiv2/workbench/mutate_test.go`

- [ ] **Step 1: Write failing tests for keyboard floating actions**

Cover:
- move
- resize
- reorder to top
- recall/center

- [ ] **Step 2: Run floating-specific tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/workbench -run 'Floating' -count=1`
Expected: FAIL

- [ ] **Step 3: Add the minimal canonical action set**

Recommended bindings:
- `h/j/k/l` move
- `H/J/K/L` resize
- `c` center/recall
- optional `[` / `]` or `Tab` for z-order only if truly needed now

- [ ] **Step 4: Keep the implementation structural**

Rule:
- keyboard floating actions should mutate `workbench` via orchestrator
- mouse behavior remains additive, not the only complete control path

- [ ] **Step 5: Update status hints to match reality**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/workbench -run 'Floating|Status' -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add tuiv2/input/actions.go tuiv2/input/keymap.go tuiv2/orchestrator/orchestrator.go tuiv2/workbench/mutate.go tuiv2/render/frame.go tuiv2/app/feature_test.go tuiv2/workbench/mutate_test.go
git commit -m "feat: 完成 floating mode 的键盘交互"
```

---

### Task 8: Deliver The Pane Lifecycle UI And Binding Presentation

**Files:**
- Modify: `tuiv2/render/coordinator.go`
- Modify: `tuiv2/render/badge.go`
- Modify: `tuiv2/render/frame.go`
- Modify: `tuiv2/runtime/visible.go`
- Modify: `tuiv2/runtime/runtime.go`
- Test: `tuiv2/render/coordinator_test.go`
- Test: `tuiv2/render/title_test.go`

- [ ] **Step 1: Write failing render tests for unconnected/exited pane presentation**

Cover:
- unconnected pane empty-state content
- exited pane badge and preserved history
- owner/follower/share count in title/meta presentation where available

- [ ] **Step 2: Run render-focused tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/render -run 'Pane|Title|Badge|Coordinator' -count=1`
Expected: FAIL

- [ ] **Step 3: Replace placeholder unbound content with actionable empty state**

Minimum content:
- attach existing terminal
- create new terminal
- open terminal pool/manager

- [ ] **Step 4: Integrate pane meta into the actual frame output**

Rule:
- left side remains terminal/title-first
- right side carries compact connection/display state
- do not regress narrow-pane rendering

- [ ] **Step 5: Run render suite**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/render -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add tuiv2/render/coordinator.go tuiv2/render/badge.go tuiv2/render/frame.go tuiv2/runtime/visible.go tuiv2/runtime/runtime.go tuiv2/render/coordinator_test.go tuiv2/render/title_test.go
git commit -m "feat: 完成 pane 生命周期与连接状态展示"
```

---

### Task 9: Move Terminal Pool From Modal To First-Class Page

**Files:**
- Modify: `tuiv2/input/mode.go`
- Modify: `tuiv2/input/keymap.go`
- Modify: `tuiv2/app/model.go`
- Modify: `tuiv2/app/update.go`
- Modify: `tuiv2/render/adapter.go`
- Modify: `tuiv2/render/coordinator.go`
- Modify: `tuiv2/render/frame.go`
- Modify: `tuiv2/modal/terminal_manager.go`
- Modify: `tui-product-definition-design.md`
- Modify: `tuiv2-current-status.md`
- Test: `tuiv2/app/feature_test.go`
- Test: `tuiv2/render/coordinator_test.go`

- [ ] **Step 1: Write failing tests for entering/leaving a full-page Terminal Pool surface**

Cover:
- global entry opens page, not overlay
- body uses dedicated page layout
- `Esc` or explicit command returns to workbench without losing workbench state

- [ ] **Step 2: Run focused tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/app ./tuiv2/render -run 'TerminalManager|TerminalPool|Global' -count=1`
Expected: FAIL

- [ ] **Step 3: Introduce a first-class page concept with minimal surface area**

Recommended approach:
- keep modal system for picker/prompt/help/workspace-picker
- move manager into a workbench-adjacent “page mode” or “surface mode”
- do not overload modal types to represent both overlay and page

- [ ] **Step 4: Reuse the data contract from Task 6**

Rule:
- no page-shape rewrite until the data/action contract already works

- [ ] **Step 5: Update docs and run verification**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add tuiv2/input/mode.go tuiv2/input/keymap.go tuiv2/app/model.go tuiv2/app/update.go tuiv2/render/adapter.go tuiv2/render/coordinator.go tuiv2/render/frame.go tuiv2/modal/terminal_manager.go tui-product-definition-design.md tuiv2-current-status.md tuiv2/app/feature_test.go tuiv2/render/coordinator_test.go
git commit -m "refactor: 将 terminal pool 升级为独立页面"
```

---

### Task 10: Tighten Architecture Boundaries After Behavior Is Stable

**Files:**
- Modify: `tuiv2/runtime/pane_binding.go`
- Modify: `tuiv2/runtime/create_attach.go`
- Modify: `tuiv2/runtime/resize.go`
- Modify: `tuiv2/orchestrator/effects.go`
- Modify: `tuiv2/orchestrator/orchestrator.go`
- Modify: `tuiv2/app/update.go`
- Modify: `tui-v2-migration-architecture-plan.md`
- Test: `tuiv2/runtime/runtime_test.go`
- Test: `tuiv2/orchestrator/orchestrator_test.go`

- [ ] **Step 1: Write failing tests around structural vs runtime binding ownership**

Cover:
- `workbench.PaneState.TerminalID` remains the structural source of truth
- runtime binding only reflects channel/connectivity/role
- resize path behaves correctly without making runtime the writable structural owner

- [ ] **Step 2: Run focused architecture tests**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/runtime ./tuiv2/orchestrator -run 'Bind|Attach|Resize|Owner|Follower' -count=1`
Expected: FAIL or expose current architectural coupling

- [ ] **Step 3: Remove structural duplication from `runtime.PaneBinding` if safely possible**

Rule:
- only do this after Tasks 1-9 are stable
- if complete removal is too risky in one pass, introduce a transitional read-only contract with clear comments and tests

- [ ] **Step 4: Push multi-module flows out of `app/update.go` where doing so reduces ambiguity**

Candidates:
- manager action execution
- prompt submit result application
- attach/split/create composite flows

- [ ] **Step 5: Run full verification**

Run: `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/... && PATH="$PWD/.toolchain/go/bin:$PATH" go build ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add tuiv2/runtime/pane_binding.go tuiv2/runtime/create_attach.go tuiv2/runtime/resize.go tuiv2/orchestrator/effects.go tuiv2/orchestrator/orchestrator.go tuiv2/app/update.go tui-v2-migration-architecture-plan.md tuiv2/runtime/runtime_test.go tuiv2/orchestrator/orchestrator_test.go
git commit -m "refactor: 收紧 tuiv2 绑定与编排边界"
```

---

## Final Verification Checklist

- [ ] `PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/...`
- [ ] `PATH="$PWD/.toolchain/go/bin:$PATH" go build ./...`
- [ ] `rg '"github.com/lozzow/termx/termx-core/tui"' tuiv2/`
- [ ] Manual smoke test:
  - start `termx`
  - create terminal from picker
  - search in picker
  - create/switch workspace from workspace picker
  - open Terminal Pool
  - attach terminal into current pane/new tab/floating pane
  - edit terminal metadata name/tags
  - save state, restart, verify floating and metadata restore
  - verify unconnected/exited pane presentation

## Notes For Implementation

- Favor small, verified steps over broad rewrites.
- Keep `Terminal Pool page migration` late; doing it early will harden the wrong data contract.
- If a task uncovers more undocumented fake behavior, stop and normalize docs/help/status before adding more code.
- Where legacy has a mature solution, copy structure and behavior, but not legacy architectural coupling.
