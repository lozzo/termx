# termx-core-v2 历史模型架构设计

## 1. 背景

当前终端历史相关问题的核心不在实时当前屏幕如何显示，而在历史记录的真相单位不稳定：旧实现长期混用 visual row、wrapped row、snapshot scrollback、grid viewport 与局部 metadata 来表达历史边界。这样会导致滚动、copy mode、older prepend、latest replace、resize、reclaim、process exit、clear scrollback 等路径在不同层各自推断历史真相。

本次重构不要求同步重写所有实时 screen、snapshot、grid viewport 数据结构。实时当前屏幕可以继续沿用现有方式。重构的核心准则是：历史记录必须按照 logical line 记录，并且只有 core-v2 的历史侧拥有 committed history truth。

本设计不再把“已持久化”和“仍可变尾部”建成两套数据模型。`persisted` 只表示某一时刻的存储落点或提交状态，不表示不可修改。程序发出的 clear scrollback、truncate、resize reclaim、retention、process exit replacement 等语义都可以删除、撤回、替换或重新提交已经进入 committed history 的 logical line。

### 1.1 为什么必须是 logical line

termx 的目标不是只维护一个随当前 terminal size 变化的内存 grid，而是支持可落盘、可分页、可长期保留的历史记录。历史规模可能远大于当前屏幕，也不应该受进程内存中的固定行缓冲限制。

如果把当前宽度下的 visual grid 当作历史 truth，会遇到几个不可接受的问题：

- 历史内容被当前 cols 绑定；terminal resize 后，旧历史需要按新宽度重新折行。
- 长历史必须全量留在内存里，或者把已经按某个 cols 切好的 visual rows 落盘，后续 resize 时仍然需要读取和重排大量历史。
- older pagination、copy mode、selection、cursor boundary 会依赖 visual row 坐标；当 cols 改变时，旧 cursor 和边界会失效或需要全局重算。
- 支持近似无限历史时，不能因为 terminal size 改变就把所有历史从 storage 读回内存并全量 reflow。

logical line 是解决这些约束的最小稳定单位：写入和落盘按 logical line 保存，visual rows 只是某个 cols 下的派生投影。resize 只使当前 projection、window token 或 cursor generation 失效；后续按需对请求窗口内的 logical lines 重新投影，而不是重写整段历史。这样才能同时支持持久化、bounded memory、older pagination、retention/truncate、copy mode 选择和不同 terminal size 下的稳定历史边界。

## 2. 重构目标

- `termx-core-v2` 拥有唯一历史真相。
- 历史真相的基本单位是 logical line，不是 visual row、wrapped row、snapshot scrollback 或 grid viewport。
- 历史侧使用单一 `LogicalLineStore` 保存 logical line truth。
- committed history 与 mutable frontier 是同一批 logical line 的索引、状态和可变边界，不是两套模型。
- 历史存储必须支持长期保留和按需分页；不能要求 resize 时把全部历史读入内存并全量重排。
- 实时当前屏幕、snapshot、grid viewport 可以继续作为 live surface 表达，但不能成为 committed history truth。
- `termx-tui-v3` 只消费 core-v2 返回的 authoritative history 数据；普通模式消费按需投影的 history window，copy mode 消费冻结的 logical-line snapshot。
- copy mode、鼠标滚轮、page up/down、older prepend、latest replace、stale response guard 都围绕 authoritative logical-line snapshot / history window contract 工作。

## 3. 设计原则

- 单一历史模型：所有历史内容、当前可变尾部、reclaimed suffix、待提交内容都以 `LogicalLine` 表达。
- 分离 truth、index 与 storage：`LogicalLineStore` 是历史真相；`CommittedHistoryIndex` 决定哪些 line 当前计入 committed history；`MutableFrontier` 决定哪些 line 仍可被终端语义修改；`StorageBackend` 只是内存或磁盘落点。
- `persisted` 不等于 immutable：已写入持久化后端的 line 仍可能被 truncate、clear、reclaim、replace 或 compact。
- 历史 truth 与实时 surface 分轨：历史记录按 logical line 维护，实时当前屏幕继续按当前数据方式维护。
- 明确边界优先：两轨之间只能传历史语义事件，不能传 snapshot 或 rows 让历史侧反推。
- 不以旧内部实现兼容为目标：旧 `terminalGridStore`、sidecar、snapshot/grid viewport scrollback、copy-mode frozen snapshot 只能作为问题背景。
- 先做纯内存模型和 harness，再设计持久化格式和协议适配。
- protocol adapter 是边界层，不能让现有 wire 字段反向污染 core-v2 domain model。

## 4. 总体架构

core-v2 采用 `HistoryTrack + LiveSurfaceTrack` 双轨架构。双轨只区分历史真相与实时 surface，不在 HistoryTrack 内部拆成两套 logical line 模型。

### 4.0 Daemon Storage

`Daemon Storage` 是 core-v2 daemon 内部的通用协作状态承载区，和 terminal history truth 分离。它只保存客户端写入的 app-scoped opaque data；daemon 不理解、解释或裁决这些数据是不是 workspace、tab、pane、layout、UI 锁、协作 metadata 或其他组织模型。

- storage 不是 `HistoryTrack`，不能保存或反推出 committed history truth。
- storage entry 以 `app_id + scope + owner_id + key` 定址，value 是协议层 opaque bytes。
- storage entry 必须带 version，写入和删除支持 check-version CAS，避免多客户端覆盖。
- storage mutation 必须通过 daemon 事件流广播 `storage.changed`，客户端以事件再拉取或重建本地投影。
- daemon 的核心职责是 terminal pool、terminal lifecycle、authoritative history 和附带的通用数据存储；TUI 的 workspace/tab/pane 只是某个客户端对 storage value 的一种组织方式，第三方客户端可以使用完全不同的组织模型。
- TUI-v3 可以把 workspace/tab/pane 数据寄存在 storage 中，并把 storage version/event 当作多客户端同步边界；但 schema、mutation 语义、active/focus 投影和 UI 组织规则属于 TUI/client，不属于 daemon 领域模型。

### 4.0.1 Client Workbench Projection Over Storage

`Client Workbench Projection` 是 TUI-v3 在 daemon storage 之上定义的客户端数据投影，不是 core-v2 daemon 的内建领域 truth。

- TUI-v3 可以把 workspace/tab/pane/split tree/pane-terminal binding 编码为 storage value，并用 check-version CAS 防止多客户端覆盖。
- mutation 应由 TUI/client 在本地按自己的 schema 计算，再通过 `storage.put/delete` 写入；daemon 只做版本检查、持久化和 `storage.changed` 广播。
- 客户端收到 `storage.changed` 后按 app/scope/key 过滤并重新拉取或重建本地投影。
- 不应继续把 `workbench.get`、`workbench.apply`、`workbench.changed` 扩展为长期 daemon 领域 API；若当前实现中存在 workbench 专用协议，它只应视为待收敛到 storage opaque model 的迁移债。
- floating/overlay 是否进入共享 storage 由 TUI/client schema 决定；daemon 不需要知道这些概念。

### 4.0.2 Terminal Attachment / View Registry

`Terminal Attachment` 是客户端连接某个 terminal 的 protocol 视图，不是新的 terminal，也不是 TUI 的 pane/workspace truth。

- `Terminal` 仍是 core-v2 daemon 管理的全局运行实体，拥有 process、PTY size、live surface、terminal lifecycle 和 authoritative history truth。
- `Attachment` 绑定 `terminal_id + channel + surface_id + view_id + resize_policy + resize ownership epoch`，用于路由 input、resize、event stream 和 detach。
- 同一个 terminal 可以同时存在多个 attachment；多个 attachment 共享同一 process、input sink、live surface truth 和 history truth。
- attachment registry 不能保存 workspace、tab、pane、floating 或布局 schema；这些只属于 TUI/client 或 storage opaque value。
- `channel -> terminal_id` 只是最低限度路由信息，不能替代 attachment registry；后续 input/resize/error 必须能定位到具体 attachment。
- detach 应移除具体 attachment 或 channel，不得因为一个 view 关闭而删除同 terminal 的其它 attachment。
- terminal kill/remove/restart 是 terminal lifecycle 操作，必须影响所有 attachment，并通过 daemon event stream 通知客户端重新投影。
- attach、detach、reattach、resize ownership 变化都不得创建 committed history；它们只能改变 attachment/session 边界、live projection 或 stale signal。

Resize ownership 是 attachment 的属性，不是 terminal history truth。

- 同一 terminal 在同一时刻最多有一个有效 resize owner attachment。
- owner attachment 可以通过 `ensure_resize` 改变 PTY size；follower/observer attachment 不能因为自身 view content rect 变化覆盖 PTY size。
- owner 转移必须显式发生，更新 ownership epoch，并让旧 owner 的 late resize response 失效。
- resize policy 不改变 logical-line history truth；resize 后 history window 仍通过 token/generation/cols 失效和重投影。

Terminal size lock 是 terminal 级协作控制状态，不是 TUI 的 pane layout 状态。

- lock truth 必须跟随 core-v2 terminal/attachment registry，由 core-v2 统一裁决并通过 protocol 投影；客户端只能缓存投影，不能各自保存一份可冲突的 lock truth。
- `ResizeOwnership.SizeLocked` 与 `ResizeControl.SizeLocked` 表达当前 terminal PTY size 被锁定；锁定时，即使某 attachment 已是 resize owner，`ensure_resize` 也不能自动修改 PTY size，必须返回 `ResizeControlReasonSizeLocked`、当前 owner、当前 size 和最新 epoch。
- 获得 resize owner 与执行 resize 是两个动作：未锁定时，显式 owner transfer 可以让新 owner 按自己的 content rect 主动发起一次 `ensure_resize`；锁定时，owner transfer 只能更新 owner/epoch 和广播 control state，不得顺手 resize。
- 解锁必须是显式用户动作，且必须走 core-v2 protocol；core-v2 解锁本身不自动 resize。用户解锁后若当前 owner view 需要匹配自己的 panel 尺寸，TUI 必须再发起一次语义明确的 owner resize request。
- lock/unlock、owner transfer、owner detach 后自动 promote、成功 resize 都必须 bump ownership epoch，并向所有订阅同一 terminal 的客户端广播 resize-control/ownership 变化；广播只携带 terminal/attachment/control metadata，不携带 workspace、tab、pane 或 floating schema。
- follower/observer 在收到广播后只能更新本地 view binding 投影和 chrome 状态；不得因为广播或自身 content rect 变化主动覆盖 PTY size。
- terminal process size、owner attachment、lock flag 和 epoch 可以随 daemon 生命周期保存在 core-v2 运行态；若未来要求跨 daemon restart 保留 lock，必须通过 core-v2 自有 terminal metadata 或明确的 protocol/storage 设计完成，不能让 TUI opaque workbench storage 成为 terminal lock truth。

### 4.1 HistoryTrack

`HistoryTrack` 是 authoritative history truth，内部由四类对象组成：

- `LogicalLineStore`：唯一 logical line truth，保存 line id、cells/runs、封口状态、版本、投影缓存和 payload metadata。
- `CommittedHistoryIndex`：当前计入 committed history 的 ordered line index，负责 older cursor、retention、clear scrollback、truncate、generation 与 logical boundary。
- `MutableFrontier`：当前仍可能被终端语义修改的 line 范围，包含 open line、sealed but still mutable line、reclaimed committed suffix、shrink resize 隐藏尾部。
- `StorageBackend`：内存、文件、mmap 或其他持久化实现，只负责保存和恢复 `LogicalLineStore` 与索引状态，不定义 mutability。

`HistoryTrack` 只能从显式历史语义事件更新历史状态，不得从 snapshot、grid viewport、visual rows 或 wrapped rows 反推 committed history。

### 4.2 LiveSurfaceTrack

`LiveSurfaceTrack` 负责实时当前屏幕和兼容实时投影，包含：

- 当前 screen
- 实时 snapshot
- grid viewport
- 当前尺寸下的 live surface projection

`LiveSurfaceTrack` 的内部数据可以继续沿用当前 row/snapshot/grid viewport 风格。它可以显示当前终端状态，但不能向历史侧提供 committed history truth，也不能成为 TUI copy mode 的 history source。

### 4.3 EventRouter

`EventRouter` 是 PTY / VT stream 进入 core-v2 后的唯一路由点。

- 同一输入流只能解码一次，然后按顺序同时产生 `HistoryEvent` 与 `LiveSurfaceMutation`。
- `HistoryEvent` 直接提交给 `HistoryTrack`。
- `LiveSurfaceMutation` 直接提交给 `LiveSurfaceTrack`。
- 两条轨道接收同一个递增 input sequence，用于测试与调试原子顺序。
- `HistoryTrack` 永远不能读取 `LiveSurfaceTrack` 的 snapshot、grid viewport 或 diff。
- `LiveSurfaceTrack` 也不能通过回调补写 committed history，只能在事件路由阶段接收同源 mutation。

这样可以避免实现时先更新 live surface，再从 surface diff 反推历史。

### 4.4 Protocol Adapter

protocol adapter 负责把 core-v2 domain model 映射到外部协议。

- core-v2 内部不直接依赖现有 `termx-proto` 或 `internal/protocol` 的 wire 结构。
- `history.window` 的 wire contract 在 core-v2 domain model 稳定后再适配。
- legacy `snapshot` / `grid.viewport` 如继续存在，只能作为实时兼容投影接口，不能作为新 TUI history path。
- latest 请求必须由 core-v2 决定是否返回 replace。
- older 请求必须携带 core-v2 上次返回的 cursor 或 logical boundary。
- older response 的 op 必须由 core-v2 决定，client 不得自行把 response 解释成 prepend。
- history response 必须携带 stable logical line id、line span clipping、token、generation、first/last logical line boundary、has-more 与 cursor。
- stale guard 只能使用 token、generation、cursor、logical boundary，不能使用 snapshot totals、row count 或 LoadedRows。
- `history.window` 是 terminal-scoped contract：请求和响应都只表达 terminal 的 authoritative history projection，不回显 TUI pane/floating/workspace truth，也不承担 attachment ownership 语义。
- `history.window` 不需要携带 `SurfaceID`、`ViewID` 或 pane id；如果客户端要把 response 重新绑定到本地 view，只能依赖本地 pending request 与 token/generation/cursor/boundary 命中后回填，不能把 daemon 端 protocol payload 扩张成 workspace schema。
- `ServeTransport` 是完整 daemon protocol session 入口；`ServeScopedTransport` 只给 remote datachannel 这类受限 transport 使用。
- `TransportScope` 只能在 protocol session 边界过滤 method、terminal id、事件类型和 stream channel，不能保存 terminal lifecycle、attachment、history 或 storage truth。
- terminal scope 会把空事件订阅收窄到目标 terminal；machine-events-only scope 只允许 terminal lifecycle/metadata 事件，不能访问 terminal method、storage 或 workbench。

attachment 相关 protocol 适配必须满足：

- `attach` 返回的 channel 必须对应一个 daemon-side attachment，不只是临时输入通道。
- `AttachParams.SurfaceID` 和 `AttachParams.ViewID` 表达客户端 view identity；core-v2 可以记录和回显，但不解释它们是不是 pane、floating 或 tab。
- `ensure_resize` 必须校验 channel、terminal id 和 attachment identity，按 resize policy/ownership 决定允许、拒绝或转移 owner。
- `ResizeControl` / `ResizeOwnership` 是 protocol 层对 attachment ownership 的投影，不得被 TUI 当成 committed history 或 workspace truth。
- event stream 可以按 terminal 过滤，也可以携带 attachment 相关 metadata，但不得把 TUI pane/floating schema 推进 daemon 领域模型。

## 5. HistoryTrack 数据模型

### 5.1 LogicalLine

每条 logical line 至少包含：

- stable logical line id
- seal 状态：open 或 sealed
- logical text、cells 或可重放 cell runs
- projection segments：按 cols 和 generation 可重算的历史窗口投影派生视图
- row kind
- timestamp 范围
- dirty 状态
- generation 或版本
- storage residency：memory、file、mmap、evicted 等实现状态

LogicalLine 的 truth 是 logical boundary 与 cells/runs。projection segments 不能成为 stored truth，也不能作为 logical line boundary 的唯一来源。

`open/sealed`、`dirty/clean`、`committed/uncommitted`、`mutable/immutable-for-now`、`memory/file` 是不同维度，不能混成一个字段。已写入 StorageBackend 的 line 仍可能重新进入 `MutableFrontier` 被修改，也可能因为 clear/truncate/retention 从 `CommittedHistoryIndex` 中删除。

### 5.2 LogicalLineStore

`LogicalLineStore` 是唯一历史数据模型。

- 不能以 visual row 作为主键或唯一边界。
- 不能只依赖 wrapped metadata 反推出 logical line truth。
- 同一 logical line id 在 retained 范围内必须稳定。
- 修改 line 内容必须 bump line generation 或 HistoryTrack generation。
- 删除 line 必须同时更新 `CommittedHistoryIndex`、`MutableFrontier` 与相关 cursor/token。
- 只有不再被 `CommittedHistoryIndex` 或 `MutableFrontier` 引用的 line payload 才能被物理删除或压缩。
- 投影缓存只能由 line truth 派生，可以丢弃和重算。

### 5.3 CommittedHistoryIndex

`CommittedHistoryIndex` 表示当前 authoritative committed history 的顺序。

- 它只保存 line id、边界、cursor、generation、retention metadata 等索引信息。
- 它不是单独的 payload store。
- append commit 是把 line id 加入 committed index，不是复制成另一套 line。
- clear scrollback / truncate committed history 可以删除 index 覆盖的 line，并通知 StorageBackend 删除、标记 tombstone 或等待 compaction。
- retention 必须以完整 logical line 为单位。
- older cursor 只能基于 committed index 和 logical boundary 生成。

`committed` 的含义是“当前会被 authoritative history window 和 older pagination 计入历史深度”，不是“永远不能修改或删除”。

### 5.4 MutableFrontier

`MutableFrontier` 表示当前仍可能被终端语义修改的 line 范围。

它可以包含：

- open logical line
- sealed but still mutable logical line
- 从 `CommittedHistoryIndex` reclaim 回来的 committed suffix
- shrink resize 后隐藏但仍可变的 logical line

reclaim 的含义是把 committed suffix 从 index 边界撤回到 mutable frontier，不是把数据从一个 store 搬到另一个 store。

- clean reclaimed line 如果没有被修改，force commit 时只把 line id 与新的 committed boundary 放回 `CommittedHistoryIndex`，不重复写 payload，也不 bump line content generation；但 index generation 必须变化。
- dirty reclaimed line force commit 时替换该 logical line 内容，并更新 line generation 与 index generation，不能让旧内容和新内容同时出现在 committed history 中。
- 如果 reclaimed line 的原 committed source 已被 `truncate-committed-history` 删除，后续 force commit 只能把它作为当前 frontier 内容重新提交到新的 committed boundary，不能恢复旧 cursor 或旧 committed source。

### 5.5 StorageBackend

`StorageBackend` 负责内存和持久化实现。

- 第一阶段只需要内存 backend。
- 后续文件或 mmap backend 不能改变 domain model。
- backend 中已有记录不代表 immutable。
- backend 可以支持 append、overwrite、truncate、delete、tombstone、compaction。
- backend 只能删除不再被 `CommittedHistoryIndex` 或 `MutableFrontier` 引用的 payload；如果 committed source 被 truncate 但 line 仍在 frontier，backend 必须先保留或转移 frontier payload。
- 恢复时必须重建 `LogicalLineStore`、`CommittedHistoryIndex`、`MutableFrontier` 和 generation，不能只恢复 row payload 后由 snapshot 反推历史。

### 5.6 HistoryWindow

`HistoryWindow` 是 `HistoryTrack` 对 TUI 和协议层输出的 authoritative projection。普通历史浏览可以直接消费按当前 `cols` 投影的 visual rows；copy mode 也可以选择在进入时请求冻结 logical-line snapshot，再由客户端本地重排。

至少包含：

- terminal id
- window token
- op：replace 或 prepend
- size
- visual rows
- row metadata
- logical line spans
- stable logical line ids
- clipped before / clipped after
- before cursor 或 older cursor
- loaded logical lines
- total logical lines
- has more
- generation
- first/last logical line boundary
- timestamp

如果用于 copy mode frozen snapshot，还必须满足：

- payload 足以让客户端在本地按任意 pane `cols` 重新 wrap，而不丢失 logical line truth。
- logical line text/cells/runs、stable logical line ids、clipping 边界和 copy 所需 metadata 必须完整可重放。
- frozen snapshot token 必须稳定绑定这次 copy 会话，后续 older 请求继续基于该 token / boundary 拉更早 logical lines。

latest window 可以投影 committed tail 和 eligible mutable frontier。普通非冻结 older cursor 只能基于 committed index 推进；mutable frontier 不得让 TUI 自行推断 committed depth。若客户端请求 frozen snapshot，core-v2 负责冻结本次 copy 会话的 logical-line 边界与 token，后续 older cursor 在这份冻结 snapshot 的完整 visible logical lines 里向前移动，不能跳过进入 copy mode 时仍在屏幕上的 frozen frontier lines；同时 core 不再为该会话后续每次本地宽度变化重复做 visual reflow。

## 6. HistoryTrack 输入事件

`HistoryTrack` 的输入只能是历史语义事件。

copy mode frozen snapshot 要落地，不能只停在 `MutableFrontier` 这个抽象词，还必须把“什么语义会 seal、什么语义会 committable、什么语义会 committed”写成明确状态机，并由 core 自己维护判定辅助信息。

### 6.1 事件路由与事务边界

- 每个 PTY / VT 输入批次必须形成一个有序事务。
- 一个事务可以同时包含多条 `HistoryEvent` 和多条 `LiveSurfaceMutation`。
- 同一事务内 `HistoryEvent` 与 `LiveSurfaceMutation` 使用相同 input sequence。
- 事务提交顺序由 `EventRouter` 保证，不能由 `LiveSurfaceTrack` 的当前状态反推。
- 如果某个输入既影响当前屏幕又影响历史，必须在同一事务中显式产生两侧事件。

### 6.2 必须支持的事件

- `write-primary-cells`：写入 primary 当前 logical line 的 cell runs 或 cells。
- `seal-logical-line`：硬换行、滚出可变区或明确封口边界导致 logical line sealed。
- `mutate-frontier`：光标移动、覆写、erase、clear 局部可变内容等对 `MutableFrontier` 内 line 的修改。
- `reset-frontier`：明确重置场景丢弃当前 primary `MutableFrontier`，但不创建 committed history；真实终端 `CSI 2J` 默认不走这个丢弃语义。
- `commit-frontier`：把 frontier 中符合条件的 logical line 加入 `CommittedHistoryIndex`。
- `force-commit-frontier`：process exit 时强制封口 primary frontier 并提交。
- `reclaim-committed-suffix`：grow resize 或其他可变性恢复场景按完整 logical line 把 committed suffix 撤回到 frontier。
- `hide-frontier`：shrink resize 把当前 screen 中不可见但仍可变的 logical lines 转入 hidden frontier。
- `truncate-committed-history`：clear scrollback、retention 或显式删除历史时，从 `CommittedHistoryIndex` 删除完整 logical line 范围，并同步删除或标记 store 中相关 line。
- `switch-alt-screen`：进入/退出 alt-screen 时冻结/恢复 primary history，不把 alt 内容写入 primary history。
- `non-history-boundary`：attach、reattach、bootstrap、recovery、full replace、resize 等非历史创建事件只影响事务边界、token 或 generation，不凭空创建 committed history。

`non-history-boundary` 只能表达事务边界和 stale signal，不能替代 `mutate-frontier`、`reset-frontier`、`commit-frontier`、`reclaim-committed-suffix`、`hide-frontier`、`truncate-committed-history`、`force-commit-frontier` 等具体 history mutation 事件。

### 6.2.1 line 状态机与 commit 条件

为了支持 frozen snapshot 和后续本地 reflow，core-v2 必须把 line 状态明确为至少这几类：

- `open`：当前仍在追加写入的 active line。
- `sealed`：已经遇到明确封口语义，但当前 primary screen 仍可能持有它。
- `committable`：已 sealed，且当前 primary screen 已不再持有它；此时允许进入 committed history。
- `committed`：已经进入 `CommittedHistoryIndex`。
- `reclaimed`：原 committed line 因 resize grow 或其他语义被撤回到 frontier，再次变为可修改。

第一版必须明确这些基础语义：

- `\n`：seal 当前 active line。
- `\r`：不 seal、不 commit；只改变后续写入位置，后续覆写仍作用于当前可变区域。
- auto-wrap：只改变 visual projection，不 seal logical line。
- 覆写、erase、cursor move：如果目标仍在可变区域，只能表达为 `mutate-frontier`。
- process exit：对当前 primary frontier 执行 `force-commit-frontier`。

也就是说，`newline` 只表达“封口”，不自动等于“沉淀”；真正进入 committed history 还必须满足 line 已不再被当前 primary screen 持有。

白话例子：

- 输出 `hello\nworld` 时，`hello` 遇到 `\n` 只会先变成 `sealed`。如果屏幕高度还足够、`hello` 仍在 primary screen 上可见，它还不能 committed。
- 只有后续继续输出，把 `hello` 真正滚出当前 primary screen ownership，它才会变成 `committable`，然后才允许进入 committed history。
- 输出 `progress 10%\rprogress 20%` 时，`\r` 只表示“回到列 0 再写”，这整条 line 仍属于当前可变区，不应因为出现 `\r` 就提前 committed。

### 6.2.2 primary screen ownership ledger

为了知道一条 sealed line 何时真正可 commit，core-v2 需要一份只用于判定的 primary screen ownership ledger：

- 它记录当前 primary screen 上哪些 visible row 仍归属于哪些 logical line。
- ledger 不是 history truth，也不是 live surface snapshot；它只是 commit 判定辅助结构。
- 当一条 line 已 sealed 且 ledger 中已经没有任何 row 归它所有时，这条 line 才能从 `sealed` 进入 `committable`，再被 `commit-frontier` 提交。
- shrink resize 只能把仍可变的 line 从 visible ownership 转为 hidden frontier ownership，不能借机提前 committed。
- grow resize / reclaim 会让某些原 committed line 再次进入 mutable ownership，此时后续修改必须以新版本表达，不能污染仍被 snapshot 引用的旧版本。

再举一个具体例子：

- 假设 terminal 当前高度是 `24` 行。
- 某条 line 已经 `\n` 封口，但它还在这 `24` 行里可见；这时 ledger 仍然持有它，所以它只是 `sealed`。
- 又输出了几行新内容，把它挤出这 `24` 行之外；ledger 不再持有它，这时它才进入 `committable`，后续 `commit-frontier` 才允许把它算进 authoritative history depth。
- 如果后来 grow resize 把更老的 committed line reclaim 回来重新参与当前可变区，这只是“重新变成可修改”，不是旧 committed payload 可以被原地污染。

### 6.3 resize 语义

- resize 本身不是历史创建事件。
- resize 本身不是历史重写事件。
- grow resize 只允许触发 `reclaim-committed-suffix`，并且必须按完整 logical line reclaim committed suffix。
- shrink resize 只允许触发 `hide-frontier`，表达 `screen -> hidden mutable frontier`。
- 不允许 reclaim 半条 logical line。
- resize 后 active history window 必须通过 new generation 或 new token 失效。
- resize 后 TUI 应通过 latest replace 获取新的 authoritative history window。
- resize 不得从 resized snapshot 或 grid viewport 重建 committed history。

对于 frozen snapshot 模式，还要增加：

- resize 不改变 snapshot token、committed upper bound 或 older boundary。
- live resize 只影响 live surface 和未来 commit 判定，不得 retroactively 改写已冻结 snapshot。

### 6.4 clear / erase / full replace 语义

- 局部 erase 或光标覆写如果作用于仍可变 logical line，必须进入 `mutate-frontier`。
- clear screen 可以清理 `LiveSurfaceTrack` 当前屏幕，但不得从清屏后的 snapshot 凭空创建 committed history。
- 真实终端 `CSI 2J` 对 `HistoryTrack` 是 page-break：先把 core 已持有的 primary frontier 封口提交，再清空 primary screen ownership，让清屏后的 UI 从新 logical page 开始。
- 这个 page-break 只能提交 `LogicalLineStore` / `MutableFrontier` 里已经存在的 line；不能读取 `LiveSurfaceTrack`、snapshot、grid viewport 或 visual rows 来补造历史。
- 如果业务明确要丢弃 mutable frontier，必须使用 `reset-frontier`，不能把它和默认 `CSI 2J` 混成一类。
- clear scrollback 或程序明确删除历史时，必须使用 `truncate-committed-history`，并 bump generation、失效旧 token/cursor。
- clear scrollback 可以删除已经写入 StorageBackend 的 line；StorageBackend 必须执行 delete、truncate、tombstone 或后续 compaction。
- `truncate-committed-history` 只删除 committed index 覆盖的历史。它不得从当前 screen 或 `MutableFrontier` 反推删除内容。
- 如果被 truncate 的 logical line 当前也在 `MutableFrontier` 中，truncate 必须切断其旧 committed source/boundary；除非同一事务还包含 `reset-frontier` 或 `mutate-frontier`，否则该 line 仍作为当前 frontier 内容保留。
- 对仍保留在 frontier 的 line，StorageBackend 删除的是旧 committed reference 或旧 committed backing，不得丢失 frontier payload；必要时先把 payload 保留在 `LogicalLineStore` 或转移到新的 storage reference。
- truncate 后保留在 frontier 的 line 后续 force commit 时进入新的 committed boundary，不能复用已删除的 old cursor/source。
- full replace 不得把当前 screen 内容作为新 committed history。
- full replace 可以重建 `LiveSurfaceTrack` 实时投影，但只能向 `HistoryTrack` 发送 `non-history-boundary` 或明确的 frontier mutation/truncate 事件。
- 任何 clear/full replace 后的 active history window 必须根据 token/generation 规则失效或通过 latest replace 重置。

### 6.5 alt-screen 与 process exit 组合语义

- 进入 alt-screen 前，primary `HistoryTrack` 必须先执行 page-break：把 core 已持有的 primary frontier 封口提交，再清空 primary screen ownership。
- 进入 alt-screen 后，primary `HistoryTrack` 冻结；alt-screen 内部的清屏、光标移动和绘制不会持续进入 primary history。
- 退出 alt-screen 时，live surface 可以保留 alt-screen 的最后一帧作为实时显示，history 也可以把同一最后一帧追加成新的 authoritative history page，避免全屏程序退出后 copy/history 里看不到这次 UI。当前默认开启，可用 `TERMX_PRESERVE_ALT_SCREEN_ON_EXIT=0` 临时关闭，后续迁到配置系统时仍只控制“退出时保留最后一帧”这一策略。

### 6.6 frozen snapshot / pagination contract

copy mode 进入后，core-v2 需要暴露一个比“当前 cols 下的 visual rows”更稳定的 contract：

- `snapshot_token`
- `committed_upper_bound`
- `frozen_frontier_lines`
- 第一批 logical-line payload
- `older_boundary`

它的要求是：

- snapshot token 必须固定绑定这次 copy 会话。
- 后续 live append 不得进入这次 snapshot 的可见范围。
- 如果 live 需要修改仍被 snapshot 引用的 frontier line，必须做 line-level copy-on-write；旧版本继续服务 snapshot，新版本服务 live。
- older 请求继续带 `snapshot_token + boundary` 回 core 拉这份冻结 snapshot 里更早的 logical lines；如果上一页只显示了最后一条 frozen frontier line，下一页必须先返回它前面的 frozen frontier line，而不是直接跳到 committed 上界。
- process exit 是 primary history 的 mutability 边界。
- process exit 时 primary `MutableFrontier` 必须先 `force-commit-frontier`。
- process exit 的 lifecycle marker 由 core 作为显式系统输出追加到 live surface 和 `HistoryTrack`，包含 terminal id、退出码、退出时间和命令；它不是从 live snapshot、grid viewport 或 TUI overlay 反推出来的 history。
- 如果 process exit 时仍在 alt-screen，alt 内容直接丢弃；primary frontier 仍按 process exit 规则 force commit。
- force commit 与 index/storage 更新必须在同一 history transaction 中产生可验证 generation 变化。

这个 contract 的白话含义是：

- snapshot 不是“整份历史全量拷贝一份”。
- 已经 committed 的历史可以被多个 snapshot 共享。
- 进入 copy mode 时，只需要额外冻结两样东西：
  - 这次 snapshot 的 committed 上界。
  - 当前仍可变、但这次 copy 也必须看见的 frontier lines。
- 如果 live 后面只是 append 新 line，这些新 line 天然不属于旧 snapshot。
- 如果 live 后面要改旧 snapshot 仍在看的 frontier line，才需要按 line 做 copy-on-write。

例子：

1. 用户进入 copy mode 时，committed 历史到 `line 1000`，当前 frontier 里还有 `line 1001-1003`。
2. core 返回 `snapshot_token=S1`，其中 committed 上界是 `1000`，并把当时的 `1001-1003` 作为 frozen frontier 一起交给客户端。
3. 之后 live 又输出了 `1004-1050`；这些新 line 不会进入 `S1`。
4. 如果 live 又把 `line 1003` 改写了，旧版本继续服务 `S1`，新版本服务 live；这就是 line-level copy-on-write。
5. 客户端如果继续往上翻，只带着 `S1 + older_boundary` 去拉更老的 logical lines，不需要重新请求一份按新 `cols` 投影好的整窗 rows。

### 6.6 禁止的输入

- snapshot 作为 history truth
- grid viewport 作为 history truth
- visual row 切片作为 committed history 边界
- wrapped row 序列作为最终 logical line truth
- TUI 本地 scrollback 作为 committed history

## 7. TUI-v3 关系

`termx-tui-v3` 不拥有 committed history truth。

tui-v3 至少包含：

- `historyview.Store`：只保存 core-v2 返回的 authoritative window、token、generation、rows、line spans、cursor、has-more、viewport、selection、pending request token。
- `historyview.Source`：先使用 fake source 做 harness，后续通过 protocol adapter 接入。
- `copymode`：只消费 `historyview.Store`，不读 local VTerm scrollback，不用 wrapped 推断 logical line。
- `history renderer`：copy mode 和历史窗口只绘制 authoritative projection，不把 visual rows 拼回 logical truth。
- `live renderer`：普通实时屏幕可以继续消费 `LiveSurfaceTrack` 的 snapshot 或 grid viewport。
- `input`：滚轮、page up/down、selection 只改变交互态或发起 history window 请求，不维护本地 committed history depth。

TUI-v3 stale guard 只能使用 core-v2 返回的 token、generation、cursor 与 logical boundary。TUI-v3 不得使用 snapshot totals、row count、LoadedRows 或本地 scrollback depth 接纳 older/latest response。

## 8. 关键硬约束

- 历史 truth 只能来自 `HistoryTrack`。
- `HistoryTrack` 内部只有一个 `LogicalLineStore` 数据模型。
- committed history 与 mutable frontier 是索引、状态和边界，不是两套 payload store。
- `persisted` 只表示存储落点或提交状态，不表示 immutable。
- StorageBackend 不得定义 history mutability。
- 实时 snapshot/grid viewport 可以保留，但只能属于 `LiveSurfaceTrack`。
- 两轨不能通过 snapshot、wrapped rows、visual rows 传递历史 truth。
- resize 不能创造 history，grow resize 必须按完整 logical line reclaim committed suffix。
- clear screen、full replace、attach、reattach、bootstrap、recovery 不能凭空创建 committed history；`CSI 2J` 只允许提交 core 已持有的 primary frontier，不能从 live snapshot 反推历史。
- clear scrollback、truncate、retention 可以删除已提交和已持久化的 logical line，但必须按完整 logical line 更新 index、store、generation 与 cursor。
- alt-screen 不写 primary history。
- process exit 必须 force commit primary mutable frontier。
- TUI 不得用本地深度计数、snapshot totals、LoadedRows、row count 推断 older/latest 接纳规则。

## 9. 第一阶段范围

第一阶段只做：

- 纯内存 `HistoryTrack`
- 明确的 `HistoryEvent` 类型
- `LogicalLineStore`
- `CommittedHistoryIndex`
- `MutableFrontier`
- 内存 `StorageBackend`
- HistoryWindow 生成
- fake event source harness
- fake history source harness

第一阶段不做：

- 真实 PTY 接入
- Bubble Tea app 接入
- 旧 CLI 默认入口迁移
- 最终持久化文件格式
- 旧 `termx-core/` 原地修补
- 旧 `tuiv2/` 原地修补

## 10. 主要风险与应对

### 10.1 事件接口退化成 row/snapshot 输入

风险：`LiveSurfaceTrack` 为了方便直接把 rows 或 snapshot 交给 `HistoryTrack`。

应对：`HistoryTrack` API 只接受历史语义事件，测试覆盖禁止 row/snapshot 反推 history truth。

### 10.2 StorageBackend 被误当作历史真相

风险：实现为了方便把“已经写到文件”解释成“不可修改”，导致 clear scrollback、truncate、reclaim、replacement 无法正确表达。

应对：domain model 只认 `LogicalLineStore`、`CommittedHistoryIndex` 与 `MutableFrontier`；StorageBackend 只是读写实现。测试必须覆盖删除已提交历史、reclaim 后修改再提交、retention 后 cursor 失效。

### 10.3 CommittedHistoryIndex 退化为 append-only ledger

风险：只记录 sealed lines，无法表达删除、撤回、reclaim、dirty replacement、process exit force commit。

应对：第一阶段就实现 `truncate-committed-history`、`reclaim-committed-suffix`、`mutate-frontier` 和 `force-commit-frontier` harness。

### 10.4 过早绑定 protocol

风险：为了兼容现有 wire 字段，把 core-v2 内部模型设计成 row-level contract。

应对：先稳定 domain model，再写 protocol adapter。

### 10.5 实时 surface 被误认为 history source

风险：TUI 或协议为了滚动继续使用 snapshot/grid viewport。

应对：TUI-v3 只接 `HistoryWindow`，legacy snapshot/grid viewport 文档明确为 live surface 兼容接口。

## 11. 推荐落地顺序

1. 创建 `termx-core-v2/` 与 `termx-tui-v3/` 最小模块。
2. 在 core-v2 中定义 `HistoryTrack`、`LiveSurfaceTrack`、`HistoryEvent`、`LogicalLine`、`LogicalLineStore`、`CommittedHistoryIndex`、`MutableFrontier`、`StorageBackend`、`HistoryWindow` 类型。
3. 实现纯内存 `LogicalLineStore`、`CommittedHistoryIndex` 与 `MutableFrontier`。
4. 补 core-v2 logical line harness：普通输出、换行、自动折行、覆写、局部 erase、clear screen、clear scrollback、truncate committed history、full replace、attach、reattach、bootstrap、recovery、resize、alt-screen、exit-while-alt、process exit、reclaim 后修改再提交。
5. 实现 HistoryWindow projection 和 pagination，覆盖 resize 后 latest replace、旧窗口 stale guard、clipped span、older cursor 与 logical boundary。
6. 在 tui-v3 中实现 fake source + historyview store harness，覆盖 latest replace、older prepend、empty older exhausted、stale response、selection clipped span。
7. 补 protocol round-trip 和 adapter harness。
8. 稳定后再进入真实入口迁移。
