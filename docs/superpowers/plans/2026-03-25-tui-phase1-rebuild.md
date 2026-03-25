# TUI Phase 1 Rebuild Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild a first runnable `termx` TUI mainline that matches the 2026-03-25 product definition: default workbench, pane/terminal decoupling, overlay-driven connect flow, and a standalone terminal pool page.

**Architecture:** Keep the public `tui.Run` / `tui.Client` API stable, but rebuild the internal TUI as four layers: pure domain state, root app shell, runtime adapters, and cell-based renderers. Reuse validated ideas from `deprecated/tui-legacy/` and `deprecated/tui-reset-2026-03-25/` as references only; transplant focused functions behind new interfaces instead of restoring either archive wholesale.

**Tech Stack:** Go, Bubble Tea, existing `protocol.Client` wrapper, local vterm/snapshot pipeline, cell canvas renderer, repository Go test suite.

---

## Scope and Non-Goals

This plan covers one coherent subsystem: the first usable TUI mainline.

Included in this phase:

- runnable workbench
- default startup into a live shell pane
- tiled/floating pane model
- overlay-driven create/connect flow
- close/disconnect/reconnect pane semantics
- standalone terminal pool page
- workspace state persistence foundation

Explicitly not in this phase:

- project-directory quick-start as a standalone subsystem
- settings page
- fully user-configurable top/bottom bars
- GUI / mobile clients

## Proposed File Structure

Public entrypoints:

- Modify: `tui/runtime.go`
- Modify: `tui/runtime_test.go`
- Modify: `tui/client.go`

Root app shell:

- Create: `tui/app/model.go`
- Create: `tui/app/model_test.go`
- Create: `tui/app/intent.go`
- Create: `tui/app/intent_test.go`
- Create: `tui/app/screen.go`
- Create: `tui/app/overlay.go`

Pure domain state:

- Create: `tui/domain/types/types.go`
- Create: `tui/domain/types/types_test.go`
- Create: `tui/domain/layout/layout.go`
- Create: `tui/domain/layout/layout_test.go`
- Create: `tui/domain/workspace/state.go`
- Create: `tui/domain/workspace/state_test.go`
- Create: `tui/domain/terminal/state.go`
- Create: `tui/domain/terminal/state_test.go`
- Create: `tui/domain/pool/query.go`
- Create: `tui/domain/pool/query_test.go`

Runtime adapters:

- Create: `tui/runtime/bootstrap.go`
- Create: `tui/runtime/bootstrap_test.go`
- Create: `tui/runtime/program.go`
- Create: `tui/runtime/program_test.go`
- Create: `tui/runtime/input_router.go`
- Create: `tui/runtime/input_router_test.go`
- Create: `tui/runtime/terminal_service.go`
- Create: `tui/runtime/terminal_service_test.go`
- Create: `tui/runtime/session_store.go`
- Create: `tui/runtime/session_store_test.go`
- Create: `tui/runtime/workspace_store.go`
- Create: `tui/runtime/workspace_store_test.go`
- Create: `tui/runtime/update_loop.go`
- Create: `tui/runtime/update_loop_test.go`

Renderer:

- Create: `tui/render/canvas/canvas.go`
- Create: `tui/render/canvas/canvas_test.go`
- Create: `tui/render/chrome/frame.go`
- Create: `tui/render/chrome/frame_test.go`
- Create: `tui/render/workbench/view.go`
- Create: `tui/render/workbench/view_test.go`
- Create: `tui/render/overlay/view.go`
- Create: `tui/render/overlay/view_test.go`
- Create: `tui/render/pool/view.go`
- Create: `tui/render/pool/view_test.go`

Reference-only files to consult while implementing:

- `deprecated/tui-legacy/pkg/render.go`
- `deprecated/tui-legacy/pkg/model.go`
- `deprecated/tui-legacy/pkg/terminal_manager.go`
- `deprecated/tui-legacy/docs/interaction-spec.md`
- `deprecated/tui-reset-2026-03-25/tui/domain/layout/layout.go.disabled`
- `deprecated/tui-reset-2026-03-25/tui/render/canvas/canvas.go.disabled`
- `deprecated/tui-reset-2026-03-25/tui/render/projection/workbench.go.disabled`

## Cross-Cutting Rules

- 所有重点函数、边界复杂处补中文注释，尤其是 pane/terminal 生命周期、overlay 焦点模型、layout 投影
- 优先写接口，再写实现，尤其是 runtime 适配层与 renderer 之间
- 每个任务都先补失败测试，再写最小实现
- 每个任务结束都提交中文 commit
- 每轮验证至少跑被修改包测试；阶段结束跑 `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1`

### Task 1: Root App Shell and Screen Router

**Files:**
- Modify: `tui/runtime.go`
- Modify: `tui/runtime_test.go`
- Create: `tui/app/model.go`
- Create: `tui/app/model_test.go`
- Create: `tui/app/screen.go`
- Create: `tui/app/overlay.go`
- Create: `tui/runtime/program.go`
- Create: `tui/runtime/program_test.go`

- [ ] **Step 1: Write the failing tests for app bootstrap**

```go
func TestRunStartsProgramWithWorkbenchScreen(t *testing.T) {
    runner := &stubProgramRunner{}
    err := runWithDependencies(stubClient{}, Config{Workspace: "main"}, nil, io.Discard, runtimeDependencies{
        ProgramRunner: runner,
    })
    if err != nil {
        t.Fatalf("runWithDependencies returned error: %v", err)
    }
    if runner.initialScreen != app.ScreenWorkbench {
        t.Fatalf("expected workbench screen, got %v", runner.initialScreen)
    }
}

func TestRootModelCanSwitchBetweenWorkbenchAndTerminalPoolScreens(t *testing.T) {
    model := app.NewModel(sampleRootState())
    model = model.Apply(app.IntentOpenTerminalPool)
    if model.Screen != app.ScreenTerminalPool {
        t.Fatalf("expected terminal pool screen, got %v", model.Screen)
    }
    model = model.Apply(app.IntentCloseScreen)
    if model.Screen != app.ScreenWorkbench {
        t.Fatalf("expected workbench screen after close, got %v", model.Screen)
    }
}
```

- [ ] **Step 2: Run the focused tests to verify they fail against the reset stub**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestRunStartsProgramWithWorkbenchScreen|TestRootModelCanSwitchBetweenWorkbenchAndTerminalPoolScreens|TestRunReturnsResetError' -count=1
```

Expected: FAIL because `Run` still returns the reset error and no root model exists.

- [ ] **Step 3: Implement the minimal root shell**

Implement:

- `app.Model` with `Screen`, `Overlay`, `FocusTarget`
- `runtime.ProgramRunner` interface
- `runtime.go` wiring that builds the app shell instead of returning the reset error
- 中文注释解释为什么顶层 screen router 和 overlay stack 要单独建模

- [ ] **Step 4: Run package tests to verify the shell passes**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/... -run 'TestRunStartsProgramWithWorkbenchScreen|TestRootModel' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/runtime.go tui/runtime_test.go tui/app/model.go tui/app/model_test.go tui/app/screen.go tui/app/overlay.go tui/runtime/program.go tui/runtime/program_test.go
git commit -m "建立TUI根应用壳层"
```

### Task 2: Pure Workspace, Pane, and Layout Domain

**Files:**
- Create: `tui/domain/types/types.go`
- Create: `tui/domain/types/types_test.go`
- Create: `tui/domain/layout/layout.go`
- Create: `tui/domain/layout/layout_test.go`
- Create: `tui/domain/workspace/state.go`
- Create: `tui/domain/workspace/state_test.go`
- Create: `tui/domain/terminal/state.go`
- Create: `tui/domain/terminal/state_test.go`
- Modify: `tui/app/model.go`
- Test: `tui/app/model_test.go`

- [ ] **Step 1: Write failing pure-domain tests**

```go
func TestLayoutSplitRemoveAndRects(t *testing.T) {
    root := layout.NewLeaf(types.PaneID("pane-1"))
    if !root.Split(types.PaneID("pane-1"), types.SplitDirectionVertical, types.PaneID("pane-2")) {
        t.Fatal("expected split to succeed")
    }
    rects := root.Rects(types.Rect{X: 0, Y: 0, W: 120, H: 40})
    if len(rects) != 2 {
        t.Fatalf("expected 2 pane rects, got %d", len(rects))
    }
}

func TestWorkspaceTracksUnconnectedPaneAndFloatingPane(t *testing.T) {
    ws := workspace.NewTemporary("main")
    if ws.ActiveTab().ActivePaneID == "" {
        t.Fatal("expected default active pane")
    }
}
```

- [ ] **Step 2: Run the focused domain tests to confirm the state model is missing**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/domain/... -run 'TestLayoutSplitRemoveAndRects|TestWorkspaceTracksUnconnectedPaneAndFloatingPane' -count=1
```

Expected: FAIL because the domain packages do not exist yet.

- [ ] **Step 3: Implement pure state packages**

Implement:

- `types`: IDs, `Rect`, split directions, focus enums
- `layout`: pure split tree and rect projection
- `workspace`: workspace/tab/pane/floating state, unconnected pane state, active pane tracking
- `terminal`: metadata and pane-binding snapshot structs

Reference:

- `deprecated/tui-reset-2026-03-25/tui/domain/layout/layout.go.disabled`
- `deprecated/tui-legacy/docs/interaction-spec.md`

- [ ] **Step 4: Run the pure-domain test packages**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/domain/... -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/domain/types tui/domain/layout tui/domain/workspace tui/domain/terminal tui/app/model.go tui/app/model_test.go
git commit -m "补齐TUI工作区与布局领域模型"
```

### Task 3: Runtime Bootstrap and Terminal Session Plumbing

**Files:**
- Create: `tui/runtime/bootstrap.go`
- Create: `tui/runtime/bootstrap_test.go`
- Create: `tui/runtime/input_router.go`
- Create: `tui/runtime/input_router_test.go`
- Create: `tui/runtime/terminal_service.go`
- Create: `tui/runtime/terminal_service_test.go`
- Create: `tui/runtime/session_store.go`
- Create: `tui/runtime/session_store_test.go`
- Create: `tui/runtime/update_loop.go`
- Create: `tui/runtime/update_loop_test.go`
- Modify: `tui/runtime.go`
- Modify: `tui/runtime_test.go`

- [ ] **Step 1: Write failing runtime tests for default startup**

```go
func TestBootstrapCreatesTemporaryWorkspaceWithLiveShellPane(t *testing.T) {
    client := &stubClient{}
    state, err := Bootstrap(context.Background(), client, Config{
        DefaultShell: "/bin/sh",
        Workspace:    "main",
    })
    if err != nil {
        t.Fatalf("Bootstrap returned error: %v", err)
    }
    if state.Workspace.ActiveTab().ActivePane().TerminalID == "" {
        t.Fatal("expected default pane to attach a terminal")
    }
}

func TestInputRouterSendsKeysToFocusedWorkbenchPaneAndResizesOwnedTerminal(t *testing.T) {
    router := NewInputRouter(stubTerminalService{})
    state := sampleFocusedLivePaneState()
    if err := router.HandleKey(state, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}); err != nil {
        t.Fatalf("HandleKey returned error: %v", err)
    }
    if err := router.HandleResize(state, 120, 40); err != nil {
        t.Fatalf("HandleResize returned error: %v", err)
    }
}
```

- [ ] **Step 2: Run runtime-focused tests to verify startup and streaming are not wired**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/... -run 'TestBootstrapCreatesTemporaryWorkspaceWithLiveShellPane|TestSessionStore' -count=1
```

Expected: FAIL because bootstrap/session runtime is still absent.

- [ ] **Step 3: Implement bootstrap and live terminal adapters**

Implement:

- bootstrap from `Config` into a temporary workbench state
- attach path for `Config.AttachID`
- `TerminalService` interface over `tui.Client`
- session store for snapshot + stream frames + metadata refresh
- update loop that converts daemon events into app messages
- input router that sends keyboard input only to the focused live workbench pane
- resize propagation rules that only submit PTY resize from the pane that currently owns resize authority
- focus guard so overlay and terminal-pool preview never implicitly steal workbench input ownership

Reference:

- `deprecated/tui-legacy/pkg/client.go`
- `deprecated/tui-reset-2026-03-25/tui/runtime.go.disabled`

- [ ] **Step 4: Run runtime package tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/... -run 'TestBootstrap|TestInputRouter|TestSessionStore|TestUpdateLoop' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/runtime.go tui/runtime_test.go tui/runtime/bootstrap.go tui/runtime/bootstrap_test.go tui/runtime/input_router.go tui/runtime/input_router_test.go tui/runtime/terminal_service.go tui/runtime/terminal_service_test.go tui/runtime/session_store.go tui/runtime/session_store_test.go tui/runtime/update_loop.go tui/runtime/update_loop_test.go
git commit -m "接通TUI启动与终端会话运行时"
```

### Task 4: Cell Canvas and Workbench Renderer

**Files:**
- Create: `tui/render/canvas/canvas.go`
- Create: `tui/render/canvas/canvas_test.go`
- Create: `tui/render/chrome/frame.go`
- Create: `tui/render/chrome/frame_test.go`
- Create: `tui/render/workbench/view.go`
- Create: `tui/render/workbench/view_test.go`
- Modify: `tui/app/model.go`
- Modify: `tui/runtime/program.go`

- [ ] **Step 1: Write failing renderer tests for workbench chrome**

```go
func TestWorkbenchViewShowsTopbarPaneTitleAndActionBar(t *testing.T) {
    view := workbench.Render(sampleWorkbenchState(), 120, 40)
    if !strings.Contains(view, "[main]") {
        t.Fatal("expected workspace label in top bar")
    }
    if !strings.Contains(view, "owner") {
        t.Fatal("expected pane status metadata")
    }
}

func TestUnconnectedPaneShowsActionableEmptyState(t *testing.T) {
    view := workbench.Render(sampleUnconnectedWorkbenchState(), 80, 24)
    if !strings.Contains(view, "connect existing terminal") {
        t.Fatal("expected empty-state action text")
    }
    if !strings.Contains(view, "create new terminal") {
        t.Fatal("expected create action text")
    }
    if !strings.Contains(view, "open terminal pool") {
        t.Fatal("expected manager action text")
    }
}

func TestWorkbenchViewRendersLivePaneBodyFromSessionSnapshot(t *testing.T) {
    view := workbench.Render(sampleLivePaneWorkbenchState(), 120, 40)
    if !strings.Contains(view, "hello from shell") {
        t.Fatal("expected live pane body content")
    }
}
```

- [ ] **Step 2: Run renderer tests to verify the render path is still missing**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/... -run 'TestWorkbenchViewShowsTopbarPaneTitleAndActionBar|TestUnconnectedPaneShowsActionableEmptyState|TestWorkbenchViewRendersLivePaneBodyFromSessionSnapshot' -count=1
```

Expected: FAIL because no render packages exist yet.

- [ ] **Step 3: Implement canvas primitives and workbench renderer**

Implement:

- cell canvas, width-normalized clipping, border drawing
- top bar, pane chrome, bottom action bar
- tiled and floating pane composition
- unconnected pane empty state with all three entry points:
  - connect existing terminal
  - create new terminal
  - open terminal pool
- active live pane body rendering from the session/snapshot pipeline, not just frame chrome

Reference:

- `deprecated/tui-legacy/pkg/render.go`
- `deprecated/tui-reset-2026-03-25/tui/render/canvas/canvas.go.disabled`

- [ ] **Step 4: Run render package tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/... -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/render/canvas tui/render/chrome tui/render/workbench tui/app/model.go tui/runtime/program.go
git commit -m "恢复TUI工作台渲染主干"
```

### Task 5: Overlay Flow and Pane/Terminal Lifecycle Actions

**Files:**
- Create: `tui/domain/pool/query.go`
- Create: `tui/domain/pool/query_test.go`
- Create: `tui/app/intent.go`
- Create: `tui/app/intent_test.go`
- Modify: `tui/app/model.go`
- Modify: `tui/app/model_test.go`
- Create: `tui/render/overlay/view.go`
- Create: `tui/render/overlay/view_test.go`
- Modify: `tui/render/workbench/view.go`

- [ ] **Step 1: Write failing interaction tests for create/connect semantics**

```go
func TestSplitCreatesPaneSlotAndOpensConnectDialog(t *testing.T) {
    model := newWorkbenchModelForTest()
    next := model.Apply(IntentSplitVertical)
    if next.Overlay.Kind != OverlayConnectDialog {
        t.Fatalf("expected connect dialog, got %v", next.Overlay.Kind)
    }
    if next.Workspace.ActiveTab().PaneCount() != 2 {
        t.Fatal("expected pane slot to be created before dialog resolves")
    }
}

func TestCancelConnectLeavesUnconnectedPane(t *testing.T) {
    model := modelWithPendingConnectDialog()
    next := model.Apply(IntentCancelOverlay)
    if !next.Workspace.ActiveTab().ActivePane().IsUnconnected() {
        t.Fatal("expected pane to remain unconnected after cancel")
    }
}

func TestConnectExistingPickerUsesGlobalScopeAndRecentUserInteractionSort(t *testing.T) {
    items := pool.BuildConnectItems(samplePoolTerminals())
    if items[0].TerminalID != "recent-input-terminal" {
        t.Fatalf("expected recent user interaction first, got %q", items[0].TerminalID)
    }
    if len(items) != 3 {
        t.Fatalf("expected all terminals in picker scope, got %d", len(items))
    }
}

func TestNewTabAndNewFloatOpenTheSameConnectDialog(t *testing.T) {
    base := newWorkbenchModelForTest()
    nextTab := base.Apply(IntentNewTab)
    if nextTab.Overlay.Kind != OverlayConnectDialog {
        t.Fatalf("expected connect dialog for new tab, got %v", nextTab.Overlay.Kind)
    }
    nextFloat := base.Apply(IntentNewFloat)
    if nextFloat.Overlay.Kind != OverlayConnectDialog {
        t.Fatalf("expected connect dialog for new float, got %v", nextFloat.Overlay.Kind)
    }
}

func TestCreateNewTerminalBranchCreatesAndBindsTerminal(t *testing.T) {
    model := modelWithPendingConnectDialog()
    next := model.Apply(IntentConfirmCreateTerminal{
        Command: []string{"/bin/sh"},
        Name:    "shell-2",
    })
    if next.Workspace.ActiveTab().ActivePane().TerminalID == "" {
        t.Fatal("expected pane to bind the newly created terminal")
    }
}

func TestClosePaneDoesNotKillTerminalByDefault(t *testing.T) {
    model := modelWithSharedTerminal()
    next := model.Apply(IntentClosePane)
    if next.Workspace.ActiveTab().PaneCount() != 1 {
        t.Fatal("expected pane to close")
    }
    if next.Terminals["term-1"].State != terminal.StateRunning {
        t.Fatal("expected terminal to keep running")
    }
}

func TestDisconnectPaneKeepsPaneAndClearsBinding(t *testing.T) {
    model := modelWithLivePane()
    next := model.Apply(IntentDisconnectPane)
    if !next.Workspace.ActiveTab().ActivePane().IsUnconnected() {
        t.Fatal("expected pane to become unconnected")
    }
}

func TestReconnectPaneRebindsToSelectedTerminal(t *testing.T) {
    model := modelWithReconnectOverlay()
    next := model.Apply(IntentConfirmReconnect("term-2"))
    if next.Workspace.ActiveTab().ActivePane().TerminalID != "term-2" {
        t.Fatal("expected pane to reconnect to chosen terminal")
    }
}

func TestClosePaneAndKillTerminalStopsTerminal(t *testing.T) {
    model := modelWithLivePane()
    next := model.Apply(IntentClosePaneAndKillTerminal)
    if next.Terminals["term-1"].State != terminal.StateExited {
        t.Fatal("expected terminal to be marked exited")
    }
}
```

- [ ] **Step 2: Run app/overlay tests to verify the interaction reducer does not exist yet**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/app ./tui/render/overlay ./tui/domain/pool -run 'TestSplitCreatesPaneSlotAndOpensConnectDialog|TestCancelConnectLeavesUnconnectedPane|TestConnectExistingPickerUsesGlobalScopeAndRecentUserInteractionSort|TestNewTabAndNewFloatOpenTheSameConnectDialog|TestCreateNewTerminalBranchCreatesAndBindsTerminal|TestClosePaneDoesNotKillTerminalByDefault|TestDisconnectPaneKeepsPaneAndClearsBinding|TestReconnectPaneRebindsToSelectedTerminal|TestClosePaneAndKillTerminalStopsTerminal' -count=1
```

Expected: FAIL because create/connect flow is not implemented.

- [ ] **Step 3: Implement unified overlay and pane lifecycle actions**

Implement:

- split/new tab/new float all go through “create pane slot -> open connect dialog”
- cancel leaves `unconnected pane`
- explicit actions for close pane, disconnect pane, reconnect pane, kill terminal
- overlay focus priority and `Esc` handling
- lightweight “connect existing terminal” picker with:
  - global terminal scope
  - default ordering by recent user interaction
  - pure output treated as auxiliary state, not sort priority
  - create-new and connect-existing paths tested separately
- `split / new tab / new float` must all share the same connect dialog contract
- lifecycle reducer tests must lock the four distinct actions:
  - close pane
  - disconnect pane
  - reconnect pane
  - close pane and kill terminal

Reference:

- `deprecated/tui-legacy/docs/interaction-spec.md`
- `deprecated/tui-legacy/pkg/model.go`

- [ ] **Step 4: Run the focused interaction tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/app ./tui/render/overlay ./tui/domain/pool -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/domain/pool/query.go tui/domain/pool/query_test.go tui/app/intent.go tui/app/intent_test.go tui/app/model.go tui/app/model_test.go tui/render/overlay/view.go tui/render/overlay/view_test.go tui/render/workbench/view.go
git commit -m "打通TUI弹层与面板连接语义"
```

### Task 6: Terminal Pool Page

**Files:**
- Create: `tui/render/pool/view.go`
- Create: `tui/render/pool/view_test.go`
- Modify: `tui/app/intent.go`
- Modify: `tui/app/intent_test.go`
- Modify: `tui/app/model.go`
- Modify: `tui/app/model_test.go`
- Modify: `tui/runtime/terminal_service.go`
- Modify: `tui/runtime/terminal_service_test.go`
- Modify: `tui/runtime/session_store.go`

- [ ] **Step 1: Write failing tests for terminal pool grouping and page rendering**

```go
func TestGroupTerminalsIntoVisibleParkedExited(t *testing.T) {
    groups := pool.Group(sampleTerminals())
    if len(groups.Visible) != 1 || len(groups.Parked) != 1 || len(groups.Exited) != 1 {
        t.Fatalf("unexpected group counts: %+v", groups)
    }
}

func TestTerminalPoolViewShowsThreeColumns(t *testing.T) {
    view := poolview.Render(samplePoolPageState(), 160, 40)
    if !strings.Contains(view, "visible") || !strings.Contains(view, "parked") || !strings.Contains(view, "exited") {
        t.Fatal("expected group headers")
    }
}

func TestTerminalPoolActionsRenameKillAndOpenTargetPane(t *testing.T) {
    model := modelWithTerminalPoolSelection()
    model = model.Apply(IntentTerminalPoolRename)
    model = model.Apply(IntentTerminalPoolKill)
    model = model.Apply(IntentTerminalPoolOpenHere)
    if !model.HasPendingRuntimeAction() {
        t.Fatal("expected terminal pool actions to enqueue runtime work")
    }
}

func TestTerminalPoolSelectionSwitchesReadonlyLivePreviewSubscription(t *testing.T) {
    model := modelWithTerminalPoolSelection()
    next := model.Apply(IntentTerminalPoolSelect("term-2"))
    if next.TerminalPool.PreviewTerminalID != "term-2" {
        t.Fatal("expected preview terminal to switch")
    }
    if !next.HasPendingPreviewSubscription("term-2") {
        t.Fatal("expected preview subscription refresh")
    }
}

func TestTerminalPoolActionsReachRuntimeService(t *testing.T) {
    svc := &stubTerminalService{}
    err := ExecuteTerminalPoolAction(context.Background(), svc, TerminalPoolAction{
        Kind:       TerminalPoolActionKill,
        TerminalID: "term-2",
    })
    if err != nil {
        t.Fatalf("ExecuteTerminalPoolAction returned error: %v", err)
    }
    if svc.lastKilledTerminalID != "term-2" {
        t.Fatal("expected runtime service to receive kill action")
    }
}
```

- [ ] **Step 2: Run focused pool tests to verify the page does not exist yet**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/domain/pool ./tui/render/pool ./tui/app ./tui/runtime -run 'TestGroupTerminalsIntoVisibleParkedExited|TestTerminalPoolViewShowsThreeColumns|TestTerminalPoolActionsRenameKillAndOpenTargetPane|TestTerminalPoolSelectionSwitchesReadonlyLivePreviewSubscription|TestTerminalPoolActionsReachRuntimeService' -count=1
```

Expected: FAIL because grouping/page rendering is missing.

- [ ] **Step 3: Implement the standalone terminal pool page**

Implement:

- `visible / parked / exited` grouping
- default sort by recent user interaction, not pure output
- middle column readonly live preview
- selection change must rebind the middle column to the selected terminal's live preview stream
- right column metadata first, connections second
- page-level actions biased toward terminal management
- explicit app intents for rename, kill, open-here, open-new-tab, open-floating
- explicit takeover action if the user wants to leave readonly preview and bind/open the selected terminal into the workbench
- runtime action executor must actually call `TerminalService`, not stop at queued UI intents
- app shell must provide reachable navigation into and out of the standalone Terminal Pool page

- [ ] **Step 4: Run pool-related tests**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/domain/pool ./tui/render/pool ./tui/app ./tui/runtime -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/render/pool tui/app/intent.go tui/app/intent_test.go tui/app/model.go tui/app/model_test.go tui/runtime/terminal_service.go tui/runtime/terminal_service_test.go tui/runtime/session_store.go
git commit -m "补齐TUI终端池独立页面"
```

### Task 7: Workspace Persistence, CLI Integration, and End-to-End Verification

**Files:**
- Create: `tui/runtime/workspace_store.go`
- Create: `tui/runtime/workspace_store_test.go`
- Modify: `tui/runtime.go`
- Modify: `tui/runtime_test.go`
- Modify: `cmd/termx/main_test.go`
- Modify: `docs/superpowers/specs/2026-03-25-tui-product-definition-design.md`

- [ ] **Step 1: Write failing tests for workspace save/restore**

```go
func TestWorkspaceStoreRoundTripsWorkbenchState(t *testing.T) {
    store := runtime.NewWorkspaceStore(tempPath)
    original := samplePersistedWorkspaceState()
    if err := store.Save(context.Background(), original); err != nil {
        t.Fatalf("save returned error: %v", err)
    }
    loaded, err := store.Load(context.Background())
    if err != nil {
        t.Fatalf("load returned error: %v", err)
    }
    if diff := cmp.Diff(original, loaded); diff != "" {
        t.Fatalf("workspace mismatch (-want +got):\n%s", diff)
    }
}
```

- [ ] **Step 2: Run persistence-focused tests to verify restore support is incomplete**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/... -run 'TestWorkspaceStoreRoundTripsWorkbenchState|TestRunRestoresWorkspaceState' -count=1
```

Expected: FAIL because workspace persistence is not implemented.

- [ ] **Step 3: Implement workspace persistence and finish CLI wiring**

Implement:

- local workspace save/load store
- startup restore path from `Config.WorkspaceStatePath`
- debounced save after workspace-affecting state mutations
- save-on-exit hook so the next launch can restore the last workbench state
- graceful fallback to temp workspace when restore fails
- update `cmd/termx` tests if any config expectations changed
- spec note updates only if implementation forced a product-level clarification

- [ ] **Step 4: Run full verification**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/... -count=1
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./cmd/termx -count=1
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/runtime/workspace_store.go tui/runtime/workspace_store_test.go tui/runtime.go tui/runtime_test.go cmd/termx/main_test.go docs/superpowers/specs/2026-03-25-tui-product-definition-design.md
git commit -m "完成TUI第一阶段持久化闭环"
```

## Implementation Notes

- `Terminal Pool` 中栏第一阶段默认只读观察，不抢日常输入焦点
- `close pane` 默认语义必须持续保持“不 kill terminal”
- `disconnect pane` 与 `reconnect pane` 必须作为两个独立动作建模，不要重新折叠成单个模糊命令
- 顶栏/底栏配置化、项目目录快速启动、settings 页面都留到后续计划，不要偷渡进本计划

## Done Criteria

完成本计划后，应满足：

- `termx` 默认进入可工作的 TUI workbench，而不是 reset stub
- workbench 具备基本 `workspace / tab / pane / floating pane` 体验
- split/new tab/new float 走统一 connect dialog 流程
- pane/terminal 生命周期语义符合 spec
- terminal pool 作为独立页面可进入、可观察、可管理
- workspace 状态可保存并在下次启动时恢复基础工作现场
- 全量 `go test ./... -count=1` 通过
