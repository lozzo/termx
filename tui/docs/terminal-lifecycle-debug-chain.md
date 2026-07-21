# Terminal lifecycle / restart debug chain

本文只梳理 tui-v3 中 panel 连接 terminal、terminal 退出、restart、退出 TUI 后重进仍显示 restart 的相关链路。它不是结论报告；目的是方便从代码上逐段查断点。

## 状态边界

### core terminal lifecycle

归属：core-v2 / protocol service。

含义：

- terminal 是否 running / exited。
- exit code、exited at、command。
- terminal process identity。
- live surface、cursor、terminal modes。
- authoritative history。

TUI 里不能自己发明 terminal lifecycle。restart 后是否还应该显示 restart，最终应该以 core 返回的 terminal 属性 / lifecycle-known live surface 为准。

### workbench storage

归属：core opaque storage 托管，TUI 自己解释 schema。

文件：

- `tui/state/workbench_storage.go`
- `tui/app/workbench_storage.go`

只应该保存：

- workspace / tab / pane / floating 布局。
- active pane / active floating。
- pane/floating 到 terminal 的连接意图。
- TerminalView 连接意图。

不应该保存：

- terminal running / exited。
- exit code、exited at、command。
- runtime channel。
- live cursor。
- copy selection。
- 当前输入路由状态。

旧 storage 中可能已经写入过 `"exited"` / `"copy-history"` pane kind。当前 restore 只在 snapshot 已有完整 `TerminalViews` 连接意图时 scrub 这些展示态为 `terminal-live`；没有 `TerminalViews`，或要恢复的 attachment identity 缺少完整 `EndpointID` / `TerminalID` / `OperationID`，必须 fail closed。workbench storage 本身只持久化 `EndpointID` + `TerminalID` 连接意图；新的 `OperationID` 只能由当前 reducer attach 时分配，不能从旧 snapshot 补造。

关键函数：

- `SnapshotRootWorkbenchForStorage`
- `WorkbenchStorageSnapshot.Validate`
- `WorkbenchStorageSnapshot.ToShellStore`
- `WorkbenchStorageSnapshot.ToTerminalViewStore`
- `workbenchStoragePane`
- `WorkbenchStorageSnapshot.validTerminalViews`
- `WorkbenchStorageSnapshot.validTerminalPaneBindings`
- `TerminalViewBinding.ForWorkbenchStorage`
- `TerminalViewBinding.ForWorkbenchRestore`

### TUI memory

归属：当前 TUI 进程内的 reducer-owned state。

关键 store：

- `ShellStore`：当前 workspace、pane/floating、active id、overlay、CTA selection。
- `TerminalViewStore`：当前 pane/floating 到 terminal attachment 的 view binding。
- `TerminalPoolStore`：core terminal list 的展示缓存和当前 action 查询结果。
- `TerminalSurfaceStore`：live surface 画面和 live event 投影缓存。
- `TerminalSessionStore`：当前前台 terminal 的 lifecycle、geometry 与 error 投影缓存；不保存 attachment channel。
- `CopyModeStore` / `HistoryStore`：copy/history 交互态和 authoritative history window。

TUI memory 不持有 terminal lifecycle truth；需要决定 restart / running / exited 时必须重新查询 core 或消费 core live surface/event 回投。

## 启动 / restore 链路

入口：

- `NewInteractiveRuntimeWithWorkbench`
- `WorkbenchStorageLoadRequestMsg`
- `reduceWorkbenchStorageLoadResult`

关键文件：

- `tui/app/workbench_storage.go`
- `tui/state/workbench_storage.go`

流程：

1. runtime 启动后从 workbench storage 读取 snapshot。
2. `ToShellStore` 还原 ShellStore。
3. `ToTerminalViewStore` 还原 TerminalViewStore。
4. restore 后清掉旧 copy/history window：
   - `root.History = root.History.InvalidateWindow()`
   - `root.CopyMode = state.CopyModeStore{}`
5. 如果有 TerminalView binding，发出：
   - `TerminalPoolListRequestMsg`
   - 每个 restored binding 对应的 `LiveAttachMsg`

应该成立的不变量：

- restore 后 pane kind 只能是 `empty` 或 `terminal-live`。
- 退出态不应该来自 pane kind。
- restore 成功必须来自 schema v2 的 `TerminalViews`：每个 terminal pane/floating 都要有匹配的 `EndpointID` + `TerminalID` binding；裸 `pane.TerminalID` 不能恢复成 default local binding。
- 旧 snapshot 缺少 `TerminalViews`、缺 endpoint-aware terminal ref，或试图把缺少 `OperationID` 的旧 runtime identity 当成已恢复 attachment 时，都不能补 binding；必须 fail closed，只有有效 `TerminalViews` 通过后才能由 reducer 发起新的 attach operation。
- 当前进程内输入和 render 应该使用 TerminalView binding，不应该使用 global session fallback。

重点日志：

- `workbench.restore`
  - `active_pane`
  - `active_floating`
  - `active_pane_kind`
  - `active_terminal`
  - `terminal_views`

## terminal list / lifecycle 查询链路

入口：

- `TerminalPoolListRequestMsg`
- `reduceTerminalPoolListResult`

关键文件：

- `tui/app/terminal_pool.go`

流程：

1. TUI 调用 terminal service list。
2. list result 写入 `TerminalPoolStore`，只服务 picker / pool 展示和当前 action 的一次性决策。
3. list result 不允许把 running / exited 写入 `root.Surface` 或 `root.Session`；pane 的 live 状态只能来自 live surface/event 或 restart/attach result。

应该成立的不变量：

- 如果 core list 返回 `term-main:running`，restart action 必须跳过，不能因为旧 surface/session 显示 exited 就继续重启。
- pool/list 是 core 查询响应和列表展示缓存，不是 pane live 状态、不作为 input routing truth，也不是 history truth。
- 如果 list 返回 running 但 UI 仍触发 restart，下一步要看 action 是否绕过了 `TerminalPoolRestartIfExitedRequestMsg`。

重点日志：

- `terminal.pool.list`
  - `items`
  - `active_terminal`
  - `surface_terminal`
  - `surface_state`
  - `session_terminal`
  - `session_state`

## attach / surface 链路

入口：

- `LiveAttachMsg`
- `TerminalAttachCompletedMsg`
- `LiveSurfaceMsg`
- `LiveEventMsg`

关键文件：

- `tui/app/live.go`
- `tui/adapter/protocol/`
- `tui/state/live.go`

流程：

1. restore 或 restart 之后会为 view binding 发出 `LiveAttachMsg`。
2. reducer 只接受仍匹配 current `TerminalAttachOperation` 的 completion，原子提交 committed view binding 后再精确 detach previous attachment；`TerminalSessionStore` 只更新前台 lifecycle/geometry 投影。
3. surface request/event 回来后变成 `LiveSurfaceMsg` 或 `LiveEventMsg`。
4. `LiveSurfaceMsg` / `LiveEventMsg` 携带一次性的 `LifecycleKnown=true` 时，才表示本次消息来自 core lifecycle 查询或事件。
5. `TerminalSurfaceStore.ApplySnapshotWithLifecycle` 投影 live surface；如果消息是 `LifecycleKnown=true` 且 `State=attached`，`root.Session.MarkAttached` 应该清掉旧 exited session metadata。

应该成立的不变量：

- running lifecycle-known 消息是 core lifecycle 权威信号，不能落到 TUI store 里当 terminal 状态缓存。
- ordinary live frame 不能覆盖或吞掉 lifecycle-known running / exited 消息。
- live cursor 只来自 core/protocol surface cursor，不从文本尾部合成。

重点日志：

- `live.surface`
  - `terminal_id`
  - `snapshot_state`
  - `lifecycle_known`
  - `surface_state`
  - `session_state`
  - `active_terminal`
- `live.event`
  - 同上。

## runtime live queue 链路

入口：

- `AppRuntime.Post`
- live update queue coalesce

关键文件：

- `tui/app/runtime.go`

关键函数：

- `queuedOrdinaryLiveUpdate`
- `ordinaryLiveSnapshot`
- `liveQueueBoundary`

背景：

runtime 会合并普通 live 帧，避免高频输出把队列撑爆。这个合并只应该用于 ordinary live frame。

当前语义：

- `LifecycleKnown=true` 的 `LiveSurfaceMsg` / `LiveEventMsg` 不是 ordinary frame。
- 带 error / exit metadata 的 snapshot 不是 ordinary frame。
- lifecycle-known frame 必须作为 queue boundary 保留。

如果这里错了，典型现象是：

1. core 已经告诉 TUI terminal running。
2. lifecycle-known running frame 被后续普通 live frame 合并掉。
3. 旧 exited cache 没有被清理。
4. render 继续显示 restart CTA。

## render 链路

入口：

- `RenderVMBuilder.Build`
- `ShellProjector.buildActiveContentVM`
- `ContentProjectorRegistry.Project`

关键文件：

- `tui/render/vm.go`
- `tui/render/content_projector_registry.go`
- `tui/render/content_viewport.go`

关键函数：

- `terminalContentStoresForPane`
- `surfaceForBinding`
- `sessionForBinding`
- `contentKindForPane`
- `buildLiveContentVMWithSelection`
- `liveExitedContent`
- `liveExitedContentLines`

流程：

1. pane/floating 找到对应 TerminalView binding。
2. 通过 binding.TerminalID 找 surface/session。
3. pane kind 只决定 empty / terminal-live / placeholder。
4. 如果 surface/session 是 exited，`buildLiveContentVMWithSelection` 追加 exited marker 和 restart / picker action。

应该成立的不变量：

- render 不应该因为 pane kind 显示 exited。
- restart CTA 只应该来自 `surface.State == TerminalLiveExited` 或 `session.State == TerminalLiveExited`。
- 如果 `surface.State=attached` 且 `session.State!=exited`，不应该显示 restart CTA，即使 live lines 里有旧 `terminal exited: ...` 文本。

## exited CTA 输入链路

键盘入口：

- `NewUIInputReducer`
- `reduceExitedPaneCTAInput`
- `activeExitedPaneCTATarget`
- `paneHasExitedTerminal`

鼠标入口：

- hit region -> `ShellShortcutActionMsg` canonical invocation
- `reduceCanonicalSurfaceAction`（仅消费 pane/floating/row 命中上下文，不解析 render projection）

关键文件：

- `tui/app/ui_input.go`
- `tui/app/shell.go`
- `tui/render/vm.go`
- `tui/render/product_content.go`

流程：

1. render 给 exited actions 生成 hit regions：
   - `exited.restart`
   - `exited.reconnect`
2. 键盘上下/Enter 只在当前 active TerminalView 对应 live surface 已显示 exited CTA 时处理。
3. restart action 先变成 `TerminalPoolRestartIfExitedRequestMsg`，重新查询 core 当前 terminal state。

应该成立的不变量：

- `paneHasExitedTerminal` 只决定当前是否有 exited CTA 输入态；真正 restart 之前必须重新查询 core。
- 不能从 storage、pane kind、global session 判断是否处于 exited CTA。

## restart 链路

入口：

- `TerminalPoolRestartRequestMsg`
- `reduceTerminalPoolRestartRequest`
- `TerminalPoolRestartResultMsg`
- `reduceTerminalPoolRestartResult`

关键文件：

- `tui/app/terminal_pool.go`
- `tui/state/live.go`
- `tui/state/terminal_view.go`

流程：

1. 用户触发 restart current terminal。
2. TUI 先发 `TerminalPoolRestartIfExitedRequestMsg` 查询 core list。
3. 只有 core list 当前仍显示该 terminal exited，才调 terminal service `Restart(ctx, TerminalRestartRequest{TerminalID})`。
4. restart result 成功后：
   - `root.TerminalPool.ApplyRestarted`
   - `root.Surface.RestartPreservingContent`
   - `root.Session.MarkRestartPendingRef`
   - 保留每个 view 的旧 committed binding，等待 replacement candidate 成功
5. 发起：
   - `TerminalPoolListRequestMsg`
   - `restartTerminalViewEffects`，逐 view reattach。

应该成立的不变量：

- restart 不删除 terminal identity。
- restart 不清 authoritative history。
- restart 不清 live tail。
- restart 会为每个 view 发起新的 `TerminalAttachOperation`；candidate 成功前旧 committed attachment 不被破坏，commit 后才按旧 binding 精确 detach。
- restart 后 core list / lifecycle-known surface 必须能把 TUI exited cache 清成 running。

重点日志：

- `terminal.restart.result`
  - `terminal_id`
  - `surface_state`
  - `session_state`
  - `active_terminal`

## 如果重进仍显示 restart，按这个顺序查

### 1. storage restore 后 panel 是否还是 exited kind

看日志：

- `workbench.restore active_pane_kind`

期望：

- `terminal-live`

如果不是：

- 查 `ToShellStore`
- 查 `workbenchStoragePane`
- 查旧 snapshot 是否绕过了 `ToShellStore`

### 2. restore 是否因为 TerminalViews 不完整而 fail closed

看日志：

- `workbench.storage` warning toast
- `workbench.restore terminal_views`
- `workbench.restore active_terminal`

期望：

- 无效 snapshot 不产生 `workbench.restore`，并报告 `invalid workbench snapshot`。
- `terminal_views > 0`
- `active_terminal = 当前 panel 连接的 terminal id`

如果没有：

- 查 `ToTerminalViewStore`
- 查 `Validate` / `validTerminalViews` / `validTerminalPaneBindings`
- 查 snapshot 是否只有 `pane.TerminalID` 而没有匹配 `TerminalViews`
- 查 binding 是否缺少 `EndpointID` / `TerminalID`
- 查当前 runtime 已 live 的同一 view 是否缺少 `OperationID`，不能把它当成同一 committed attachment 复用
- 查 active pane/floating id 是否和 snapshot 中 pane id 对不上

### 3. core list 是否返回 running

看日志：

- `terminal.pool.list items`

期望：

- 对应 terminal 是 `running`

如果 core list 还是 exited：

- 问题在 core restart 状态或 protocol adapter，不在 render。

### 4. running list 是否清了 surface/session

看日志：

- `terminal.pool.list surface_state`
- `terminal.pool.list session_state`

期望：

- `terminal.restart_if_exited.result state=running` 时不会出现后续 restart request。

如果还是 exited：

- 查 restart action 是否直接发了 `TerminalPoolRestartRequestMsg`
- 查 live surface/event 是否还没有返回 core 当前 running snapshot

### 5. lifecycle-known running surface 是否到达

看日志：

- `live.surface lifecycle_known=true snapshot_state=attached`
- 或 `live.event lifecycle_known=true snapshot_state=attached`

期望：

- 这个消息必须出现并且不能被 queue coalesce 丢掉。

如果没有出现：

- 查 protocol adapter 是否给 `TerminalSurfaceResult` / `TerminalLiveEvent` 填了 `LifecycleKnown=true`。
- 查 terminal service `Surface` / event stream。

如果出现过但最终 UI 仍 restart：

- 查 `ordinaryLiveSnapshot`
- 查 `liveQueueBoundary`
- 查后续普通帧是否覆盖了 surface/session state。

### 6. render 是否仍看到 exited state

关键函数：

- `terminalContentStoresForPane`
- `surfaceForBinding`
- `sessionForBinding`
- `buildLiveContentVMWithSelection`

期望：

- active pane binding 的 terminal id 对应 surface/session 是 attached。

如果 render 还是 restart：

- 打印 active pane/floating。
- 打印 TerminalView binding。
- 打印该 terminal 的 surface/session。
- 确认没有拿 sibling view 或 global session。

## 当前相关回归测试

重点测试：

- `TestInteractiveRuntimeWorkbenchRestoreLegacyExitedPaneUsesCoreRunningLifecycle`
- `TestInteractiveRuntimeWorkbenchRestoreTerminalPaneWithoutBindingFailsClosed`
- `TestLiveSurfaceAuthoritativeRunningClearsExitedSessionAndSurface`
- `TestLiveQueueKeepsAuthoritativeRunningLifecycleWhenOrdinaryFrameFollows`

建议现场复现时打开：

```bash
TERMX_TUI_INPUT_TRACE=1 go run ./cmd/muxvia
```

重点 grep：

```bash
workbench.restore
terminal.pool.list
terminal.restart.result
live.surface
live.event
```

## 最小状态判断

重进后是否显示 restart 的最小判断不应该超过这条链：

```text
active pane/floating
  -> TerminalView binding
  -> binding.TerminalID
  -> core lifecycle projection in TerminalSurface/Session
  -> render live/exited content
```

任何从下面来源判断 restart 都是错的：

- workbench storage pane kind。
- old snapshot runtime channel。
- global session fallback。
- sibling panel binding。
- live lines 中存在 `terminal exited: ...` 文本。
