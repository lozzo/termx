# compact semantic history journal 设计

状态：R430 后本文只作为 R396-R418 高压 PTY 输出与 compact journal 热路径背景。
当前 production 正文 truth 已从 logical-line store 迁到 `ScreenHistoryBuffer`
physical rows；compact journal 在默认路径中只保留 backlog、boundary、meta/event
角色，不再拥有正文 payload truth。本文提到的 logical-line store 是 pre-screen-buffer
mutation-backed 路径说明，只能用于显式 legacy harness。

本文支撑根目录 `workflow.md` 的 R379+ 切片。它不替代
`history-logical-renderer-design.md` 的 history truth 定义。R396 之后，R372-R386 的
single `SemanticTap` 目标被真实 TUI 可见延迟推翻：live latest screen 与 history semantic
consumer 允许在 PTY 后分成两条 vterm 链路，但只有 live 链路能回写 terminal response，
history 链路只能产出 compact semantic journal；R430 后默认正文 truth 由 screen-backed
physical row store 持有，logical line 只在查询/复制阶段投影。

## 1. 背景

R372-R373 曾把真实 PTY 输出从 live/history 双 vterm consumer 切到 single `SemanticTap`：

```text
PTY bytes / resize
  -> single SemanticTap
       owns one termx-vterm parser/emulator, cursor, modes, alt state and terminal response
       maintains latest native screen
       emits semantic evidence for history
  -> live latest invalidation
  -> history backlog
```

这修复了双 vterm 带来的 parser state、resize order 和 terminal response owner 分裂问题。
但高压普通输出暴露出新的热路径问题：live 最新屏现在被 `WriteWithSemanticDamage`、
完整 semantic transaction 构造、deep copy、frame/source payload 和 history reducer 速度定住。

`f3c6070` 附近的 live surface 能跟上 100K 行 PTY 输出，是因为 live 只走
`SurfaceTrack.Write -> WriteForLatestFrame`：

```text
PTY bytes -> vterm latest screen update -> publish live invalidation
```

它不构造完整 history semantic transaction，不在每个 write 上 clone frame/snapshot，也不把
普通输出拆成大量 ordered ops 交给 logical-line reducer。因此它能接近 PTY 生产速度。

R396 的目标不是把 vterm scrollback 直接当 authoritative history，也不是让 history 回到
raw PTY parser fallback。目标是恢复 `f3c6070` 附近 live latest 的快速观察链路，同时保留
history 的语义 owner：同一 PTY bytes 分别进入 live `SurfaceTrack` 和 history semantic
consumer；live 只维护 latest native screen，history semantic consumer 产出 journal。R430
后默认最终 truth 是 core-v2 `ScreenHistoryBuffer` physical rows，旧 logical-line store
仅保留为显式迁移路径。

## 2. 一句话目标

一次 PTY bytes 到达后分成两条 owner 明确的链路：

```text
PTY bytes
  -> live SurfaceTrack
       WriteForLatestFrame 更新 latest screen
       唯一 response owner
       bump LiveRevision + publish invalidation
  -> history SemanticTap
       无 response 回写
       compact semantic history journal emission
       async journal apply to authoritative HistoryStore
```

live 热路径必须接近 `WriteForLatestFrame` 成本；history backlog 必须保存 compact semantic
journal，而不是完整 `TerminalSemanticTransaction` 的所有 per-op payload 和 frame/source clone。

## 3. 非目标

- 不引入 raw PTY history replay、raw parser fallback 或程序名特殊分支。
- 不从 live snapshot、current screen diff、TUI rows、renderer rows 或 local VTerm scrollback
  反推 authoritative history。
- 不让 history semantic consumer 回写 OSC/DA/DSR response；response owner 只能是 live path。
- 不把 journal backlog 或 live screen 提升为最终 history truth；R430 后默认 truth 是
  core-v2 screen-backed physical row/cell state，logical line 是 projection。
- 不改变 `history.window`、`history.copy` 对外 authoritative 合同。
- 不要求第一步删除现有 full semantic transaction path；切片期间可以保留旧路径作为未切换实现，
  但不能作为新 fast path 的 fallback。

## 4. 基本原则

### 4.1 live/history owner 分层

PTY bytes 可以在 core terminal ingest 处分发给 live 和 history 两个 consumer，但 owner 不能混淆：

- live `SurfaceTrack` 是 native latest screen owner，也是唯一 terminal response owner。
- history semantic consumer 是 history journal owner，必须使用 termx-vterm semantic pass 解释
  PTY/resize，不能读取 live snapshot、TUI rows 或 raw parser fallback。
- resize 必须按同一输入顺序进入两条链路，避免 old-size pending bytes 在任一侧被 new-size 解释。

compact journal 必须来自 history semantic consumer 的 vterm semantic pass。它可以是
`TerminalSemanticTransaction` 的 history-specific 裁剪或命令化结果，但不能由 raw PTY scanner、
live snapshot diff、TUI rows 或 live `SurfaceTrack` 生成。

### 4.2 journal 是未应用语义命令，不是第二份 truth

journal backlog 只是 history renderer/store 尚未应用的命令队列。它可以异步、batch、spill，
但不能提供 `HistoryWindow`、copy、search 或 pagination truth。

权威 truth 仍在：

- 默认 production：`ScreenHistoryBuffer` 的 sealed/current physical rows 与
  `ScreenPhysicalRowBackend` row range。
- 查询边界：`ScreenBackedHistoryStore` 从 physical rows 生成的 frozen projection。
- protocol 返回的 authoritative `HistoryWindow`。

旧 mutation-backed `HistoryStore` 的 logical lines、sealed timeline、open line 和 frame
journal 只服务显式 legacy harness，不得重新接成默认 daemon truth。

### 4.3 ordinary 输出按行生命周期批量化

普通 stdout 的核心事件不是每个 `WriteSpan` 或 `LF` op，而是 logical line 生命周期：

- 追加或编辑当前 open logical line。
- 换行、scroll-out、process close 等边界 seal line。
- 大量完整行可以作为 `OrdinaryLineBatch` 进入 history。

这条路径必须避免每行/每 span 生成多份 cell clone、ordered op clone、open-line mutation clone。

### 4.4 pseudo-TUI 走 mutable frame state machine

pseudo-TUI 不走普通 append。primary screen app 的 repaint 只更新 current mutable frame；只有
明确 terminal 语义边界才 archive、hide、clear、final seal 或 drop transient content。

## 5. 总体链路

目标链路：

```text
PTY bytes / resize
  -> SemanticTap
       vterm latest-screen fast write
       compact semantic journal recorder
       response owner
       live revision bump
  -> live publisher
       terminal.live.invalidated(termID, revision)
  -> history journal queue
       applied/target seq, flush fence, catchup diagnostics
  -> HistoryJournalRenderer
       journal commands -> HistoryMutationBatch
  -> HistoryStore
       logical lines + sealed timeline + frame journal + frozen projection
```

`live.screen.get` pull snapshot 时读取 tap/vterm 当前 latest screen。tap write hot path 不应每个 batch
都 clone native screen snapshot。

## 6. Journal 数据模型

下面是设计形态。实现可以按 Go 类型拆分，但语义边界必须保持。

### 6.1 HistoryJournal

```go
type HistoryJournal struct {
    TerminalID string
    Seq        uint64
    Size       TerminalSemanticSize
    Items      []HistoryJournalItem
}
```

`Seq` 是 tap 输入序号或 history journal 序号，用于 backlog applied/target fence。`Items` 必须按
terminal semantic order 排列。普通 line batch 与 boundary 不能跨越改变语义的 barrier 重排。

### 6.2 OrdinaryLineBatch

```go
type OrdinaryLineBatch struct {
    Cols        int
    Lines       []JournalLogicalLine
    OpenUpdate  *JournalOpenLineUpdate
    Origin      HistoryOrigin
}
```

`Lines` 是本 journal 内已按 terminal 语义 seal 的 ordinary logical lines。它们可以来自：

- 普通 `LF` / `NEL` / `IND`。
- 普通流滚出 viewport 后已经能证明完整离开 open mutable 区域的 line。
- process close / lifecycle close 前由 renderer seal 的 open line。

`OpenUpdate` 只能表达“把这些 cells 写入/open line 状态推进到这里”的命令，不能让 tap 成为
open-line truth owner。权威 open line 仍由 history renderer/store 持有。

### 6.3 Boundary

```go
type HistoryBoundaryKind string

const (
    BoundaryED2       HistoryBoundaryKind = "ed2"
    BoundaryED3       HistoryBoundaryKind = "ed3"
    BoundaryRIS       HistoryBoundaryKind = "ris"
    BoundaryResize    HistoryBoundaryKind = "resize"
    BoundaryAltEnter  HistoryBoundaryKind = "alt_enter"
    BoundaryAltExit   HistoryBoundaryKind = "alt_exit"
    BoundarySyncBegin HistoryBoundaryKind = "sync_begin"
    BoundarySyncEnd   HistoryBoundaryKind = "sync_end"
)

type Boundary struct {
    Kind   HistoryBoundaryKind
    Size   TerminalSemanticSize
    Reason string
}
```

Boundary 是 history state machine 的控制命令，不携带 live snapshot。它决定 ordinary stream
是否 flush、primary current frame 是否 archive/hide/clear、alt transient 是否 publish/drop，以及
resize 是否只作为 non-history boundary。

### 6.4 ScrollOutProof

```go
type ScrollOutProof struct {
    Rows []TerminalSemanticScrollOut
}
```

该 proof 只能来自 vterm 在同一 write/clear/scroll 过程中的 scroll-out evidence。不能从当前屏幕
最终状态或 live snapshot 反推。

### 6.5 FrameEvent

```go
type FrameEventKind string

const (
    FrameReplacePrimary FrameEventKind = "replace_primary"
    FrameArchivePrimary FrameEventKind = "archive_primary"
    FrameClearPrimary   FrameEventKind = "clear_primary"
    FrameReplaceAlt     FrameEventKind = "replace_alt"
    FrameClearAlt       FrameEventKind = "clear_alt"
    FrameFinalPrimary   FrameEventKind = "final_primary"
)

type FrameEvent struct {
    Kind        FrameEventKind
    Frame       *TerminalSemanticFrame
    TouchedRows []int
    FixedCols   int
    Reason      string
}
```

Frame event 只能在明确 semantic boundary 下产生：

- synchronized repaint / active primary frame。
- ED2 clear-time primary frame boundary。
- alt enter/exit。
- full replace / final process close。

普通 stdout 不得因为当前 native screen 内容变化而产生 primary frame event。`Frame` 也不能来自
“比较 before/after screen diff 后猜测要存历史”，只能来自 vterm semantic owner 在 transaction
边界明确提供的 frame proof。

## 7. HistoryJournalRenderer

`HistoryJournalRenderer` 是现有 `HistoryLogicalRenderer` 的 fast-path 输入层。它不替代
`HistoryStore`，也不改变 authoritative window/copy API。

建议分两层：

```text
HistoryJournal
  -> JournalClassifier / state machine
  -> HistoryMutationBatch
  -> HistoryStore.Apply
```

ordinary path：

```text
OrdinaryLineBatch
  -> append sealed logical lines
  -> update renderer-owned open line
  -> emit one compact mutation batch
```

screen app path：

```text
Boundary + FrameEvent + ScrollOutProof
  -> FrameReducer / StreamLineReducer existing semantic rules
  -> archive / replace / clear / final frame mutations
```

现有 `HistoryLogicalRenderer.Apply(TerminalSemanticTransaction, HistoryDecision)` 可以先保留，
用于复杂 coverage 对照或未迁移场景；但新高压普通输出路径必须使用 journal apply，不能再绕回
逐 op `HistorySemanticEventsFromTransaction`。

## 8. 状态机

```text
OrdinaryPrimary
  normal stdout; append/seal logical lines

PrimaryMutableFrame
  primary screen app current frame; repaint mutates current frame

AltTransient
  alternate screen current frame; visible/copy-current only, not primary history

ResizeBoundary
  non-history boundary; changes projection state, does not rewrite sealed history
```

### 8.1 OrdinaryPrimary

- `OrdinaryLineBatch` 直接写 logical-line history。
- `OpenUpdate` 只更新 history renderer/store 的 open line。
- 遇到 ED2/ED3/RIS/alt/sync/full frame boundary 时，必须先 flush/settle ordinary state，再切换状态。

### 8.2 PrimaryMutableFrame

- repaint 更新 current primary frame。
- clear-time scroll-out proof 可以把 current frame 中真实离开 viewport 的 rows 写入 scrollable history。
- 未经 proof 的 transient repaint 不进入 sealed timeline。
- 恢复普通输出前必须关闭或归档 current frame，避免 prompt 插入 frame 中间。

### 8.3 AltTransient

- alt 内容不写 primary history。
- alt enter 前 primary current frame 需要 archive/hide。
- alt exit 后清 alt transient；后续 primary 输出按新的 ordinary/frame boundary 处理。

### 8.4 ResizeBoundary

- resize 不重写 sealed logical history。
- ordinary logical line 只在 view-time rewrap。
- mutable frame 维持 mutable 语义；final frame 固定生成时 cols。

## 9. 语义拦截规则

### 9.1 ED2

ED2 清当前 viewport，不等于 ED3 clear scrollback。

规则：

- ordinary open line 先按 terminal boundary 处理。
- 若存在 primary current frame，ED2 只能清 current ownership；旧屏进入 history 必须来自同一
  journal 的 clear-time scroll-out proof。
- ED2 后 redraw 可以开启或更新新的 primary current frame。
- 不得物理删除 authoritative history。

### 9.2 ED3

ED3 是 clear scrollback/history boundary。在当前 iTerm2 式无限历史目标下，它是软页边界：

- 记录 boundary，影响后续 window/page/copy 语义。
- 旧 logical-line-first 路径默认不 truncate logical-line truth；R430 后默认生产路径也
  不靠 journal 或 storage scrub 删除 screen-backed physical row truth。
- 不靠 storage scrub 删除旧 payload。

### 9.3 RIS

RIS 是 reset boundary：

- seal ordinary open line。
- clear primary current frame / alt transient。
- reset terminal modes 对后续 journal classification 的影响。
- 不从 live snapshot 生成 sealed history。

### 9.4 synchronized output

2026 begin/end 是 primary repaint 的强边界：

- begin/active payload 可以进入 PrimaryMutableFrame。
- end 后若同包出现普通 prompt，需要按 op order 先关闭 frame，再处理 ordinary stream。
- split begin/payload/end transaction 不能因 journal batching 乱序。

### 9.5 alt enter/exit

- alt enter archive/hide primary current，进入 AltTransient。
- alt frame 只维护 transient current frame。
- alt exit 清 transient，不写 primary history。

## 10. Live 热路径要求

tap write hot path 目标：

```text
normalize PTY
write emulator/latest screen
record compact journal commands
bump live revision
enqueue journal
publish invalidation
```

禁止在 hot path 做：

- 每个 write deep clone native screen snapshot。
- 普通输出生成 full `PrimaryFrame`。
- 普通输出附带 full `SourceDamage` payload。
- 普通输出逐 span/逐 control clone 多份 cells。
- history reducer 每个 op emit open-line mutation。

`live.screen.get` 请求时再 pull `NativeScreenSnapshot`，并按既有 latest-only/stale guard 投影给 TUI。

## 11. Backlog 与 flush

history backlog 从 transaction queue 迁移为 journal queue：

```text
terminalHistoryJournalQueue
  target_seq
  applied_seq
  pending journals
  in_flight
  flush(ctx)
  status()
```

flush 语义保持：

- `FlushHistory` 先等待 tap input queue 推进到调用时 target。
- 再等待 journal queue applied >= target。
- 不等待客户端 render。
- 不把未来输出纳入当前 flush。

copy/history 的快速入口优化必须封装在 `history.window` 内部，不能暴露第二套 copy-entry projection API。

## 12. Storage 与 projection

`HistoryStore` 对外不变：

- latest/older/oldest/newer window。
- freeze/release。
- copy/search。

内部可以新增 journal apply 专用 mutation：

- bulk append sealed logical lines。
- bulk update open line。
- frame replace/archive/clear/final。
- soft page boundary。

这些 mutation 必须继续维护同一套 logical line store、sealed timeline、frame journal 和 frozen projection。

## 13. 测试策略

### 13.1 合同 harness

- journal item 必须来自 history semantic tap，不含 raw PTY replay。
- live SurfaceTrack 与 history semantic tap owner 分离，history 不读取 live snapshot。
- resize 与 PTY write 在两条 owner 链路内都保持同 sequence。
- response exactly once，且只能由 live SurfaceTrack 回写。
- journal 不携带 full snapshot clone。

### 13.2 ordinary harness

- 100K/1M 普通行输出形成 `OrdinaryLineBatch`。
- CR/BS/EL/CUP 等行内编辑仍修改 open line。
- 遇 ED2/RIS/alt/sync 时 ordinary fast path 退出并保持顺序。
- CJK width 和 styled cells 不丢。

### 13.3 boundary/frame harness

- ED2 clear-time proof 不重复 sealed shell tail。
- ED3 soft page boundary 不物理删除 old history。
- primary app repaint 不逐帧 append。
- alt transient 不进 primary history。
- sync begin/end split 和同包 prompt 顺序正确。
- resize 不重写 sealed history，final frame fixed cols。

### 13.4 production stress

- `generate_terminal_stress.py --lines 100000`：live DONE 应接近 Python process 完成时间。
- `scripts/termx_tui_stress_memory.sh --lines 100000`：latest/copy/oldest 正确，RSS 稳定。
- 1M 行 stress：daemon RSS 不随 backlog 线性驻留，history file size 符合 payload 规模。
- copy/history 入口只走 `history.window`；catchup 等待或快速返回策略必须封装在该 API 内部。

## 14. 风险与防线

| 风险 | 防线 |
| --- | --- |
| journal 覆盖不足导致 screen app/clear/resize 历史错误 | 先做 coverage matrix harness，再接生产 |
| FrameEvent 从 snapshot diff 反推历史 | 类型和测试要求 frame 只能来自 semantic boundary proof |
| tap 持有 open line 形成第二份 truth | tap 只发 command；renderer/store 持有权威 open line |
| boundary 与 ordinary batch 乱序 | journal item 单 sequence；禁止跨 boundary 合并 |
| fast path 变成程序名或局部 fallback | 只按 terminal semantic boundary 分类 |
| journal payload 再次膨胀 | 普通输出禁止 full frame/source/snapshot clone |

## 15. 迁移顺序

1. 先立类型、文档和 coverage harness。
2. 移除 tap write hot path 的 per-write snapshot clone。
3. ordinary stdout 用 journal line batch 跑通，保持 old full tx path 对照。
4. 增加 boundary journal 和 state machine。
5. 增加 frame journal。
6. queue/backlog 改 journal queue。
7. 压力验证后删除旧 full transaction 热路径依赖。
