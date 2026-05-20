# Terminal History 分层 Backing Grid 设计文档

本文档记录一个面向 TermX 的后续架构方向：在不放弃 file-backed 无限历史、恢复能力、多端共享和 WebRTC structured viewport 的前提下，把 terminal history 语义进一步收敛到更接近 tmux 的“单真相源”模型。

本文档不是对 tmux 实现的逐字移植，而是对 tmux terminal history 语义的分层化适配。

相关文档：

- `docs/terminal-history-grid-decision.md`
- `workflow.md`
- `docs/workflows/archive/grid-history-rebase-workflow.md`

## 1. 背景

当前 TermX terminal history 主路径已经回到 parsed grid/cell history，而不是 raw PTY journal replay。现状可概括为：

- live screen 真相在 `vterm`
- committed history 真相在 `terminalGridStore`
- 两者通过 `WriteDamage.ScrollbackAppend`、wrapped metadata、snapshot overlap trimming、resize plan/fallback 进行桥接

这条路线已经解决了大量问题：

- `1000` 行 stress 可以稳定翻到 `000000`
- retention 已具备 logical-line / byte / age policy
- `ScrollbackLogicalTotal` 已成为可验证的附加调试状态
- shared floating attach/close/re-entry 的历史回退问题已经收敛

但仍有一个长期结构性问题：

- 当前模型存在两份 terminal content 真相源：
  - `vterm` live screen
  - `terminalGridStore` persisted history
- resize、copy-mode、attach/re-entry、history/screen boundary 的很多 bug，本质上都是两份真相在边界处需要推断和拼接造成的

tmux 在 terminal history 这一层更稳定，关键原因不是它“功能更少”，而是它在 pane history 模型上更接近：

- 一个 backing grid
- 一个 row address space
- history 和 visible screen 是同一套 canonical rows
- resize 是 grid mutation，不是 screen-diff inference

TermX 不能直接照搬 tmux 的全内存实现，因为我们还需要：

- file-backed 深历史与 crash recovery
- 多 observer viewport
- remote/App/WebRTC structured viewport
- row timestamps / row kinds 等产品元数据

因此，目标不是“变成 tmux”，而是：

- 在 terminal history 语义上更像 tmux
- 在存储和 transport 上保留 TermX 的产品能力

## 2. 问题陈述

当前模型的长期痛点：

### 2.1 Resize 仍然存在 bridge/fallback 复杂度

虽然 tail-plan 已经成为 covered cases 的首选 authority，但对于 no-plan shrink 场景，我们仍然需要基于 before/after screen 做 defensive matching 才能推断哪些 rows 应当进入 history。

这意味着：

- resize authority 仍然部分属于 `vterm` 的 post-resize inference
- 而不是 core history boundary 上的 canonical row movement

### 2.2 History / screen 边界仍需要显式拼接

当前 `Snapshot(offset=0)` 仍可能需要做 overlap trimming，说明：

- history 和 live screen 不是一个统一地址空间
- 边界所有权还不够明确

### 2.3 Copy-mode 坐标语义还不是完全 canonical

copy-mode 已经有 generation / row id / loaded depth / bounded window 语义，但 frozen snapshot + paged materialization 仍然不是一个真正的 backing model。

### 2.4 深历史和 resize 的耦合方式不理想

对于很久远的历史，真正不希望因为每次 PTY resize 就物理重写整个 persisted history。但如果“真相”定义成物理 visual rows，则 resize 很容易逼出全量重排压力。

## 3. 核心目标

本方案目标：

1. 在 core terminal history 边界引入一个更单一的 canonical content model。
2. 让 resize、copy-mode、viewport、retention、stale-page 语义围绕这套 canonical model 收敛。
3. 保留 file-backed 深历史，不因为 observer width 或频繁 resize 重写冷历史。
4. 保留一个真实 PTY size 和多个 observer viewport 的产品模型。
5. 不把 UI history 再退回 raw PTY journal replay。

## 4. 非目标

本方案明确不做：

1. 不把 TermX 变成 tmux 的 pane/window/session 产品模型。
2. 不要求所有历史常驻内存。
3. 不要求每次 resize 物理重写全部 persisted history。
4. 不重新引入 raw PTY journal 作为 UI history query path。
5. 不要求 observer viewport 宽度成为历史真相的一部分。

## 5. 总体设计

核心思想：把 terminal history 拆成两层。

### 5.1 热层：Hot Grid

Hot Grid 是一个 mutable backing grid，语义上最接近 tmux 当前 pane 的 backing grid。

它包含：

- visible screen
- 最近一段已提交 history
- 当前 canonical row identity / generation 的活跃工作集

它负责：

- 实时 PTY 输出吸收
- resize grid mutation
- history/screen boundary 的直接管理
- copy-mode 的近距离交互
- observer 的最新视口投影

### 5.2 冷层：Cold Logical Store

Cold Logical Store 是一个 file-backed、结构化、append-mostly 的历史存储层。

它不存“当前宽度下的 visual rows 真相”，而存：

- 完整 logical line
- 结构化 cells/runs
- 样式、链接、timestamp、rowKind 等元数据

它负责：

- 深历史持久化
- crash recovery
- retention rewrite
- 对 older pages 的按需 materialization

### 5.3 核心边界

Hot / Cold 边界必须满足：

- Cold 只接收完整 logical lines
- 不能在 logical line 中间切边界
- Hot 中允许存在未完成 logical line
- Cold 中不应长期保留“逻辑行前缀，后缀仍在 Hot”这种不完整状态

## 6. 数据结构

以下是建议数据结构，不是最终 API。

### 6.1 Canonical Row Identity

```go
type TerminalHistoryGeneration uint64

type CanonicalRowID uint64

type CanonicalRowRef struct {
  Generation TerminalHistoryGeneration
  RowID      CanonicalRowID
}
```

要求：

- `RowID` 对 terminal 内部 canonical row space 单调递增
- retention drop 后 `Generation` 变化，用于 stale-page reject
- observer / runtime / copy-mode 统一使用这套坐标语义

### 6.2 Hot Grid

```go
type HotGrid struct {
  Cols int
  Rows int

  // 按 canonical row 顺序排列，包含 history tail + visible screen。
  Rowset []HotGridRow

  // 当前 visible screen 起点在 Rowset 中的位置。
  VisibleStart int

  Generation TerminalHistoryGeneration
  NextRowID   CanonicalRowID
}

type HotGridRow struct {
  Ref       CanonicalRowRef
  Cells     []Cell
  Timestamp time.Time
  RowKind   string
  Wrapped   bool
}
```

语义：

- `Rowset[:VisibleStart]` 是 hot history
- `Rowset[VisibleStart:]` 是 visible screen
- resize 时，mutation 发生在整个 `Rowset` 上

### 6.3 Cold Logical Store

```go
type ColdLogicalStore struct {
  // page/index/meta 文件布局
}

type LogicalLineRecord struct {
  FirstRowID CanonicalRowID
  RowCount   int

  CellsOrRuns []CompactCellOrRun
  Timestamp   time.Time
  RowKind     string
}
```

说明：

- Cold 层存的是逻辑行级 record
- 仍然保留 row identity 信息，用于回放为 canonical rows 时映射回统一地址空间
- 逻辑行展开成 visual rows 时，使用当前 canonical width 或读取契约要求的 width

### 6.4 Observer Viewport

```go
type ViewportWindow struct {
  TerminalID  string
  Generation  TerminalHistoryGeneration
  BeforeOffset int
  Limit        int

  // Rows 是当前 projection 结果，不是持久真相。
  Rows []ProjectedRow

  TotalCommittedRows int
  TotalLogicalLines  int
  LoadedCommittedRows int
  FirstRowID CanonicalRowID
  LastRowID  CanonicalRowID
}
```

说明：

- committed row totals 继续作为分页主语义
- logical totals 是附加状态，不替代 committed-row paging

## 7. 写入路径

### 7.1 实时 PTY 输出

实时输出先进入 Hot Grid。

流程：

```text
PTY bytes
  -> emulator parses bytes
  -> core updates Hot Grid screen/hot-history rows
  -> observers consume projected visible screen
```

与当前不同点：

- 不再以“先让 `vterm` 产出 `ScrollbackAppend`，再补到 file-backed history”作为唯一主路径
- 主路径应是“core 更新 canonical Hot Grid，再决定哪些 rows 离开 hot visible region”

### 7.2 Flush / Compact 到 Cold

当 Hot Grid 达到某个容量阈值时，触发 compact。

建议规则：

- 不是按“前 X 个 visual rows”切
- 而是按“最老的一批完整 logical lines”切

伪代码：

```go
func FlushCold(grid *HotGrid, cold *ColdLogicalStore, targetRows int) error {
  lines := collectOldestCompleteLogicalLines(grid, targetRows)
  records := compactLogicalLines(lines)
  cold.Append(records)
  grid.DropCommittedRows(lines.RowCount())
  grid.Generation++
}
```

要求：

- 不能把一个 still-open logical line flush 到 Cold
- 不能在 wrap continuation 中间切断 flush 边界

## 8. 读取路径

### 8.1 近历史读取

优先从 Hot Grid 读。

优点：

- 当前 screen 和最近 history 的 row identity 统一
- copy-mode / selection / top/bottom movement 更稳定
- resize 后无需通过 before/after content matching 猜边界

### 8.2 深历史读取

当 viewport 请求更老数据时：

1. 从 Cold Logical Store 读取 older logical lines
2. 用 terminal canonical width reflow 成 committed/projection rows
3. 映射回统一的 committed-row coordinate contract

注意：

- Cold 层读取返回的是 projection，不是改写 Cold 真相
- observer width 仍然不应成为历史真相来源

## 9. Resize 合约

### 9.1 目标

resize 必须成为 grid mutation，而不是 screen-diff inference。

### 9.2 Hot Grid Resize

resize 时：

1. Hot Grid 对 `Rowset` 做 reflow
2. 重新计算 `VisibleStart`
3. 自然得到：
   - 哪些 rows 仍属于 visible screen
   - 哪些 rows 落入 hot history

因此：

- 不再需要用 before/after visible-content matching 猜测 history append
- 也不再需要把 resize 主语义押在 `vterm.ResizeWithDamage` 推断上

### 9.3 Cold Store Resize

Cold Store 不应因为 resize 被全量物理重写。

这意味着：

- resize mutation 只发生在 Hot Grid
- Cold 的 logical-line truth 保持稳定
- 从 Cold 读取 older history 时按当前 canonical width 做 projection

这是本方案和 tmux 原始内存 grid 的关键区别。

## 10. Copy Mode 合约

copy-mode 应从“frozen materialized snapshot”进一步收敛到“canonical row identity + bounded page materialization”。

建议：

- cursor/selection anchor 使用 `CanonicalRowRef`
- materialized page 只是当前渲染窗口
- page 被丢弃后，cursor/selection 坐标仍然有效

这样可以减少：

- 把 `len(snapshot.Scrollback)` 当坐标真相
- page trim 后 selection / top paging / attach re-entry 易错

## 11. Retention 合约

retention 仍发生在 committed history truth 上，但建议：

- Hot / Cold 分层下，优先从 Cold oldest logical lines 开始 drop
- Hot 层只保留最近窗口所需
- logical-line / byte / age 三类 budget 仍然共存
- smallest budget wins

关键点：

- retention drop 必须更新 generation
- stale page / stale cursor / stale selection 需要统一 reject / refresh

## 12. Crash Recovery

Crash recovery 是 tmux 原始模型没有的 TermX 需求。

建议：

- Hot Grid 不要求完整持久化每一步 mutation
- Cold Logical Store 作为 durable truth
- restart 时：
  - 先恢复 Cold
  - 再恢复最近一次 durable screen / latest snapshot checkpoint
  - 如有必要，把 checkpoint 区域重建为新的 Hot Grid

也就是说：

- Hot 更像 working set
- Cold 更像 durable history truth

## 13. 对多端共享的影响

这是本方案的正收益，而不是阻碍。

因为 observer 统一变成：

- 看同一套 canonical history row space 的不同 projection

结果：

- floating pane、tiled pane、mobile App、remote-ui 不再各自拼接 history/live boundary
- stale-page 语义更一致
- attach / re-entry / restore 的 loaded depth 更容易统一

## 14. 为什么不能直接照搬 tmux

tmux 的优势：

- 一个 backing grid
- 一个 row address space
- resize 直接 mutate grid

tmux 不需要处理的问题：

- file-backed 深历史
- restart recovery 的 page/index commit semantics
- remote/App/WebRTC structured viewport
- observer 只投影而不改写 truth

因此，直接照搬 tmux 的“全量内存 grid”实现会带来两个大问题：

1. resize 可能逼迫深历史物理重写
2. durable history 和 in-memory mutation 边界不清

所以本方案是：

- 学 tmux 的单真相源语义
- 不学 tmux 的全量内存实现

## 15. 迁移计划

建议分阶段推进。

### 阶段 1：强化 canonical row identity/generation

目标：

- 让 runtime/app/remote-ui/copy-mode 完全围绕 canonical row identity 说话
- 进一步弱化 materialized snapshot length 的语义地位

### 阶段 2：引入 Hot Grid working set

目标：

- 把当前 visible screen + recent history 放进一套 unified mutable row set
- 让 resize/copy-mode 主要操作这个 working set

### 阶段 3：定义 Cold Logical Store

目标：

- 新增逻辑行级落盘结构
- 从 Hot 最老完整 logical lines 做 compact / flush

### 阶段 4：替换 resize authority

目标：

- 把 resize 的主 authority 从 `vterm` damage inference 转移到 Hot Grid row movement
- `ResizeWithDamage` 退化成 screen projection / compatibility 层

### 阶段 5：替换 copy-mode backing model

目标：

- selection / cursor / paging 全部基于 canonical row identity

### 阶段 6：把 overlap trimming 退成真正 fallback

目标：

- `Snapshot(offset=0)` 不再长期依赖 boundary overlap trimming

## 16. 风险

### 16.1 实现复杂度会上升

短期内这是一轮中型重构，不是简单 bugfix。

### 16.2 Hot/Cold flush 边界容易出错

如果 flush 错切到未完成 logical line，会重新引入跨层拼接问题。

### 16.3 需要重新定义部分协议坐标

虽然 committed-row paging contract 可以保留，但内部数据结构会更复杂，需要更严格的 generation / row id 语义。

### 16.4 resize / recovery 迁移期可能引入新 bug

这类 bug 的形式会从“内容匹配错”变成“identity 映射错”。

## 17. 开放问题

1. Cold Logical Store 的 durable unit 是否应当是：
   - 纯 logical line
   - committed-row group
   - 或两者混合索引
2. Hot Grid 的 checkpoint 是否需要单独持久化文件，还是完全由 latest snapshot 构建
3. Cold -> viewport projection 时，row id 应如何映射到 committed-row contract 上，才能既保持分页兼容，又不让 resize 后 older pages 的 identity 失真
4. `HistoryReplay` 是否继续长期保留，还是最终只做 compatibility/debug

## 18. 建议结论

建议采用：

- **Hot mutable backing grid**
- **Cold file-backed structured logical store**
- **统一 canonical row identity / generation**

不建议采用：

- 直接把 tmux 全量内存 grid 原样照搬到深历史与持久化路径

本方案兼容：

- file-backed 深历史
- crash recovery
- one real PTY size + many observer viewports
- remote/App/WebRTC structured viewport

本方案的主要代价不是产品能力冲突，而是核心 terminal history 模型的一次中型重构。
