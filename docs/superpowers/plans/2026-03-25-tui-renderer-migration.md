# TUI Renderer Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在保留现有 `state / reducer / runtime / input` 主干的前提下，迁回旧版已经验证过的渲染、布局、命中和浮层能力，完成新 TUI 的真实工作台渲染。

**Architecture:** 新增 `tui/render` 分层，把旧版可复用的纯画布、纯布局和纯几何处理迁入新目录；`runtimeRenderer` 缩成入口装配层；workbench 命中改为几何命中；overlay 维持独立盖板合成。优先恢复正确性，再恢复 dirty redraw 和性能优化。

**Tech Stack:** Go, Bubble Tea, Charmbracelet x/ansi, 现有 `tui/domain` / `tui/app/reducer` / `tui/runtime` 主链路

---

## Spec And Preconditions

- 设计文档：`docs/superpowers/specs/2026-03-25-tui-renderer-migration-design.md`
- 相关文档：`docs/tui/architecture.md`, `docs/tui/current-status.md`, `docs/tui/roadmap.md`
- 旧版参考：`deprecated/tui-legacy/pkg/render.go`, `deprecated/tui-legacy/pkg/layout.go`, `deprecated/tui-legacy/pkg/input.go`
- 执行前运行：

```bash
git status --short
```

预期：

- 只看到本任务相关改动
- 如果仍然存在用户自己的未提交改动，例如 `AGENTS.md`，提交时不要带上

## File Map

### New files

- `tui/render/renderer.go`
  - 新 renderer 主入口接口，连接 projection / surface / compositor / overlay / hittest
- `tui/render/projection/workbench.go`
  - 从 `AppState + RuntimeTerminalStore` 生成 tiled/floating/overlay 几何投影
- `tui/render/projection/workbench_test.go`
  - projection 几何、z-order、active pane 测试
- `tui/render/canvas/canvas.go`
  - 从旧版迁回 `drawCell`、canvas、clipping、基础 draw API
- `tui/render/canvas/canvas_test.go`
  - canvas set/fill/clipping/overlap 基础测试
- `tui/render/surface/pane_surface.go`
  - live/snapshot/empty/waiting/exited pane surface 统一构建
- `tui/render/surface/pane_surface_test.go`
  - 各类 slot state surface 渲染测试
- `tui/render/compositor/workbench.go`
  - tiled + floating 合成与 pane frame/title/body 绘制
- `tui/render/compositor/workbench_test.go`
  - workbench 合成、floating overlap、z-order 测试
- `tui/render/overlay/compositor.go`
  - overlay backdrop、modal placement、close cleanup 合成
- `tui/render/overlay/compositor_test.go`
  - overlay open/close/return focus/backdrop 测试
- `tui/render/hittest/workbench.go`
  - paneAtPoint、resize handle hit、overlay geometry hit
- `tui/render/hittest/workbench_test.go`
  - pane 命中、浮窗边角命中、遮挡命中测试

### Modified files

- `tui/runtime.go`
  - 切换 renderer 入口到新 `tui/render`
- `tui/runtime_renderer.go`
  - 缩成兼容入口或删除大部分过渡拼接逻辑
- `tui/runtime_modern_renderer.go`
  - 迁出真实 workbench 逻辑，剩余部分只做兼容过渡或直接清理
- `tui/bt/intent_mapper.go`
  - workbench 鼠标改为走几何 hit-test 和拖拽状态机
- `tui/bt/mouse_hit.go`
  - 删除 workbench 文本命中，overlay 文本命中暂时保留
- `tui/domain/types/types.go`
  - 必要时新增 drag session / render geometry 所需状态
- `tui/app/intent/intent.go`
  - 必要时新增 begin/update/end drag intents
- `tui/app/reducer/reducer.go`
  - 处理 drag session、floating move/resize/z-order 统一路径
- `tui/app/reducer/reducer_test.go`
  - reducer 对 drag/floating 新路径的测试

### Existing tests to keep green

- `tui/runtime_program_test.go`
- `tui/runtime_updates_test.go`
- `tui/runtime_session_test.go`
- `tui/runtime_input_test.go`
- `tui/bt/intent_mapper_test.go`
- `tui/app/reducer/reducer_test.go`
- `tui/domain/layout/layout_test.go`

## Execution Rules

- 每个任务先写失败测试，再做最小实现
- 每个任务完成后提交一次中文 commit
- 运行测试必须使用：

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ...
```

- 不要把 `deprecated/tui-legacy` 整块复制回新主线
- 不要再扩展 `screen_shell / chrome_* / section_* / wireframe` 过渡逻辑

### Task 1: Build The Renderer Skeleton

**Files:**
- Create: `tui/render/renderer.go`
- Create: `tui/render/projection/workbench.go`
- Create: `tui/render/projection/workbench_test.go`
- Modify: `tui/runtime.go`
- Modify: `tui/runtime_renderer.go`
- Test: `tui/render/projection/workbench_test.go`

- [ ] **Step 1: Write the failing projection test**

```go
func TestProjectWorkbenchReturnsActivePaneAndOrderedFloating(t *testing.T) {
	state := newProjectionAppState()
	view := ProjectWorkbench(state, nil, 120, 40)

	if view.ActivePaneID != types.PaneID("pane-1") {
		t.Fatalf("expected active pane pane-1, got %q", view.ActivePaneID)
	}
	if len(view.Floating) != 2 {
		t.Fatalf("expected 2 floating panes, got %d", len(view.Floating))
	}
	if view.Floating[1].PaneID != types.PaneID("float-2") {
		t.Fatalf("expected top floating pane float-2, got %q", view.Floating[1].PaneID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/projection -run TestProjectWorkbenchReturnsActivePaneAndOrderedFloating -count=1
```

Expected:

- FAIL with missing package/function errors

- [ ] **Step 3: Write the minimal renderer skeleton**

```go
type WorkbenchView struct {
	ActivePaneID types.PaneID
	Tiled        []PaneProjection
	Floating     []PaneProjection
	Overlay      OverlayProjection
}

func ProjectWorkbench(state types.AppState, screens RuntimeTerminalStore, width, height int) WorkbenchView {
	// 先只投影当前 workspace/tab/pane 和 floating order，后续任务再补 rect/body/overlay。
	return WorkbenchView{}
}
```

- [ ] **Step 4: Run the focused tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/projection -count=1
```

Expected:

- PASS

- [ ] **Step 5: Wire runtime to the new entry**

最小改法：

```go
if deps.Renderer == nil {
	deps.Renderer = render.NewRenderer(render.Config{
		DebugVisible: boolPointer(cfg.DebugUI),
	})
}
```

并让 `tui/runtime_renderer.go` 退化成兼容层或直接转调 `tui/render`。

- [ ] **Step 6: Run the affected runtime tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestBubbleteaProgramRunnerEntersAltScreenAndRendersView|TestRun' -count=1
```

Expected:

- PASS

- [ ] **Step 7: Commit**

```bash
git add tui/render/renderer.go tui/render/projection/workbench.go tui/render/projection/workbench_test.go tui/runtime.go tui/runtime_renderer.go
git commit -m "建立TUI渲染骨架"
```

### Task 2: Backfill Layout Geometry From Legacy

**Files:**
- Modify: `tui/domain/layout/layout.go`
- Modify: `tui/domain/layout/layout_test.go`
- Create: `tui/render/projection/layout_helpers.go`
- Test: `tui/domain/layout/layout_test.go`

- [ ] **Step 1: Write failing tests for missing geometry helpers**

```go
func TestNodeAdjustPaneBoundaryMovesSplitRatioWithinBounds(t *testing.T) {
	root := NewLeaf("pane-1")
	root.Split("pane-1", types.SplitDirectionVertical, "pane-2")

	ok := root.AdjustPaneBoundary("pane-1", types.DirectionRight, 5, 10, types.Rect{W: 80, H: 24})
	if !ok {
		t.Fatal("expected boundary adjust to succeed")
	}
}

func TestNodeSwapWithNeighborSwapsLeafOrder(t *testing.T) {
	root := NewLeaf("pane-1")
	root.Split("pane-1", types.SplitDirectionVertical, "pane-2")

	if !root.SwapWithNeighbor("pane-1", 1) {
		t.Fatal("expected swap to succeed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/domain/layout -run 'TestNodeAdjustPaneBoundaryMovesSplitRatioWithinBounds|TestNodeSwapWithNeighborSwapsLeafOrder' -count=1
```

Expected:

- FAIL with missing method errors

- [ ] **Step 3: Port the pure geometry logic**

从旧版迁回以下最小实现，并统一改成新 `types`：

```go
func (n *Node) ContainsPane(paneID types.PaneID) bool
func (n *Node) LeafIDs() []types.PaneID
func (n *Node) SwapWithNeighbor(paneID types.PaneID, delta int) bool
func (n *Node) AdjustPaneBoundary(paneID types.PaneID, dir types.Direction, step, minSpan int, root types.Rect) bool
```

要求：

- 保持纯函数/纯算法
- 注释用中文，说明 ratio 调整边界与最小 span 约束

- [ ] **Step 4: Run layout tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/domain/layout -count=1
```

Expected:

- PASS

- [ ] **Step 5: Commit**

```bash
git add tui/domain/layout/layout.go tui/domain/layout/layout_test.go tui/render/projection/layout_helpers.go
git commit -m "补齐TUI布局几何能力"
```

### Task 3: Port Canvas Primitives

**Files:**
- Create: `tui/render/canvas/canvas.go`
- Create: `tui/render/canvas/canvas_test.go`
- Test: `tui/render/canvas/canvas_test.go`

- [ ] **Step 1: Write failing canvas tests**

```go
func TestCanvasDrawRespectsClipping(t *testing.T) {
	canvas := New(8, 4)
	canvas.Fill(types.Rect{X: 0, Y: 0, W: 8, H: 4}, BlankCell())
	canvas.DrawText(types.Rect{X: 6, Y: 1, W: 2, H: 1}, 6, 1, "hello", DrawStyle{})

	got := canvas.Lines()
	if got[1] != "      he" {
		t.Fatalf("unexpected clipped row: %q", got[1])
	}
}

func TestCanvasDrawOrderLetsFloatingOverwriteTiled(t *testing.T) {
	canvas := New(6, 3)
	canvas.DrawText(types.Rect{X: 0, Y: 1, W: 6, H: 1}, 0, 1, "tiled-", DrawStyle{})
	canvas.DrawText(types.Rect{X: 2, Y: 1, W: 3, H: 1}, 2, 1, "TOP", DrawStyle{})

	if got := canvas.Lines()[1]; got != "tiTOP-" {
		t.Fatalf("unexpected row: %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/canvas -count=1
```

Expected:

- FAIL with missing package/function errors

- [ ] **Step 3: Port the minimal canvas implementation**

最小结构：

```go
type Cell struct {
	Content string
	Width   int
	Style   DrawStyle
}

type Canvas struct {
	width  int
	height int
	cells  [][]Cell
}
```

本任务只迁：

- `BlankCell`
- `Fill`
- `Set`
- `DrawText`
- `Lines`
- clipping

暂不迁：

- dirty rows
- row cache
- redrawDamage

- [ ] **Step 4: Run focused canvas tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/canvas -count=1
```

Expected:

- PASS

- [ ] **Step 5: Commit**

```bash
git add tui/render/canvas/canvas.go tui/render/canvas/canvas_test.go
git commit -m "迁移TUI画布基础原语"
```

### Task 4: Build Pane Surface And Tiled Workbench

**Files:**
- Create: `tui/render/surface/pane_surface.go`
- Create: `tui/render/surface/pane_surface_test.go`
- Create: `tui/render/compositor/workbench.go`
- Create: `tui/render/compositor/workbench_test.go`
- Modify: `tui/render/renderer.go`
- Modify: `tui/runtime_renderer.go`
- Modify: `tui/runtime_modern_renderer.go`
- Test: `tui/render/surface/pane_surface_test.go`
- Test: `tui/render/compositor/workbench_test.go`

- [ ] **Step 1: Write failing surface tests**

```go
func TestBuildPaneSurfaceReturnsWaitingCopyForWaitingPane(t *testing.T) {
	pane := types.PaneState{ID: "pane-wait", SlotState: types.PaneSlotWaiting}

	got := BuildPaneSurface(types.AppState{}, pane, nil, 20, 5)
	if got.Body[0] != "waiting slot" {
		t.Fatalf("unexpected waiting body: %+v", got.Body)
	}
}

func TestComposeWorkbenchDrawsTwoTiledPanes(t *testing.T) {
	view := newSplitWorkbenchProjection()
	lines := ComposeWorkbench(view).Lines()

	if !strings.Contains(strings.Join(lines, "\n"), "pane-left") {
		t.Fatal("expected left pane title")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "pane-right") {
		t.Fatal("expected right pane title")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/surface ./tui/render/compositor -count=1
```

Expected:

- FAIL with missing symbols

- [ ] **Step 3: Implement pane surface first**

`BuildPaneSurface` 至少支持：

```go
switch pane.SlotState {
case types.PaneSlotConnected:
	// live snapshot/vterm body
case types.PaneSlotEmpty:
	// "empty pane"
case types.PaneSlotWaiting:
	// "waiting slot"
case types.PaneSlotExited:
	// "process exited"
}
```

- [ ] **Step 4: Implement tiled compositor**

`ComposeWorkbench` 至少支持：

- tiled pane rect
- frame/title/body
- active/inactive pane 标记
- cursor 最小占位

- [ ] **Step 5: Switch runtime renderer to the new tiled path**

要求：

- 默认 `Render` 输出来自新 workbench compositor
- 保留 debug 模式 fallback，便于比较
- 不再给过渡 `screen_shell` 添加新行为

- [ ] **Step 6: Run focused tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/surface ./tui/render/compositor ./tui -run 'TestBubbleteaProgramRunnerEntersAltScreenAndRendersView' -count=1
```

Expected:

- PASS

- [ ] **Step 7: Commit**

```bash
git add tui/render/surface/pane_surface.go tui/render/surface/pane_surface_test.go tui/render/compositor/workbench.go tui/render/compositor/workbench_test.go tui/render/renderer.go tui/runtime_renderer.go tui/runtime_modern_renderer.go
git commit -m "恢复TUI平铺工作台渲染"
```

### Task 5: Add Geometry Hit-Test And Drag Session

**Files:**
- Create: `tui/render/hittest/workbench.go`
- Create: `tui/render/hittest/workbench_test.go`
- Modify: `tui/domain/types/types.go`
- Modify: `tui/app/intent/intent.go`
- Modify: `tui/app/reducer/reducer.go`
- Modify: `tui/app/reducer/reducer_test.go`
- Modify: `tui/bt/intent_mapper.go`
- Modify: `tui/bt/mouse_hit.go`
- Test: `tui/render/hittest/workbench_test.go`
- Test: `tui/app/reducer/reducer_test.go`
- Test: `tui/bt/intent_mapper_test.go`

- [ ] **Step 1: Write failing tests for geometric hit and drag state**

```go
func TestHitTestReturnsTopFloatingPane(t *testing.T) {
	view := newOverlappedWorkbenchProjection()

	hit, ok := HitPane(view, 12, 6)
	if !ok || hit.PaneID != types.PaneID("float-top") {
		t.Fatalf("expected float-top hit, got %+v ok=%v", hit, ok)
	}
}

func TestReducerBeginFloatingDragStoresDragSession(t *testing.T) {
	state := newFloatingActiveRectAppState()

	result := reducer.New().Reduce(state, intent.BeginFloatingDragIntent{
		PaneID: types.PaneID("float-1"),
		Mode:   intent.FloatingDragMove,
		MouseX: 12,
		MouseY: 8,
	})

	if result.State.UI.Drag == nil {
		t.Fatal("expected drag session to be stored")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/hittest ./tui/app/reducer ./tui/bt -run 'TestHitTestReturnsTopFloatingPane|TestReducerBeginFloatingDragStoresDragSession' -count=1
```

Expected:

- FAIL with missing state/intent errors

- [ ] **Step 3: Add drag session state and intents**

最小新增结构：

```go
type DragState struct {
	PaneID       PaneID
	Mode         DragMode
	StartMouseX  int
	StartMouseY  int
	StartRect    Rect
}
```

最小新增 intents：

```go
type BeginFloatingDragIntent struct { ... }
type UpdateFloatingDragIntent struct { ... }
type EndFloatingDragIntent struct{}
```

- [ ] **Step 4: Map Bubble Tea mouse messages to geometry hit-test**

要求：

- `MapMouse` 不再通过 `View()` 文本解析 workbench pane
- `mouse_hit.go` 只保留 overlay 文本命中
- workbench click / drag / release 改成走 projection + hit-test

- [ ] **Step 5: Reuse existing floating move/resize reducer logic**

要求：

- 鼠标拖动最终仍复用 `MoveFloatingPaneIntent` / `ResizeFloatingPaneIntent`
- 拖拽 session 只负责记录初始几何和模式
- 不复制一套新的 rect 更新算法

- [ ] **Step 6: Run focused tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/hittest ./tui/app/reducer ./tui/bt -count=1
```

Expected:

- PASS

- [ ] **Step 7: Commit**

```bash
git add tui/render/hittest/workbench.go tui/render/hittest/workbench_test.go tui/domain/types/types.go tui/app/intent/intent.go tui/app/reducer/reducer.go tui/app/reducer/reducer_test.go tui/bt/intent_mapper.go tui/bt/mouse_hit.go
git commit -m "切换工作台几何命中与拖拽状态"
```

### Task 6: Restore Floating Compositor

**Files:**
- Modify: `tui/render/projection/workbench.go`
- Modify: `tui/render/compositor/workbench.go`
- Modify: `tui/render/compositor/workbench_test.go`
- Modify: `tui/render/hittest/workbench.go`
- Modify: `tui/runtime_renderer.go`
- Test: `tui/render/compositor/workbench_test.go`

- [ ] **Step 1: Write failing floating compositor tests**

```go
func TestComposeWorkbenchDrawsFloatingAboveTiled(t *testing.T) {
	view := newWorkbenchWithFloatingProjection()
	lines := ComposeWorkbench(view).Lines()

	if !strings.Contains(strings.Join(lines, "\n"), "float-1") {
		t.Fatal("expected floating title to render")
	}
}

func TestComposeWorkbenchKeepsFloatingOrderAsZOrder(t *testing.T) {
	view := newWorkbenchWithTwoFloatingProjection()
	got := ComposeWorkbench(view).Lines()

	if !topFloatingVisible(got, "float-top") {
		t.Fatal("expected top floating pane to remain visible")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/compositor -run 'TestComposeWorkbenchDrawsFloatingAboveTiled|TestComposeWorkbenchKeepsFloatingOrderAsZOrder' -count=1
```

Expected:

- FAIL with incorrect draw order

- [ ] **Step 3: Implement floating draw entries**

从旧版迁回思路，但不回迁旧结构：

```go
type PaneProjection struct {
	PaneID    types.PaneID
	Rect      types.Rect
	Title     string
	Meta      string
	Floating  bool
	ZIndex    int
}
```

要求：

- tiled 先画
- floating 按 `FloatingOrder` 叠加
- top pane 最后画

- [ ] **Step 4: Implement move/resize handle hit helpers**

要求：

- 命中右下角 resize handle
- 标题栏区域进入 move 模式
- body 区域单击只切焦点，不进入 resize

- [ ] **Step 5: Run focused tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/compositor ./tui/render/hittest -count=1
```

Expected:

- PASS

- [ ] **Step 6: Commit**

```bash
git add tui/render/projection/workbench.go tui/render/compositor/workbench.go tui/render/compositor/workbench_test.go tui/render/hittest/workbench.go tui/runtime_renderer.go
git commit -m "恢复TUI浮窗合成与缩放命中"
```

### Task 7: Split Overlay Compositor From Workbench

**Files:**
- Create: `tui/render/overlay/compositor.go`
- Create: `tui/render/overlay/compositor_test.go`
- Modify: `tui/render/renderer.go`
- Modify: `tui/runtime_modern_renderer.go`
- Modify: `tui/bt/mouse_hit.go`
- Test: `tui/render/overlay/compositor_test.go`
- Test: `tui/bt/intent_mapper_test.go`

- [ ] **Step 1: Write failing overlay tests**

```go
func TestComposeOverlayCoversWorkbenchAndReturnsFocusMetadata(t *testing.T) {
	view := newOverlayProjection()
	lines := ComposeOverlay(view).Lines()

	if !strings.Contains(strings.Join(lines, "\n"), "return to pane-1") {
		t.Fatal("expected return focus text")
	}
}

func TestCloseOverlayLeavesWorkbenchFrameClean(t *testing.T) {
	renderer := newOverlayRendererForTest()
	before := renderer.Render(newOverlayState(), nil)
	after := renderer.Render(newClosedOverlayState(), nil)

	if strings.Contains(after, "terminal picker") {
		t.Fatalf("expected overlay content to disappear, before=%q after=%q", before, after)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/overlay ./tui/bt -run 'TestComposeOverlayCoversWorkbenchAndReturnsFocusMetadata|TestCloseOverlayLeavesWorkbenchFrameClean' -count=1
```

Expected:

- FAIL with missing package/function errors

- [ ] **Step 3: Implement overlay compositor**

最小能力：

- backdrop
- modal rect placement
- title/body/footer
- return focus line
- close cleanup

- [ ] **Step 4: Keep overlay text hit, remove workbench text hit**

要求：

- `mouse_hit.go` 只服务 terminal picker / workspace picker / manager / prompt / layout resolve
- workbench pane 命中完全走几何 hit-test

- [ ] **Step 5: Run focused tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/overlay ./tui/bt -count=1
```

Expected:

- PASS

- [ ] **Step 6: Commit**

```bash
git add tui/render/overlay/compositor.go tui/render/overlay/compositor_test.go tui/render/renderer.go tui/runtime_modern_renderer.go tui/bt/mouse_hit.go
git commit -m "拆分TUI覆盖层合成器"
```

### Task 8: Reconnect Business Flows And Remove Transitional Renderer Code

**Files:**
- Modify: `tui/render/renderer.go`
- Modify: `tui/runtime_renderer.go`
- Modify: `tui/runtime_modern_renderer.go`
- Modify: `tui/bt/intent_mapper_test.go`
- Modify: `tui/runtime_input_test.go`
- Modify: `tui/runtime_program_test.go`
- Modify: `tui/runtime_updates_test.go`
- Test: `tui/...`

- [ ] **Step 1: Write failing end-to-end renderer flow tests**

```go
func TestRendererShowsWaitingPaneInsideWorkbench(t *testing.T) {
	renderer := newRendererForTest()
	out := renderer.Render(newWaitingPaneState(), nil)

	if !strings.Contains(out, "waiting slot") {
		t.Fatalf("expected waiting pane text, got %q", out)
	}
}

func TestRendererShowsTerminalManagerAsOverlayAboveWorkbench(t *testing.T) {
	renderer := newRendererForTest()
	out := renderer.Render(newTerminalManagerOverlayState(), nil)

	if !strings.Contains(out, "overlay terminal_manager") {
		t.Fatalf("expected overlay marker, got %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestRendererShowsWaitingPaneInsideWorkbench|TestRendererShowsTerminalManagerAsOverlayAboveWorkbench' -count=1
```

Expected:

- FAIL before business flow reconnection

- [ ] **Step 3: Reconnect render paths for business states**

至少覆盖：

- terminal picker
- terminal manager
- prompt
- layout resolve
- waiting / empty / exited pane
- restore / startup 展示

- [ ] **Step 4: Delete transitional code**

删除或瘦身以下内容：

- `screen_shell`
- `section_*`
- `chrome_*`
- `wireframe`
- workbench 文本命中解析

要求：

- 删除前必须有新测试覆盖对应行为
- 如果某段旧代码仍被 debug 模式依赖，先缩成单独 fallback，不要继续扩展

- [ ] **Step 5: Run affected package tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui ./tui/bt ./tui/app/reducer -count=1
```

Expected:

- PASS

- [ ] **Step 6: Commit**

```bash
git add tui/render/renderer.go tui/runtime_renderer.go tui/runtime_modern_renderer.go tui/bt/intent_mapper_test.go tui/runtime_input_test.go tui/runtime_program_test.go tui/runtime_updates_test.go
git commit -m "回接TUI业务渲染并清理过渡代码"
```

### Task 9: Add Dirty Redraw And Full Verification

**Files:**
- Modify: `tui/render/canvas/canvas.go`
- Modify: `tui/render/canvas/canvas_test.go`
- Modify: `tui/render/compositor/workbench.go`
- Modify: `tui/render/compositor/workbench_test.go`
- Test: `tui/render/canvas/canvas_test.go`
- Test: `tui/render/compositor/workbench_test.go`
- Test: `./...`

- [ ] **Step 1: Write failing tests for dirty rows**

```go
func TestCanvasRedrawDamageOnlyTouchesDirtyRows(t *testing.T) {
	canvas := New(10, 4)
	canvas.DrawText(types.Rect{X: 0, Y: 1, W: 10, H: 1}, 0, 1, "before", DrawStyle{})
	canvas.ResetDirty()
	canvas.DrawText(types.Rect{X: 0, Y: 1, W: 10, H: 1}, 0, 1, "after", DrawStyle{})

	if !canvas.RowDirty(1) {
		t.Fatal("expected row 1 dirty")
	}
	if canvas.RowDirty(0) {
		t.Fatal("expected row 0 clean")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/canvas -run TestCanvasRedrawDamageOnlyTouchesDirtyRows -count=1
```

Expected:

- FAIL with missing dirty-row behavior

- [ ] **Step 3: Port dirty row / redrawDamage**

要求：

- 只迁最小必要能力
- 保持 full redraw fallback
- 不做过早微优化

- [ ] **Step 4: Run render package tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/... -count=1
```

Expected:

- PASS

- [ ] **Step 5: Run full test suite**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1
```

Expected:

- PASS

- [ ] **Step 6: Commit**

```bash
git add tui/render/canvas/canvas.go tui/render/canvas/canvas_test.go tui/render/compositor/workbench.go tui/render/compositor/workbench_test.go
git commit -m "补齐TUI增量重绘与全量验证"
```

## Final Verification Checklist

- [ ] `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/... -count=1`
- [ ] `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/bt ./tui/app/reducer ./tui/domain/layout -count=1`
- [ ] `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -count=1`
- [ ] `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1`

## Notes For The Implementer

- 如果 `tui/runtime_renderer.go` 或 `tui/runtime_modern_renderer.go` 拆文件过大，允许在实现过程中继续细拆，但必须保持职责清晰
- 如果拖拽状态放到 `AppState` 会让 reducer 复杂度明显失控，可以改放到 `bt.Model` 的短生命周期状态里；但前提是测试要证明 reducer 仍然是唯一的几何更新入口
- overlay 行列表点击短期可继续用文本命中；workbench 本体不允许继续依赖文本命中
- 旧版 `layout_decl.go` 不阻塞当前计划，除非 renderer 回接 restore/layout 流时发现缺口
