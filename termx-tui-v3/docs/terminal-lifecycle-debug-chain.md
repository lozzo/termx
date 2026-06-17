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

- `termx-tui-v3/state/workbench_storage.go`
- `termx-tui-v3/app/workbench_storage.go`

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

旧 storage 中可能已经写入过 `"exited"` / `"copy-history"` pane kind。当前兼容策略是在 restore 边界迁移成 `terminal-live` 连接意图。

关键函数：

- `SnapshotRootWorkbenchForStorage`
- `WorkbenchStorageSnapshot.ToShellStore`
- `WorkbenchStorageSnapshot.ToTerminalViewStore`
- `workbenchStoragePane`
- `TerminalViewStore.withLegacyWorkbenchPaneBindings`

### TUI memory

归属：当前 TUI 进程内的 reducer-owned state。

关键 store：

- `ShellStore`：当前 workspace、pane/floating、active id、overlay、CTA selection。
- `TerminalViewStore`：当前 pane/floating 到 terminal attachment 的 view binding。
- `TerminalPoolStore`：core terminal list 的当前投影缓存。
- `TerminalSurfaceStore`：live surface / lifecycle 投影缓存。
- `TerminalSessionStore`：当前 attach/session/input channel 投影缓存。
- `CopyModeStore` / `HistoryStore`：copy/history 交互态和 authoritative history window。

TUI memory 可以缓存 exited/running 投影，但必须能被 core 的 running/exited 权威信号覆盖。

## 启动 / restore 链路

入口：

- `NewInteractiveRuntimeWithWorkbench`
- `WorkbenchStorageLoadRequestMsg`
- `reduceWorkbenchStorageLoadResult`

关键文件：

- `termx-tui-v3/app/workbench_storage.go`
- `termx-tui-v3/state/workbench_storage.go`

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
- 如果旧 snapshot 只有 `pane.TerminalID` 没有 `TerminalViews`，`withLegacyWorkbenchPaneBindings` 应该补 detached view binding。
- 当前进程内输入和 render 应该使用 TerminalView binding，不应该使用 global session fallback。

重点日志：

- `workbench.restore`
  - `active_pane`
  - `active_floating`
  - `active_pane_kind`
  - `active_terminal`
  - `terminal_views`

## terminal list / lifecycle 投影链路

入口：

- `TerminalPoolListRequestMsg`
- `reduceTerminalPoolListResult`

关键文件：

- `termx-tui-v3/app/terminal_pool.go`

流程：

1. TUI 调用 terminal service list。
2. list result 写入 `TerminalPoolStore`。
3. `projectTerminalPoolLifecycleMetadata` 把 core list 中的 terminal state 投影到 TUI memory：
   - running / attached / pending -> `root.Surface.MarkAttached`
   - exited -> `root.Surface.MarkExitedWithMetadata`

应该成立的不变量：

- 如果 core list 返回 `term-main:running`，旧 exited surface/session 应该被清掉。
- pool/list 是 lifecycle metadata 投影，不是输入路由依据，也不是 history truth。
- 如果 list 返回 running 但 UI 仍显示 restart，下一步要看 `root.Surface` 和 `root.Session` 有没有被覆盖回 exited。

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
- `LiveAttachResultMsg`
- `LiveSurfaceMsg`
- `LiveEventMsg`

关键文件：

- `termx-tui-v3/app/live.go`
- `termx-tui-v3/services/protocol_terminal_adapter.go`
- `termx-tui-v3/state/live.go`

流程：

1. restore 或 restart 之后会为 view binding 发出 `LiveAttachMsg`。
2. attach result 会更新 view binding / session channel。
3. surface request/event 回来后变成 `LiveSurfaceMsg` 或 `LiveEventMsg`。
4. `TerminalSurfaceStore.ApplySnapshot` 投影 live surface。
5. 如果 snapshot 是 `LifecycleKnown=true` 且 `State=attached`，`root.Session.MarkAttached` 应该清掉旧 exited session metadata。

应该成立的不变量：

- running lifecycle-known snapshot 是 core lifecycle 权威信号。
- ordinary live frame 不能覆盖或吞掉 lifecycle-known running / exited 状态。
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

- `termx-tui-v3/app/runtime.go`

关键函数：

- `queuedOrdinaryLiveUpdate`
- `ordinaryLiveSnapshot`
- `liveQueueBoundary`

背景：

runtime 会合并普通 live 帧，避免高频输出把队列撑爆。这个合并只应该用于 ordinary live frame。

当前语义：

- `LifecycleKnown=true` 的 snapshot 不是 ordinary frame。
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

- `termx-tui-v3/render/vm.go`
- `termx-tui-v3/render/content_projector_registry.go`
- `termx-tui-v3/render/content_viewport.go`

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

- hit region -> `ShellContentActionMsg`
- `reduceShellContentAction`

关键文件：

- `termx-tui-v3/app/ui_input.go`
- `termx-tui-v3/app/shell.go`
- `termx-tui-v3/render/vm.go`
- `termx-tui-v3/render/product_content.go`

流程：

1. render 给 exited actions 生成 hit regions：
   - `exited.restart`
   - `exited.reconnect`
2. 键盘上下/Enter 只在当前 active TerminalView 对应 terminal 已 exited 时处理。
3. restart action 变成 `TerminalPoolRestartRequestMsg`。

应该成立的不变量：

- `paneHasExitedTerminal` 只通过 TerminalView binding 找 terminal id，然后看 surface state。
- 不能从 storage、pane kind、global session 判断是否处于 exited CTA。

## restart 链路

入口：

- `TerminalPoolRestartRequestMsg`
- `reduceTerminalPoolRestartRequest`
- `TerminalPoolRestartResultMsg`
- `reduceTerminalPoolRestartResult`

关键文件：

- `termx-tui-v3/app/terminal_pool.go`
- `termx-tui-v3/state/live.go`
- `termx-tui-v3/state/terminal_view.go`

流程：

1. 用户触发 restart current terminal。
2. TUI 调 terminal service `Restart(ctx, TerminalRestartRequest{TerminalID})`。
3. restart result 成功后：
   - `root.TerminalPool.ApplyRestarted`
   - `root.Surface.RestartPreservingContent`
   - `root.TerminalViews.MarkTerminalReattaching`
   - `root.Session.ClearInputChannel`
4. 发起：
   - `TerminalPoolListRequestMsg`
   - `restartTerminalViewEffects`，逐 view reattach。

应该成立的不变量：

- restart 不删除 terminal identity。
- restart 不清 authoritative history。
- restart 不清 live tail。
- restart 会让旧 channel 失效，后续每个 view 重新 attach。
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

### 2. restore 后是否有 active TerminalView binding

看日志：

- `workbench.restore terminal_views`
- `workbench.restore active_terminal`

期望：

- `terminal_views > 0`
- `active_terminal = 当前 panel 连接的 terminal id`

如果没有：

- 查 `ToTerminalViewStore`
- 查 `withLegacyWorkbenchPaneBindings`
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

- surface/session 不再是 exited。

如果还是 exited：

- 查 `projectTerminalPoolLifecycleMetadata`
- 查 `TerminalSurfaceStore.MarkAttached`
- 查 `TerminalSessionStore.MarkAttached`

### 5. lifecycle-known running surface 是否到达

看日志：

- `live.surface lifecycle_known=true snapshot_state=attached`
- 或 `live.event lifecycle_known=true snapshot_state=attached`

期望：

- 这个消息必须出现并且不能被 queue coalesce 丢掉。

如果没有出现：

- 查 protocol adapter 是否给 snapshot 填了 `LifecycleKnown=true`。
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
- `TestInteractiveRuntimeWorkbenchRestoreLegacyPaneWithoutTerminalViewsUsesCoreRunningLifecycle`
- `TestLiveSurfaceAuthoritativeRunningClearsExitedSessionAndSurface`
- `TestLiveQueueKeepsAuthoritativeRunningLifecycleWhenOrdinaryFrameFollows`

建议现场复现时打开：

```bash
TERMX_TUI_INPUT_TRACE=1 go run ./termx-cli/cmd/termx
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

