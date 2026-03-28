# Boundary Closure Cycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 收口 Phase 1–5 已经引入的正式对象边界，进一步压薄 `Model`，删除迁移过渡路径，并用 focused regression tests 固化最终主线。

**Architecture:** 本周期不新增新的 Phase 对象，而是做 boundary-closure。实现上优先清理 `Model` 中残留的高层编排与渲染调度旁路，再把 runtime / resize 的最后直连入口收进 `TerminalCoordinator`，最后收紧结构真相与读取路径，确保 `App`、`Workbench`、`TerminalStore`、`TerminalCoordinator`、`Resizer`、`Renderer`、`RenderLoop` 各自成为唯一正式主线归宿。

**Tech Stack:** Go, Bubble Tea, existing `tui/` package, Go tests

---

## File map

### Existing files to modify
- `tui/app.go`
  - Add small high-level helpers so `Model` no longer manually syncs `Workbench` before picker/workspace flows
- `tui/app_test.go`
  - Add focused tests for new app-owned orchestration helpers
- `tui/model.go`
  - Keep Bubble Tea shell responsibilities only; remove or shrink residual orchestration and duplicated render/runtime glue
- `tui/model_test.go`
  - Add regression coverage for `Model` delegating through `App`, `RenderLoop`, and workbench-backed read paths
- `tui/picker.go`
  - Route picker-related workbench syncing and read-path selection through app/model helpers instead of open-coded logic
- `tui/render_coordinator.go`
  - Eliminate duplicated scheduling fallback logic and make `RenderLoop` the single scheduling entrypoint
- `tui/render_loop.go`
  - Expose narrow public methods used by `Model` and cover them with focused tests
- `tui/render_loop_test.go`
  - Add tests proving scheduling/invalidation goes through `RenderLoop`
- `tui/terminal_coordinator.go`
  - Add runtime resize entrypoint so `Resizer` stops reaching through coordinator internals
- `tui/terminal_coordinator_test.go`
  - Add focused tests for coordinator-owned resize behavior
- `tui/resizer.go`
  - Delegate resizing entirely to `TerminalCoordinator`
- `tui/resizer_test.go`
  - Adjust tests to verify coordinator-owned runtime path
- `tui/renderer.go`
  - Keep frame generation focused and avoid cache/scheduling leakage back into `Model`
- `tui/renderer_test.go`
  - Add a focused regression test proving `Model.View()` uses renderer-backed path

### New files to create
- None by default

### Boundaries to preserve in this cycle
- Do **not** add a Phase 6 object
- Do **not** redesign terminal runtime protocol or attach semantics
- Do **not** rewrite pane compositor or low-level render algorithm
- Do **not** redesign workspace/tab/pane product behavior
- Do **not** introduce a second orchestration path alongside existing boundaries

---

## Task 1: Move picker/workspace orchestration fully behind `App`

**Files:**
- Modify: `tui/app.go`
- Modify: `tui/app_test.go`
- Modify: `tui/picker.go`
- Modify: `tui/model_test.go`

- [ ] **Step 1: Write the failing app orchestration tests**

Add to `tui/app_test.go`:

```go
func TestAppSyncCurrentWorkspaceSnapshotsWorkbench(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})
	app := NewApp(workbench, nil, nil, nil)
	workspace := Workspace{Name: "dev", Tabs: []*Tab{newTab("2")}, ActiveTab: 0}

	app.SyncCurrentWorkspace(workspace)

	if workbench.CurrentWorkspace().Name != "dev" {
		t.Fatalf("expected workbench current workspace dev, got %q", workbench.CurrentWorkspace().Name)
	}
}

func TestAppTerminalPickerContextSyncsWorkspaceBeforeSelection(t *testing.T) {
	workbench := NewWorkbench(Workspace{
		Name: "main",
		Tabs: []*Tab{{Name: "1", Panes: map[string]*Pane{"p1": {ID: "p1", Title: "Pane 1", Viewport: &Viewport{TerminalID: "term-1"}}}, ActivePaneID: "p1"}},
		ActiveTab: 0,
	})
	app := NewApp(workbench, nil, nil, nil)
	workspace := Workspace{
		Name: "main",
		Tabs: []*Tab{{Name: "1"}, {Name: "2", Panes: map[string]*Pane{"p2": {ID: "p2", Title: "Pane 2", Viewport: &Viewport{}}}, ActivePaneID: "p2"}},
		ActiveTab: 1,
	}

	action, allowCreate := app.TerminalPickerContextForWorkspace(workspace)

	if action.TabIndex != 1 {
		t.Fatalf("expected synced tab index 1, got %d", action.TabIndex)
	}
	if !allowCreate {
		t.Fatal("expected create to be allowed for synced empty pane")
	}
}
```

Add to `tui/model_test.go`:

```go
func TestOpenTerminalPickerCmdUsesAppWorkspaceSync(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.workspace = Workspace{
		Name: "main",
		Tabs: []*Tab{{Name: "1"}, {Name: "2", Panes: map[string]*Pane{"p2": {ID: "p2", Viewport: &Viewport{}}}, ActivePaneID: "p2"}},
		ActiveTab: 1,
	}
	if model.workbench == nil {
		t.Fatal("expected workbench")
	}

	_ = model.openTerminalPickerCmd()

	if model.workbench.CurrentWorkspace().ActiveTab != 1 {
		t.Fatalf("expected picker open to sync active tab 1, got %d", model.workbench.CurrentWorkspace().ActiveTab)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestAppSyncCurrentWorkspaceSnapshotsWorkbench|TestAppTerminalPickerContextSyncsWorkspaceBeforeSelection|TestOpenTerminalPickerCmdUsesAppWorkspaceSync' -count=1`
Expected: FAIL with undefined `SyncCurrentWorkspace` and `TerminalPickerContextForWorkspace`, or with picker sync not happening through `App`.

- [ ] **Step 3: Add minimal app-owned workspace sync helpers**

Extend `tui/app.go` with:

```go
func (a *App) SyncCurrentWorkspace(workspace Workspace) {
	if a == nil || a.workbench == nil {
		return
	}
	current := a.workbench.Current()
	if current == nil {
		return
	}
	*current = workspace
	a.workbench.SnapshotCurrent()
}

func (a *App) TerminalPickerContextForWorkspace(workspace Workspace) (terminalPickerAction, bool) {
	if a == nil {
		return terminalPickerAction{}, false
	}
	a.SyncCurrentWorkspace(workspace)
	return a.TerminalPickerContext()
}
```

- [ ] **Step 4: Route picker open through the new app helpers**

Update `tui/picker.go` in `openTerminalPickerCmd()` to replace the open-coded workbench mutation:

```go
if m.app != nil {
	action, allowCreate = m.app.TerminalPickerContextForWorkspace(m.workspace)
} else {
	// existing fallback stays here only for nil-app safety
}
```

Remove the manual `workbench.Current()` / `SnapshotCurrent()` block from `openTerminalPickerCmd()`.

- [ ] **Step 5: Run focused orchestration tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestAppSyncCurrentWorkspaceSnapshotsWorkbench|TestAppTerminalPickerContextSyncsWorkspaceBeforeSelection|TestOpenTerminalPickerCmdUsesAppWorkspaceSync|TestAppOpenTerminalPickerUsesWorkbenchSelection|TestAppHandlesWorkspaceActivatedBySyncingWorkbench' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit Task 1**

```bash
git add tui/app.go tui/app_test.go tui/picker.go tui/model_test.go
git commit -m "重构 picker 与工作区同步编排入口"
```

---

## Task 2: Make `RenderLoop` the only render scheduling entrypoint

**Files:**
- Modify: `tui/render_loop.go`
- Modify: `tui/render_loop_test.go`
- Modify: `tui/render_coordinator.go`
- Modify: `tui/renderer_test.go`

- [ ] **Step 1: Write failing render-loop delegation tests**

Add to `tui/render_loop_test.go`:

```go
func TestRenderLoopInvalidateRenderMarksModelDirty(t *testing.T) {
	loop := NewRenderLoop(NewRenderer(nil, nil))
	model := &Model{}
	loop.bindModel(model)

	loop.invalidateRender()

	if !model.renderDirty {
		t.Fatal("expected render loop invalidate to mark model dirty")
	}
}

func TestRenderLoopScheduleRenderMarksPendingWhenBatching(t *testing.T) {
	loop := NewRenderLoop(NewRenderer(nil, nil))
	model := &Model{renderBatching: true}
	loop.bindModel(model)

	loop.scheduleRender()

	if !model.renderPending.Load() {
		t.Fatal("expected render loop to mark render pending")
	}
}
```

Add to `tui/renderer_test.go`:

```go
func TestModelViewUsesRendererBackedPath(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 40
	model.renderDirty = true

	out := model.View()
	if out == "" {
		t.Fatal("expected rendered output")
	}
	if model.renderCache == "" {
		t.Fatal("expected renderer-backed view to finish frame into cache")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail or expose duplicate logic gaps**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestRenderLoopInvalidateRenderMarksModelDirty|TestRenderLoopScheduleRenderMarksPendingWhenBatching|TestModelViewUsesRendererBackedPath' -count=1`
Expected: FAIL if render-loop methods are still too private/duplicated to serve as the sole scheduling entrypoint, or if `Model.View()` still relies on fallback duplication.

- [ ] **Step 3: Expose narrow render-loop entrypoints for model use**

Update `tui/render_loop.go` by renaming or wrapping the scheduling methods to be the explicit model-facing API:

```go
func (l *RenderLoop) Invalidate() {
	l.invalidateRender()
}

func (l *RenderLoop) Schedule() {
	l.scheduleRender()
}

func (l *RenderLoop) FlushPending() {
	l.flushPendingRender()
}

func (l *RenderLoop) RequestInteractiveRender() {
	l.requestInteractiveRender()
}

func (l *RenderLoop) StartTicker() {
	l.startTicker()
}
```

Keep the existing implementation bodies, but make these wrappers the public API used outside the file.

- [ ] **Step 4: Remove duplicated scheduling branches from `Model`**

Update `tui/render_coordinator.go` so `Model` methods call the render-loop wrappers directly when present and keep only the minimal nil-safe fallback:

```go
func (m *Model) startRenderTicker() {
	if loop := m.renderLoop(); loop != nil {
		loop.StartTicker()
		return
	}
}

func (m *Model) invalidateRender() {
	if loop := m.renderLoop(); loop != nil {
		loop.Invalidate()
		return
	}
	m.renderDirty = true
}

func (m *Model) scheduleRender() {
	if loop := m.renderLoop(); loop != nil {
		loop.Schedule()
		return
	}
	m.renderDirty = true
}

func (m *Model) flushPendingRender() {
	if loop := m.renderLoop(); loop != nil {
		loop.FlushPending()
		return
	}
}

func (m *Model) requestInteractiveRender() {
	if loop := m.renderLoop(); loop != nil {
		loop.RequestInteractiveRender()
		return
	}
	m.renderDirty = true
}
```

Directionally, this task should delete duplicated fallback logic rather than preserve two full scheduling implementations.

- [ ] **Step 5: Run focused render-loop tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestNewRenderLoopHoldsRenderer|TestRenderLoopInvalidateRenderMarksModelDirty|TestRenderLoopScheduleRenderMarksPendingWhenBatching|TestModelViewUsesRendererBackedPath|TestRendererCanServeCachedFrame' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit Task 2**

```bash
git add tui/render_loop.go tui/render_loop_test.go tui/render_coordinator.go tui/renderer_test.go
git commit -m "收口渲染调度主线到 RenderLoop"
```

---

## Task 3: Make `TerminalCoordinator` the only runtime resize entrypoint

**Files:**
- Modify: `tui/terminal_coordinator.go`
- Modify: `tui/terminal_coordinator_test.go`
- Modify: `tui/resizer.go`
- Modify: `tui/resizer_test.go`

- [ ] **Step 1: Write failing runtime resize tests**

Add to `tui/terminal_coordinator_test.go`:

```go
func TestTerminalCoordinatorResizeTerminalUsesClientResize(t *testing.T) {
	store := NewTerminalStore()
	client := &fakeClient{}
	coordinator := NewTerminalCoordinator(client, store)
	pane := &Pane{Viewport: &Viewport{TerminalID: "term-1", Channel: 7, ResizeAcquired: true}}

	coordinator.ResizeTerminal(context.Background(), pane, 120, 40)

	if client.resizeCalls != 1 {
		t.Fatalf("expected one resize call, got %d", client.resizeCalls)
	}
}
```

Update `tui/resizer_test.go` to prove `Resizer` goes through the coordinator-owned API, not coordinator internals:

```go
func TestResizerSyncsTerminalResizeThroughCoordinator(t *testing.T) {
	client := &fakeClient{}
	store := NewTerminalStore()
	coordinator := NewTerminalCoordinator(client, store)
	resizer := NewResizer(coordinator)
	pane := &Pane{Viewport: &Viewport{TerminalID: "term-1", Channel: 7, ResizeAcquired: true}}

	resizer.SyncPaneResize(pane, 120, 40)

	if client.resizeCalls != 1 {
		t.Fatalf("expected one resize call, got %d", client.resizeCalls)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalCoordinatorResizeTerminalUsesClientResize|TestResizerSyncsTerminalResizeThroughCoordinator' -count=1`
Expected: FAIL with undefined `ResizeTerminal` or with `Resizer` still reaching into `coordinator.client` directly.

- [ ] **Step 3: Add coordinator-owned resize entrypoint**

Extend `tui/terminal_coordinator.go` with:

```go
func (c *TerminalCoordinator) ResizeTerminal(ctx context.Context, pane *Pane, cols, rows int) {
	if c == nil || c.client == nil || pane == nil || pane.Viewport == nil {
		return
	}
	if !pane.ResizeAcquired || pane.Channel == 0 || cols <= 0 || rows <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_ = c.client.Resize(ctx, pane.Channel, uint16(cols), uint16(rows))
}
```

- [ ] **Step 4: Make `Resizer` delegate entirely to `TerminalCoordinator`**

Update `tui/resizer.go`:

```go
func (r *Resizer) SyncPaneResize(pane *Pane, cols, rows int) {
	if r == nil || r.coordinator == nil {
		return
	}
	r.coordinator.ResizeTerminal(context.Background(), pane, cols, rows)
}
```

This task should remove direct access to `r.coordinator.client` from `Resizer`.

- [ ] **Step 5: Run focused runtime coordination tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalCoordinatorAttachLoadsSnapshotAndUpdatesStore|TestTerminalCoordinatorResizeTerminalUsesClientResize|TestResizerSyncsTerminalResizeThroughCoordinator|TestTerminalCoordinatorMarksTerminalExitedInStore' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit Task 3**

```bash
git add tui/terminal_coordinator.go tui/terminal_coordinator_test.go tui/resizer.go tui/resizer_test.go
git commit -m "收口运行时 resize 主线到 TerminalCoordinator"
```

---

## Task 4: Tighten workbench-backed read paths and final verification

**Files:**
- Modify: `tui/model.go`
- Modify: `tui/model_test.go`
- Modify: `tui/picker.go`
- Modify: `tui/renderer.go`
- Modify: `tui/renderer_test.go`

- [ ] **Step 1: Write failing read-boundary regression tests**

Add to `tui/model_test.go`:

```go
func TestTerminalLocationsUsesWorkbenchBackedWorkspaceState(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.workspace = Workspace{
		Name: "main",
		Tabs: []*Tab{{
			Name: "1",
			Panes: map[string]*Pane{
				"p1": {ID: "p1", Terminal: &Terminal{ID: "term-1"}, Viewport: &Viewport{TerminalID: "term-1"}},
			},
		}},
		ActiveTab: 0,
	}
	model.snapshotCurrentWorkspace()

	locations := model.terminalLocations()
	if len(locations["term-1"]) == 0 {
		t.Fatal("expected workbench-backed terminal location")
	}
}
```

Add to `tui/renderer_test.go`:

```go
func TestRendererFinishFrameCachesAndClearsDirty(t *testing.T) {
	renderer := NewRenderer(nil, nil)
	model := &Model{renderDirty: true, timeNow: func() time.Time { return time.Unix(100, 0) }}

	out := renderer.FinishFrame(model, "frame")

	if out != "frame" {
		t.Fatalf("expected frame output, got %q", out)
	}
	if model.renderCache != "frame" {
		t.Fatalf("expected render cache to be updated, got %q", model.renderCache)
	}
	if model.renderDirty {
		t.Fatal("expected render dirty flag cleared")
	}
}
```

Update the imports in `tui/renderer_test.go` to include `time`.

- [ ] **Step 2: Run tests to verify they fail or expose boundary gaps**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalLocationsUsesWorkbenchBackedWorkspaceState|TestRendererFinishFrameCachesAndClearsDirty' -count=1`
Expected: FAIL if read paths still depend on stale mirrors or if renderer finalization semantics are not fully nailed down.

- [ ] **Step 3: Tighten terminal location and renderer finalization paths**

In `tui/picker.go`, keep `terminalLocations()` explicitly grounded in the workbench-snapshotted workspace state by ensuring it always snapshots before reading and prefers `pane.Terminal.ID` when present:

```go
func (m *Model) terminalLocations() map[string][]string {
	m.snapshotCurrentWorkspace()
	locations := make(map[string][]string)
	// existing iteration stays, but always prefer pane.Terminal.ID over raw TerminalID when available
}
```

In `tui/renderer.go`, keep `FinishFrame()` as the single renderer-owned finalization path and make `Model.View()` rely on it instead of open-coded cache finalization.

- [ ] **Step 4: Run focused boundary-closure regression suite**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalLocationsUsesWorkbenchBackedWorkspaceState|TestRendererFinishFrameCachesAndClearsDirty|TestModelViewUsesRendererBackedPath|TestOpenTerminalPickerCmdUsesAppWorkspaceSync|TestResizerSyncsTerminalResizeThroughCoordinator' -count=1`
Expected: PASS.

- [ ] **Step 5: Run package, repo, and build verification**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -count=1`
Expected: PASS.

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1`
Expected: PASS.

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go build ./cmd/termx`
Expected: build succeeds.

- [ ] **Step 6: Commit Task 4**

```bash
git add tui/model.go tui/model_test.go tui/picker.go tui/renderer.go tui/renderer_test.go
git commit -m "完成边界收口周期并固化验证"
```

---

## Self-review

### Spec coverage
- `Model` 继续变薄、减少残留主线路径：Task 1 + Task 2 + Task 4
- `App` 成为更明确的高层编排入口：Task 1
- `TerminalCoordinator + Resizer` 成为唯一 resize/runtime 入口：Task 3
- `Renderer + RenderLoop` 成为单一渲染主线：Task 2 + Task 4
- workbench-backed structure/read boundary 固化：Task 1 + Task 4
- package/repo/build 验证：Task 4

### Placeholder scan
- No `TODO` / `TBD`
- Every task has exact files, concrete code snippets, and exact commands
- No “similar to previous task” references

### Type consistency
- App helpers use `SyncCurrentWorkspace` and `TerminalPickerContextForWorkspace` consistently
- Render-loop wrappers use `Invalidate`, `Schedule`, `FlushPending`, `RequestInteractiveRender`, `StartTicker` consistently
- Runtime resize entrypoint uses `ResizeTerminal(ctx context.Context, pane *Pane, cols, rows int)` consistently

---

Plan complete and saved to `docs/superpowers/plans/2026-03-28-boundary-closure-cycle.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
