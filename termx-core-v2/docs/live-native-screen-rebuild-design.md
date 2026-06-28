# live native screen 重构设计

状态：R348 live native screen 重构与清场基准。

本文只定义普通实时终端显示链路，不定义 authoritative history truth。history 仍以
`termx-core-v2/docs/history-logical-renderer-design.md` 为准。live screen 可以被 copy mode
进入瞬间冻结为“当时正在看的屏幕”，但它不能成为 history/window/search/copy 的历史真值来源。

## 1. 一句话

core 只维护每个 terminal 的最新 native screen；TUI 自己主导渲染循环，渲染完一帧后如果期间又有
dirty，就重新拉 core 当前最新屏幕并渲染。core 不关心任何客户端渲染到了哪一帧，也不为客户端保存
待渲染帧 backlog。

```text
PTY bytes
  -> core terminal live ingest
  -> termx-vterm / live.SurfaceTrack
  -> core NativeScreen + LiveRevision
  -> terminal.live.invalidated(termID, revision)
  -> TUI 标记 dirty / wanted revision
  -> TUI 拉取 latest NativeScreenSnapshot
  -> TUI renderer 输出宿主终端 frame
```

`terminal.live.invalidated` 是 wake-up signal，不是 frame delivery guarantee。压力输出下，core revision
可以从 1 增到 100000；TUI 可能只实际渲染 120、1800、7400、26000、91000、100000。这不是丢
live 数据，因为 live screen 的目标是尽快靠近最新状态，不是补放中间帧。中间输出是否进入可回看历史，
由 history logical renderer 和 history store 决定。

## 2. 设计边界

### 2.1 core owner

core 是 native screen owner。

core 负责：

- 持有 terminal process、PTY size、lifecycle、attachment registry、resize ownership。
- 持有唯一 `live.SurfaceTrack` / vterm 当前 screen state。
- 在同一批 PTY bytes 解码后，更新 native screen 并产出同源 semantic transaction 给 history renderer。
- 为每个 terminal 维护单调 `LiveRevision`。
- 在 native screen 变化后发布 `terminal.live.invalidated { terminal_id, revision }`。
- 在客户端请求时返回当前最新 `NativeScreenSnapshot`。

core 不负责：

- 不知道 TUI 当前渲染到了哪个 revision。
- 不等待客户端 ack。
- 不为每个客户端维护待渲染 frame 队列。
- 不把每次 changed 都转换成需要发送给客户端的完整 screen frame。
- 不用 snapshot、grid viewport、renderer rows 或 TUI 状态反推 history。

### 2.2 TUI owner

TUI 是渲染循环 owner。

TUI 负责：

- 订阅 `terminal.live.invalidated`。
- 把 invalidation 合并成本地 `dirty + wanted_revision`。
- 如果当前没有正在拉取的 live screen，则异步请求 core 当前最新 `NativeScreenSnapshot`。
- 收到 snapshot 后按 revision、terminal id、view binding、resize epoch 做 stale guard。
- 接受 snapshot 后更新本地 `LatestScreenByTerminal`，再由 renderer 画当前 view-model。
- 一帧渲染完成后，如果期间又有 dirty 或 wanted revision 大于当前 revision，再拉一次最新 screen。

TUI 不负责：

- 不逐帧追 core changed backlog。
- 不把 live screen 当 history truth。
- 不从 snapshot rows、loaded rows、本地 VTerm scrollback 或 renderer output 推断 copy/history。
- 不让 service 或 renderer 绕过 reducer 直接改 UI state。

### 2.3 protocol owner

protocol adapter 只做 domain 到 wire 的映射。

protocol 负责：

- 暴露 `live.screen.get` 这类 latest screen 请求。
- 暴露 `terminal.live.invalidated` 这类失效通知。
- 把 lifecycle、resize-control、metadata、read-error 与 live invalidation 分成不同事件。

protocol 不负责：

- 不把历史字段塞进 live snapshot。
- 不用 `HistoryGeneration` 承载 live revision。
- 不用 `ScreenUpdate` stream 给 v3 live display 推完整帧。
- 不让 `Snapshot/CompactSnapshot/grid.viewport` 的旧 wire 结构反向污染新的 domain model。

## 3. 最小 domain contract

### 3.1 NativeScreenSnapshot

`NativeScreenSnapshot` 是 core 对外暴露的当前 native screen 投影。

```go
type NativeScreenSnapshot struct {
    TerminalID string
    Revision   uint64
    Size       NativeScreenSize
    Rows       []NativeScreenRow
    Cursor     NativeScreenCursor
    Modes      NativeScreenModes
    AltScreen  bool
    Timestamp  time.Time
}
```

它必须包含：

- terminal id
- 单调 live revision
- 当前 PTY/native screen 尺寸
- cell matrix rows，包括文本、宽度、样式、链接等渲染所需属性
- cursor 可见性与位置
- mouse、bracketed paste、application cursor、alternate screen、synchronized output 等 live input/render 相关 modes
- alt-screen 状态
- snapshot 生成时间

它不能包含：

- scrollback rows
- loaded rows
- row ownership
- history generation
- history window token
- older cursor
- copy/history selection
- pane/floating/workspace id
- renderer frame metadata

### 3.2 LiveScreenInvalidated

`LiveScreenInvalidated` 是 core 对客户端的唤醒事件。

```go
type LiveScreenInvalidated struct {
    TerminalID string
    Revision   uint64
}
```

事件语义：

- 表示 core 当前 native screen 至少已经变到该 revision。
- 同一 terminal 的多个 invalidation 可以合并成最大 revision。
- 客户端收到 revision N 后可以直接请求最新 screen；core 可以返回 revision 大于 N 的 snapshot。
- 事件不保证中间 revision 可补取。
- 事件不携带 screen rows。

### 3.3 lifecycle / resize 事件

live invalidation 不能夹带 lifecycle 和 resize ownership truth。

```go
type TerminalLifecycleChanged struct {
    TerminalID string
    State      TerminalLifecycleState
    ExitCode   *int
    ExitedAt   *time.Time
    Command    []string
}

type TerminalResizeChanged struct {
    TerminalID string
    Size       NativeScreenSize
    Owner      ResizeOwner
    Epoch      uint64
}
```

TUI 可以在 render view-model 中把 lifecycle overlay 和 latest screen 合成显示，但 store 和消息链路上必须分开。

## 4. TUI render loop

TUI 侧的 reducer 需要维护 per-terminal request state。

```go
type LiveRenderRequestState struct {
    WantedRevision uint64
    CurrentRevision uint64
    InFlight bool
    InFlightRequestID string
    InFlightRevision uint64
    Dirty bool
    RequestedSize NativeScreenSize
    ResizeEpoch uint64
}
```

推荐循环：

```text
on terminal.live.invalidated(term, rev):
  wanted_revision = max(wanted_revision, rev)
  dirty = true
  if !in_flight:
    start live.screen.get(term)

on live.screen.snapshot(snapshot):
  in_flight = false
  if snapshot.term != bound term:
    discard
  else if snapshot.revision < current_revision:
    discard
  else if snapshot size/resize epoch stale:
    discard and request latest for current size
  else:
    current_screen = snapshot
    current_revision = snapshot.revision
    dirty = false
    request render

after render frame written:
  if dirty || wanted_revision > current_revision:
    start live.screen.get(term)
```

关键点：

- `live.screen.get` 总是取 core 当前最新 screen，不取某个历史 revision。
- snapshot response 可以比触发它的 invalidation 更新。
- response 只要不是 stale，就直接替换 TUI 本地 latest screen。
- render 期间出现的新 invalidation 只设置 dirty，不打断当前 render。
- render 完再拉最新，不补放中间帧。

## 5. 当前代码里必须保留的东西

这些代码或设计不能因为清场 live 链路而删除，只能重命名、收口或搬到新 contract 下。

- `termx-core-v2/live.SurfaceTrack`：core 当前 native screen 的 owner，仍是 vterm screen state 的承载点。
- `SurfaceTrack.WriteWithResult`：同一批 PTY bytes 更新 live screen，同时把 semantic transaction 交给 history。
- `Terminal.handleLiveSurfaceResponse`：OSC/DA/DSR 等 terminal response 必须回写 PTY。
- `terminalLiveIngestQueue`：PTY output goroutine 和 terminal write/history apply 之间仍需要 bounded ingest 边界；可以重写，但不能让 PTY 热路径被 TUI render 或 protocol backlog 阻塞。
- terminal lifecycle、restart、remove、process exit marker：这是 core terminal truth，不能塞进 TUI pane state。
- attachment registry、input channel、resize ownership、size lock：这是 terminal/view 协作边界，不是 live display patch。
- live modes/cursor/cell style conversion：mouse passthrough、bracketed paste、cursor、宽字符、emoji、zero-width placeholder 都依赖这些字段。
- TUI per-terminal surface cache：多个 pane/floating 可能绑定不同 terminal，不能退化成单个全局 active screen。
- TUI resize stale guard：旧尺寸 snapshot 不能覆盖新尺寸 view。
- copy mode 进入瞬间冻结 live screen 的能力：它只能作为进入态上下文，不能成为 authoritative history source。
- `terminalhost.LatestFrameSink`：宿主终端输出侧仍需要 latest frame 背压。

## 6. 当前代码建议清场的东西

下面这些是现有 live 链路中补丁式或双路径的主要来源。后续实现切片应该先按新 contract 写 harness，再删除它们。

### 6.1 core-v2 / protocol service

文件：`termx-core-v2/protocol_service.go`

建议删除或替换：

- `liveCompactSnapshot`：现在通过 `SnapshotCompact.HistoryGeneration` 承载 live revision。应替换为明确的 `NativeScreenSnapshot` adapter。
- `liveScreenUpdatePayload` / `sendLiveScreenUpdate`：v3 live display 不应再走 `TypeScreenUpdate` full replace stream。
- `startAttachmentStream` 里的 screen update full-replace 路径：attachment 可以保留 input/resize/channel registry，但 screen delivery 应改成 invalidation + pull latest。
- `ordinaryCoreLiveInvalidationEvent` / `sameCoreLiveInvalidationTarget` / `drainCoreLiveInvalidationEvents`：这些是把含混 `terminal.changed` 事件补丁式合并成 latest-only 的临时函数。应由明确 `terminal.live.invalidated` 事件替代。
- 用 `EventTerminalChanged` 且 `StateChanged == nil` 表达普通 live changed 的协议语义：应拆成 live invalidated、lifecycle changed、metadata changed。

保留但重命名/收口：

- terminal event bus 本身。
- attachment registry。
- resize-control event。
- read-error / exited / metadata 事件。

### 6.2 internal/protocol

文件：`internal/protocol/messages.go`、`internal/protocol/client.go`、`internal/protocol/control_payload.go`、`internal/protocol/binary_payload.go`

建议删除或隔离：

- `Snapshot` / `CompactSnapshot` 中用于 live display 的 scrollback、loaded rows、row ownership、history generation 等字段。
- `grid.viewport` request/response。当前 v3 live display 和 history 都不应该依赖它。
- `TypeScreenUpdate` 作为 v3 live display 内容流。如果还有 legacy 测试依赖，先标记为非 v3 live path，再在清场切片删除。
- `snapshot_overlap.go` / `TrimSnapshotScrollbackScreenVisualOverlap`。这是旧 snapshot/history 拼接补丁，新 live contract 不应存在 visual overlap trimming。

替代方向：

- 新增 `live.screen.get` payload。
- 新增 `terminal.live.invalidated` event payload。
- lifecycle、resize-control、metadata 使用独立 payload。

### 6.3 termx-tui-v3 services

文件：`termx-tui-v3/services/protocol_terminal_adapter.go`、`termx-tui-v3/services/types.go`

建议删除或替换：

- `ProtocolTerminalServiceAdapter.LiveSurface` 同时取 screen 和 lifecycle 的行为。应拆成 `LiveScreen` 与 `TerminalLifecycle`。
- `TerminalLiveEvent` 巨型 union。普通 live invalidation、ready snapshot、metadata、lifecycle、read-error 不应挤在同一个事件类型里。
- `liveEventFromProtocol` 中普通 changed 与 lifecycle 查询混合的路径。
- `drainProtocolLiveRefreshEvents`。明确 live invalidation 后，service 只需合并同 terminal 的 revision，不再按旧 event 类型猜边界。
- `liveSnapshotRevision` 从 `HistoryGeneration` 读 live revision 的临时映射。
- `LiveSurfaceSnapshot.Lines` 文本 fallback。真实 live render 应走 cell matrix。

保留但收口：

- protocol cell -> TUI cell conversion。
- modes/cursor conversion。
- fake service harness，但类型要换成 `NativeScreenSnapshot` / `LiveScreenInvalidated`。

### 6.4 termx-tui-v3 app/state/runtime

文件：`termx-tui-v3/state/live.go`、`termx-tui-v3/app/live.go`、`termx-tui-v3/app/runtime.go`

建议删除或替换：

- `ApplySnapshotWithLifecycle`：screen snapshot 不应携带 lifecycle boundary。
- `LiveLifecycleQueryMsg` 作为 live surface path 的补丁。workbench restore 可保留 lifecycle query，但应走独立 lifecycle service。
- `LiveSurfaceMsg.LifecycleKnown` / `LiveEventMsg.LifecycleKnown` 这类把生命周期混进 screen apply 的字段。
- `maybeScheduleDirtyLiveSurfaceRefresh` 的当前形态。应替换为明确 per-terminal render request controller。
- runtime 队列里的 `coalesceQueuedLiveUpdate` / `queuedOrdinaryLiveUpdate` / `liveQueueBoundary`。live coalesce 应在 `LiveScreenInvalidated` 语义上按 terminal + revision 合并，不应识别旧 union msg。
- `TerminalSurfaceStore` 中把 refresh、lifecycle、snapshot fallback 混在一起的状态字段。

保留但重写：

- `TerminalSurfaceStore` 作为 per-terminal latest screen cache。
- `LiveSurfaceRefreshState` 的思想，即 in-flight + dirty + requested size，但字段和消息语义要改成 `LiveRenderRequestState`。
- `ResizeBoundary` / requested cols rows stale guard。
- `CopyModeStore.BindLatest(... enteringLive ...)` 的进入时 live snapshot 参数，但类型要换成新的 native screen snapshot。
- floating auto-fit 对 surface size 变化的响应。

## 7. 清场顺序

后续不建议继续在现有链路上小修。建议切成三步先删后写，但每步都必须保持可编译和有 harness。

### 7.1 core contract 先落地

范围：

- `termx-core-v2/`
- `internal/protocol/`

任务：

- 定义 core domain `NativeScreenSnapshot`、`LiveRevision`、`LiveScreenInvalidated`。
- 给 `Terminal` 增加明确的 live screen getter。
- 把普通 live changed 改成明确 live invalidation event。
- 新协议请求先和旧 snapshot 并存一小步，但 TUI v3 只能使用新请求。
- 补 core/protocol harness：100k changed 只产生可合并 invalidation，snapshot revision 单调，lifecycle 不混进 snapshot。

### 7.2 TUI render loop 重写

范围：

- `termx-tui-v3/services/`
- `termx-tui-v3/state/`
- `termx-tui-v3/app/`

任务：

- service 改为订阅 `LiveScreenInvalidated` 并拉取 `NativeScreenSnapshot`。
- reducer 实现 `wanted_revision + in_flight + dirty` render request controller。
- renderer 只消费 `LatestScreenByTerminal`。
- lifecycle/metadata/resize-control 走独立消息。
- 补 TUI harness：持续 invalidation 期间不追帧，render 后继续拉最新；resize stale snapshot 被拒绝；exit lifecycle 不被普通 screen refresh 覆盖。

### 7.3 删除旧链路

范围：

- `termx-core-v2/protocol_service.go`
- `internal/protocol/`
- `termx-tui-v3/`

任务：

- 删除 v3 live 对 `Snapshot/CompactSnapshot` 的依赖。
- 删除 v3 live 对 `TypeScreenUpdate` stream 的依赖。
- 删除 `grid.viewport` live/history fallback。
- 删除 `TrimSnapshotScrollbackScreenVisualOverlap`。
- 删除旧 `LiveSurface` / `TerminalLiveEvent` union 和队列补丁 coalescer。
- 删除或改写表达旧模型的测试。

## 8. 回归风险清单

清场时最容易误删或破坏这些行为：

- OSC/DA/DSR response 回写 PTY。
- mouse passthrough mode、bracketed paste mode、application cursor mode。
- cursor visibility 和宽字符/emoji/zero-width placeholder。
- 多 terminal、多 pane/floating 的 surface 隔离。
- resize owner、size lock、late resize response stale guard。
- process exit marker 和 restart/reconnect UI。
- copy mode 进入时冻结当前 live screen。
- authoritative `history.window` / `history.copy` 不 fallback 到 live rows。
- floating terminal auto-fit。
- host `LatestFrameSink` 背压。

这些能力应在新 contract harness 中保住；旧测试如果只是在验证 `Snapshot`/`ScreenUpdate`/`grid.viewport`
旧形态，应改写为新语义测试，而不是为了旧测试保留旧链路。
