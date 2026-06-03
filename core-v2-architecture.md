# termx-core-v2 历史模型架构设计

## 1. 背景

当前终端历史相关问题的核心不在实时当前屏幕如何显示，而在历史记录的真相单位不稳定：旧实现长期混用 visual row、wrapped row、snapshot scrollback、grid viewport 与局部 metadata 来表达历史边界。这样会导致滚动、copy mode、older prepend、latest replace、resize、reclaim、process exit 等路径在不同层各自推断历史真相。

本次重构不要求同步重写所有实时 screen、snapshot、grid viewport 数据结构。实时当前屏幕可以继续沿用现有方式。重构的核心准则是：历史记录必须按照 logical line 记录，并且只有历史侧拥有 committed history truth。

## 2. 重构目标

- `termx-core-v2` 拥有唯一历史真相。
- committed history 的基本单位是 logical line，不是 visual row、wrapped row、snapshot scrollback 或 grid viewport。
- 实时当前屏幕、snapshot、grid viewport 可以继续作为 live surface 表达，但不能成为 committed history truth。
- `termx-tui-v3` 只消费 core-v2 返回的 authoritative history window，不本地重建历史。
- copy mode、鼠标滚轮、page up/down、older prepend、latest replace、stale response guard 都围绕 authoritative history window 工作。

## 3. 设计原则

- 历史 truth 与实时 surface 分轨：历史记录按 logical line 维护，实时当前屏幕继续按当前数据方式维护。
- 明确边界优先：两轨之间只能传历史语义事件，不能传 snapshot 或 rows 让历史侧反推。
- 不以旧内部实现兼容为目标：旧 `terminalGridStore`、live-tail sidecar、snapshot/grid viewport scrollback、copy-mode frozen snapshot 只能作为问题背景。
- 先做纯内存模型和 harness，再设计持久化格式和协议适配。
- protocol adapter 是边界层，不能让现有 wire 字段反向污染 core-v2 domain model。

## 4. 总体架构

core-v2 采用 `HistoryTrack + LiveSurfaceTrack` 双轨架构。

### 4.1 HistoryTrack

`HistoryTrack` 是 authoritative history truth，负责：

- logical line store
- persisted history store
- mutable live tail
- history window projection
- older/latest cursor
- window token
- generation
- logical line boundary

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

## 5. HistoryTrack 数据模型

### 5.1 LogicalLine

每条 logical line 至少包含：

- stable logical line id
- seal 状态：open 或 sealed
- origin：live 或 reclaimed
- logical text、cells 或可重放 cell runs
- projection segments：按 cols 和 generation 可重算的历史窗口投影派生视图
- row kind
- timestamp 范围
- dirty 状态
- generation 或版本
- residency：live tail 或 persisted store

LogicalLine 的 truth 是 logical boundary 与 cells/runs。projection segments 不能成为 persisted truth，也不能作为 logical line boundary 的唯一来源。

### 5.2 Persisted History Store

`persisted history store` 只保存 sealed logical lines。

- 不能以 visual row 作为主键或唯一边界。
- 不能只依赖 wrapped metadata 反推出 logical line truth。
- retention 必须以完整 logical line 为单位。
- append、truncate、reclaim 后必须保持 logical line id 与 generation 可验证。

### 5.3 Mutable Live Tail

`mutable live tail` 保存仍可能被终端行为修改的 logical lines。

它可以包含：

- open logical line
- sealed live logical line
- reclaimed persisted suffix
- shrink resize 后隐藏但仍可变的 logical line

`open/sealed` 与 `origin=live/reclaimed` 是正交属性。已 persisted 的 logical line 后续仍可能被 reclaim、修改、重新提交。

### 5.4 HistoryWindow

`HistoryWindow` 是 `HistoryTrack` 对 TUI 和协议层输出的 authoritative projection。

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

## 6. HistoryTrack 输入事件

`HistoryTrack` 的输入只能是历史语义事件。

### 6.1 事件路由与事务边界

- 每个 PTY / VT 输入批次必须形成一个有序事务。
- 一个事务可以同时包含多条 `HistoryEvent` 和多条 `LiveSurfaceMutation`。
- 同一事务内 `HistoryEvent` 与 `LiveSurfaceMutation` 使用相同 input sequence。
- 事务提交顺序由 `EventRouter` 保证，不能由 `LiveSurfaceTrack` 的当前状态反推。
- 如果某个输入既影响当前屏幕又影响历史，必须在同一事务中显式产生两侧事件。

### 6.2 必须支持的事件

- `write-primary-cells`：写入 primary 当前 open logical line 的 cell runs 或 cells。
- `seal-logical-line`：硬换行、滚出可变区或明确封口边界导致 logical line sealed。
- `mutate-live-tail`：光标移动、覆写、erase、clear 局部可变内容等对仍可变 logical line 的修改。
- `reset-live-tail`：clear screen 或明确重置场景清空当前 primary mutable live tail，但不创建 persisted history。
- `reclaim-persisted-suffix`：grow resize 或其他可变性恢复场景按完整 logical line reclaim persisted suffix。
- `hide-live-tail`：shrink resize 把当前 screen 中不可见但仍可变的 logical lines 转入 hidden live tail。
- `force-seal-all`：process exit 时强制封口 primary mutable live tail 并提交 persisted history。
- `switch-alt-screen`：进入/退出 alt-screen 时冻结/恢复 primary history，不把 alt 内容写入 primary history。
- `non-history-boundary`：attach、reattach、bootstrap、recovery、full replace、clear screen、resize 等非历史创建事件只影响状态边界，不凭空创建 persisted history。

`non-history-boundary` 只能表达事务边界和 stale signal，不能替代 `mutate-live-tail`、`reset-live-tail`、`reclaim-persisted-suffix`、`hide-live-tail`、`force-seal-all` 等具体 history mutation 事件。

### 6.3 resize 语义

- resize 本身不是历史创建事件。
- resize 本身不是历史重写事件。
- grow resize 只允许触发 `reclaim-persisted-suffix`，并且必须按完整 logical line reclaim persisted suffix。
- shrink resize 只允许触发 `hide-live-tail`，表达 `screen -> hidden mutable live tail`。
- 不允许 reclaim 半条 logical line。
- resize 后 active history window 必须通过 new generation 或 new token 失效。
- resize 后 TUI 应通过 latest replace 获取新的 authoritative history window。
- resize 不得从 resized snapshot 或 grid viewport 重建 committed history。

### 6.4 clear / erase / full replace 语义

- 局部 erase 或光标覆写如果作用于仍可变 logical line，必须进入 `mutate-live-tail`。
- clear screen 可以清理 `LiveSurfaceTrack` 当前屏幕，但不得凭空创建 persisted history。
- clear screen 如果明确影响 primary mutable live tail，必须以 `mutate-live-tail` 或 `reset-live-tail` 表达，不能从清屏后的 snapshot 反推历史。
- clear scrollback / full replace 不得把当前 screen 内容作为新 persisted history。
- full replace 可以重建 `LiveSurfaceTrack` 实时投影，但只能向 `HistoryTrack` 发送 `non-history-boundary` 或明确的 live-tail mutation。
- 任何 clear/full replace 后的 active history window 必须根据 token/generation 规则失效或通过 latest replace 重置。

### 6.5 alt-screen 与 process exit 组合语义

- 进入 alt-screen 时 primary `HistoryTrack` 冻结。
- alt-screen 期间输出只影响 alt `LiveSurfaceTrack`，不进入 primary history。
- 退出 alt-screen 时恢复 primary surface，不把 alt 内容混入 primary history。
- process exit 是 primary history 的 mutability 边界。
- process exit 时 primary mutable live tail 必须先 `force-seal-all`，再提交 persisted history。
- 如果 process exit 时仍在 alt-screen，alt 内容直接丢弃；primary mutable live tail 仍按 process exit 规则 force seal。
- force seal 与 persisted append 必须在同一 history transaction 中产生可验证 generation 变化。

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
- 实时 snapshot/grid viewport 可以保留，但只能属于 `LiveSurfaceTrack`。
- 两轨不能通过 snapshot、wrapped rows、visual rows 传递历史 truth。
- resize 不能创造 history，grow resize 必须按完整 logical line reclaim persisted suffix。
- clear screen、full replace、attach、reattach、bootstrap、recovery 不能凭空创建 persisted history。
- alt-screen 不写 primary history。
- process exit 必须 force seal primary mutable live tail。
- TUI 不得用本地深度计数、snapshot totals、LoadedRows、row count 推断 older/latest 接纳规则。

## 9. 第一阶段范围

第一阶段只做：

- 纯内存 `HistoryTrack`
- 明确的 `HistoryEvent` 类型
- logical line store 数据结构
- mutable live tail 数据结构
- persisted store 的内存实现
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

### 10.2 Ledger 退化为 append-only 日志

风险：只记录 sealed lines，无法表达 reclaimed suffix、覆写、erase、process exit force seal。

应对：第一阶段就实现 mutable live tail，并用覆写、resize reclaim、process exit harness 固定语义。

### 10.3 过早绑定 protocol

风险：为了兼容现有 wire 字段，把 core-v2 内部模型设计成 row-level contract。

应对：先稳定 domain model，再写 protocol adapter。

### 10.4 实时 surface 被误认为 history source

风险：TUI 或协议为了滚动继续使用 snapshot/grid viewport。

应对：TUI-v3 只接 `HistoryWindow`，legacy snapshot/grid viewport 文档明确为 live surface 兼容接口。

## 11. 推荐落地顺序

1. 创建 `termx-core-v2/` 与 `termx-tui-v3/` 最小模块。
2. 在 core-v2 中定义 `HistoryTrack`、`LiveSurfaceTrack`、`HistoryEvent`、`LogicalLine`、`HistoryWindow` 类型。
3. 实现纯内存 logical line store 与 mutable live tail。
4. 补 core-v2 logical line harness：普通输出、换行、自动折行、覆写、局部 erase、clear screen、full replace、attach、reattach、bootstrap、recovery、resize、alt-screen、exit-while-alt、process exit、reclaim 后修改再提交。
5. 实现 HistoryWindow projection 和 pagination，覆盖 resize 后 latest replace、旧窗口 stale guard、clipped span、older cursor 与 logical boundary。
6. 在 tui-v3 中实现 fake source + historyview store harness，覆盖 latest replace、older prepend、empty older exhausted、stale response、selection clipped span。
7. 补 protocol round-trip 和 adapter harness。
8. 稳定后再进入真实入口迁移。
