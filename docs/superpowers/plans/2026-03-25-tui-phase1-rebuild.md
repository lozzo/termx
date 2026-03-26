# TUI 第一阶段重建实现计划

> **给执行型 agent：** 必须使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans` 按任务逐步实现。本计划使用 checkbox（`- [ ]`）跟踪步骤。

**目标：** 重建一个首个可运行的 `termx` TUI 主线，实现 2026-03-25 产品定义：默认进入 workbench、pane/terminal 解耦、overlay 驱动的连接流程，以及独立的 terminal pool 页面。

**架构：** 保持公开 `tui.Run` / `tui.Client` API 稳定，但把内部 TUI 重建为四层：状态模型层、根应用壳层、runtime 适配层、cell-based 渲染层。这里不做领域驱动开发，不引入额外业务术语；目录拆分只服务于状态归属清晰、测试好写、渲染链路稳定。`deprecated/tui-legacy/` 只作为参考源，只抽取被验证过的局部思路和函数，不整包恢复任何旧实现。

**技术栈：** Go、Bubble Tea、现有 `protocol.Client` 封装、本地 vterm/snapshot 管线、cell canvas 渲染器、仓库内 Go 测试体系。

---

## 范围与非目标

本计划只覆盖一个完整子系统：首个可用的 TUI 主线。

本阶段包含：

- runnable workbench
- default startup into a live shell pane
- tiled/floating pane model
- overlay-driven create/connect flow
- close/disconnect/reconnect pane semantics
- standalone terminal pool page
- workspace state persistence foundation

本阶段明确不做：

- project-directory quick-start as a standalone subsystem
- settings page
- fully user-configurable top/bottom bars
- GUI / mobile clients

## 已确认规则同步

下面这些规则已经在口头设计中确认，执行时必须视为前置约束：

- 一期不做 `fit / fixed / pin / size lock warn / auto-acquire`
- 一个 terminal 同时最多一个 owner
- 新 pane 连接已有 terminal 时：无 owner 则自动成为 owner，否则默认 follower
- owner 关闭/解绑后不自动迁移；其他 pane 需显式执行 `Become Owner`
- `kill terminal` 与 `remove terminal` 分开：
  - `kill` 后进入 `exited pane`
  - `remove` 后进入 `unconnected pane`
- `R` 是对原 terminal 对象做 restart，不是新建替换
- pane 小于 terminal 时默认左上裁切
- 支持 viewport move 模式与鼠标拖拽移动内部观察位置
- 裁切侧显示 `+`
- pane 大于 terminal 的空白区显示小圆点
- pane 内部观察偏移属于 workspace 可恢复状态
- terminal 内容以 `stream` 为主维护，本地长期状态模型负责承载正文；`snapshot` 仅用于纠偏与恢复
- terminal 保持一份主状态；pane 不复制正文，只保存显示/观察状态
- shared 连接关系归在 terminal 侧
- reducer 只产出 effect 描述，runtime 统一执行副作用
- 渲染链路固定为：
  `state -> screen snapshot -> canvas composition -> output`
- 鼠标命中优先级固定为：
  `overlay > floating > tiled > pane 正文`
- Terminal Pool 第一阶段要支持 terminal metadata/tags 的显示、编辑与搜索
- help 第一阶段就做成分组式帮助层
- floating pane 允许拖出主视口，但必须始终保留左上角拖动锚点在大窗口内，并支持“呼回并居中”

## 文件结构规划

这里的目录拆分是工程分工，不是 DDD：

- `app/` 负责顶层页面、意图、overlay 与焦点
- `state/` 负责 workspace / layout / terminal / pool 的纯数据与纯函数
- `runtime/` 负责 client 调用、订阅、输入路由、持久化
- `render/` 负责把 screen snapshot 画成最终 TUI 文本

如果执行中发现某个文件继续膨胀，可以继续拆小，但不要引入额外抽象层来“显得高级”。

公开入口：

- Modify: `tui/runtime.go`
- Modify: `tui/runtime_test.go`
- Modify: `tui/client.go`

根应用壳层：

- Create: `tui/app/model.go`
- Create: `tui/app/model_test.go`
- Create: `tui/app/intent.go`
- Create: `tui/app/intent_test.go`
- Create: `tui/app/screen.go`
- Create: `tui/app/overlay.go`

状态模型层：

- Create: `tui/state/types/types.go`
- Create: `tui/state/types/types_test.go`
- Create: `tui/state/layout/layout.go`
- Create: `tui/state/layout/layout_test.go`
- Create: `tui/state/workspace/state.go`
- Create: `tui/state/workspace/state_test.go`
- Create: `tui/state/terminal/state.go`
- Create: `tui/state/terminal/state_test.go`
- Create: `tui/state/pool/query.go`
- Create: `tui/state/pool/query_test.go`

Runtime 适配层：

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

渲染层：

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

只读参考文件：

- `deprecated/tui-legacy/pkg/render.go`
- `deprecated/tui-legacy/pkg/model.go`
- `deprecated/tui-legacy/pkg/terminal_manager.go`
- `deprecated/tui-legacy/docs/interaction-spec.md`
- `deprecated/tui-reset-2026-03-25/tui/domain/layout/layout.go.disabled`
- `deprecated/tui-reset-2026-03-25/tui/render/canvas/canvas.go.disabled`
- `deprecated/tui-reset-2026-03-25/tui/render/projection/workbench.go.disabled`

## 通用规则

- 所有重点函数、边界复杂处补中文注释，尤其是 pane/terminal 生命周期、overlay 焦点模型、layout 投影
- 优先写接口，再写实现，尤其是 runtime 适配层与 renderer 之间
- 每个任务都先补失败测试，再写最小实现
- 每个任务结束都提交中文 commit
- 每轮验证至少跑被修改包测试；阶段结束跑 `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1`

## 实施批次与验收闭环

不要按“把所有底层都搭完再看界面”的顺序推进，按下面的闭环批次做：

### 批次 A：最小可启动闭环

目标：

- `termx` 能进入 workbench
- 默认有一个 live shell pane
- 顶栏 / pane 标题栏 / 底栏能画出来
- 单 pane 正文能显示 session 内容

对应任务：

- Task 1
- Task 2 的最小必需部分
- Task 3 的 bootstrap / session 基础
- Task 4 的单 pane 渲染主干

验收：

- 启动不再返回 reset stub
- 默认进入 workbench
- 至少能看到一个 live pane 的真实正文

### 批次 B：工作流闭环

目标：

- split / new tab / new float 走统一 connect dialog
- `unconnected pane` / `exited pane` 行为正确
- close / disconnect / reconnect / kill 的语义分开

对应任务：

- Task 5
- Task 4 中与空态、状态态相关的剩余部分

验收：

- 连接流程完整
- 生命周期动作不混淆
- 线框图中的主路径与状态路径能跑通

### 批次 C：管理页闭环

目标：

- Terminal Pool 可进入、可观察、可执行核心动作
- `kill` 与 `remove` 在页面和 workbench 上反馈不同结果

对应任务：

- Task 6

验收：

- Pool 三栏页可用
- open here / new tab / floating / kill / remove 可工作

### 批次 D：恢复与收尾

目标：

- workspace 状态可保存和恢复
- CLI 接线收口
- 全量测试通过

对应任务：

- Task 7

验收：

- `go test ./... -count=1` 通过
- 重启后可恢复基础工作现场

### Task 1：根应用壳层与页面路由

**Files:**
- Modify: `tui/runtime.go`
- Modify: `tui/runtime_test.go`
- Create: `tui/app/model.go`
- Create: `tui/app/model_test.go`
- Create: `tui/app/screen.go`
- Create: `tui/app/overlay.go`
- Create: `tui/runtime/program.go`
- Create: `tui/runtime/program_test.go`

- [ ] **Step 1：先写启动与页面切换的失败测试**

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

- [ ] **Step 2：运行聚焦测试，确认它们会在 reset stub 上失败**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestRunStartsProgramWithWorkbenchScreen|TestRootModelCanSwitchBetweenWorkbenchAndTerminalPoolScreens|TestRunReturnsResetError' -count=1
```

Expected: FAIL because `Run` still returns the reset error and no root model exists.

- [ ] **Step 3：实现最小根壳层**

Implement:

- `app.Model` with `Screen`, `Overlay`, `FocusTarget`
- `runtime.ProgramRunner` interface
- `runtime.go` wiring that builds the app shell instead of returning the reset error
- 中文注释解释为什么顶层 screen router 和 overlay stack 要单独建模

- [ ] **Step 4：运行包测试，确认根壳层通过**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/... -run 'TestRunStartsProgramWithWorkbenchScreen|TestRootModel' -count=1
```

Expected: PASS.

- [ ] **Step 5：提交**

```bash
git add tui/runtime.go tui/runtime_test.go tui/app/model.go tui/app/model_test.go tui/app/screen.go tui/app/overlay.go tui/runtime/program.go tui/runtime/program_test.go
git commit -m "建立TUI根应用壳层"
```

### Task 2：补齐 workspace / pane / layout 状态模型

**Files:**
- Create: `tui/state/types/types.go`
- Create: `tui/state/types/types_test.go`
- Create: `tui/state/layout/layout.go`
- Create: `tui/state/layout/layout_test.go`
- Create: `tui/state/workspace/state.go`
- Create: `tui/state/workspace/state_test.go`
- Create: `tui/state/terminal/state.go`
- Create: `tui/state/terminal/state_test.go`
- Modify: `tui/app/model.go`
- Test: `tui/app/model_test.go`

- [ ] **Step 1：先写状态模型层失败测试**

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

- [ ] **Step 2：运行聚焦测试，确认纯状态模型尚不存在**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/state/... -run 'TestLayoutSplitRemoveAndRects|TestWorkspaceTracksUnconnectedPaneAndFloatingPane' -count=1
```

Expected: FAIL because the state packages do not exist yet.

- [ ] **Step 3：实现状态模型层包**

Implement:

- `types`: IDs, `Rect`, split directions, focus enums
- `layout`: pure split tree and rect projection
- `workspace`: workspace/tab/pane/floating state, unconnected pane state, active pane tracking
- `terminal`: metadata and pane-binding snapshot structs

Reference:

- `deprecated/tui-reset-2026-03-25/tui/domain/layout/layout.go.disabled`
- `deprecated/tui-legacy/docs/interaction-spec.md`

- [ ] **Step 4：运行状态模型层测试**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/state/... -count=1
```

Expected: PASS.

- [ ] **Step 5：提交**

```bash
git add tui/state/types tui/state/layout tui/state/workspace tui/state/terminal tui/app/model.go tui/app/model_test.go
git commit -m "补齐TUI工作区与布局状态模型"
```

### Task 3：接通启动流程与 terminal session runtime

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

- [ ] **Step 1：先写默认启动与输入路由的失败测试**

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

- [ ] **Step 2：运行 runtime 聚焦测试，确认启动与 streaming 尚未接线**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/... -run 'TestBootstrapCreatesTemporaryWorkspaceWithLiveShellPane|TestSessionStore' -count=1
```

Expected: FAIL because bootstrap/session runtime is still absent.

- [ ] **Step 3：实现 bootstrap 与 live terminal 适配层**

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

- [ ] **Step 4：运行 runtime 测试**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/... -run 'TestBootstrap|TestInputRouter|TestSessionStore|TestUpdateLoop' -count=1
```

Expected: PASS.

- [ ] **Step 5：提交**

```bash
git add tui/runtime.go tui/runtime_test.go tui/runtime/bootstrap.go tui/runtime/bootstrap_test.go tui/runtime/input_router.go tui/runtime/input_router_test.go tui/runtime/terminal_service.go tui/runtime/terminal_service_test.go tui/runtime/session_store.go tui/runtime/session_store_test.go tui/runtime/update_loop.go tui/runtime/update_loop_test.go
git commit -m "接通TUI启动与终端会话运行时"
```

### Task 4：实现 cell canvas 与 workbench 渲染主干

**Files:**
- Create: `tui/render/canvas/canvas.go`
- Create: `tui/render/canvas/canvas_test.go`
- Create: `tui/render/chrome/frame.go`
- Create: `tui/render/chrome/frame_test.go`
- Create: `tui/render/workbench/view.go`
- Create: `tui/render/workbench/view_test.go`
- Modify: `tui/app/model.go`
- Modify: `tui/runtime/program.go`

- [ ] **Step 1：先写 workbench 渲染失败测试**

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

- [ ] **Step 2：运行渲染测试，确认渲染路径尚不存在**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/... -run 'TestWorkbenchViewShowsTopbarPaneTitleAndActionBar|TestUnconnectedPaneShowsActionableEmptyState|TestWorkbenchViewRendersLivePaneBodyFromSessionSnapshot' -count=1
```

Expected: FAIL because no render packages exist yet.

- [ ] **Step 3：实现 canvas 基元与 workbench 渲染器**

Implement:

- cell canvas, width-normalized clipping, border drawing
- top bar, pane chrome, bottom action bar
- tiled and floating pane composition
- unconnected pane empty state with all three entry points:
  - connect existing terminal
  - create new terminal
  - open terminal pool
- active live pane body rendering from the session/snapshot pipeline, not just frame chrome
- Task 4 交付不算完成，除非 `unconnected pane` 的三个入口文案和布局都能在渲染测试里被锁住

Reference:

- `deprecated/tui-legacy/pkg/render.go`
- `deprecated/tui-reset-2026-03-25/tui/render/canvas/canvas.go.disabled`

- [ ] **Step 4：运行渲染层测试**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/... -count=1
```

Expected: PASS.

- [ ] **Step 5：提交**

```bash
git add tui/render/canvas tui/render/chrome tui/render/workbench tui/app/model.go tui/runtime/program.go
git commit -m "恢复TUI工作台渲染主干"
```

### Task 5：打通 overlay 流程与 pane/terminal 生命周期动作

**Files:**
- Create: `tui/state/pool/query.go`
- Create: `tui/state/pool/query_test.go`
- Create: `tui/app/intent.go`
- Create: `tui/app/intent_test.go`
- Modify: `tui/app/model.go`
- Modify: `tui/app/model_test.go`
- Modify: `tui/runtime/terminal_service.go`
- Modify: `tui/runtime/terminal_service_test.go`
- Create: `tui/render/overlay/view.go`
- Create: `tui/render/overlay/view_test.go`
- Modify: `tui/render/workbench/view.go`

- [ ] **Step 1：先写创建/连接/生命周期语义的失败测试**

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

func TestRuntimeExecutesCreateAndKillTerminalActions(t *testing.T) {
    svc := &stubTerminalService{}
    if err := ExecuteWorkbenchAction(context.Background(), svc, PendingWorkbenchAction{
        Kind:    PendingWorkbenchActionCreateTerminal,
        Command: []string{"/bin/sh"},
        Name:    "shell-2",
    }); err != nil {
        t.Fatalf("create action returned error: %v", err)
    }
    if err := ExecuteWorkbenchAction(context.Background(), svc, PendingWorkbenchAction{
        Kind:       PendingWorkbenchActionKillTerminal,
        TerminalID: "term-2",
    }); err != nil {
        t.Fatalf("kill action returned error: %v", err)
    }
    if svc.lastCreatedName != "shell-2" || svc.lastKilledTerminalID != "term-2" {
        t.Fatal("expected runtime service to receive create/kill actions")
    }
}

func TestRemoveTerminalTurnsAttachedPaneIntoUnconnectedPane(t *testing.T) {
    model := modelWithLivePane()
    next := model.Apply(IntentRemoveTerminal("term-1"))
    if !next.Workspace.ActiveTab().ActivePane().IsUnconnected() {
        t.Fatal("expected attached pane to become unconnected")
    }
}

func TestRestartExitedTerminalKeepsOriginalTerminalIdentity(t *testing.T) {
    model := modelWithExitedPane()
    next := model.Apply(IntentRestartTerminal("term-1"))
    if next.Workspace.ActiveTab().ActivePane().TerminalID != "term-1" {
        t.Fatal("expected restart to keep original terminal id")
    }
}

func TestRemoteRemoveNoticeOnlyAppearsForVisibleAffectedPane(t *testing.T) {
    model := modelWithVisibleTerminal("term-1")
    next := model.Apply(IntentRemoteTerminalRemoved("term-1", "api-dev"))
    if next.Notice == nil {
        t.Fatal("expected visible remove notice")
    }
}

func TestHelpOverlayOpensAsGroupedHelpScreen(t *testing.T) {
    model := newWorkbenchModelForTest()
    next := model.Apply(IntentOpenHelp)
    if next.Overlay.Kind != OverlayHelp {
        t.Fatalf("expected help overlay, got %v", next.Overlay.Kind)
    }
}

func TestFloatingPaneMoveRespectsAnchorLimitAndCenterRecall(t *testing.T) {
    model := modelWithFloatingPane()
    moved := model.Apply(IntentMoveFloatingPane("float-1", -999, -999))
    if !moved.Workspace.ActiveTab().FloatingPane("float-1").AnchorVisible() {
        t.Fatal("expected floating anchor to remain visible")
    }
    centered := moved.Apply(IntentCenterFloatingPane("float-1"))
    if !centered.Workspace.ActiveTab().FloatingPane("float-1").IsCentered() {
        t.Fatal("expected floating pane to return to centered position")
    }
}
```

- [ ] **Step 2：运行 app/overlay 测试，确认交互 reducer 尚未实现**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/app ./tui/render/overlay ./tui/state/pool ./tui/runtime -run 'TestSplitCreatesPaneSlotAndOpensConnectDialog|TestCancelConnectLeavesUnconnectedPane|TestConnectExistingPickerUsesGlobalScopeAndRecentUserInteractionSort|TestNewTabAndNewFloatOpenTheSameConnectDialog|TestCreateNewTerminalBranchCreatesAndBindsTerminal|TestClosePaneDoesNotKillTerminalByDefault|TestDisconnectPaneKeepsPaneAndClearsBinding|TestReconnectPaneRebindsToSelectedTerminal|TestClosePaneAndKillTerminalStopsTerminal|TestRuntimeExecutesCreateAndKillTerminalActions|TestRemoveTerminalTurnsAttachedPaneIntoUnconnectedPane|TestRestartExitedTerminalKeepsOriginalTerminalIdentity|TestRemoteRemoveNoticeOnlyAppearsForVisibleAffectedPane|TestHelpOverlayOpensAsGroupedHelpScreen|TestFloatingPaneMoveRespectsAnchorLimitAndCenterRecall' -count=1
```

Expected: FAIL because create/connect flow is not implemented.

- [ ] **Step 3：实现统一 overlay 与 pane 生命周期动作**

Implement:

- split/new tab/new float all go through “create pane slot -> open connect dialog”
- cancel leaves `unconnected pane`
- explicit actions for close pane, disconnect pane, reconnect pane, kill terminal
- explicit actions for remove terminal, restart terminal, remote remove notice
- overlay focus priority and `Esc` handling
- grouped help overlay under the same overlay system, not a standalone page
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
- runtime executor must lower `create new terminal` and `kill terminal` into concrete `TerminalService` calls; reducer success alone is not enough
- runtime executor must also lower `remove terminal` and `restart terminal` into concrete `TerminalService` calls
- floating pane interactions must cover:
  - drag outside viewport while keeping left-top anchor visible
  - recall and center
  - active floating auto-raise remains model-driven, not ad hoc in renderer

Reference:

- `deprecated/tui-legacy/docs/interaction-spec.md`
- `deprecated/tui-legacy/pkg/model.go`

- [ ] **Step 4：运行聚焦交互测试**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/app ./tui/render/overlay ./tui/state/pool -count=1
```

Expected: PASS.

- [ ] **Step 5：提交**

```bash
git add tui/state/pool/query.go tui/state/pool/query_test.go tui/app/intent.go tui/app/intent_test.go tui/app/model.go tui/app/model_test.go tui/runtime/terminal_service.go tui/runtime/terminal_service_test.go tui/render/overlay/view.go tui/render/overlay/view_test.go tui/render/workbench/view.go
git commit -m "打通TUI弹层与面板连接语义"
```

### Task 6：实现 Terminal Pool 独立页面

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

- [ ] **Step 1：先写 terminal pool 分组、渲染与动作失败测试**

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

func TestTerminalPoolSupportsMetadataTagsSearchAndEdit(t *testing.T) {
    groups := pool.Group(sampleTerminals())
    filtered := pool.Filter(groups.Visible, "backend")
    if len(filtered) == 0 {
        t.Fatal("expected tag search result")
    }
    model := modelWithTerminalPoolSelection()
    next := model.Apply(IntentTerminalPoolEditMetadata("term-2"))
    if next.Overlay.Kind != OverlayMetadataPrompt {
        t.Fatalf("expected metadata prompt, got %v", next.Overlay.Kind)
    }
}

func TestTerminalPoolPreviewIsReadonlyLiveAttach(t *testing.T) {
    model := modelWithTerminalPoolSelection()
    next := model.Apply(IntentTerminalPoolSelect("term-2"))
    if !next.TerminalPool.PreviewReadonly {
        t.Fatal("expected readonly preview")
    }
    if !next.HasPendingPreviewSubscription("term-2") {
        t.Fatal("expected live preview subscription")
    }
}
```

- [ ] **Step 2：运行聚焦测试，确认 terminal pool 页面尚未实现**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/state/pool ./tui/render/pool ./tui/app ./tui/runtime -run 'TestGroupTerminalsIntoVisibleParkedExited|TestTerminalPoolViewShowsThreeColumns|TestTerminalPoolActionsRenameKillAndOpenTargetPane|TestTerminalPoolSelectionSwitchesReadonlyLivePreviewSubscription|TestTerminalPoolActionsReachRuntimeService|TestTerminalPoolSupportsMetadataTagsSearchAndEdit|TestTerminalPoolPreviewIsReadonlyLiveAttach' -count=1
```

Expected: FAIL because grouping/page rendering is missing.

- [ ] **Step 3：实现独立 terminal pool 页面**

Implement:

- `visible / parked / exited` grouping
- default sort by recent user interaction, not pure output
- middle column readonly live preview
- selection change must rebind the middle column to the selected terminal's live preview stream
- terminal pool tests必须锁住：
  - 首次进入时如何确定 preview terminal
  - 切换 selection 时如何切换 preview subscription
  - preview 保持只读，不抢 workbench 输入焦点
- right column metadata first, connections second
- page-level actions biased toward terminal management
- explicit app intents for rename, metadata/tags edit, kill, remove, open-here, open-new-tab, open-floating
- explicit takeover action if the user wants to leave readonly preview and bind/open the selected terminal into the workbench
- runtime action executor must actually call `TerminalService`, not stop at queued UI intents
- app shell must provide reachable navigation into and out of the standalone Terminal Pool page
- metadata/tags 第一阶段必须覆盖三件事：
  - 列表与详情展示
  - 搜索匹配
  - prompt/overlay 编辑并回写到 terminal 本体

- [ ] **Step 4：运行 terminal pool 相关测试**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/state/pool ./tui/render/pool ./tui/app ./tui/runtime -count=1
```

Expected: PASS.

- [ ] **Step 5：提交**

```bash
git add tui/render/pool tui/app/intent.go tui/app/intent_test.go tui/app/model.go tui/app/model_test.go tui/runtime/terminal_service.go tui/runtime/terminal_service_test.go tui/runtime/session_store.go
git commit -m "补齐TUI终端池独立页面"
```

### Task 7：补齐 workspace 持久化、CLI 集成与最终验证

**Files:**
- Create: `tui/runtime/workspace_store.go`
- Create: `tui/runtime/workspace_store_test.go`
- Modify: `tui/runtime.go`
- Modify: `tui/runtime_test.go`
- Modify: `tui/runtime/program.go`
- Modify: `tui/runtime/program_test.go`
- Modify: `tui/runtime/update_loop.go`
- Modify: `tui/runtime/update_loop_test.go`
- Modify: `cmd/termx/main_test.go`
- Modify: `docs/superpowers/specs/2026-03-25-tui-product-definition-design.md`

- [ ] **Step 1：先写 workspace 保存/恢复失败测试**

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

- [ ] **Step 2：运行持久化聚焦测试，确认恢复支持尚不完整**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/... -run 'TestWorkspaceStoreRoundTripsWorkbenchState|TestRunRestoresWorkspaceState' -count=1
```

Expected: FAIL because workspace persistence is not implemented.

- [ ] **Step 3：实现 workspace 持久化并收尾 CLI 接线**

Implement:

- local workspace save/load store
- startup restore path from `Config.WorkspaceStatePath`
- debounced save after workspace-affecting state mutations
- save-on-exit hook so the next launch can restore the last workbench state
- `update_loop` is responsible for identifying workspace-affecting mutations and scheduling debounced save requests
- `program.go` is responsible for exit-time flush and final save before Bubble Tea shutdown returns
- graceful fallback to temp workspace when restore fails
- update `cmd/termx` tests if any config expectations changed
- spec note updates only if implementation forced a product-level clarification

- [ ] **Step 4：运行完整验证**

Run:

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/... -count=1
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./cmd/termx -count=1
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 5：提交**

```bash
git add tui/runtime/workspace_store.go tui/runtime/workspace_store_test.go tui/runtime.go tui/runtime_test.go tui/runtime/program.go tui/runtime/program_test.go tui/runtime/update_loop.go tui/runtime/update_loop_test.go cmd/termx/main_test.go docs/superpowers/specs/2026-03-25-tui-product-definition-design.md
git commit -m "完成TUI第一阶段持久化闭环"
```

## 额外实现说明

- `Terminal Pool` 中栏第一阶段默认只读观察，不抢日常输入焦点
- `close pane` 默认语义必须持续保持“不 kill terminal”
- `disconnect pane` 与 `reconnect pane` 必须作为两个独立动作建模，不要重新折叠成单个模糊命令
- 顶栏/底栏配置化、项目目录快速启动、settings 页面都留到后续计划，不要偷渡进本计划
- `metadata / tags` 第一阶段先作为 terminal 原生信息处理，重点是显示、编辑、搜索，不扩展成规则系统
- `help` 第一阶段需要进入 overlay 体系，至少覆盖 most used、shared terminal、floating、exit/close
- floating pane 需要实现“允许拖出主视口但左上锚点仍留在窗口内”和“呼回并居中”
- `kill terminal` / `remove terminal` / `restart terminal` / 远端 remove notice 的差异语义必须在实现中分开，不要重新合并

## 完成标准

完成本计划后，应满足：

- `termx` 默认进入可工作的 TUI workbench，而不是 reset stub
- workbench 具备基本 `workspace / tab / pane / floating pane` 体验
- split/new tab/new float 走统一 connect dialog 流程
- pane/terminal 生命周期语义符合 spec
- terminal pool 作为独立页面可进入、可观察、可管理
- workspace 状态可保存并在下次启动时恢复基础工作现场
- 全量 `go test ./... -count=1` 通过
