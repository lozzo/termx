# Workbench Migration Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce `Workbench` as the real main-workbench object by migrating workspace-tree ownership, workbench entrypoints, and workbench read boundaries out of `tui/model.go` without changing terminal/runtime architecture yet.

**Architecture:** This plan performs a migration, not a rewrite. Phase 1 first gives `Workbench` ownership of the workspace tree, then routes high-level workbench actions through `Workbench`, then creates read-side accessors so render code can begin reading workbench state through a stable boundary. Terminal proxy/store/coordinator work is explicitly deferred.

**Tech Stack:** Go, Bubble Tea, existing `tui/` package, Go tests

---

## File map

### Existing files to modify
- `tui/model.go`
  - Current `Model`, `Workspace`, `Tab`, `Pane` definitions and many workbench helpers
  - Will stop being the semantic owner of the workspace tree
- `tui/workspace_model.go`
  - Existing `Workspace` / `Tab` object methods
  - Will remain the home for domain behavior already on those objects
- `tui/workspace_picker.go`
  - Current workspace switching / snapshot / workspace store logic
  - Will move to call `Workbench` methods
- `tui/workspace_state.go`
  - Export/import workspace-set state
  - Will start reading the workspace tree through `Workbench`
- `tui/input_router.go`
  - High-level workbench input entrypoints
  - Will call `Workbench` methods instead of model-local workspace tree helpers
- `tui/picker.go`
  - Uses current tab / pane / workspace selection helpers
  - Will use `Workbench` read APIs where appropriate
- `tui/render.go`
  - Main workbench render reads
  - Will start reading visible workbench state via `Workbench`
- `tui/bootstrap.go`
  - Startup restore/autolayout fallback paths
  - Will initialize `Workbench` state instead of assuming model-owned workspace tree
- `tui/model_test.go`
  - Existing broad TUI behavior tests
  - Add/adjust tests for workbench ownership and entrypoints
- `tui/workspace_state_test.go`
  - Existing workspace persistence tests
  - Add/adjust tests for workbench-backed workspace tree
- `tui/workspace_picker_test.go`
  - Existing workspace switching tests
  - Add/adjust tests for workbench-backed switching

### New files to create
- `tui/workbench.go`
  - `Workbench` type definition
  - Workspace-tree ownership (`Current`, `Store`, `Order`, `ActiveIndex` or equivalent)
  - Workspace-tree root accessors and mutators
- `tui/workbench_actions.go`
  - High-level workbench entrypoints (current object lookup, tab/pane/floating action entrypoints)
- `tui/workbench_view.go`
  - Read-side visible-state accessors for rendering and UI reads
- `tui/workbench_test.go`
  - Focused tests for workbench ownership and action forwarding

### Boundaries to preserve in Phase 1
- Do **not** introduce `TerminalStore`
- Do **not** introduce `Terminal` proxy objects
- Do **not** rewrite renderer internals or render loop strategy
- Do **not** move terminal runtime/stream/resize-owner logic out of current locations yet

---

## Task 1: Introduce the `Workbench` root object

**Files:**
- Create: `tui/workbench.go`
- Modify: `tui/model.go`
- Test: `tui/workbench_test.go`

- [ ] **Step 1: Write the failing ownership test**

```go
func TestWorkbenchOwnsWorkspaceTree(t *testing.T) {
	wb := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})

	if wb.Current().Name != "main" {
		t.Fatalf("expected current workspace main, got %q", wb.Current().Name)
	}
	if len(wb.Order()) != 1 || wb.Order()[0] != "main" {
		t.Fatalf("expected workbench order [main], got %v", wb.Order())
	}
	if wb.ActiveWorkspaceIndex() != 0 {
		t.Fatalf("expected active workspace index 0, got %d", wb.ActiveWorkspaceIndex())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run TestWorkbenchOwnsWorkspaceTree -count=1`
Expected: FAIL with `undefined: NewWorkbench` or missing `Workbench` methods.

- [ ] **Step 3: Add the minimal `Workbench` type and ownership API**

Create `tui/workbench.go` with the smallest ownership surface needed for Phase 1.1:

```go
package tui

type Workbench struct {
	current Workspace
	store   map[string]Workspace
	order   []string
	active  int
}

func NewWorkbench(initial Workspace) *Workbench {
	name := initial.Name
	if name == "" {
		name = "main"
		initial.Name = name
	}
	return &Workbench{
		current: initial,
		store:   map[string]Workspace{name: initial},
		order:   []string{name},
		active:  0,
	}
}

func (w *Workbench) Current() *Workspace {
	if w == nil {
		return nil
	}
	return &w.current
}

func (w *Workbench) SetCurrent(workspace Workspace) {
	if w == nil {
		return
	}
	w.current = workspace
}

func (w *Workbench) Store() map[string]Workspace {
	if w == nil {
		return nil
	}
	return w.store
}

func (w *Workbench) Order() []string {
	if w == nil {
		return nil
	}
	return w.order
}

func (w *Workbench) ActiveWorkspaceIndex() int {
	if w == nil {
		return 0
	}
	return w.active
}
```

Modify `tui/model.go` to add a field on `Model`:

```go
type Model struct {
	// existing fields...
	workbench *Workbench
}
```

And initialize it in `NewModel(...)` after the initial workspace is created:

```go
model.workbench = NewWorkbench(model.workspace)
```

- [ ] **Step 4: Run targeted tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestWorkbenchOwnsWorkspaceTree' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/workbench.go tui/model.go tui/workbench_test.go
git commit -m "refactor: introduce workbench root object"
```

---

## Task 2: Move workspace-tree ownership reads behind `Workbench`

**Files:**
- Modify: `tui/model.go`
- Modify: `tui/workspace_picker.go`
- Modify: `tui/workspace_state.go`
- Test: `tui/workbench_test.go`
- Test: `tui/workspace_picker_test.go`
- Test: `tui/workspace_state_test.go`

- [ ] **Step 1: Write failing tests for snapshot/switch through `Workbench`**

```go
func TestWorkbenchSnapshotCurrentWorkspaceUpdatesStore(t *testing.T) {
	wb := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})
	wb.SetCurrent(Workspace{Name: "renamed", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})

	wb.SnapshotCurrent()

	if _, ok := wb.Store()["renamed"]; !ok {
		t.Fatal("expected current workspace snapshot stored under renamed")
	}
}

func TestWorkbenchSwitchWorkspaceChangesCurrent(t *testing.T) {
	wb := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})
	wb.Store()["alt"] = Workspace{Name: "alt", Tabs: []*Tab{newTab("1")}, ActiveTab: 0}
	wb.SetOrder([]string{"main", "alt"})

	if err := wb.SwitchTo("alt"); err != nil {
		t.Fatalf("switch failed: %v", err)
	}
	if wb.Current().Name != "alt" {
		t.Fatalf("expected alt current workspace, got %q", wb.Current().Name)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestWorkbench(SnapshotCurrentWorkspaceUpdatesStore|SwitchWorkspaceChangesCurrent)' -count=1`
Expected: FAIL with undefined `SnapshotCurrent`, `SetOrder`, or `SwitchTo`.

- [ ] **Step 3: Add ownership methods to `Workbench`**

Extend `tui/workbench.go` with workspace-tree mutators:

```go
func (w *Workbench) SetOrder(order []string) {
	if w == nil {
		return
	}
	w.order = append([]string(nil), order...)
}

func (w *Workbench) SnapshotCurrent() {
	if w == nil {
		return
	}
	if w.store == nil {
		w.store = map[string]Workspace{}
	}
	name := w.current.Name
	if name == "" {
		name = "main"
		w.current.Name = name
	}
	w.store[name] = w.current
	if len(w.order) == 0 {
		w.order = []string{name}
		w.active = 0
	}
}

func (w *Workbench) SwitchTo(name string) error {
	if w == nil {
		return nil
	}
	workspace, ok := w.store[name]
	if !ok {
		return fmt.Errorf("workspace %q not found", name)
	}
	w.current = workspace
	idx := slices.Index(w.order, name)
	if idx >= 0 {
		w.active = idx
	}
	return nil
}
```

- [ ] **Step 4: Rewire existing workspace helpers to delegate to `Workbench`**

Modify `tui/workspace_picker.go` so current model helpers use the workbench-owned tree instead of model-owned tree. The direction should look like this:

```go
func (m *Model) snapshotCurrentWorkspace() {
	if m == nil || m.workbench == nil {
		return
	}
	m.workbench.SetCurrent(m.workspace)
	m.workbench.SnapshotCurrent()
}

func (m *Model) switchWorkspaceCmd(name string) tea.Cmd {
	m.snapshotCurrentWorkspace()
	return func() tea.Msg {
		if err := m.workbench.SwitchTo(name); err != nil {
			return errMsg{err}
		}
		m.workspace = *m.workbench.Current()
		return noticeMsg{text: fmt.Sprintf("switched to workspace %q", name)}
	}
}
```

Also update `tui/workspace_state.go` reads so export/import walk `m.workbench.Order()` and `m.workbench.Store()` instead of directly trusting `model.workspaceOrder` / `model.workspaceStore`.

- [ ] **Step 5: Run focused workspace tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestWorkbench(SnapshotCurrentWorkspaceUpdatesStore|SwitchWorkspaceChangesCurrent)|TestLoadWorkspaceStateCmd|TestSwitchWorkspace' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add tui/workbench.go tui/workspace_picker.go tui/workspace_state.go tui/workbench_test.go tui/workspace_picker_test.go tui/workspace_state_test.go
git commit -m "refactor: move workspace tree ownership to workbench"
```

---

## Task 3: Add `Workbench` lookup entrypoints for current workspace/tab/pane

**Files:**
- Create: `tui/workbench_actions.go`
- Modify: `tui/model.go`
- Modify: `tui/input_router.go`
- Modify: `tui/picker.go`
- Test: `tui/workbench_test.go`

- [ ] **Step 1: Write failing lookup tests**

```go
func TestWorkbenchCurrentTabReturnsActiveTab(t *testing.T) {
	ws := Workspace{Name: "main", Tabs: []*Tab{newTab("1"), newTab("2")}, ActiveTab: 1}
	wb := NewWorkbench(ws)

	tab := wb.CurrentTab()
	if tab == nil || tab.Name != "2" {
		t.Fatalf("expected active tab 2, got %#v", tab)
	}
}

func TestWorkbenchActivePaneReturnsWorkspaceActivePane(t *testing.T) {
	tab := newTab("1")
	pane := &Pane{ID: "pane-1", Title: "one", Viewport: &Viewport{}}
	tab.Panes[pane.ID] = pane
	tab.ActivePaneID = pane.ID
	wb := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{tab}, ActiveTab: 0})

	got := wb.ActivePane()
	if got == nil || got.ID != pane.ID {
		t.Fatalf("expected active pane %q, got %#v", pane.ID, got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestWorkbench(CurrentTabReturnsActiveTab|ActivePaneReturnsWorkspaceActivePane)' -count=1`
Expected: FAIL with undefined `CurrentTab` / `ActivePane`.

- [ ] **Step 3: Add workbench lookup APIs**

Create `tui/workbench_actions.go`:

```go
package tui

func (w *Workbench) CurrentWorkspace() *Workspace {
	return w.Current()
}

func (w *Workbench) CurrentTab() *Tab {
	if w == nil {
		return nil
	}
	return w.current.CurrentTab()
}

func (w *Workbench) ActivePane() *Pane {
	return activePane(w.CurrentTab())
}

func (w *Workbench) FindPane(paneID string) *Pane {
	if w == nil {
		return nil
	}
	for _, tab := range w.current.Tabs {
		if tab == nil {
			continue
		}
		if pane, ok := tab.Panes[paneID]; ok {
			return pane
		}
	}
	return nil
}
```

- [ ] **Step 4: Rewire model helpers to delegate lookups to `Workbench`**

Modify `tui/model.go` helper reads in this style:

```go
func (m *Model) currentTab() *Tab {
	if m == nil || m.workbench == nil {
		return nil
	}
	return m.workbench.CurrentTab()
}
```

Update other high-read callers in `tui/input_router.go` and `tui/picker.go` to rely on `m.currentTab()` / `m.workbench.ActivePane()` rather than directly reaching for `m.workspace` as their conceptual owner.

- [ ] **Step 5: Run focused lookup tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestWorkbench(CurrentTabReturnsActiveTab|ActivePaneReturnsWorkspaceActivePane)' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add tui/workbench_actions.go tui/model.go tui/input_router.go tui/picker.go tui/workbench_test.go
git commit -m "refactor: add workbench lookup entrypoints"
```

---

## Task 4: Move high-level tab switching and pane focus entrypoints onto `Workbench`

**Files:**
- Modify: `tui/workbench_actions.go`
- Modify: `tui/input_router.go`
- Modify: `tui/prefix.go`
- Modify: `tui/model.go`
- Test: `tui/workbench_test.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Write failing action-forwarding tests**

```go
func TestWorkbenchActivateTabDelegatesToWorkspace(t *testing.T) {
	wb := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1"), newTab("2")}, ActiveTab: 0})

	if !wb.ActivateTab(1) {
		t.Fatal("expected activate tab to succeed")
	}
	if wb.CurrentWorkspace().ActiveTab != 1 {
		t.Fatalf("expected active tab 1, got %d", wb.CurrentWorkspace().ActiveTab)
	}
}

func TestWorkbenchFocusPaneDelegatesToWorkspace(t *testing.T) {
	tab := newTab("1")
	first := &Pane{ID: "p1", Viewport: &Viewport{}}
	second := &Pane{ID: "p2", Viewport: &Viewport{}}
	tab.Panes[first.ID] = first
	tab.Panes[second.ID] = second
	tab.ActivePaneID = first.ID
	wb := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{tab}, ActiveTab: 0})

	if !wb.FocusPane(second.ID) {
		t.Fatal("expected focus pane to succeed")
	}
	if wb.CurrentTab().ActivePaneID != second.ID {
		t.Fatalf("expected active pane %q, got %q", second.ID, wb.CurrentTab().ActivePaneID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestWorkbench(ActivateTabDelegatesToWorkspace|FocusPaneDelegatesToWorkspace)' -count=1`
Expected: FAIL with undefined methods.

- [ ] **Step 3: Add minimal workbench action entrypoints**

Extend `tui/workbench_actions.go`:

```go
func (w *Workbench) ActivateTab(index int) bool {
	if w == nil {
		return false
	}
	if !w.current.ActivateTab(index) {
		return false
	}
	w.store[w.current.Name] = w.current
	return true
}

func (w *Workbench) FocusPane(paneID string) bool {
	if w == nil {
		return false
	}
	if !w.current.FocusPane(paneID) {
		return false
	}
	w.store[w.current.Name] = w.current
	return true
}
```

- [ ] **Step 4: Rewire high-level callers to go through `Workbench`**

Modify `tui/model.go` / `tui/input_router.go` / `tui/prefix.go` in this style:

```go
func (m *Model) activateTab(index int) {
	if m == nil || m.workbench == nil {
		return
	}
	if !m.workbench.ActivateTab(index) {
		return
	}
	m.workspace = *m.workbench.CurrentWorkspace()
	m.invalidateRender()
}
```

And similarly for high-level pane focus entrypoints.

- [ ] **Step 5: Run focused behavior tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestWorkbench(ActivateTabDelegatesToWorkspace|FocusPaneDelegatesToWorkspace)|Test.*ActivateTab|Test.*FocusPane' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add tui/workbench_actions.go tui/input_router.go tui/prefix.go tui/model.go tui/workbench_test.go tui/model_test.go
git commit -m "refactor: route workbench actions through workbench"
```

---

## Task 5: Move high-level pane lifecycle entrypoints onto `Workbench`

**Files:**
- Modify: `tui/workbench_actions.go`
- Modify: `tui/model.go`
- Modify: `tui/input_router.go`
- Test: `tui/workbench_test.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Write failing workbench pane-lifecycle tests**

```go
func TestWorkbenchRemovePaneDelegatesToWorkspace(t *testing.T) {
	tab := newTab("1")
	pane := &Pane{ID: "pane-1", Viewport: &Viewport{}}
	tab.Panes[pane.ID] = pane
	tab.ActivePaneID = pane.ID
	tab.Root = NewLeaf(pane.ID)
	wb := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{tab}, ActiveTab: 0})

	removed, workspaceEmpty, terminalID := wb.RemovePane(pane.ID)
	if removed || workspaceEmpty {
		t.Fatalf("expected pane removal only, got removed=%v workspaceEmpty=%v", removed, workspaceEmpty)
	}
	if terminalID != "" {
		t.Fatalf("expected empty terminal id for unbound pane, got %q", terminalID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestWorkbenchRemovePaneDelegatesToWorkspace' -count=1`
Expected: FAIL with undefined `RemovePane`.

- [ ] **Step 3: Add workbench lifecycle entrypoints without moving runtime cleanup yet**

Extend `tui/workbench_actions.go`:

```go
func (w *Workbench) RemovePane(paneID string) (tabRemoved bool, workspaceEmpty bool, terminalID string) {
	if w == nil {
		return false, false, ""
	}
	tabRemoved, workspaceEmpty, terminalID = w.current.RemovePane(paneID)
	w.store[w.current.Name] = w.current
	return tabRemoved, workspaceEmpty, terminalID
}
```

Important: keep terminal/runtime cleanup in `Model` for this phase. Only move the workbench-owned structure mutation entrypoint.

- [ ] **Step 4: Rewire `Model.removePane` to call `Workbench.RemovePane` for the structural part**

Modify `tui/model.go` in this style:

```go
tabRemoved, workspaceEmpty, removedTerminalID := m.workbench.RemovePane(paneID)
m.workspace = *m.workbench.CurrentWorkspace()
```

Keep the existing stream/resize/runtime cleanup after that call in `Model` unchanged.

- [ ] **Step 5: Run focused lifecycle tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestWorkbenchRemovePaneDelegatesToWorkspace|TestRemovePane' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add tui/workbench_actions.go tui/model.go tui/input_router.go tui/workbench_test.go tui/model_test.go
git commit -m "refactor: move pane lifecycle entrypoints into workbench"
```

---

## Task 6: Add workbench read-side APIs for visible workspace/tab/pane state

**Files:**
- Create: `tui/workbench_view.go`
- Modify: `tui/render.go`
- Modify: `tui/model.go`
- Test: `tui/workbench_test.go`

- [ ] **Step 1: Write failing read-boundary tests**

```go
func TestWorkbenchVisibleStateExposesCurrentTabAndActivePane(t *testing.T) {
	tab := newTab("1")
	pane := &Pane{ID: "pane-1", Viewport: &Viewport{}}
	tab.Panes[pane.ID] = pane
	tab.ActivePaneID = pane.ID
	wb := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{tab}, ActiveTab: 0})

	view := wb.VisibleState()
	if view.Workspace == nil || view.Workspace.Name != "main" {
		t.Fatalf("expected visible workspace main, got %#v", view.Workspace)
	}
	if view.Tab == nil || view.Tab.Name != "1" {
		t.Fatalf("expected visible tab 1, got %#v", view.Tab)
	}
	if view.ActivePane == nil || view.ActivePane.ID != pane.ID {
		t.Fatalf("expected visible active pane %q, got %#v", pane.ID, view.ActivePane)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestWorkbenchVisibleStateExposesCurrentTabAndActivePane' -count=1`
Expected: FAIL with undefined `VisibleState`.

- [ ] **Step 3: Add a minimal workbench read model**

Create `tui/workbench_view.go`:

```go
package tui

type WorkbenchVisibleState struct {
	Workspace  *Workspace
	Tab        *Tab
	ActivePane *Pane
}

func (w *Workbench) VisibleState() WorkbenchVisibleState {
	if w == nil {
		return WorkbenchVisibleState{}
	}
	workspace := w.CurrentWorkspace()
	tab := w.CurrentTab()
	return WorkbenchVisibleState{
		Workspace:  workspace,
		Tab:        tab,
		ActivePane: activePane(tab),
	}
}
```

- [ ] **Step 4: Rewire render reads to use the workbench read boundary where practical**

Modify workbench-facing render reads in `tui/render.go` in this style:

```go
state := m.workbench.VisibleState()
tab := state.Tab
pane := state.ActivePane
```

Do not rewrite renderer internals. Only replace direct conceptual reads of current workbench state with the `Workbench` read boundary.

- [ ] **Step 5: Run focused render-facing tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestWorkbenchVisibleStateExposesCurrentTabAndActivePane|TestRender' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add tui/workbench_view.go tui/render.go tui/workbench_test.go tui/model.go
git commit -m "refactor: add workbench read boundary for rendering"
```

---

## Task 7: Verify Phase 1 end-to-end without touching terminal architecture

**Files:**
- Modify: `docs/tui-architecture-migration-brainstorm.md` (optional status note only if desired)
- Test: `tui/model_test.go`
- Test: `tui/workspace_picker_test.go`
- Test: `tui/workspace_state_test.go`

- [ ] **Step 1: Run focused regression suite for workbench migration paths**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestWorkbench|TestSwitchWorkspace|TestLoadWorkspaceStateCmd|TestRemovePane|TestRender' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full TUI package tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -count=1`
Expected: PASS.

- [ ] **Step 3: Run full repository tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1`
Expected: PASS.

- [ ] **Step 4: Build the CLI**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go build ./cmd/termx`
Expected: build succeeds with no new errors.

- [ ] **Step 5: Commit the completed phase**

```bash
git add tui/workbench.go tui/workbench_actions.go tui/workbench_view.go tui/model.go tui/workspace_picker.go tui/workspace_state.go tui/input_router.go tui/picker.go tui/prefix.go tui/render.go tui/workbench_test.go tui/model_test.go tui/workspace_picker_test.go tui/workspace_state_test.go
git commit -m "refactor: migrate workbench phase 1 ownership and entrypoints"
```

---

## Notes for the implementing engineer

### Phase 1 success criteria
- `Workbench` is a real object, not a façade
- workspace-tree ownership is meaningfully centralized in `Workbench`
- high-level workbench entrypoints begin at `Workbench`
- render-facing reads of current workbench state have a stable `Workbench` boundary
- terminal/runtime architecture is intentionally **not** migrated in this phase

### Explicit non-goals
- No `TerminalStore`
- No TUI-local `Terminal` proxy object yet
- No `TerminalCoordinator` extraction yet
- No `Resizer` extraction yet
- No renderer/render-loop redesign
- No large naming cleanup like `Pane -> Panel`

---

## Self-review

### Spec coverage
- `Phase 1.1` covered by Tasks 1-2
- `Phase 1.2` covered by Tasks 3-5
- `Phase 1.3` covered by Task 6
- Phase-wide verification covered by Task 7

### Placeholder scan
- No `TODO`/`TBD`
- Every task has concrete files, test commands, and code skeletons
- Terminal/runtime work is explicitly deferred rather than vaguely referenced

### Type consistency
- `Workbench` is introduced in `tui/workbench.go`
- Lookup/action/read APIs are consistently named `CurrentWorkspace`, `CurrentTab`, `ActivePane`, `VisibleState`, `SnapshotCurrent`, `SwitchTo`, `ActivateTab`, `FocusPane`, `RemovePane`

---

Plan complete and saved to `docs/superpowers/plans/2026-03-27-workbench-migration-phase1.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
