# history logical renderer 设计重写

状态：R318 新设计基准。

本文替代旧 screen app 无限历史文档里的 `commit/committed` 语义。旧文档仍可作为 R300-R317
问题背景，但只要本文与旧文档、旧代码类型名或旧测试描述冲突，以本文为准。

## 1. 一句话

history 是另一种 renderer。

普通 live renderer 的目标是宿主终端屏幕：

```text
TerminalSemanticTransaction -> LiveSurfaceSnapshot -> TUI RenderVM -> host screen
```

history renderer 的目标是可分页、可复制、可搜索的 logical history model：

```text
TerminalSemanticTransaction -> HistoryRenderCommand -> logical lines + frame journal -> HistoryWindow
```

两者消费同一份 vterm 语义结果，但输出对象不同。live renderer 画到屏幕，history renderer
写到 core-v2 的历史对象。history 不消费 TUI renderer 的输出，也不重新解析 raw PTY。

白话说：程序输出一批字节后，vterm 先把它解释成“终端到底发生了什么”。屏幕显示拿这份结果去画
当前屏幕；历史拿这份结果去更新自己的账本。历史不应该等屏幕画完以后再截图猜账本。

## 2. 基本概念

| 概念 | 含义 | 白话 |
| --- | --- | --- |
| PTY observable content | 真实经过 PTY 并被 vterm 解释到 terminal 语义里的内容 | 程序真的往终端写了，才算我们能记 |
| TerminalSemanticTransaction | vterm 对一次 PTY write/resize 的有序解释结果 | 这一批输出里有写字、移动光标、清行、滚动、切 alt-screen 等动作 |
| LogicalLine | history 的基本内容单位 | 用户理解的一行历史，不是当前宽度切出来的一行屏幕格子 |
| Mutable | 当前还可能被后续 terminal 语义改写 | Codex 正在刷新这一屏，这一屏还活着 |
| Sealed | 已经按终端语义离开当前可变区域 | 这段内容已经封口，可以进入历史时间线 |
| OpenLine | 普通输出流里当前还没换行封口的 logical line | shell 正在输出半行 |
| CurrentPrimaryFrame | primary screen app 当前可变的一屏 | Codex 当前这一屏 |
| ArchivedPrimaryFrame | primary current frame 因明确边界离开 current 后保留下来的 frame | Codex 进 alt 选择器前那一屏，被放到旁边保存 |
| AltTransientFrame | alt-screen 当前 frame | vim/htop/选择器当前屏，可看可选，但不写 primary 历史 |
| HistoryTimeline | sealed records 的统一时间线 | 上滑时按时间顺序走的一条路 |
| HistoryWindow | 对 timeline + active mutable frame 的一个可显示窗口 | TUI/copy mode 能拿来显示的一页历史 |
| FrozenProjection | 进入 copy/history 时冻结的投影边界 | 进去那一刻看到什么，后面程序刷新不偷偷改这一页 |
| StorageBackend | 内存或文件落点 | 存在哪里，不决定内容是不是还能改 |

本文不再使用 `commit` 表达领域语义。真正需要表达的是：

- mutable：还活着，还可能被改。
- sealed：按 terminal/session 语义封口，不再属于当前可变区域。
- persisted：已经写到某个存储落点，和 mutable/sealed 是不同维度。

sealed 不等于永久不可删除。retention、truncate、clear policy、compaction 仍然可以让 sealed record
从当前可见历史里移除。它只表示“不会再被当前 open line 或 current frame 继续改写”。

## 3. Owner 和边界

```text
PTY bytes / resize
  -> termx-vterm
       owns terminal parser, mode state, cursor, screen cells
  -> TerminalSemanticTransaction
       ordered ops + scroll-out proof + primary/alt frame cells
  -> HistorySemanticClassifier
       terminal semantics -> output mode decision
  -> HistoryLogicalRenderer
       semantic tx -> history mutations
  -> HistoryStore
       owns logical lines, sealed timeline, frame journal, frozen windows
  -> history.window / history.copy
       protocol/TUI only consume projection
```

live display 是旁路投影：

```text
same vterm state -> LiveSurfaceSnapshot -> TUI renderer -> host screen
```

禁止反向读取：

- history 不从 TUI rows 反推。
- history 不从 renderer output 反推。
- history 不从 live snapshot 反推 sealed history。
- history 不重新跑 raw PTY parser。
- history 不按 Codex、Claude Code、vim、htop 等程序名分支。
- StorageBackend 不决定领域 mutability。

## 4. 数据对象

下面是设计对象，不要求第一版代码完全同名，但新代码和新测试应按这些语义写。

### 4.1 HistoryState

`HistoryState` 是单个 terminal 的 history truth。

```go
type HistoryState struct {
    TerminalID string
    Generation uint64

    OpenLine OpenLine
    Timeline SealedTimeline
    Frames FrameJournal
    Frozen map[HistoryToken]FrozenProjection
}
```

它只保存 history 领域状态，不保存 process handle、TUI pane、renderer cache 或 protocol client。

### 4.2 LogicalLine

`LogicalLine` 是用户能复制、搜索、重新 wrap 的文本单位。

```go
type LogicalLine struct {
    ID LogicalLineID
    Seq uint64
    Cells []HistoryCell
    State LineState
    Origin HistoryOrigin
    Version uint64
}

type LineState string

const (
    LineOpen LineState = "open"
    LineSealed LineState = "sealed"
)
```

`Cells` 保存 terminal 语义属性，不提前把默认主题颜色烘焙成 RGB。visual row 只是查看时按 cols
投影出来的结果，不能当 truth。

### 4.3 OpenLine

`OpenLine` 只服务普通 stream 输出。

```go
type OpenLine struct {
    Active bool
    Line LogicalLine
    CursorCol int
}
```

普通 `write cells` 写进 open line。`CR`、`BS`、`EL`、`CUP` 这类行内编辑修改 open line。
`LF`、scroll-out proof、process close 等边界让 open line seal。

白话说：普通 shell 输出还没换行时，只是在改当前半行；换行或离开屏幕后，这一行才封口进入历史时间线。

### 4.4 SealedTimeline

`SealedTimeline` 是 sealed records 的顺序索引。

```go
type SealedTimeline struct {
    Records []HistoryRecordRef
    Generation uint64
}
```

它不是第二份 payload store，只保存顺序、cursor、generation 和 record 引用。payload 仍在
logical line / frame record 里。

可以进入 timeline 的 record：

- ordinary stream sealed line
- primary scroll-out sealed line
- archived primary frame row
- closed primary frame row

不能直接进入 primary timeline 的 record：

- current primary mutable frame
- alt transient frame

### 4.5 FrameJournal

`FrameJournal` 保存 screen app 的 frame 状态。

```go
type FrameJournal struct {
    PrimaryCurrent *MutableFrame
    PrimaryArchived []SealedFrame
    AltCurrent *TransientFrame
}
```

`PrimaryCurrent` 是正在被 primary screen app 改写的 frame。每次 vterm 产出新的 primary frame，
history renderer 用同一份 cells 全量替换它。

`PrimaryArchived` 是因为明确边界离开 current 的 primary frame，例如进入 alt-screen 前要保存
当前 Codex primary frame。archived frame 是 sealed frame，但它不是 ordinary stream line。

`AltCurrent` 是 alt-screen 当前 frame。它可以进入 latest/frozen projection，用于选择和复制当前屏幕，
但默认不进入 primary sealed timeline。

### 4.6 MutableFrame / SealedFrame

```go
type MutableFrame struct {
    ID FrameID
    Seq uint64
    Cols int
    Rows []LogicalLineDraft
    Source FrameSource
}

type SealedFrame struct {
    ID FrameID
    Seq uint64
    Cols int
    Lines []LogicalLine
    Reason SealReason
}
```

frame rows 在 history window 里也以 logical line 形式投影，但 frame 有固定生成宽度：

- current frame 可以被后续 tx 替换。
- archived/closed frame 后续不随 resize 改写。
- 普通 stream line 可以在查看时按当前 cols 重新 wrap。

### 4.7 HistoryRecord

`HistoryRecord` 是 timeline 的统一元素。

```go
type HistoryRecord struct {
    ID HistoryRecordID
    Seq uint64
    Kind HistoryRecordKind
    LineIDs []LogicalLineID
    FrameID FrameID
}
```

`Kind` 至少包括：

- `ordinary-line`
- `primary-scroll-out-line`
- `archived-primary-frame`
- `closed-primary-frame`

HistoryWindow 可以把这些 record 投影成 visual rows，但分页 cursor 必须沿同一条 timeline 走，
不能把 archived frame 临时追加到 latest 尾部。

## 5. 接口

### 5.1 TerminalSemanticSource

vterm 仍是唯一 terminal semantics owner。

```go
type TerminalSemanticSource interface {
    ApplyPTYWrite(raw []byte) (TerminalSemanticTransaction, error)
    Resize(size TerminalSemanticSize) (TerminalSemanticTransaction, error)
}
```

transaction 必须包含：

- ordered ops
- primary scroll-out proof
- current primary frame cells
- current alt frame cells
- alt enter/exit
- synchronized output begin/active/end
- resize/full-replace boundary

如果一次 raw write 同时包含多个 mode boundary，adapter 应拆分 transaction，或者 transaction 必须能表达
边界的有序位置。history renderer 不能靠 bool flag 猜 raw order。

### 5.2 HistorySemanticClassifier

classifier 只根据 terminal 语义和当前 history state 决策，不看程序名。

```go
type HistorySemanticClassifier interface {
    Classify(tx TerminalSemanticTransaction, state HistoryReadState) HistoryDecision
}

type HistoryDecision struct {
    Mode HistoryOutputMode
    PublishPrimaryFrame bool
    ArchivePrimaryBeforeAlt bool
    ClearPrimaryCurrent bool
    PublishAltFrame bool
    ClearAltFrame bool
    ClosePrimaryFrame bool
    NonHistoryBoundary bool
}
```

`Mode` 可以是：

- `ordinary-stream`
- `primary-frame-session`
- `alt-transient`
- `boundary-only`

### 5.3 HistoryLogicalRenderer

HistoryLogicalRenderer 是 “terminal semantic transaction -> history mutation” 的统一入口。

```go
type HistoryLogicalRenderer interface {
    Apply(tx TerminalSemanticTransaction, decision HistoryDecision) (HistoryMutationBatch, error)
    Close(reason TerminalCloseReason) (HistoryMutationBatch, error)
}
```

它内部可以拆成两个 reducer：

```go
type StreamLineReducer interface {
    ApplyOp(op TerminalSemanticOp) []HistoryMutation
    SealOpenLine(reason SealReason) []HistoryMutation
    SealScrollOut(proof ScrollOutProof) []HistoryMutation
}

type FrameReducer interface {
    ReplacePrimaryCurrent(frame TerminalSemanticFrame, reason FrameReason) []HistoryMutation
    ArchivePrimaryCurrent(reason SealReason) []HistoryMutation
    ReplaceAltCurrent(frame TerminalSemanticFrame) []HistoryMutation
    ClearAltCurrent(reason FrameReason) []HistoryMutation
    ClosePrimaryCurrent(reason SealReason) []HistoryMutation
}
```

这两个 reducer 消费同一份 transaction。普通输出主要走 `StreamLineReducer`；Codex 这类
primary screen app 主要走 `FrameReducer`；scroll-out proof 会把真实离开 primary viewport 的内容
seal 成 logical lines。

### 5.4 HistoryStore

HistoryStore 应只接收 renderer mutation，不自己解释 terminal ops。

```go
type HistoryStore interface {
    Apply(batch HistoryMutationBatch) error

    LatestWindow(req HistoryWindowRequest) (HistoryWindow, error)
    OlderWindow(req HistoryWindowRequest) (HistoryWindow, error)
    NewerWindow(req HistoryWindowRequest) (HistoryWindow, error)
    Freeze(req FreezeHistoryRequest) (FrozenProjection, error)
    Copy(req HistoryCopyRequest) (string, error)
    Release(token HistoryToken) error
}
```

Store 的职责：

- 保存 logical line payload。
- 保存 sealed timeline。
- 保存 frame journal。
- 维护 generation、cursor、frozen token。
- 生成 authoritative HistoryWindow。

Store 不负责：

- raw PTY parsing。
- 程序名判断。
- renderer fallback。
- TUI viewport 修补。

### 5.5 StorageBackend

StorageBackend 只负责 residency。

```go
type StorageBackend interface {
    ApplyStorageBatch(batch StorageBatch) error
    Recover() (HistoryStateSnapshot, error)
    Compact(policy CompactPolicy) error
}
```

storage operation 使用下面这些语义，不使用 commit 语义：

- `UpsertMutableLine`
- `SealLine`
- `AppendTimelineRecord`
- `UpsertMutableFrame`
- `SealFrame`
- `ClearTransientFrame`
- `DeleteUnreferencedPayload`

写入文件不代表 sealed。sealed 也不代表文件不能 compact。domain mutability 由 HistoryState 决定。

## 6. 处理流程

### 6.1 普通输出

输入：

```text
hello\r\n
```

流程：

```text
tx.Ops: write "hello", cr, lf
StreamLineReducer writes "hello" into OpenLine
LF seals OpenLine
HistoryStore appends sealed ordinary-line record to SealedTimeline
HistoryWindow projects that logical line by requested cols
```

白话说：普通输出不是每个字符都变成历史；它先攒在当前行里，换行后这一行封口。

### 6.2 行内改写

输入：

```text
abc\rX
```

流程：

```text
write "abc" -> OpenLine = "abc"
CR -> cursor col = 0
write "X" -> OpenLine = "Xbc"
```

没有 seal 前，不产生新的 timeline record。用户历史里不应该看到 `abc` 和 `Xbc` 两条中间态。

### 6.3 primary screen app repaint

Codex primary screen app 输出一批 repaint：

```text
tx.PrimaryFrame = vterm 当前 primary screen cells
decision.Mode = primary-frame-session
```

流程：

```text
FrameReducer.ReplacePrimaryCurrent(tx.PrimaryFrame)
HistoryStore upserts Frames.PrimaryCurrent
LatestWindow = sealed timeline tail + current primary frame projection
```

每次 Codex 更新屏幕，history 的 current frame 也用同一份 cells 全量替换。它不是把每次 repaint
都追加成新的 sealed line。

白话说：屏幕上那一屏怎么被 vterm 算出来，history 当前 frame 就拿同一屏来更新。只是 live 把它画出来，
history 把它放进“当前可浏览历史片段”。

### 6.4 primary scroll-out proof

如果 primary screen app 在同步输出里产生超过屏幕高度的真实 scroll-out：

```text
tx.PrimaryScrollOut = rows that left primary viewport
tx.PrimaryFrame = final visible frame
```

流程：

```text
StreamLineReducer.SealScrollOut(proof)
FrameReducer.ReplacePrimaryCurrent(tx.PrimaryFrame)
```

离开 viewport 的内容是真实经过 PTY 的可观察内容，应 seal 成 logical lines；最后仍可见的屏幕保留为
current frame。

### 6.5 进入 alt-screen

Codex 打开 `/resume` picker 或 vim/htop 进入 alt：

```text
tx.AltEntered = true
```

流程：

```text
FrameReducer.ArchivePrimaryCurrent(reason=alt-enter)
FrameReducer.ClearPrimaryCurrent()
FrameReducer.ReplaceAltCurrent(tx.AltFrame)
```

archived primary frame 进入 sealed timeline 的正确位置；alt current 只作为 transient frame 出现在
latest/frozen projection。

不能做的事：

- 不把 alt frame 写入 primary timeline。
- 不保留一个跨 alt 生命周期继续可 final close 的旧 primary current。
- 不在 latest 尾部临时追加早期 archived frame。

### 6.6 退出 alt-screen

```text
tx.AltExited = true
```

流程：

```text
FrameReducer.ClearAltCurrent(reason=alt-exit)
```

如果后续又出现 primary frame：

```text
FrameReducer.ReplacePrimaryCurrent(newPrimaryFrame)
```

这个 primary frame 是新的 current publish。它可以属于同一个 terminal session timeline，但不能复活
alt enter 前的 current frame。

### 6.7 resize

resize 是 terminal boundary：

```text
tx.Size changed
```

规则：

- sealed ordinary logical line 不重写，只在 HistoryWindow 投影时按新 cols reflow。
- archived/closed frame 固定生成时 cols。
- current primary frame 可以在下一次 tx 用新的 primary frame cells 替换。
- resize 本身不能凭空产生 sealed history。

### 6.8 terminal close

process exit、terminal kill、detach cleanup 不是 vterm write。

流程：

```text
HistoryLogicalRenderer.Close(reason)
  -> StreamLineReducer.SealOpenLine(reason=terminal-close)
  -> FrameReducer.ClosePrimaryCurrent(reason=terminal-close), if policy says keep final frame
  -> ClearAltCurrent(reason=terminal-close)
```

这里也不是 commit。它只是关闭 terminal/session 时把仍然活着的可变对象按规则封口或丢弃。

## 7. HistoryWindow 投影

HistoryWindow 从同一个 timeline 和 active frame 生成。

```text
latest:
  sealed timeline tail
  + active primary current frame, if any
  + active alt transient frame, if terminal is in alt

older:
  walk backward through unified timeline cursor
  include archived frame records at their real sequence position
  include ordinary sealed lines at their real sequence position
```

cursor 不能只看 visual row index。它至少要绑定：

- terminal id
- history generation
- requested cols
- timeline record boundary
- row/line offset
- frozen token, if window is frozen

进入 copy/history 后，`Freeze` 必须冻结当时的 active mutable frame payload 和 cursor boundary。后续 Codex
继续刷新，只能影响新的 latest；不能改写已经进入 copy/history 的 frozen projection。

## 8. Copy/Search

copy/search 只消费 authoritative HistoryWindow 或 FrozenProjection。

规则：

- TUI 可以本地重排已经拿到的 logical lines，但不能从 live surface 补历史。
- selection 保存 logical line id + offset，不保存“当前 visual row 是第几行”作为唯一 truth。
- search 使用 logical text。
- visual row markers、clipped markers、scrollbar、status 都是 UI 投影，不写回 HistoryStore。

## 9. 旧词替换

后续新代码、新文档、新测试应使用右侧词。旧代码里暂存的类型名可以分切片迁移，但注释和 harness
必须按新语义理解。

| 旧词 | 新词 | 说明 |
| --- | --- | --- |
| commit line | seal line | 一行按语义封口 |
| committed history | sealed timeline | 当前可分页历史时间线 |
| committed index | sealed timeline index | sealed record 的顺序索引 |
| committed tail | sealed timeline tail | latest 里 sealed 部分的尾部 |
| force commit | close/seal on lifecycle | terminal close 时按规则封口 |
| uncommitted current frame | mutable current frame | 当前还会被 repaint 改写 |
| committed bool | projection state / sealed flag | UI 需要知道是不是 mutable/sealed，但不要叫 committed |
| persisted | storage residency | 只是落点，不是 mutability |

## 10. Harness 基线

后续实现切片应先补这些 harness，再改真实链路。

1. 普通输出：`hello\r\n` 只产生一个 sealed ordinary logical line。
2. 行内改写：`abc\rX\n` 只产生 `Xbc`，不保留中间态。
3. Codex primary repaint：多次 `PrimaryFrame` replace 只更新 current frame，不追加 sealed records。
4. Primary scroll-out：scroll-out proof seal 离屏 logical lines，最后一屏作为 current frame。
5. Alt enter：primary current archive 到 timeline 正确位置，alt current transient，不进 primary timeline。
6. Alt exit：清 alt transient；后续 primary frame 是新 current，不复活 pre-alt current。
7. Resize：sealed ordinary line reflow，sealed frame 固定 cols，resize 不新增 history。
8. Freeze：进入 copy/history 后 frozen projection 不被后续 repaint 改写。
9. Pagination：latest/older 沿统一 timeline，不把 archive 临时挂在最新尾部。
10. Storage recovery：recover 后 restored HistoryState 的 mutable/sealed/frame/timeline 边界一致。

## 11. 实现迁移原则

- 先改文档和 harness，再改 projector/store。
- 先引入 `HistoryLogicalRenderer`、`StreamLineReducer`、`FrameReducer` 的内部边界，再逐步重命名旧类型。
- 不为当前 case 加 fallback。发现错序、黑屏、空洞时，先查 timeline、mutable frame、frozen token 和 cursor 边界。
- 旧 `commit/committed` 类型名如果暂时未删，必须用中文注释说明它只是旧名字，不再表达新领域概念。
- TUI/protocol 只跟 `HistoryWindow` contract 对齐；不要因为 TUI 当前展示缺字段而让 TUI 猜 history。

这份设计的核心判断是：history 确实是另一种 renderer，但它渲染的是 logical history state，
不是屏幕。屏幕 renderer 和 history renderer 的共同入口必须是 vterm terminal semantic transaction。
