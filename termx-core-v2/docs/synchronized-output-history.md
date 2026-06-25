# synchronized output 与 primary frame history 设计

## 1. 背景

Codex、Claude Code、opencode 等现代终端应用不一定进入 alt-screen。它们经常在 primary screen 上使用光标定位、清屏、清行、scroll region、reverse index 和 synchronized output 组合来更新界面。

普通 shell 输出大多是 append 型：

```text
write text -> newline -> line scrolls out -> committed history
```

这条路径现在基本稳定。问题集中在 pseudo-TUI 输出：

```text
CSI ? 2026 h     begin synchronized update
CSI H / CSI J    定位、清局部屏幕
CSI r / RI       scroll region + reverse index
CSI K / text     在屏幕中间重写内容
CSI ? 2026 l     end synchronized update
```

用户在 live 模式看到的是同步更新结束后的最终 screen frame；进入 history/copy 后也应该看到同一批内容。当前实现已经逐步把很多控制序列迁到 shared vterm semantic projector，但仍然偏向逐条事件维护 `MutableFrontier`。对 synchronized output 来说，这会暴露中间状态、漏掉最终 screen rows，或者让 TUI 只能用入口锚点补救。

本设计把 `DECSET 2026` / `DECRST 2026` 作为终端语义事务处理：事务期间 vterm 继续处理所有控制序列，history 不把中间 repaint 逐条沉淀为用户历史；事务结束时，core 从同一 vterm write transaction 的最终 primary screen 状态生成 authoritative mutable frame。

## 2. 标准和事实标准来源

这不是 RFC 领域。终端控制语义主要来自 ECMA-48 / ISO 6429、DEC VT 系列手册、xterm control sequences 和现代终端事实标准扩展。

本设计依赖这些语义：

- ECMA-48 定义 CSI、C0/C1 control、字符成像设备控制函数这一基础层。
- xterm control sequences 记录 `CUP`、`ED`、`EL`、`SU`、`DECSTBM`、DEC private modes 等现代终端事实标准。
- `DECSET 2026` synchronized output 是现代终端扩展。Contour 的说明是：启用同步模式后，终端继续处理输入文本和控制序列，但渲染保持上一帧；禁用同步模式时，渲染器读取最新 grid buffer 状态。WezTerm 也把 `DECSET 2026` 描述为 hold rendering，reset 后 flush queued screen data。

参考：

- ECMA-48: https://ecma-international.org/publications-and-standards/standards/ecma-48/
- xterm control sequences: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html
- Contour synchronized output: https://contour-terminal.org/vt-extensions/synchronized-output/
- WezTerm escape sequences: https://wezterm.org/escape-sequences.html

## 3. 设计目标

- 不按进程名特判 Codex、Claude Code 或 opencode。
- 不让 TUI 通过 live snapshot、VTerm scrollback 或本地 cache 修补 history truth。
- 不把 synchronized output 内的中间 repaint 追加到 committed history。
- `history.window latest` 必须能返回当前 primary mutable frame，使进入 history/copy 的第一屏内容不丢。
- frame 内容可以是当前 terminal size 下的 screen-frame logical lines；它不计入 committed history depth，不参与 older committed pagination。
- 普通 shell append 输出继续走现有 logical-line-first 模型。
- alt-screen 继续按 alt-screen 规则处理，running alt-screen 内容不写 primary history。

## 4. 术语

### BSU / ESU

- BSU：begin synchronized update，`CSI ? 2026 h`。
- ESU：end synchronized update，`CSI ? 2026 l`。

### primary synchronized transaction

从 BSU 到对应 ESU 的 primary-screen 更新事务。事务内 vterm 继续消费所有 bytes；live 渲染可以被 host hold，但 core-v2 的 vterm state 必须持续更新。

### primary screen frame

ESU 时 primary screen 的最终可见内容。它来自同一个 vterm transaction 的最终 primary screen buffer，不来自 TUI、renderer、live snapshot fallback 或 xterm scrollback。

### frame logical line

为了让 copy/history 能消费当前 frame，core 把 primary screen frame 的可见 screen rows 转成一组 mutable logical lines。这些 line 是 current-frame projection，不是 committed transcript truth。

## 5. 权威边界

### owner

`termx-core-v2` 是唯一 owner。

- vterm 负责解释终端协议。
- EventRouter 负责把同一批 PTY bytes 变成 ordered semantic ops、live mutation 和 history event。
- `HistoryTrack` 负责 committed history、mutable frontier 和 latest/frozen history window。
- TUI 只消费 `history.window`，不参与生成 frame truth。

### 禁止的数据流

这些路径仍然禁止：

```text
TUI live rows -> history rows
LiveSurfaceTrack.Snapshot() -> HistoryTrack
vterm scrollback rows -> committed history truth
renderer frame -> copy/history source
process name -> history mode switch
```

### 允许的数据流

允许的是 EventRouter 在同一 PTY transaction 内发出显式 domain event：

```text
PTY bytes
  -> shared vterm write transaction
  -> ordered semantic ops + final primary screen frame
  -> HistoryEventReplacePrimaryFrame
  -> HistoryTrack mutable frontier
  -> history.window latest
```

这里的 frame 是 terminal semantic transaction 的输出，不是 live surface 回读结果。

## 6. 现有问题模型

当前逐条 semantic projector 对普通输出合适，但对 synchronized output 有两个风险：

1. 中间 repaint 暴露

   程序在 BSU/ESU 内可能先清掉一片区域，再分批写 card、输入框、footer。逐条写入 `MutableFrontier` 时，history 可能短暂或最终保留中间态。

2. 最终 screen frame 丢失

   复杂组合如 `DECSTBM + RI + CUP + ED0 + EL` 会让 screen ownership 发生块级变化。仅靠 ordered text/control event 维护 logical line，容易漏掉屏幕中间未按 append 顺序出现的 rows。

标准语义给出的修正方向是：synchronized output 的用户可见结果是 ESU 后的最终 screen buffer，而不是 BSU/ESU 内每一步中间状态。

## 7. 目标架构

### 7.1 新 domain event

建议新增一个历史事件：

```go
EventReplacePrimaryFrame
```

payload：

```go
type PrimaryFrameRow struct {
    ScreenRow int
    Cells     []Cell
    TailFill  *RowTailFill
    Used      bool
    Wrapped   bool
}

type HistoryEvent struct {
    Kind EventKind
    Rows [][]Cell              // 迁移期可先复用；后续收敛为 PrimaryFrameRows
    PrimaryFrameRows []PrimaryFrameRow
    PrimaryFrameSource PrimaryFrameSource
}
```

`PrimaryFrameSource` 只表达 primary-screen current-frame 来源，至少区分：

- `synchronized-output`
- `primary-fullscreen-repaint`
- `synchronized-output-exit-flush`

第一阶段可以先复用 `Rows [][]Cell`，但文档层面必须明确它不是 committed visual-row truth，而是 current-frame replacement payload。

alt-screen current/final frame 是另一条既有策略，不走 `ReplacePrimaryFrame`。running/exit alt-screen 仍不写 committed primary history；如果保留 alt-screen 当前画面，必须通过 alt-screen 专用 transient frame 处理，不能混入 primary frame frontier 或 committed depth。

### 7.2 发布隔离

synchronized output 必须有发布隔离，不能让 BSU/ESU 中间态进入 `history.window latest`。

`HistoryTrack` 至少区分两份 frame 状态：

```text
published primary frame
  - 已经进入 MutableFrontier
  - latest/frozen 可以看见
  - 代表上一次完整发布的 primary frame

pending synchronized frame
  - 只属于当前 BSU/ESU transaction
  - 不进入 LogicalLineStore
  - 不进入 HistoryWindow
  - ESU 或 process exit flush 时才替换 published frame
```

BSU 只打开 pending frame staging，不改 published frame。BSU 到 ESU 之间，`CUP`、`ED0/ED1`、`EL`、scroll region 内部重排、局部 repaint 等 visible-frame mutation 只更新 core-owned vterm 当前 primary screen 和 pending frame dirty 标记；`latest` 继续返回上一份 published frame，或者在没有 published frame 时只返回 committed tail。

事务内仍允许即时处理不可逆历史语义：

- vterm 明确上报的 primary scroll-out rows 可以按现有 ownership 规则提交。
- `ED3` / clear scrollback 只作为 scrollback soft boundary，不裁剪 committed index；真正删除历史仍必须走显式 `truncate-committed-history`。
- `CSI 2J` 这类 page-break 语义可以提交 BSU 前已经存在的 frontier，但清屏后的新 visible frame 仍停留在 pending frame，直到 ESU 发布。

也就是说，发布隔离只隔离“当前可见 primary frame 的中间 repaint”，不阻止真实离开 screen ownership 的 transcript/history 事件进入 committed history。

### 7.3 FrameBuffer / FrameFrontier

在 `HistoryTrack` 内部引入 frame 专用边界，或在现有 `MutableFrontier` 中增加 frame membership 标记：

```text
CommittedHistoryIndex
MutableFrontier
  - normal logical lines
  - published primary frame logical lines
PendingPrimaryFrame
  - not visible to latest/frozen
```

规则：

- `ReplacePrimaryFrame` 是一个原子 history transaction。
- 它先删除上一份 published primary frame logical lines。
- 再把 final primary screen rows 转成新的 frame logical lines。
- frame logical lines `Committed=false`。
- `TotalLines` 仍只统计 `CommittedHistoryIndex`。
- latest/frozen window 可以包含 frame logical lines。
- older window 默认只基于 committed lines，不从 frame lines 继续分页。
- 如果已有 frozen snapshot 引用了旧 frame line，替换时必须按 line-level copy-on-write 保留旧版本，不能让旧 copy 会话看到新 frame。

### 7.4 与 primary screen ownership 的关系

现有 `primaryScreenLineMap` 仍用于普通输出的 commit 判定。同步帧需要更高层的 frame replacement：

- BSU 不自动 commit。
- 如果同步事务内出现真实 primary scroll-out，仍按 ordered semantic ops 提交已经离开 ownership 的 committed lines。
- ESU 的 final frame replacement 只覆盖 published current visible frame，不倒推 committed history。
- frame replacement 会更新 screen-row ownership，让 latest window 的 current frame 顺序等于 final screen top-to-bottom 顺序。

### 7.5 与 page-break 的关系

BSU 本身不是 page-break。下面这些语义才可能触发 page-break 或 frame replacement：

- `CUP 1;1 + ED0/ED2`：典型 full/current page repaint。
- `DECSTBM + RI`：常见 TUI 在当前区域插入/重排 frame。
- ESU：事务结束，产生 final primary frame。

第一版建议：

- 事务内仍执行 ordered semantic ops，保留 scroll-out 和 clear-scrollback 等真实历史语义。
- current-frame repaint ops 只更新 pending frame，不修改 published frame。
- ESU 时统一执行 `ReplacePrimaryFrame`，用最终 primary screen state 重建 current frame。
- 如果事务没有改变 primary screen，ESU 不产生 frame replacement。

### 7.6 frame logical line 构造

把 final primary screen rows 转成 logical lines时：

- 空白且无 style footprint 的 row 可以跳过，除非它位于两个非空 frame rows 之间并且需要保持 UI 空行布局。
- 有 styled blank footprint 的 row 保留 `TailFill`，但不把整行 default blank 写成 text payload。
- 每个 screen row 默认变成一条 sealed frame logical line，但这些 line 必须带 `Origin=primary-frame` / `FrameID` / `Ephemeral=true` 这类等价元数据。
- wrapped row 只作为 terminal projection 信息；frame line 不进入 committed history，所以不需要和普通 transcript line 强行合并。
- line id 每次 frame replacement 可以重新分配。frame line id 只要求在同一个 frozen/latest window 内稳定。

这和普通 transcript logical line 不冲突：普通 history 仍以真实 logical line 为单位；frame logical line 是 current UI frame 的 mutable projection。它不参与 older cursor、不计入 committed depth、不作为长期 storage truth。只有 process exit force commit 时，当前 published frame 才会在同一个 history transaction 内转成普通 committed logical lines。

frame line force commit 后，copy/search 文本以转成普通 logical line 的 cell runs 为准；`ScreenRow` 只服务 current-frame replacement，不保留为 committed cursor、older boundary 或 storage 主键。

### 7.7 process exit force commit

process exit 语义不留开放解释：primary `MutableFrontier` 必须 force commit。

规则：

- 如果退出时有 pending synchronized frame，先从 core-owned vterm primary screen 生成 `ReplacePrimaryFrame(source=synchronized-output-exit-flush)`。
- 然后把当前 published frame 转成普通 committed logical lines，只提交最终 frame 一次。
- frame 转 committed 时清除 ephemeral/frame membership，分配或稳定普通 logical line id，进入 `CommittedHistoryIndex`。
- 这个 force commit 与 lifecycle marker 必须在同一个 history transaction 中产生 generation 变化。
- 如果退出时仍在 alt-screen，alt 内容只按 alt-screen transient current-frame 策略处理，不 force commit；primary frame force commit 只作用于 primary-screen frontier。

这条规则避免两种错误：运行中 repaint 不会污染 older history，进程退出后用户仍能在历史里看到最终 primary screen。

### 7.8 resize、attach 和 reattach

resize、attach、reattach 都不能让 frame committed。

- resize 只使 active latest/frozen token 或 generation 失效；already frozen frame 通过 copy-on-write 保持旧版本。
- 更准确地说，resize 会让 active latest window 失效；已经创建的 frozen snapshot token、committed upper bound 和 older boundary 不变，旧 frame 版本通过 copy-on-write 继续服务该 copy 会话。
- 如果 resize 发生在 pending synchronized transaction 内，pending frame 继续隐藏，ESU 按 resize 后的 core-owned vterm primary screen 发布。
- 如果 resize 后应用没有输出新 frame，latest 可以继续返回上一份 published frame projection；下一次 primary repaint 或 ESU 再替换它。
- older pagination 仍只走 committed index，不从 frame rows 推进 cursor。
- attach/reattach 不创建 frame、不提交 frame，只重新请求 authoritative latest window。

## 8. EventRouter 流程

### 8.1 普通输出

```text
PTY bytes
  -> shared vterm write
  -> ordered semantic ops
  -> write/lf/cr/cup/el/ed/scroll HistoryEvents
  -> HistoryTrack logical-line frontier
```

行为不变。

### 8.2 synchronized output

```text
BSU (?2026h)
  -> mark transaction synchronized
  -> open pending primary frame staging
  -> continue processing bytes normally

middle ops
  -> vterm state changes
  -> irreversible scroll-out/clear-scrollback may update committed history
  -> visible-frame repaint stays pending and is not exposed

ESU (?2026l)
  -> capture final primary screen rows from same vterm transaction
  -> emit EventReplacePrimaryFrame(source=synchronized-output)
  -> latest window includes final frame
```

关键点：capture 发生在 shared vterm write transaction 结束点或 ESU segment 结束点，不能由 TUI 后续向 core 再请求 live snapshot 来补。

### 8.3 未闭合 synchronized output

如果程序开启 BSU 后崩溃或长时间不关闭：

- vterm 仍然持有最新 screen state。
- live renderer 可按终端策略超时或继续 hold；core history 不应无限等待。
- process exit boundary 应强制 flush 当前 synchronized frame：
  - 如果在 primary screen，生成 `ReplacePrimaryFrame(source=synchronized-output-exit-flush)`。
  - 然后按 process exit 规则 force commit 当前 published frame。

第一版可以只在 process exit 强制 flush，timeout 先作为后续项。

## 9. TUI contract

TUI 不需要知道 `DECSET 2026`。

进入 copy/history 时：

```text
TUI -> history.window latest
core -> committed tail + current primary frame
TUI -> render authoritative rows
```

TUI 可以继续保留 `EnteringLive` 作为等待态显示，但不应靠它修补 missing rows。若 latest window 没有 current frame，那是 core bug。

Row ownership 仍有价值：

- committed rows：普通历史。
- live-tail / frame rows：当前 primary frame。

TUI 的入口锚点可以用 ownership 选择第一屏位置，但不能决定哪些内容存在。

## 10. Protocol contract

第一阶段不需要改 wire 字段，只要现有 `HistoryWindow` 能表达：

- `Rows`
- `Lines`
- `RowLineIDs`
- `RowInLine`
- `RowOwnership`
- `LogicalTotal`
- `Generation`
- `Token`

如果后续需要更明确区分 frame rows，可以新增 protocol row ownership：

```text
live-tail-frame
```

但不应把 synchronized mode、process name 或 renderer state 暴露给 TUI 作为逻辑分支。

## 11. 测试计划

### 11.1 vterm 标准语义 harness

覆盖：

- `CSI ? 2026 h/l` mode semantic ops raw order。
- BSU/ESU 内 `CUP`、`ED0`、`EL`、`DECSTBM`、`RI` 的最终 screen buffer。
- ESU 后 screen rows 等于 expected final frame。

### 11.2 core domain harness

最小序列：

```text
shell prompt\ncodex --yolo\n
CSI ? 2026 h
CSI H CSI J
CSI 3;1H Update available
CSI 4;1H release note
CSI 7;1H OpenAI Codex
CSI 10;1H CSI J
CSI 10;1H > input
CSI 12;1H status
CSI ? 2026 l
```

断言：

- committed depth 只包含进入 frame 前的 shell lines。
- latest window 包含 final frame rows。
- update card、release note、input、status 都存在。
- latest/frozen 的 normalized frame text 必须覆盖 ESU 后所有非空或有 style footprint 的 primary screen rows；允许 reflow/样式表达不同，但文本不能缺。
- `Committed=false` 或 row ownership 表达 current frame。
- 不出现中间 stale rows。
- older pagination 不返回 frame rows，不把 frame 计入 `LogicalTotal`。

### 11.3 分批 ESU harness

Codex 的真实输出可能分多次 write：

```text
chunk 1: BSU + partial frame
chunk 2: more frame
chunk 3: ESU
```

断言：

- chunk 1/2 不让 latest/frozen 暴露 incomplete frame；如果已有上一帧，只能继续暴露上一帧。
- chunk 3 后 latest/frozen 显示完整 final frame。
- chunk 1/2 内真实 scroll-out 仍可以进入 committed history。

### 11.4 无 ESU process exit harness

断言：

- 进程退出时 flush 当前 synchronized primary frame。
- 只把最终 published frame force commit 一次。
- 不把中间 repaint 逐条 committed。

### 11.5 非 BSU primary repaint harness

断言：

- 没有 `DECSET 2026` 的 primary middle-screen repaint 仍走 existing fullscreen/current-frame 规则。
- 该路径不能按进程名分支，也不能从 TUI live snapshot 回填。

### 11.6 resize / attach / alt-screen negative harness

断言：

- resize 只让 latest token/generation 失效，不提交 frame。
- attach/reattach 只重新请求 latest，不创建 committed history。
- running alt-screen 输出不通过 `ReplacePrimaryFrame` 写 primary history，只能作为 transient latest/frozen frame。

### 11.7 真实 Codex raw dump 回归

使用已有 tmux/Codex raw dump 或等价 fixture：

- live final primary screen 的 normalized text 与 `history.window latest/frozen` current frame normalized text 做 equality/subset 比较。
- 重点覆盖 update card、release note、输入框、status/footer 这些在截图里被吞的区域。
- 格式和行宽允许差异，但文本内容不能丢。
- 同一 harness 固定第一版 separator row 策略：保留有文本、有 styled footprint、或夹在两个保留 frame rows 之间的空白 row；丢弃其余 default blank rows。

### 11.8 TUI integration harness

使用 fake protocol 返回 committed shell rows + frame rows：

- 进入 copy/history 第一屏包含 frame head。
- copy/search 从 authoritative rows 取文本。
- 不读取 live snapshot/VTerm scrollback。

## 12. 迁移步骤

建议按下面切片执行：

1. `R201DC` 文档和 review：确认 synchronized output 设计边界。
2. `R201DD` vterm/core harness：先写 failing tests，覆盖 ESU final screen frame。
3. `R201DE` EventRouter frame event：从 shared vterm transaction 产出 `ReplacePrimaryFrame`。
4. `R201DF` HistoryTrack frame frontier：实现 frame logical lines，不计入 committed depth。
5. `R201DG` protocol/TUI smoke：保证 latest window 返回 frame rows，TUI 只消费 authoritative rows。
6. `R201DH` cleanup：收缩旧 primary fullscreen 特判，删除不再需要的 TUI live-match 补救。

## 13. 失败条件

实现后如果出现以下情况，说明模型仍错：

- latest window 不包含 ESU 后用户能看到的 primary frame 内容。
- BSU/ESU 中间态被 latest/frozen 暴露成半帧。
- history/copy 只能靠 TUI live snapshot 才不丢内容。
- Codex/opencode 被按进程名分支识别。
- 每次 repaint 都增加 committed depth。
- frame rows 进入 older pagination 或污染 `LogicalTotal`。
- stale input/status 出现在 older history。
- resize 或 attach/reattach 触发 frame committed。
- alt-screen running frame 写入 committed primary history 或增加 `LogicalTotal`。

## 14. 明确不做

- 不实现终端 renderer hold/flush；这是 live render 层策略，history 只需要理解 transaction boundary。
- 不把 synchronized output 当作新 protocol method 暴露给 TUI。
- 不保留旧 raw parser 作为并行语义 truth。
- 不为旧 storage/wire format 做兼容。
- 不把 running current frame 存成长期 committed transcript；只有 process exit force commit 会提交最终 frame 一次。

## 15. 未决问题

1. ESU 后是否保留空白 separator rows。

   建议第一版只保留有文本或 styled footprint 的 rows；若 Codex UI 依赖空行布局，再按 harness 增加“非空 rows 之间的空白 separator”规则。

2. nested BSU。

   DEC mode 是 mode state，不是栈。重复 BSU 应保持 enabled；第一个 ESU reset 后 flush。第一版可按 vterm mode state 处理，不单独计数。

3. timeout。

   标准资料没有统一 timeout。第一版不主动 timeout，只在 ESU 或 process exit flush。
