# termx-tui-v3 历史视图架构设计

## 1. 定位

`termx-tui-v3` 是 core-v2 authoritative history window 的消费者，不拥有 committed history truth。

tui-v3 的目标是把 copy mode、滚轮、page up/down、selection、older prepend、latest replace 与 stale response guard 都收敛到 core-v2 返回的 `HistoryWindow` 上。它不得从 local VTerm scrollback、snapshot、grid viewport、wrapped rows 或 visual rows 反推出历史真相。

## 2. 与 core-v2 的边界

- core-v2 负责 `LogicalLineStore`、`CommittedHistoryIndex`、`MutableFrontier`、`StorageBackend` 与 `HistoryWindow`。
- tui-v3 只保存 core-v2 返回的 authoritative window 和交互态。
- latest window 使用 replace。
- older window 使用 prepend。
- replace/prepend op 由 core-v2 决定，tui-v3 不自行解释 response。
- stale guard 只能使用 core-v2 返回的 token、generation、cursor 与 logical boundary。
- legacy snapshot/grid viewport 如果仍存在，只能用于 live renderer，不能进入 history path。

## 3. 主要对象

### 3.1 historyview.Store

`historyview.Store` 保存当前 authoritative history window 与交互态。

至少包含：

- terminal id
- window token
- generation
- rows
- logical line spans
- stable logical line ids
- cursor
- has more
- viewport top
- selection
- pending request token
- exhausted older state

Store 不维护本地 committed depth，不根据 row count、LoadedRows、snapshot totals 或本地 scrollback 判断 older/latest 是否有效。

### 3.2 historyview.Source

`historyview.Source` 是 TUI 向 core 请求 history window 的抽象。

第一阶段使用 fake source 做 harness。后续通过 protocol adapter 接入真实 core-v2。

### 3.3 copymode

copy mode 只消费 `historyview.Store`。

- 不读 local VTerm scrollback。
- 不用 wrapped rows 拼 logical line。
- selection 依赖 authoritative line spans 和 stable logical line ids。
- clipped span 不得被当作完整 logical line。

### 3.4 renderer

- history renderer 只绘制 authoritative projection。
- live renderer 可以继续绘制实时 snapshot 或 grid viewport。
- 两条渲染路径不得共享 history truth。

## 4. 输入语义

- 鼠标滚轮上滚进入 copy mode，并请求 latest history window。
- page up 或 viewport 到顶时请求 older window。
- older response 只能在 token、generation、cursor、logical boundary 匹配时 prepend。
- page down 只在当前 authoritative window 内移动；如需回到最新状态，必须通过明确 latest replace。
- resize 后必须丢弃旧窗口或等待 core-v2 latest replace。
- selection 跨 logical line 时使用 authoritative line span，不用 visual row 自行拼接。

## 5. 禁止事项

- 不得引入本地 committed history depth 状态机。
- 不得引入本地 history exhausted truth，exhausted 只能绑定 core response 和请求 cursor。
- 不得引入 copy mode frozen snapshot 分页合并路径。
- 不得把 local VTerm scrollback 当历史滚动主路径。
- 不得使用 snapshot totals、LoadedRows、row count、HistoryGeneration 空值接纳 response。
- 不得通过 wrapped rows 自行恢复 logical line。

## 6. 第一阶段范围

第一阶段只做：

- `historyview.Store`
- fake `historyview.Source`
- latest replace harness
- older prepend harness
- stale response harness
- selection clipped span harness

第一阶段不做：

- Bubble Tea app 完整接入
- 真实 protocol adapter
- 旧 `tuiv2/` 原地修补
- local VTerm scrollback 迁移
