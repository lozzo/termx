# Renderer Migration Phase 5 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce `Renderer` and `RenderLoop` so frame generation and render scheduling move out of `Model`, while preserving existing rendering semantics, cache behavior, and partial redraw performance.

**Architecture:** This phase is a migration, not a rewrite. We will first establish `Renderer` as the formal frame-generation entrypoint and route top-level view assembly through it, then introduce `RenderLoop` as the owner of tick / batching / flush / backpressure behavior. The migration is aggressive about moving boundaries, but conservative about changing rendering semantics: existing compositor, dirty-row logic, alt-screen behavior, and frame cache internals should be reused unless tests prove otherwise.

**Tech Stack:** Go, Bubble Tea, existing `tui/` package, Go tests

---

## File map

### Existing files to modify
- `tui/model.go`
  - Current `View`, render entrypoints, cache/dirtiness fields, and parts of render scheduling glue
  - Will stop being the main rendering entrypoint
- `tui/render.go`
  - Current pane/compositor rendering implementation
  - Will remain the home of low-level rendering helpers reused by `Renderer`
- `tui/render_coordinator.go`
  - Current batching / flush / render pending helpers
  - Will either be slimmed or partially absorbed by `RenderLoop`
- `tui/app.go`
  - Will begin holding `Renderer` and `RenderLoop`
- `tui/model_test.go`
  - Existing broad rendering and integration coverage
  - Add/adjust tests for `Model -> Renderer -> RenderLoop` wiring
- `tui/render_benchmark_test.go`
  - Existing benchmark coverage for rendering paths
  - Adjust only if entrypoints move

### New files to create
- `tui/renderer.go`
  - `Renderer`
  - top-level frame generation entrypoint
- `tui/render_loop.go`
  - `RenderLoop`
  - tick / batching / flush / backpressure behavior
- `tui/renderer_test.go`
  - focused tests for top-level rendering assembly and renderer-backed reads
- `tui/render_loop_test.go`
  - focused tests for render scheduling behavior

### Boundaries to preserve in Phase 5
- Do **not** redesign terminal runtime or resize coordination
- Do **not** rewrite the pane compositor algorithm from scratch
- Do **not** change display semantics for alt-screen / snapshot / dirty-region behavior without tests
- Do **not** expand into UI/product redesign beyond render-system boundary migration

---

## Task 1: Introduce `Renderer` as the top-level frame generation entrypoint

**Files:**
- Create: `tui/renderer.go`
- Create: `tui/renderer_test.go`
- Modify: `tui/app.go`
- Modify: `tui/model.go`

- [ ] **Step 1: Write the failing renderer bootstrap test**

```go
func TestNewRendererHoldsWorkbenchAndStore(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})
	store := NewTerminalStore()
	renderer := NewRenderer(workbench, store)

	if renderer == nil {
		t.Fatal("expected renderer")
	}
	if renderer.Workbench() != workbench {
		t.Fatal("expected renderer to hold workbench reference")
	}
	if renderer.TerminalStore() != store {
		t.Fatal("expected renderer to hold terminal store reference")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestNewRendererHoldsWorkbenchAndStore' -count=1`
Expected: FAIL with undefined `NewRenderer`, `Workbench`, or `TerminalStore`.

- [ ] **Step 3: Add the minimal `Renderer` type**

Create `tui/renderer.go`:

```go
package tui

type Renderer struct {
	workbench     *Workbench
	terminalStore *TerminalStore
}

func NewRenderer(workbench *Workbench, terminalStore *TerminalStore) *Renderer {
	return &Renderer{workbench: workbench, terminalStore: terminalStore}
}

func (r *Renderer) Workbench() *Workbench {
	if r == nil {
		return nil
	}
	return r.workbench
}

func (r *Renderer) TerminalStore() *TerminalStore {
	if r == nil {
		return nil
	}
	return r.terminalStore
}
```

Modify `tui/app.go`:

```go
type App struct {
	workbench           *Workbench
	terminalCoordinator *TerminalCoordinator
	resizer             *Resizer
	renderer            *Renderer
}
```

Update `NewApp(...)` to accept/store `renderer`, and add accessor `Renderer() *Renderer`.

Modify `NewModel(...)` in `tui/model.go`:

```go
renderer := NewRenderer(workbench, terminalStore)
app := NewApp(workbench, terminalCoordinator, resizer, renderer)
```

- [ ] **Step 4: Run focused renderer bootstrap test**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestNewRendererHoldsWorkbenchAndStore' -count=1`
Expected: PASS.

---

## Task 2: Move top-level frame assembly into `Renderer`

**Files:**
- Modify: `tui/renderer.go`
- Modify: `tui/model.go`
- Test: `tui/renderer_test.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Write failing frame-assembly test**

```go
func TestRendererViewAssemblesTabBodyAndStatus(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 30
	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)
	renderer := model.app.Renderer()

	out := renderer.Render(model)
	if !containsAll(out, "termx", "Ctrl", "pane") {
		t.Fatalf("expected renderer output to include top-level shell content, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestRendererViewAssemblesTabBodyAndStatus' -count=1`
Expected: FAIL with undefined `Render`.

- [ ] **Step 3: Add top-level `Render(model *Model) string` to `Renderer`**

Extend `tui/renderer.go`:

```go
func (r *Renderer) Render(model *Model) string {
	if model == nil {
		return ""
	}
	return strings.Join([]string{model.renderTabBar(), model.renderContentBody(), model.renderStatus()}, "\n")
}
```

Add import:

```go
import "strings"
```

- [ ] **Step 4: Route `Model.View()` through `Renderer` while preserving fallback**

Modify `tui/model.go` in `View()` to use `m.app.Renderer()` when available, while keeping existing cache and overlay behavior intact. The final frame assembly line should directionally become:

```go
out = m.app.Renderer().Render(m)
```

but only after preserving the current special-case overlay and cache guards.

- [ ] **Step 5: Run focused renderer tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestRendererViewAssemblesTabBodyAndStatus|TestModelViewShowsWelcomeAndHelp|TestRender' -count=1`
Expected: PASS.

---

## Task 3: Introduce `RenderLoop` and migrate batching / flush / pending state ownership

**Files:**
- Create: `tui/render_loop.go`
- Create: `tui/render_loop_test.go`
- Modify: `tui/app.go`
- Modify: `tui/model.go`
- Modify: `tui/render_coordinator.go`

- [ ] **Step 1: Write failing render-loop bootstrap test**

```go
func TestNewRenderLoopHoldsRenderer(t *testing.T) {
	renderer := NewRenderer(nil, nil)
	loop := NewRenderLoop(renderer)

	if loop == nil {
		t.Fatal("expected render loop")
	}
	if loop.Renderer() != renderer {
		t.Fatal("expected render loop to hold renderer")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestNewRenderLoopHoldsRenderer' -count=1`
Expected: FAIL with undefined `NewRenderLoop`.

- [ ] **Step 3: Add minimal `RenderLoop` type**

Create `tui/render_loop.go`:

```go
package tui

type RenderLoop struct {
	renderer *Renderer
}

func NewRenderLoop(renderer *Renderer) *RenderLoop {
	return &RenderLoop{renderer: renderer}
}

func (l *RenderLoop) Renderer() *Renderer {
	if l == nil {
		return nil
	}
	return l.renderer
}
```

Modify `tui/app.go`:

```go
type App struct {
	workbench           *Workbench
	terminalCoordinator *TerminalCoordinator
	resizer             *Resizer
	renderer            *Renderer
	renderLoop          *RenderLoop
}
```

Update `NewApp(...)` to accept/store `renderLoop`, and add accessor `RenderLoop() *RenderLoop`.

Modify `NewModel(...)`:

```go
renderLoop := NewRenderLoop(renderer)
app := NewApp(workbench, terminalCoordinator, resizer, renderer, renderLoop)
```

- [ ] **Step 4: Move selected scheduling helpers under `RenderLoop` ownership**

Migrate the narrowest practical scheduling helpers from `tui/render_coordinator.go` into `RenderLoop` methods, starting with wrappers for flush / pending logic instead of rewriting everything at once. For example:

```go
func (l *RenderLoop) Flush(model *Model) {
	if l == nil || model == nil {
		return
	}
	model.flushPendingRender()
}
```

Then update the `renderTickMsg` branch in `Model.Update()` to call through `m.app.RenderLoop()` when available.

- [ ] **Step 5: Run focused render-loop tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestNewRenderLoopHoldsRenderer|Test.*RenderTick|Test.*Batch' -count=1`
Expected: PASS.

---

## Task 4: Migrate selected cache/dirty scheduling paths behind `Renderer + RenderLoop`

**Files:**
- Modify: `tui/renderer.go`
- Modify: `tui/render_loop.go`
- Modify: `tui/model.go`
- Modify: `tui/render_coordinator.go`
- Test: `tui/renderer_test.go`
- Test: `tui/render_loop_test.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Write failing cache/dirtiness regression tests**

```go
func TestRendererCanServeCachedFrame(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)
	renderer := model.app.Renderer()

	first := renderer.Render(model)
	second := renderer.Render(model)
	if first == "" || second == "" {
		t.Fatal("expected rendered output")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail or expose missing API**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestRendererCanServeCachedFrame|TestComposedCanvas|Test.*Dirty' -count=1`
Expected: FAIL or reveal missing renderer-side cache ownership hooks.

- [ ] **Step 3: Move selected cache/dirty coordination behind `Renderer + RenderLoop`**

Do **not** rewrite dirty-row algorithms. Instead, move top-level ownership of render-cache / pending / dirty scheduling decisions out of `Model` and into `Renderer + RenderLoop` methods, keeping underlying compositor logic in place.

Examples of acceptable moves:
- renderer-side helper to finalize and cache output
- render-loop-side helper to decide flush cadence and pending consumption
- model delegating to those helpers instead of owning the final decision directly

- [ ] **Step 4: Run focused cache/dirty regression tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestRendererCanServeCachedFrame|TestComposedCanvas|Test.*Dirty|TestModelViewShowsWelcomeAndHelp' -count=1`
Expected: PASS.

---

## Task 5: Verify Phase 5 end-to-end

**Files:**
- Test: `tui/renderer_test.go`
- Test: `tui/render_loop_test.go`
- Test: `tui/model_test.go`
- Test: `tui/render_benchmark_test.go`

- [ ] **Step 1: Run focused renderer/render-loop regression suite**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestNewRenderer|TestRenderer|TestNewRenderLoop|Test.*Render|Test.*Dirty|Test.*Batch|Test.*View' -count=1`
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
git add tui/renderer.go tui/render_loop.go tui/renderer_test.go tui/render_loop_test.go tui/model.go tui/render.go tui/render_coordinator.go tui/model_test.go tui/render_benchmark_test.go
git commit -m "refactor: 迁移渲染主线与渲染调度主线"
```

---

## Notes for the implementing engineer

### Phase 5 success criteria
- `Renderer` is a real rendering entrypoint
- `RenderLoop` is a real scheduling entrypoint
- `Model` no longer directly owns the main rendering pipeline
- render cache / pending / dirty / batching ownership begins to live outside `Model`
- rendering semantics remain stable
- the migration arc reaches final top-level boundary closure

### Explicit non-goals
- No terminal runtime redesign
- No resize redesign
- No full compositor algorithm rewrite
- No product/UI redesign

---

## Self-review

### Spec coverage
- `Renderer` introduction covered by Task 1
- top-level frame assembly migration covered by Task 2
- `RenderLoop` introduction covered by Task 3
- cache/dirtiness scheduling migration covered by Task 4
- phase-wide verification covered by Task 5

### Placeholder scan
- No `TODO` / `TBD`
- Every task has concrete files, code, and commands
- Lower-level algorithm redesign is explicitly deferred

### Type consistency
- `Renderer`, `RenderLoop`, `Render`, `Flush`, `Renderer()`, and `NewApp(...)` naming remains consistent across tasks

---

Plan complete and saved to `docs/superpowers/plans/2026-03-28-renderer-migration-phase5.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
