# TerminalCoordinator Migration Phase 4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce `TerminalCoordinator` and `Resizer` so terminal runtime coordination and resize synchronization move out of `Model` and begin updating `TerminalStore + Terminal` through explicit service objects.

**Architecture:** This phase is a migration, not a rewrite. We will first establish `TerminalCoordinator` as the runtime coordination home for attach / snapshot / stream / lifecycle handling, then add `Resizer` as the resize synchronization home that consumes already-settled layout geometry. The transition must preserve existing shared-terminal behavior and resize-owner semantics while deliberately leaving renderer/render-loop work for the next phase.

**Tech Stack:** Go, Bubble Tea, existing `tui/` package, Go tests

---

## File map

### Existing files to modify
- `tui/model.go`
  - Currently holds attach / stream / snapshot / remove / resize coordination helpers
  - Will stop being the main home for terminal runtime coordination
- `tui/app.go`
  - Currently holds high-level coordination objects and helpers
  - Will begin holding `TerminalCoordinator` and `Resizer`
- `tui/picker.go`
  - Current attach / create / bootstrap picker flows call client/runtime helpers directly
  - Selected terminal attach paths will move behind `TerminalCoordinator`
- `tui/workbench.go`
  - Workbench owns the workspace tree and pane structure
  - May need small hooks for resize/layout settlement consumption
- `tui/model_test.go`
  - Existing integration coverage for attach, close pane, shared terminal, resize-owner behavior
  - Add/adjust tests for coordinator / resizer routing
- `tui/terminal_model.go`
  - Terminal proxy type
  - Runtime fields may start to receive coordinated updates from `TerminalCoordinator`

### New files to create
- `tui/terminal_coordinator.go`
  - `TerminalCoordinator`
  - attach / snapshot / stream / bind / unbind / lifecycle coordination
- `tui/resizer.go`
  - `Resizer`
  - resize synchronization logic
- `tui/terminal_coordinator_test.go`
  - Focused tests for runtime coordination and lifecycle routing
- `tui/resizer_test.go`
  - Focused tests for resize synchronization behavior

### Boundaries to preserve in Phase 4
- Do **not** redesign renderer or render loop
- Do **not** move rendering bookkeeping into coordinator/resizer
- Do **not** rebuild `TerminalPoolPage`
- Do **not** do large UI or page-level restructuring
- Do **not** collapse `TerminalCoordinator` and `Resizer` into one object

---

## Task 1: Introduce `TerminalCoordinator` as the runtime coordination root

**Files:**
- Create: `tui/terminal_coordinator.go`
- Create: `tui/terminal_coordinator_test.go`
- Modify: `tui/app.go`
- Modify: `tui/model.go`

- [ ] **Step 1: Write the failing coordinator bootstrap test**

```go
func TestNewTerminalCoordinatorHoldsStoreAndClient(t *testing.T) {
	store := NewTerminalStore()
	client := &fakeClient{}
	coordinator := NewTerminalCoordinator(client, store)

	if coordinator == nil {
		t.Fatal("expected coordinator")
	}
	if coordinator.Store() != store {
		t.Fatal("expected coordinator to hold store reference")
	}
	if coordinator.Client() != client {
		t.Fatal("expected coordinator to hold client reference")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestNewTerminalCoordinatorHoldsStoreAndClient' -count=1`
Expected: FAIL with undefined `NewTerminalCoordinator`, `Store`, or `Client`.

- [ ] **Step 3: Add the minimal `TerminalCoordinator` type**

Create `tui/terminal_coordinator.go`:

```go
package tui

type TerminalCoordinator struct {
	client Client
	store  *TerminalStore
}

func NewTerminalCoordinator(client Client, store *TerminalStore) *TerminalCoordinator {
	return &TerminalCoordinator{client: client, store: store}
}

func (c *TerminalCoordinator) Client() Client {
	if c == nil {
		return nil
	}
	return c.client
}

func (c *TerminalCoordinator) Store() *TerminalStore {
	if c == nil {
		return nil
	}
	return c.store
}
```

Modify `tui/app.go` to add a field:

```go
type App struct {
	workbench            *Workbench
	terminalCoordinator  *TerminalCoordinator
}
```

Add accessor:

```go
func (a *App) TerminalCoordinator() *TerminalCoordinator {
	if a == nil {
		return nil
	}
	return a.terminalCoordinator
}
```

Modify `NewApp(...)` to accept/store the coordinator:

```go
func NewApp(workbench *Workbench, terminalCoordinator *TerminalCoordinator) *App {
	return &App{workbench: workbench, terminalCoordinator: terminalCoordinator}
}
```

Modify `NewModel(...)` in `tui/model.go`:

```go
terminalCoordinator := NewTerminalCoordinator(client, terminalStore)
app := NewApp(workbench, terminalCoordinator)
```

- [ ] **Step 4: Run focused coordinator bootstrap test**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestNewTerminalCoordinatorHoldsStoreAndClient' -count=1`
Expected: PASS.

---

## Task 2: Move terminal attach/snapshot coordination into `TerminalCoordinator`

**Files:**
- Modify: `tui/terminal_coordinator.go`
- Modify: `tui/picker.go`
- Modify: `tui/model.go`
- Test: `tui/terminal_coordinator_test.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Write failing attach coordination tests**

```go
func TestTerminalCoordinatorAttachLoadsSnapshotAndUpdatesStore(t *testing.T) {
	store := NewTerminalStore()
	client := &fakeClient{
		snapshotByID: map[string]*protocol.Snapshot{
			"term-1": {TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}},
		},
	}
	coordinator := NewTerminalCoordinator(client, store)
	info := protocol.TerminalInfo{ID: "term-1", Name: "worker", Command: []string{"bash"}, State: "running"}

	view, err := coordinator.AttachTerminal(info)
	if err != nil {
		t.Fatalf("expected attach to succeed, got %v", err)
	}
	if view == nil {
		t.Fatal("expected viewport result")
	}
	terminal := store.Get("term-1")
	if terminal == nil || terminal.Name != "worker" {
		t.Fatalf("expected store updated with terminal metadata, got %#v", terminal)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalCoordinatorAttachLoadsSnapshotAndUpdatesStore' -count=1`
Expected: FAIL with undefined `AttachTerminal`.

- [ ] **Step 3: Add minimal attach coordination method**

Extend `tui/terminal_coordinator.go`:

```go
import "github.com/lozzow/termx/protocol"

func (c *TerminalCoordinator) AttachTerminal(info protocol.TerminalInfo) (*Viewport, error) {
	if c == nil || c.client == nil || c.store == nil {
		return nil, fmt.Errorf("terminal coordinator unavailable")
	}
	attached, err := c.client.Attach(context.Background(), info.ID, "collaborator")
	if err != nil {
		return nil, err
	}
	snap, err := c.client.Snapshot(context.Background(), info.ID, 0, 200)
	if err != nil {
		return nil, err
	}
	terminal := c.store.GetOrCreate(info.ID)
	terminal.SetMetadata(info.Name, info.Command, info.Tags)
	terminal.State = info.State
	terminal.Snapshot = snap
	terminal.Channel = attached.Channel
	terminal.AttachMode = attached.Mode
	return viewportWithTerminalInfo((&Model{cfg: Config{DefaultShell: "/bin/sh"}}).newViewport(info.ID, attached.Channel, snap), info), nil
}
```

Then adjust the helper to set channel/mode after constructing the viewport if needed.

- [ ] **Step 4: Rewire one selected picker attach path to use `TerminalCoordinator`**

Modify `attachTerminalToBootstrapCmd` in `tui/picker.go` to delegate its attach/snapshot coordination through `m.app.TerminalCoordinator()` when available, keeping the fallback path otherwise.

- [ ] **Step 5: Run focused attach tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalCoordinatorAttachLoadsSnapshotAndUpdatesStore|TestInitWithAttachIDBootstrapsTUILayout|TestStartupPickerCanAttachExistingTerminalIntoFirstPane' -count=1`
Expected: PASS.

---

## Task 3: Move terminal lifecycle coordination into `TerminalCoordinator`

**Files:**
- Modify: `tui/terminal_coordinator.go`
- Modify: `tui/model.go`
- Test: `tui/terminal_coordinator_test.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Write failing lifecycle coordination tests**

```go
func TestTerminalCoordinatorMarksTerminalExitedInStore(t *testing.T) {
	store := NewTerminalStore()
	terminal := store.GetOrCreate("term-1")
	terminal.State = "running"
	coordinator := NewTerminalCoordinator(&fakeClient{}, store)

	coordinator.MarkExited("term-1", 42)

	if terminal.State != "exited" {
		t.Fatalf("expected exited state, got %q", terminal.State)
	}
	if terminal.ExitCode == nil || *terminal.ExitCode != 42 {
		t.Fatalf("expected exit code 42, got %#v", terminal.ExitCode)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalCoordinatorMarksTerminalExitedInStore' -count=1`
Expected: FAIL with undefined `MarkExited`.

- [ ] **Step 3: Add lifecycle update helpers**

Extend `tui/terminal_coordinator.go`:

```go
func (c *TerminalCoordinator) MarkExited(terminalID string, exitCode int) {
	if c == nil || c.store == nil || terminalID == "" {
		return
	}
	terminal := c.store.GetOrCreate(terminalID)
	terminal.State = "exited"
	code := exitCode
	terminal.ExitCode = &code
}

func (c *TerminalCoordinator) MarkKilled(terminalID string) {
	if c == nil || c.store == nil || terminalID == "" {
		return
	}
	terminal := c.store.GetOrCreate(terminalID)
	terminal.State = "killed"
	terminal.ExitCode = nil
}
```

- [ ] **Step 4: Rewire selected model lifecycle branches through `TerminalCoordinator`**

Modify `markTerminalExited` / `markTerminalKilled` in `tui/model.go` to call `m.app.TerminalCoordinator().MarkExited(...)` / `MarkKilled(...)` before preserving existing pane-side runtime behavior.

- [ ] **Step 5: Run focused lifecycle tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalCoordinatorMarksTerminalExitedInStore|Test.*Exited|Test.*Killed' -count=1`
Expected: PASS.

---

## Task 4: Introduce `Resizer` and route resize synchronization through it

**Files:**
- Create: `tui/resizer.go`
- Create: `tui/resizer_test.go`
- Modify: `tui/app.go`
- Modify: `tui/model.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Write failing resizer tests**

```go
func TestResizerSyncsTerminalResizeForOwnerPane(t *testing.T) {
	client := &fakeClient{}
	store := NewTerminalStore()
	terminal := store.GetOrCreate("term-1")
	coordinator := NewTerminalCoordinator(client, store)
	resizer := NewResizer(coordinator)
	pane := &Pane{ID: "pane-1", Terminal: terminal, Viewport: &Viewport{TerminalID: "term-1", Channel: 7, ResizeAcquired: true}}

	resizer.SyncPaneResize(pane, 120, 40)

	if client.resizeCalls != 1 {
		t.Fatalf("expected one resize call, got %d", client.resizeCalls)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestResizerSyncsTerminalResizeForOwnerPane' -count=1`
Expected: FAIL with undefined `NewResizer` or `SyncPaneResize`.

- [ ] **Step 3: Add minimal `Resizer` type**

Create `tui/resizer.go`:

```go
package tui

type Resizer struct {
	coordinator *TerminalCoordinator
}

func NewResizer(coordinator *TerminalCoordinator) *Resizer {
	return &Resizer{coordinator: coordinator}
}

func (r *Resizer) SyncPaneResize(pane *Pane, cols, rows int) {
	if r == nil || r.coordinator == nil || pane == nil || pane.Viewport == nil {
		return
	}
	if !pane.ResizeAcquired || pane.Channel == 0 || cols <= 0 || rows <= 0 {
		return
	}
	ctx := context.Background()
	_ = r.coordinator.client.Resize(ctx, pane.Channel, uint16(cols), uint16(rows))
}
```

- [ ] **Step 4: Wire `Resizer` into `App` and route a selected resize path**

Modify `tui/app.go`:

```go
type App struct {
	workbench           *Workbench
	terminalCoordinator *TerminalCoordinator
	resizer             *Resizer
}
```

Update `NewApp(...)` and add accessor `Resizer() *Resizer`.

Modify `NewModel(...)` in `tui/model.go` to create:

```go
resizer := NewResizer(terminalCoordinator)
app := NewApp(workbench, terminalCoordinator, resizer)
```

Then route a selected resize sync path through `m.app.Resizer()` in `resizeVisiblePanesCmd()` or the narrowest practical owner-pane resize path, preserving current behavior.

- [ ] **Step 5: Run focused resize tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestResizerSyncsTerminalResizeForOwnerPane|Test.*Resize' -count=1`
Expected: PASS.

---

## Task 5: Verify Phase 4 end-to-end

**Files:**
- Test: `tui/terminal_coordinator_test.go`
- Test: `tui/resizer_test.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Run focused coordinator/resizer regression suite**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestNewTerminalCoordinator|TestTerminalCoordinator|TestResizer|Test.*Resize|Test.*Attach|Test.*Exited|Test.*Killed' -count=1`
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
git add tui/terminal_coordinator.go tui/resizer.go tui/terminal_coordinator_test.go tui/resizer_test.go tui/app.go tui/model.go tui/picker.go tui/model_test.go
git commit -m "refactor: 迁移 terminal runtime 协调与 resize 同步"
```

---

## Notes for the implementing engineer

### Phase 4 success criteria
- `TerminalCoordinator` is a real runtime coordination object
- `Resizer` is a real resize synchronization object
- `Model` no longer directly owns most terminal runtime coordination
- `TerminalStore + Terminal` receive coordinated runtime updates
- shared-terminal / resize-owner / lifecycle behavior remains stable
- renderer mainline remains deferred

### Explicit non-goals
- No renderer redesign
- No render loop migration
- No `TerminalPoolPage` full implementation
- No major UI structure rewrite
- No collapsing coordinator and resizer into one object

---

## Self-review

### Spec coverage
- `TerminalCoordinator` introduction covered by Task 1
- attach/snapshot coordination covered by Task 2
- lifecycle coordination covered by Task 3
- `Resizer` introduction and resize routing covered by Task 4
- phase-wide verification covered by Task 5

### Placeholder scan
- No `TODO` / `TBD`
- Every task has concrete files, code, and commands
- Renderer/render-loop work is explicitly deferred

### Type consistency
- `TerminalCoordinator`, `Resizer`, `AttachTerminal`, `MarkExited`, `MarkKilled`, `SyncPaneResize`, and `NewApp(...)` naming remains consistent across tasks

---

Plan complete and saved to `docs/superpowers/plans/2026-03-28-terminal-coordinator-migration-phase4.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
