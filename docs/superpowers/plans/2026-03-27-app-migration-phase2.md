# App Migration Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce `App` as the sole high-level coordination entrypoint above `Workbench`, so `Model` begins to shrink toward a Bubble Tea shell while preserving current runtime behavior.

**Architecture:** This phase is a migration, not a rewrite. We will first establish a real `App` object and wire it into `Model`, then move a carefully scoped set of high-level action and message-routing entrypoints from `Model` into `App`, while deliberately leaving terminal runtime, renderer, render loop, and other low-level paths in place. The key constraint is that `App` must own high-level orchestration, but must not become a second `Model` or absorb runtime internals.

**Tech Stack:** Go, Bubble Tea, existing `tui/` package, Go tests

---

## File map

### Existing files to modify
- `tui/model.go`
  - Current `Model` type, `NewModel`, `Update`, and many high-level action entrypoints
  - Will stop being the semantic high-level application root
- `tui/prefix.go`
  - Prefix-mode dispatch and high-level command result flow
  - Some high-level dispatch paths will move behind `App`
- `tui/input_router.go`
  - Keyboard and event input routing
  - High-level routing entrypoints may be redirected through `App`
- `tui/picker.go`
  - High-level picker open paths
  - Selected opener entrypoints may move behind `App`
- `tui/model_test.go`
  - Existing broad integration coverage for TUI behavior
  - Add/adjust tests for `Model -> App -> Workbench` wiring and regressions
- `tui/workbench.go`
  - Already owns workspace-tree and high-level workbench actions
  - `App` will hold and call into it

### New files to create
- `tui/app.go`
  - `App` type definition
  - `NewApp(...)`
  - Ownership of high-level coordination dependencies
- `tui/app_actions.go`
  - High-level action / command routing entrypoints
- `tui/app_test.go`
  - Focused tests for `App` high-level forwarding and routing behavior

### Boundaries to preserve in Phase 2
- Do **not** introduce `TerminalStore`
- Do **not** introduce TUI-local `Terminal` proxy objects
- Do **not** extract `TerminalCoordinator`
- Do **not** extract `Resizer`
- Do **not** redesign renderer or render loop
- Do **not** migrate stream / attach / recovery / resize-owner runtime internals into `App`

---

## Task 1: Introduce the `App` root object

**Files:**
- Create: `tui/app.go`
- Modify: `tui/model.go`
- Test: `tui/app_test.go`

- [ ] **Step 1: Write the failing `App` bootstrap test**

```go
func TestNewAppHoldsWorkbenchReference(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})
	app := NewApp(workbench)

	if app == nil {
		t.Fatal("expected app")
	}
	if app.Workbench() != workbench {
		t.Fatal("expected app to hold workbench reference")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestNewAppHoldsWorkbenchReference' -count=1`
Expected: FAIL with `undefined: NewApp` or missing `Workbench()`.

- [ ] **Step 3: Add the minimal `App` type**

Create `tui/app.go`:

```go
package tui

type App struct {
	workbench *Workbench
}

func NewApp(workbench *Workbench) *App {
	return &App{workbench: workbench}
}

func (a *App) Workbench() *Workbench {
	if a == nil {
		return nil
	}
	return a.workbench
}
```

Modify `tui/model.go` to add a field:

```go
type Model struct {
	// existing fields...
	app *App
}
```

And initialize it in `NewModel(...)` after `workbench` is created:

```go
app := NewApp(workbench)
```

Then store it in the model literal:

```go
app: app,
```

- [ ] **Step 4: Run focused bootstrap test**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestNewAppHoldsWorkbenchReference' -count=1`
Expected: PASS.

---

## Task 2: Move high-level tab and pane action routing into `App`

**Files:**
- Modify: `tui/app.go`
- Create: `tui/app_actions.go`
- Modify: `tui/model.go`
- Test: `tui/app_test.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Write failing `App` action-forwarding tests**

Add to `tui/app_test.go`:

```go
func TestAppActivateTabDelegatesToWorkbench(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1"), newTab("2")}, ActiveTab: 0})
	app := NewApp(workbench)

	if !app.ActivateTab(1) {
		t.Fatal("expected activate tab to succeed")
	}
	if workbench.CurrentWorkspace().ActiveTab != 1 {
		t.Fatalf("expected workbench active tab 1, got %d", workbench.CurrentWorkspace().ActiveTab)
	}
}

func TestAppFocusPaneDelegatesToWorkbench(t *testing.T) {
	tab := &Tab{
		Name:         "1",
		Panes:        map[string]*Pane{"p1": {ID: "p1", Viewport: &Viewport{}}, "p2": {ID: "p2", Viewport: &Viewport{}}},
		ActivePaneID: "p1",
	}
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{tab}, ActiveTab: 0})
	app := NewApp(workbench)

	if !app.FocusPane("p2") {
		t.Fatal("expected focus pane to succeed")
	}
	if workbench.CurrentTab().ActivePaneID != "p2" {
		t.Fatalf("expected active pane p2, got %q", workbench.CurrentTab().ActivePaneID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestApp(ActivateTabDelegatesToWorkbench|FocusPaneDelegatesToWorkbench)' -count=1`
Expected: FAIL with undefined `ActivateTab` / `FocusPane` on `App`.

- [ ] **Step 3: Add minimal `App` action routing**

Create `tui/app_actions.go`:

```go
package tui

func (a *App) ActivateTab(index int) bool {
	if a == nil || a.workbench == nil {
		return false
	}
	return a.workbench.ActivateTab(index)
}

func (a *App) FocusPane(paneID string) bool {
	if a == nil || a.workbench == nil {
		return false
	}
	return a.workbench.FocusPane(paneID)
}
```

- [ ] **Step 4: Rewire `Model` tab/pane action entrypoints to call `App`**

Modify `tui/model.go` so `activateTab` uses `App`:

```go
func (m *Model) activateTab(index int) tea.Cmd {
	if m == nil || m.app == nil || m.workbench == nil {
		return nil
	}
	if current := m.workbench.Current(); current != nil {
		*current = *cloneWorkspace(m.workspace)
		m.workbench.SnapshotCurrent()
	}
	if !m.app.ActivateTab(index) {
		return nil
	}
	if workspace := m.workbench.CurrentWorkspace(); workspace != nil {
		m.workspace = *cloneWorkspace(*workspace)
	}
	m.invalidateRender()
	return tea.Batch(m.resizeVisiblePanesCmd(), m.autoAcquireCurrentTabResizeCmd())
}
```

Modify `focusPaneByID` in the same file:

```go
func (m *Model) focusPaneByID(paneID string) {
	if m == nil || strings.TrimSpace(paneID) == "" {
		return
	}
	if m.app == nil || m.workbench == nil {
		if m.workspace.FocusPane(paneID) {
			m.invalidateRender()
		}
		return
	}
	if current := m.workbench.Current(); current != nil {
		*current = *cloneWorkspace(m.workspace)
		m.workbench.SnapshotCurrent()
	}
	if !m.app.FocusPane(paneID) {
		return
	}
	if workspace := m.workbench.CurrentWorkspace(); workspace != nil {
		m.workspace = *cloneWorkspace(*workspace)
	}
	m.invalidateRender()
}
```

- [ ] **Step 5: Run focused action tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestApp(ActivateTabDelegatesToWorkbench|FocusPaneDelegatesToWorkbench)|Test.*ActivateTab|Test.*FocusPane' -count=1`
Expected: PASS.

---

## Task 3: Move high-level picker openers into `App`

**Files:**
- Modify: `tui/app_actions.go`
- Modify: `tui/picker.go`
- Modify: `tui/model.go`
- Test: `tui/app_test.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Write failing picker-routing tests**

Add to `tui/app_test.go`:

```go
func TestAppOpenTerminalPickerUsesWorkbenchSelection(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{{Name: "1", Panes: map[string]*Pane{"pane-1": {ID: "pane-1", Viewport: &Viewport{}}}, ActivePaneID: "pane-1"}}, ActiveTab: 0})
	app := NewApp(workbench)

	action, allowCreate := app.TerminalPickerContext()
	if action.Kind != terminalPickerActionReplace {
		t.Fatalf("expected replace picker action, got %v", action.Kind)
	}
	if allowCreate {
		t.Fatal("expected active bound pane not to force create")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestAppOpenTerminalPickerUsesWorkbenchSelection' -count=1`
Expected: FAIL with undefined `TerminalPickerContext`.

- [ ] **Step 3: Add minimal picker context API to `App`**

Extend `tui/app_actions.go`:

```go
func (a *App) TerminalPickerContext() (terminalPickerAction, bool) {
	action := terminalPickerAction{Kind: terminalPickerActionReplace, TabIndex: 0}
	allowCreate := false
	if a == nil || a.workbench == nil {
		return action, true
	}
	if workspace := a.workbench.CurrentWorkspace(); workspace != nil {
		action.TabIndex = workspace.ActiveTab
	}
	pane := a.workbench.ActivePane()
	if pane == nil {
		action.Kind = terminalPickerActionBootstrap
		allowCreate = true
	} else if strings.TrimSpace(pane.TerminalID) == "" {
		allowCreate = true
	}
	return action, allowCreate
}
```

Add import to `tui/app_actions.go`:

```go
import "strings"
```

- [ ] **Step 4: Rewire `openTerminalPickerCmd` to consume `App` context**

Modify `tui/picker.go`:

```go
func (m *Model) openTerminalPickerCmd() tea.Cmd {
	action := terminalPickerAction{Kind: terminalPickerActionReplace, TabIndex: m.workspace.ActiveTab}
	allowCreate := false
	if m.app != nil {
		action, allowCreate = m.app.TerminalPickerContext()
	} else {
		pane := activePane(m.currentTab())
		if pane == nil {
			action.Kind = terminalPickerActionBootstrap
			allowCreate = true
		} else if strings.TrimSpace(pane.TerminalID) == "" {
			allowCreate = true
		}
	}
	return m.openPickerCmd(
		action,
		"Terminal Picker",
		"[Enter] attach  [Tab] split+attach  [Ctrl-e] edit  [Ctrl-k] kill  [Esc] close",
		allowCreate,
	)
}
```

- [ ] **Step 5: Run focused picker tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestAppOpenTerminalPickerUsesWorkbenchSelection|Test.*TerminalPicker' -count=1`
Expected: PASS.

---

## Task 4: Move selected high-level `Update` routing into `App`

**Files:**
- Modify: `tui/app.go`
- Modify: `tui/app_actions.go`
- Modify: `tui/model.go`
- Test: `tui/app_test.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Write failing `App` message-routing test**

Add to `tui/app_test.go`:

```go
func TestAppHandlesWorkspaceActivatedBySyncingWorkbench(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})
	app := NewApp(workbench)
	workspace := Workspace{Name: "dev", Tabs: []*Tab{newTab("2")}, ActiveTab: 0}

	notice, bootstrap := app.HandleWorkspaceActivated(workspace, 1)
	if notice != "" {
		t.Fatalf("expected empty notice passthrough, got %q", notice)
	}
	if bootstrap {
		t.Fatal("expected non-empty workspace not to require bootstrap")
	}
	if workbench.CurrentWorkspace().Name != "dev" {
		t.Fatalf("expected workbench current workspace dev, got %q", workbench.CurrentWorkspace().Name)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestAppHandlesWorkspaceActivatedBySyncingWorkbench' -count=1`
Expected: FAIL with undefined `HandleWorkspaceActivated`.

- [ ] **Step 3: Add minimal `App` high-level routing helper**

Extend `tui/app_actions.go`:

```go
func (a *App) HandleWorkspaceActivated(workspace Workspace, index int) (notice string, bootstrap bool) {
	if a == nil || a.workbench == nil {
		return "", workspaceNeedsBootstrap(workspace)
	}
	if current := a.workbench.Current(); current != nil {
		*current = *cloneWorkspace(workspace)
		a.workbench.SnapshotCurrent()
	}
	order := a.workbench.Order()
	if index >= 0 && index < len(order) {
		_ = a.workbench.SwitchTo(order[index])
	}
	return "", workspaceNeedsBootstrap(workspace)
}
```

- [ ] **Step 4: Rewire a selected `Update` high-level branch through `App`**

Modify the `workspaceActivatedMsg` branch in `tui/model.go` to keep current behavior but delegate the workbench-side high-level sync through `App`:

```go
case workspaceActivatedMsg:
	m.notice = msg.notice
	m.err = nil
	m.activeWorkspace = msg.index
	m.replaceWorkspace(msg.workspace)
	m.workspaceStore[m.workspace.Name] = m.workspace
	if m.app != nil {
		_, _ = m.app.HandleWorkspaceActivated(msg.workspace, msg.index)
	} else {
		m.syncWorkbenchFromWorkspaceStore()
	}
	m.syncWorkspaceStoreFromWorkbench()
	m.invalidateRender()
	if msg.bootstrap {
		return m, m.startEmptyWorkspaceBootstrapCmd()
	}
	return m, tea.Batch(m.resizeVisiblePanesCmd(), m.autoAcquireCurrentTabResizeCmd())
```

- [ ] **Step 5: Run focused routing tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestAppHandlesWorkspaceActivatedBySyncingWorkbench|TestPrefixWorkspaceSubPrefixCreateActivatesNewWorkspace|TestLoadWorkspaceStateCmd' -count=1`
Expected: PASS.

---

## Task 5: Verify Phase 2 end-to-end

**Files:**
- Test: `tui/app_test.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Run focused `App` migration regression suite**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestNewApp|TestApp|Test.*ActivateTab|Test.*FocusPane|Test.*Workspace' -count=1`
Expected: PASS.

- [ ] **Step 2: Run full TUI package tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -count=1`
Expected: PASS.

- [ ] **Step 3: Run full repository tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1`
Expected: PASS.

- [ ] **Step 4: Build the CLI**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go build ./cmd/termx`
Expected: build succeeds.

- [ ] **Step 5: Commit the completed phase**

```bash
git add tui/app.go tui/app_actions.go tui/app_test.go tui/model.go tui/model_test.go tui/picker.go tui/prefix.go
git commit -m "refactor: 建立 App 高层协调入口"
```

---

## Notes for the implementing engineer

### Phase 2 success criteria
- `App` 是真实对象，而不是空壳
- `App` 成为高层协调入口
- `Model` 开始真正退化为 shell + runtime 承载体
- `Workbench` 继续作为主工作流对象
- runtime / renderer 主线没有被提前卷入

### Explicit non-goals
- No `TerminalStore`
- No TUI-local `Terminal` proxy object yet
- No `TerminalCoordinator` extraction yet
- No `Resizer` extraction yet
- No renderer/render-loop redesign
- No terminal runtime migration in this phase

---

## Self-review

### Spec coverage
- `App` root object and initialization covered by Task 1
- High-level action routing covered by Task 2
- High-level picker entrypoint migration covered by Task 3
- Selected `Update` high-level routing covered by Task 4
- Phase-wide verification covered by Task 5

### Placeholder scan
- No `TODO` / `TBD`
- Every task has concrete files, commands, and code blocks
- Runtime / renderer work is explicitly deferred

### Type consistency
- `App`, `NewApp`, `Workbench`, `ActivateTab`, `FocusPane`, `TerminalPickerContext`, and `HandleWorkspaceActivated` naming is consistent across tasks

---

Plan complete and saved to `docs/superpowers/plans/2026-03-27-app-migration-phase2.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
