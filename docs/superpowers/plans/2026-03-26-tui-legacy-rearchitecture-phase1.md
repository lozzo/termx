# TUI 旧实现解耦重构第一阶段实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在保持 `tui.Run`、`tui.Client`、`tui.Config` 外部接口稳定的前提下，把 TUI 主线重建为 `core / app / runtime / render / input / features` 架构，并完成第一阶段必须覆盖的 Workbench、pane/terminal 解耦语义、三种 pane 状态、生命周期动作与独立 Terminal Pool 页面。

**Architecture:** 控制流固定为 `input -> intent -> reducer -> effect -> runtime -> message -> reducer`，渲染链路固定为 `state -> projection -> render`。`tui/core` 只放纯状态与纯规则，`tui/app` 只放可测试的应用状态机，`tui/runtime` 统一执行副作用与恢复逻辑，`tui/render` 只消费投影快照，`tui/input` 只做输入翻译，`tui/features` 按产品切片组织 Workbench、Terminal Pool 与 Overlay。

**Tech Stack:** Go、Bubble Tea、仓库内 `protocol.Client` 封装、daemon event 流、snapshot/stream 管线、cell canvas renderer、仓库内 Go 测试体系。

---

## 范围检查

这份计划只覆盖一个连续可交付子系统：TUI 第一阶段重构主线。它不再沿用旧 `state/` 目录结构，也不保留旧 terminal manager 为中心的产品结构。

本阶段必须完成：

- Workbench 默认入口
- pane / terminal 解耦
- `live / exited / unconnected` pane 状态
- `close / disconnect / reconnect / kill / remove`
- 独立 Terminal Pool 页面
- overlay 承载 connect / prompt / help 等局部动作

本阶段明确不做：

- `fit / fixed / pin`
- `auto-acquire resize`
- 复杂 prefix 子模式
- 旧 terminal manager / workspace picker 的中心地位

## 文件结构规划

公开入口与装配：

- Modify: `tui/runtime.go`
- Modify: `tui/runtime_test.go`
- Modify: `tui/client.go`
- Modify: `tui/render_init.go`
- Create: `tui/test_helpers_test.go`

纯状态与纯规则：

- Create: `tui/core/types/types.go`
- Create: `tui/core/types/types_test.go`
- Create: `tui/core/layout/tree.go`
- Create: `tui/core/layout/tree_test.go`
- Create: `tui/core/terminal/state.go`
- Create: `tui/core/terminal/state_test.go`
- Create: `tui/core/workspace/state.go`
- Create: `tui/core/workspace/state_test.go`
- Create: `tui/core/pool/query.go`
- Create: `tui/core/pool/query_test.go`

应用层：

- Create: `tui/app/model.go`
- Create: `tui/app/model_test.go`
- Create: `tui/app/screen.go`
- Create: `tui/app/intent.go`
- Create: `tui/app/message.go`
- Create: `tui/app/effect.go`
- Create: `tui/app/reducer.go`
- Create: `tui/app/reducer_test.go`

运行时：

- Create: `tui/runtime/bootstrap.go`
- Create: `tui/runtime/bootstrap_test.go`
- Create: `tui/runtime/effect_runner.go`
- Create: `tui/runtime/effect_runner_test.go`
- Create: `tui/runtime/session_store.go`
- Create: `tui/runtime/session_store_test.go`
- Create: `tui/runtime/event_adapter.go`
- Create: `tui/runtime/event_adapter_test.go`
- Create: `tui/runtime/workspace_store.go`
- Create: `tui/runtime/workspace_store_test.go`
- Create: `tui/runtime/program.go`
- Create: `tui/runtime/program_test.go`

输入层：

- Create: `tui/input/router.go`
- Create: `tui/input/router_test.go`

渲染层：

- Create: `tui/render/projection/screen.go`
- Create: `tui/render/projection/screen_test.go`
- Create: `tui/render/canvas/canvas.go`
- Create: `tui/render/canvas/canvas_test.go`
- Create: `tui/render/workbench/view.go`
- Create: `tui/render/workbench/view_test.go`
- Create: `tui/render/terminalpool/view.go`
- Create: `tui/render/terminalpool/view_test.go`
- Create: `tui/render/overlay/view.go`
- Create: `tui/render/overlay/view_test.go`

产品切片：

- Create: `tui/features/workbench/state.go`
- Create: `tui/features/workbench/state_test.go`
- Create: `tui/features/terminalpool/state.go`
- Create: `tui/features/terminalpool/state_test.go`
- Create: `tui/features/overlay/state.go`
- Create: `tui/features/overlay/state_test.go`

只读参考资产：

- `deprecated/tui-legacy/pkg/layout.go`
- `deprecated/tui-legacy/pkg/connection_state.go`
- `deprecated/tui-legacy/pkg/render.go`
- `deprecated/tui-legacy/pkg/workspace_state.go`
- `deprecated/tui-legacy/pkg/layout_decl.go`

## 执行约束

- 重点函数、边界复杂处补中文注释，尤其是 pane/terminal 生命周期、effect 边界、渲染投影
- 优先定义接口再写实现，优先在 `runtime`、`render`、`input` 交界处做解耦
- 每个任务先写失败测试，再写最小实现
- 每个任务结束都提交中文 commit
- 每轮最低验证：
  - `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test <modified packages>`
  - `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1`

## 实施批次

### 批次 A：主骨架闭环

目标：

- `Run` 不再依赖旧目录结构
- 能启动到 Workbench
- `app` / `runtime` / `render` / `input` / `features` 基础边界成立

对应任务：

- Task 1
- Task 2 的最小状态骨架

### 批次 B：Workbench 生命周期闭环

目标：

- 单 pane live shell 启动
- `unconnected / live / exited` 渲染与状态切换成立
- `close / disconnect / reconnect / kill / remove` 语义不混淆

对应任务：

- Task 3
- Task 4
- Task 5 的 Workbench 部分

### 批次 C：Terminal Pool 闭环

目标：

- 独立 Terminal Pool 页面可进入、可观察、可执行核心动作
- Workbench 与 Pool 对同一 terminal 的反馈一致

对应任务：

- Task 6

### 批次 D：恢复与收口

目标：

- workspace 保存与恢复接回
- 全量测试通过
- CLI 入口维持稳定

对应任务：

- Task 7

### Task 1: 重建公开入口与根应用壳层

**Files:**
- Modify: `tui/runtime.go`
- Modify: `tui/runtime_test.go`
- Modify: `tui/client.go`
- Modify: `tui/render_init.go`
- Create: `tui/app/model.go`
- Create: `tui/app/model_test.go`
- Create: `tui/app/screen.go`
- Create: `tui/app/intent.go`
- Create: `tui/app/message.go`
- Create: `tui/app/effect.go`
- Create: `tui/app/reducer.go`
- Create: `tui/app/reducer_test.go`
- Create: `tui/runtime/program.go`
- Create: `tui/runtime/program_test.go`

- [ ] **Step 1: 写根入口失败测试**

```go
func TestRunStartsWithWorkbenchScreen(t *testing.T) {
	runner := &captureProgramRunner{}
	restore := swapProgramRunnerForTest(runner)
	t.Cleanup(restore)

	if err := Run(nil, Config{Workspace: "main"}, nil, io.Discard); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	root := capturedAppModelForRunTest(t, runner.model)
	if root.Screen != app.ScreenWorkbench {
		t.Fatalf("expected workbench screen, got %q", root.Screen)
	}
}

func TestReducerCanSwitchBetweenWorkbenchAndTerminalPool(t *testing.T) {
	model := app.NewModel("main")
	model, _ = app.Reduce(model, app.IntentOpenTerminalPool)
	if model.Screen != app.ScreenTerminalPool {
		t.Fatalf("expected terminal pool screen, got %q", model.Screen)
	}
}
```

- [ ] **Step 2: 运行入口聚焦测试，确认当前骨架缺失**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui ./tui/app ./tui/runtime -run 'TestRunStartsWithWorkbenchScreen|TestReducerCanSwitchBetweenWorkbenchAndTerminalPool' -count=1`
Expected: FAIL，因为 `tui/` 新骨架与 reducer 尚不存在。

- [ ] **Step 3: 实现最小入口装配**

实现内容：

- `tui.Client` 保持外部方法集合稳定，并在 `runtime` 内部通过更细粒度接口消费
- `app.Model` 只保存 screen、feature state、overlay state 与 notice
- `app.Reduce` 只做纯状态变更并产出 effect 描述
- `runtime.ProgramRunner` 负责运行 Bubble Tea，不持有业务状态
- 重点函数补中文注释，说明为什么 `Run` 只做依赖装配而不直接承载业务逻辑

- [ ] **Step 4: 运行包测试确认入口骨架通过**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui ./tui/app ./tui/runtime -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add tui/runtime.go tui/runtime_test.go tui/client.go tui/render_init.go tui/app tui/runtime/program.go tui/runtime/program_test.go
git commit -m "重建TUI公开入口与根应用壳层"
```

### Task 2: 建立 core 纯状态骨架与 pane/terminal 解耦模型

**Files:**
- Create: `tui/core/types/types.go`
- Create: `tui/core/types/types_test.go`
- Create: `tui/core/layout/tree.go`
- Create: `tui/core/layout/tree_test.go`
- Create: `tui/core/terminal/state.go`
- Create: `tui/core/terminal/state_test.go`
- Create: `tui/core/workspace/state.go`
- Create: `tui/core/workspace/state_test.go`
- Create: `tui/core/pool/query.go`
- Create: `tui/core/pool/query_test.go`
- Modify: `tui/app/model.go`
- Modify: `tui/app/reducer_test.go`

- [ ] **Step 1: 写纯状态失败测试**

```go
func TestWorkspaceCreatesUnconnectedPaneByDefault(t *testing.T) {
	ws := workspace.New("main")
	pane := ws.ActiveTab().ActivePane()
	if pane.SlotState != types.PaneSlotUnconnected {
		t.Fatalf("expected unconnected pane, got %q", pane.SlotState)
	}
}

func TestLayoutSplitAndProjectRects(t *testing.T) {
	root := layout.NewLeaf(types.PaneID("pane-1"))
	root, ok := root.Split(types.PaneID("pane-1"), types.SplitVertical, types.PaneID("pane-2"))
	if !ok {
		t.Fatal("expected split success")
	}
	if got := root.Project(types.Rect{W: 120, H: 40}); len(got) != 2 {
		t.Fatalf("expected 2 rects, got %d", len(got))
	}
}
```

- [ ] **Step 2: 运行 core 包测试，确认纯状态包还不存在**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/core/... -run 'TestWorkspaceCreatesUnconnectedPaneByDefault|TestLayoutSplitAndProjectRects' -count=1`
Expected: FAIL，因为 `core` 包尚未建立。

- [ ] **Step 3: 实现纯状态模型**

实现内容：

- `types`：ID、矩形、分屏方向、pane slot state、连接角色
- `layout`：纯 layout tree、切分、删除、矩形投影
- `workspace`：workspace/tab/pane/floating 基础状态
- `terminal`：terminal metadata、runtime-independent state、连接摘要
- `pool`：Pool 分组、排序、搜索查询纯逻辑

- [ ] **Step 4: 运行 core 包测试**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/core/... -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add tui/core tui/app/model.go tui/app/reducer_test.go
git commit -m "建立TUI核心状态模型"
```

### Task 3: 建立 app reducer/effect 并打通 Workbench 生命周期语义

**Files:**
- Modify: `tui/app/model.go`
- Modify: `tui/app/intent.go`
- Modify: `tui/app/message.go`
- Modify: `tui/app/effect.go`
- Modify: `tui/app/reducer.go`
- Modify: `tui/app/reducer_test.go`
- Create: `tui/features/workbench/state.go`
- Create: `tui/features/workbench/state_test.go`
- Create: `tui/features/overlay/state.go`
- Create: `tui/features/overlay/state_test.go`

- [ ] **Step 1: 写 Workbench 生命周期失败测试**

```go
func TestReducerDistinguishesDisconnectKillAndRemove(t *testing.T) {
	model := sampleLiveWorkbenchModel()

	model, _ = app.Reduce(model, app.MessageTerminalDisconnected{PaneID: "pane-1"})
	if got := model.Workbench.Panes["pane-1"].SlotState; got != types.PaneSlotUnconnected {
		t.Fatalf("expected unconnected after disconnect, got %q", got)
	}

	model = sampleLiveWorkbenchModel()
	model, _ = app.Reduce(model, app.MessageTerminalExited{TerminalID: "term-1"})
	if got := model.Workbench.Panes["pane-1"].SlotState; got != types.PaneSlotExited {
		t.Fatalf("expected exited after kill, got %q", got)
	}
}

func TestReducerOpensConnectOverlayForUnconnectedPane(t *testing.T) {
	model := sampleUnconnectedWorkbenchModel()
	model, _ = app.Reduce(model, app.IntentOpenConnectOverlay)
	if model.Overlay.Active.Kind != overlay.KindConnectPicker {
		t.Fatalf("expected connect overlay, got %q", model.Overlay.Active.Kind)
	}
}
```

- [ ] **Step 2: 运行 app 聚焦测试，确认生命周期语义尚未落地**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/app ./tui/features/... -run 'TestReducerDistinguishesDisconnectKillAndRemove|TestReducerOpensConnectOverlayForUnconnectedPane' -count=1`
Expected: FAIL，因为 Workbench/Overlay feature state 尚未接线。

- [ ] **Step 3: 实现 reducer 与 feature state**

实现内容：

- `Intent` 只描述用户意图，不直接携带 client 调用
- `Effect` 覆盖 create/connect/disconnect/reconnect/kill/remove/list/restore
- `Message` 表达 runtime 回流结果与 daemon 事件
- `features/workbench` 保存 pane 展示状态与焦点
- `features/overlay` 保存 connect/help/prompt 的局部 UI 状态

- [ ] **Step 4: 运行 app 与 feature 测试**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/app ./tui/features/... -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add tui/app tui/features/workbench tui/features/overlay
git commit -m "打通TUI应用状态机与工作台生命周期"
```

### Task 4: 实现 runtime bootstrap、effect runner 与 session store

**Files:**
- Create: `tui/runtime/bootstrap.go`
- Create: `tui/runtime/bootstrap_test.go`
- Create: `tui/runtime/effect_runner.go`
- Create: `tui/runtime/effect_runner_test.go`
- Create: `tui/runtime/session_store.go`
- Create: `tui/runtime/session_store_test.go`
- Create: `tui/runtime/event_adapter.go`
- Create: `tui/runtime/event_adapter_test.go`
- Modify: `tui/runtime.go`

- [ ] **Step 1: 写 runtime 失败测试**

```go
func TestBootstrapCreatesLiveShellPane(t *testing.T) {
	client := &stubClient{}
	model, err := runtime.Bootstrap(context.Background(), client, runtime.BootstrapConfig{
		Workspace: "main",
		DefaultShell: "/bin/sh",
	})
	if err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}
	if pane := model.Workbench.ActivePane(); pane.SlotState != types.PaneSlotLive {
		t.Fatalf("expected live pane, got %q", pane.SlotState)
	}
}

func TestEffectRunnerMapsRemoveToUnconnectedPaneMessage(t *testing.T) {
	runner := runtime.NewEffectRunner(&stubClient{})
	msg := runner.Run(context.Background(), app.EffectRemoveTerminal{TerminalID: "term-1"})
	if _, ok := msg.(app.MessageTerminalRemoved); !ok {
		t.Fatalf("expected MessageTerminalRemoved, got %T", msg)
	}
}
```

- [ ] **Step 2: 运行 runtime 聚焦测试，确认 effect runner 尚未实现**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/runtime -run 'TestBootstrapCreatesLiveShellPane|TestEffectRunnerMapsRemoveToUnconnectedPaneMessage' -count=1`
Expected: FAIL，因为 bootstrap/session/effect runner 尚未建立。

- [ ] **Step 3: 实现 runtime 适配层**

实现内容：

- `Bootstrap`：默认 shell 启动、attach 模式、初始 workspace 装配
- `EffectRunner`：统一消费 effect 并调用 `tui.Client`
- `SessionStore`：attach/snapshot/stream 聚合与只读预览状态
- `EventAdapter`：daemon event 规范化为 app message
- 中文注释说明 reducer/runtime 边界，避免副作用回流再次潜入 `app`

- [ ] **Step 4: 运行 runtime 包测试**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/runtime -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add tui/runtime.go tui/runtime
git commit -m "接通TUI运行时副作用与会话存储"
```

### Task 5: 建立投影层与 Workbench 渲染主路径

**Files:**
- Create: `tui/render/projection/screen.go`
- Create: `tui/render/projection/screen_test.go`
- Create: `tui/render/canvas/canvas.go`
- Create: `tui/render/canvas/canvas_test.go`
- Create: `tui/render/workbench/view.go`
- Create: `tui/render/workbench/view_test.go`
- Create: `tui/render/overlay/view.go`
- Create: `tui/render/overlay/view_test.go`
- Modify: `tui/render_init.go`
- Modify: `tui/runtime/program.go`

- [ ] **Step 1: 写 Workbench 渲染失败测试**

```go
func TestWorkbenchViewShowsLiveExitedAndUnconnectedPaneState(t *testing.T) {
	view := workbench.Render(sampleProjectionWithThreePaneStates(), 120, 40)
	for _, want := range []string{"live", "exited", "unconnected"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q", want)
		}
	}
}

func TestUnconnectedPaneShowsActions(t *testing.T) {
	view := workbench.Render(sampleUnconnectedProjection(), 80, 24)
	for _, want := range []string{"connect existing", "create terminal", "open pool"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q", want)
		}
	}
}
```

- [ ] **Step 2: 运行渲染聚焦测试，确认投影层尚未存在**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/... -run 'TestWorkbenchViewShowsLiveExitedAndUnconnectedPaneState|TestUnconnectedPaneShowsActions' -count=1`
Expected: FAIL，因为 projection/canvas/workbench view 尚未建立。

- [ ] **Step 3: 实现渲染主路径**

实现内容：

- `projection` 把 app state 与 session snapshot 合成为屏幕快照
- `canvas` 负责 cell composition，不直接感知 runtime
- `workbench view` 渲染顶栏、pane 标题、正文与空态动作
- `overlay view` 渲染 connect/help/prompt 覆盖层

- [ ] **Step 4: 运行渲染包测试**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/render/... -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add tui/render_init.go tui/runtime/program.go tui/render
git commit -m "建立TUI工作台投影与渲染主路径"
```

### Task 6: 建立输入翻译层与 Terminal Pool 页面

**Files:**
- Create: `tui/input/router.go`
- Create: `tui/input/router_test.go`
- Create: `tui/features/terminalpool/state.go`
- Create: `tui/features/terminalpool/state_test.go`
- Create: `tui/render/terminalpool/view.go`
- Create: `tui/render/terminalpool/view_test.go`
- Modify: `tui/app/intent.go`
- Modify: `tui/app/reducer.go`
- Modify: `tui/runtime/effect_runner.go`

- [ ] **Step 1: 写输入与 Pool 页面失败测试**

```go
func TestInputRouterMapsWorkbenchAndPoolKeysToIntents(t *testing.T) {
	router := input.NewRouter()
	if got := router.Translate(sampleWorkbenchContext(), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}); got != app.IntentOpenTerminalPool {
		t.Fatalf("expected open pool intent, got %#v", got)
	}
}

func TestTerminalPoolViewShowsVisibleParkedExitedGroups(t *testing.T) {
	view := terminalpool.Render(samplePoolProjection(), 120, 40)
	for _, want := range []string{"visible", "parked", "exited"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q", want)
		}
	}
}
```

- [ ] **Step 2: 运行输入与 Pool 聚焦测试，确认页面切片尚未实现**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/input ./tui/features/terminalpool ./tui/render/terminalpool -run 'TestInputRouterMapsWorkbenchAndPoolKeysToIntents|TestTerminalPoolViewShowsVisibleParkedExitedGroups' -count=1`
Expected: FAIL，因为 input router、Pool feature 与 Pool renderer 尚未建立。

- [ ] **Step 3: 实现输入层与 Pool 页面**

实现内容：

- `input.Router` 只把键鼠事件翻译为 intent，不直接操作 runtime
- `features/terminalpool` 保存 query、selection、preview focus、编辑态
- `terminalpool view` 画三栏布局、分组列表、预览和 metadata/connection 面板
- 接通 open here / new tab / floating / kill / remove 对应 intent/effect

- [ ] **Step 4: 运行输入与 Pool 相关测试**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui/input ./tui/features/terminalpool ./tui/render/terminalpool ./tui/app -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add tui/input tui/features/terminalpool tui/render/terminalpool tui/app/intent.go tui/app/reducer.go tui/runtime/effect_runner.go
git commit -m "补齐TUI输入翻译与终端池页面"
```

### Task 7: 接回 workspace 持久化、恢复与全量验证

**Files:**
- Create: `tui/runtime/workspace_store.go`
- Create: `tui/runtime/workspace_store_test.go`
- Modify: `tui/runtime.go`
- Modify: `tui/runtime_test.go`
- Modify: `tui/runtime/bootstrap.go`
- Modify: `tui/runtime/program.go`
- Modify: `tui/app/message.go`
- Modify: `tui/app/reducer.go`

- [ ] **Step 1: 写恢复与持久化失败测试**

```go
func TestRunRestoresWorkspaceStateAndRebindsSessions(t *testing.T) {
	runner := &captureProgramRunner{}
	restore := swapProgramRunnerForTest(runner)
	t.Cleanup(restore)

	err := Run(&stubClient{}, Config{
		Workspace: "main",
		WorkspaceStatePath: filepath.Join(t.TempDir(), "workspace.json"),
	}, nil, io.Discard)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}
```

- [ ] **Step 2: 运行恢复相关测试，确认持久化尚未接回**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui ./tui/runtime -run 'TestRunRestoresWorkspaceStateAndRebindsSessions' -count=1`
Expected: FAIL，因为新架构下的 workspace store 与恢复逻辑尚未完整接线。

- [ ] **Step 3: 实现持久化与收口**

实现内容：

- `WorkspaceStore` 只保存可持久化状态，不混入 runtime session
- `Run` 启动时先 restore，再按需 rebind session
- attach 模式跳过 workspace persistence
- 完整接通 `go test ./... -count=1` 所需的入口测试辅助

- [ ] **Step 4: 运行定向测试与全量测试**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui ./tui/runtime -count=1`
Expected: PASS。

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add tui/runtime.go tui/runtime_test.go tui/runtime/bootstrap.go tui/runtime/program.go tui/runtime/workspace_store.go tui/runtime/workspace_store_test.go tui/app/message.go tui/app/reducer.go
git commit -m "接回TUI工作区恢复并完成一期收口"
```
