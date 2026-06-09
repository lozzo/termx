# 终端逻辑行与驻留分层架构

本文是当前阶段对终端历史、实时渲染、落盘数据、内存驻留数据、TUI/Core 边界的重新整理。

本文的核心结论是：**系统不再使用冷热数据作为语义边界。**

所谓“冷数据”只表示数据已经落盘或进入 mmap/page-backed 存储；所谓“热数据”只表示数据近期驻留在内存中，便于修改和投影。它们都是存储策略，不是历史语义，也不决定数据是否允许被修改。

## 1. 核心定义

### 1.1 逻辑行

逻辑行是 primary terminal history 的核心语义单位。

逻辑行不是屏幕上的一行。屏幕上的一行是 visual row，是当前宽度下的投影结果。

同一条逻辑行在不同终端宽度下可以投影成不同数量的 visual rows。

示例：

```text
逻辑行：
  abcdefghijklmn

宽度 5 的投影：
  abcde
  fghij
  klmn

宽度 10 的投影：
  abcdefghij
  klmn
```

resize 只改变投影，不应该仅因为宽度变化就创建新的逻辑行。

### 1.2 逻辑行状态

逻辑行至少有两个语义状态：

```text
open
  仍可被后续输入、控制序列、光标移动、erase、覆盖等操作继续修改。

sealed
  当前语义上已经闭合，但不等于永远不可修改。
```

sealed 只表示这条逻辑行当前已经形成完整边界。后续终端指令仍可能要求修改已经落盘或已经 sealed 的逻辑行；系统必须能表达这种修改，而不是把 sealed 或 disk resident 误当成 immutable。

### 1.3 驻留状态

驻留状态只描述数据在哪里，不描述数据语义。

```text
resident memory
  近期逻辑行仍在内存中，便于快速修改、reflow、投影。

disk / mmap backed
  逻辑行内容已落盘或可通过 mmap/page 读取。

dirty
  内存中的逻辑行相对落盘版本有修改，需要后续 flush。
```

一条逻辑行可以已经落盘，同时仍允许被修改。修改时可以通过内存驻留、copy-on-write、page patch、append-only delta 或后续 compaction 等机制实现。具体存储策略不是语义边界。

## 2. 不再使用冷热语义

旧模型容易把数据分成：

```text
hot = 可修改
cold = 已落盘且不可修改
```

这个模型现在废弃。

新的模型是：

```text
逻辑语义：logical line / open / sealed / screen projection
存储策略：resident memory / disk / mmap / dirty / flush
传输边界：live surface / authoritative history window
```

这三组维度相互正交。

## 3. 架构总图

```text
PTY bytes
  |
  v
termx-vterm parser
  |
  v
logical line truth store
  |---------------------------------------------------|
  |                                                   |
  v                                                   v
resident logical line cache                  disk / mmap line store
  |                                                   ^
  |                                                   |
  |---------------- dirty flush / page update --------|
  |
  v
screen projection engine
  |
  |-- live surface -------------------------> TUI live render
  |
  |-- authoritative history window ---------> TUI history/copy-mode render
```

其中：

- `logical line truth store` 是唯一的历史真相。
- `resident logical line cache` 是性能优化，用于减少频繁读取或修改 mmap/page 数据。
- `disk / mmap line store` 是持久化和大历史容量策略，不是不可变历史语义。
- `screen projection engine` 只把逻辑行按当前宽度和 viewport 投影成 visual rows。
- TUI 不再通过 wrapped row、LoadedRows、row id 连续性等信息重建历史真相。

## 4. 核心数据结构草案

### 4.1 逻辑行

```go
type LogicalLineID struct {
    TerminalID string
    Sequence   uint64
}

type LogicalLineState string

const (
    LogicalLineOpen   LogicalLineState = "open"
    LogicalLineSealed LogicalLineState = "sealed"
)

type LogicalLine struct {
    ID        LogicalLineID
    Version   uint64
    State     LogicalLineState
    Cells     []Cell
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

说明：

- `Version` 用于 stale response、copy-on-write、TUI window token、并发修改检测。
- `State` 不代表能否修改，只代表当前行边界是否闭合。
- `Cells` 是逻辑行内容，不是当前 visual rows。

### 4.2 驻留描述

```go
type ResidencyTier string

const (
    ResidencyMemory ResidencyTier = "memory"
    ResidencyDisk   ResidencyTier = "disk"
    ResidencyMmap   ResidencyTier = "mmap"
)

type LogicalLineResidency struct {
    LineID LogicalLineID
    Tier   ResidencyTier
    Dirty  bool
}
```

说明：

- `Tier=disk` 不代表不可修改。
- `Dirty=true` 表示需要把内存版本同步到持久层。
- 长历史可以按 page 管理，但 page 不是逻辑语义单位。

### 4.3 投影行

```go
type ProjectedRow struct {
    LineID LogicalLineID
    Start  int
    End    int
    Cells  []Cell
}

type LogicalLineSpan struct {
    LineID   LogicalLineID
    StartRow int
    EndRow   int
    Version  uint64
    State    LogicalLineState
}
```

说明：

- `ProjectedRow` 是某个宽度下的 visual row。
- `LogicalLineSpan` 告诉消费者哪些 visual rows 属于同一条逻辑行。
- TUI copy mode 应该用 `LogicalLineSpan` 处理选择、跳行、复制，不再根据 wrapped flag 自己拼逻辑行。

### 4.4 对 TUI 的 live surface

```go
type LiveSurface struct {
    TerminalID string
    Revision   uint64
    Size       Size
    Screen     ScreenData
    Cursor     CursorState
    Modes      TerminalModes
    Timestamp  time.Time
}
```

live surface 只表达当前实时屏幕，不承载 committed history truth。

### 4.5 对 TUI 的权威历史窗口

```go
type HistoryWindowToken string

type HistoryWindowOp string

const (
    HistoryWindowReplace HistoryWindowOp = "replace"
    HistoryWindowPrepend HistoryWindowOp = "prepend"
)

type HistoryWindow struct {
    TerminalID string
    Token      HistoryWindowToken
    Op         HistoryWindowOp
    Width      int
    Rows       []ProjectedRow
    Lines      []LogicalLineSpan
    HasMore    bool
    Timestamp  time.Time
}
```

说明：

- `Token` 是窗口边界版本，由 core 生成。
- `Op` 由 core 决定，TUI 不判断 older page 是否连续。
- `Rows` 是投影结果，不是历史真相。
- `Lines` 是逻辑行边界的权威投影。

## 5. 数据处理流程

### 5.1 普通输出

```text
PTY bytes
  -> vterm parser
  -> 修改 logical line truth store
  -> 更新 resident logical line cache
  -> 标记 dirty / 决定是否 flush
  -> 更新 screen projection
  -> 发 live surface / screen update
```

普通输出不直接创建 visual-row 历史。visual row 只是投影。

### 5.2 换行或行闭合

```text
当前 open logical line
  -> seal
  -> 仍可保留在 resident cache
  -> 可按策略 flush 到 disk/mmap store
```

seal 不是“变成不可修改”。seal 只是当前 logical line 边界闭合。

### 5.3 后续指令修改已落盘逻辑行

```text
定位 logical line
  -> 如果不在内存，按 page/mmap 读入 resident cache
  -> 修改 line 内容或状态
  -> version++
  -> dirty=true
  -> 重建相关 projection
  -> 后续 flush / page patch / compaction
```

这说明 disk resident 数据不是 immutable。

### 5.4 resize

```text
resize(width)
  -> 不创建历史
  -> 不因为 visual row 数变化改写 logical line truth
  -> 对需要显示的 logical lines 重新 projection
  -> 生成新的 live surface / history window
```

grow resize 时，为了重建 screen，可以把已落盘的逻辑行读回 resident cache。这是 residency 迁移，不是历史语义变化。

shrink resize 时，屏幕上方不可见内容仍然是 logical line truth 的一部分，可以保留在 resident cache 或后续落盘。

### 5.5 历史窗口请求

```text
TUI request latest/older history window(token, width, limit)
  -> core 根据 token 判断窗口边界
  -> core 从 logical line truth store 选择逻辑行集合
  -> core 按 width 投影为 visual rows
  -> core 生成 LogicalLineSpan
  -> core 返回 HistoryWindow{Op, Token, Rows, Lines}
```

TUI 不做：

- row id 连续性判断；
- LoadedRows 计算；
- generation 合并；
- wrapped 拼接；
- latest replace / older prepend 判定。

这些都属于 core。

### 5.6 copy mode

```text
进入 copy mode
  -> TUI 请求 latest authoritative history window
  -> TUI 保存 cursor / selection / viewport top / token

向上滚动到窗口顶部
  -> TUI 用 token 请求 older window
  -> core 返回 replace 或 prepend
  -> TUI 只应用 Op

复制文本
  -> TUI 根据 Lines 组合 logical line 文本
```

copy mode 不再冻结本地 snapshot 作为历史真相。

## 6. Core 与 TUI 边界

### 6.1 Core 负责

- 维护 logical line truth。
- 维护 logical line version。
- 维护 resident cache 与 disk/mmap store 的同步策略。
- 决定 history window 的 replace / prepend。
- 生成 window token。
- 生成 logical line spans。
- 处理 resize 后的 projection。
- 处理 stale window 请求。

### 6.2 TUI 负责

- 实时渲染 live surface。
- 渲染 authoritative history window。
- 保存交互态：viewport top、cursor、selection、active token。
- 根据 core 返回的 `Op` 应用窗口。

### 6.3 TUI 不负责

- 不重建历史真相。
- 不根据 wrapped flag 拼 logical line。
- 不根据 row count / LoadedRows / generation 推断边界。
- 不判断 older page 是否连续。
- 不把 snapshot scrollback 当 committed history store。

## 7. 与现有实现的关系

当前实现已经具备部分基础：

- `termx-core` grid row index 保存了 wrapped flag。
- retention、viewport、reclaim 已能按 wrapped flag 推导完整 logical line 边界。
- `primaryLiveTail` 已表达 segment、origin、seal state、wrap pending。
- `tuiv2` 旧本地 `CommittedLoadedDepth` / viewport merge 状态机已经删除。

但当前仍缺少：

- 显式 `LogicalLine` 结构；
- 显式 logical line ID / version；
- 显式 line residency 描述；
- `HistoryWindow` protocol contract；
- core 生成的 `LogicalLineSpan`；
- TUI 对 `HistoryWindow` 的正式消费路径。

## 8. 推荐落地顺序

### 8.1 第一切片：显式投影结构

先不改底层存储格式，只在 core 内部从现有 row + wrapped metadata 生成：

```text
LogicalLineSpan
HistoryWindow
WindowToken
```

这一切片不要求立即引入新的磁盘格式。

### 8.2 第二切片：protocol contract

新增 `terminal.history_window` 方法，并保持旧 `snapshot` / `grid.viewport` 作为 legacy 边界。

### 8.3 第三切片：TUI 接入

让 `tuiv2/historyview.Source` 调用 core 新接口，让 copy mode 和历史滚动只消费 `HistoryWindow`。

### 8.4 第四切片：显式 LogicalLine store

把底层从 row + wrapped 推导逐步收敛到显式 logical line store。

迁移前允许内部仍用 row index 计算 span，但对外 contract 必须已经是 logical-line-first。

### 8.5 第五切片：驻留与落盘策略

引入 resident cache、dirty tracking、page update / compaction 策略。

此时再讨论 mmap 修改成本、page patch、flush batching，而不是把它们混入历史语义。

## 9. 术语收敛

后续代码和文档应避免把下面这些词作为语义边界：

- hot；
- cold；
- immutable history；
- committed depth；
- loaded rows as truth。

推荐使用：

- logical line truth；
- resident logical line cache；
- disk / mmap line store；
- dirty line；
- live surface；
- authoritative history window；
- logical line span；
- screen projection。

## 10. 最终原则

一句话原则：

```text
逻辑行是真相；屏幕是投影；内存和磁盘只是驻留策略；TUI 只消费 core 给出的投影结果。
```
