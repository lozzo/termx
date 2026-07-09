# screen app 会话历史设计

状态：R437 后本文是 logical-line-first / screen-backed screen app session 的历史设计背景。当前 screen app 无限历史生产路径以 `history/linehist` 为默认实现：vterm 当前屏是唯一 active screen truth，滚出 primary 的行 seal 到 file-backed logical lines，HistoryWindow/Copy 阶段再与 hot screen 一起投影 logical rows。本文提到的 `HistoryTrack`、`LogicalLineStore`、`ScreenFrameJournal`、screen-backed truth 边界若与 linehist 基准冲突，以 `workflow.md` R431+ 和 `history/linehist` 为准。

## 1. 背景

`synchronized-output-history.md` 已经解决了第一层问题：Codex 这类 primary-screen pseudo-TUI 在 `DECSET 2026` / `DECRST 2026` 之间重绘时，history latest 必须看到 ESU 后的最终 primary frame，而不是中间半帧，也不能从 TUI live snapshot 回填。

用户复测暴露了第二层问题：当前 `screen-frame` / `alt-screen-frame` 仍是 latest-only。它能保证当前屏不丢，但当 Codex `/resume` 恢复旧会话、或 htop 这类 TUI 连续 repaint 时，older 只能回到普通 shell committed history，看不到同一 screen app 会话中上一帧或滚出当前 viewport 的内容。

标准终端语义本身只保证当前可见 grid 与普通 scrollback：

- 普通 append 输出会随着 LF/IND/scroll-out 进入 terminal scrollback。
- alt-screen 的内容默认不进入 primary scrollback。
- primary-screen TUI 可以用 CUP/ED/EL/scroll region/RI/2026 在当前 screen 中间重写内容；这些 repaint 不一定产生 terminal scrollback。

如果产品要求“进入 history/copy 后看到的内容和 live 当前终端一致，并且能继续向上浏览 screen app 产生过的内容”，core-v2 需要在标准终端语义之上增加一个明确的 screen app 会话历史模型。这个模型不能把 TUI 本地渲染、snapshot、vterm scrollback 或应用名当 truth；它必须仍然由 core-v2 `HistoryTrack` 拥有，并且以 logical line 为单位。

### 1.1 与 synchronized-output-history.md 的关系

`synchronized-output-history.md` 是当前已落地的 latest-only frame 设计基线：ESU 发布 current frame，frame 不进入 `CommittedHistoryIndex`，older 默认只回 committed history。

本文是下一阶段扩展设计，不是对当前实现的隐式改口。它只增加一个新的中间层：`ScreenFrameJournal`。该 journal 让 older 可以浏览 archived frame segment，但这些 segment 仍不计入 ordinary committed history，也不增加 `LogicalTotal`。

因此后续实现必须同时更新：

- `synchronized-output-history.md` 中“older 不返回 frame rows”的阶段性描述。
- core-v2 history/window harness 里 latest-only frame 的预期。
- protocol/TUI/App 对 cursor、row ownership 和 fixed-grid frame row 的 contract。

在这组 contract 改完之前，当前实现继续以 `synchronized-output-history.md` 的 latest-only 规则为准。

## 2. 目标

- 普通 shell / CLI 输出继续按现有 logical-line 模型提交 committed history。
- Codex、Claude Code、opencode、htop 等 TUI 或 semi-TUI 输出不按应用名特判，而是按终端语义进入 screen app 会话。
- screen app 运行中，repaint 修改的是会话当前 frame；不把每次 repaint 当普通 committed transcript。
- 在一个 screen app 会话内，旧 published frame 可以被归档为 frame journal，让 history older 先浏览本会话旧帧，再回到普通 committed history。
- primary screen app session 关闭时，只把最终 primary frame force commit 成普通 committed logical lines 一次。
- alt-screen 运行内容不写 primary committed history；是否归档为 screen app frame journal 由同一 screen app policy 决定。
- TUI-v3 和 App 仍只消费 core-v2 authoritative `history.window` / `history.copy`，不能从本地 VTerm scrollback、xterm buffer、snapshot rows 或 renderer rows 拼历史。
- frame journal 必须有去重、节流和容量边界，避免 htop 这类高频 repaint 无限制占内存或落盘。

## 3. 非目标

- 不实现“完整 PTY raw bytes 永久回放”。raw bytes 可以作为调试 fixture，但不是 history truth。
- 不模拟 tmux 的所有 scrollback 内部策略。termx 的 truth 仍是 core-v2 logical-line domain。
- 不让 alt-screen 默认污染普通 shell history。
- 不保证 screen app 运行期间每个动画帧都可回溯；归档的是有用户意义的发布帧，不是 renderer tick。
- 不为旧 storage、旧 protocol 或旧 TUI fallback 保留兼容分支。

## 4. 术语

### ordinary transcript

普通终端输出形成的 committed logical line。它进入 `CommittedHistoryIndex`，参与 `LogicalTotal`、older pagination、copy/search 和持久化 retention。

### screen app session

core-v2 对一个前台 screen app 输出阶段的 domain 表达。它可以发生在 primary screen，也可以发生在 alt-screen。进入条件来自终端语义组合，不来自进程名。

### current frame

当前会话最新发布的可见 frame。它由 core-v2 shared vterm 的 final screen cells 生成，进入 `LogicalLineStore`，但不进入 `CommittedHistoryIndex`。`history.window latest` 必须能看到它。

### archived frame

同一 screen app session 中被后续 frame 替换下来的旧 frame。它仍是 `LogicalLineStore` 里的 logical line 版本，并由 `ScreenFrameJournal` 索引；它不计入 ordinary committed depth。

### final frame commit

primary screen app session 关闭时，把最后一个 primary current frame 转成普通 committed logical lines 的动作。它只发生一次，不提交中间 repaint。alt-screen current frame 不执行 final frame commit。

它不是 screen app 会话历史的唯一保存点。Codex 聊了一晚上后，长期可浏览内容必须来自 `ScreenFrameJournal` / session history；final frame commit 只是在 ordinary history 里留下一个“程序退出时最后画面”的收尾，不能替代 session journal。

### frame journal

`HistoryTrack` 内部的 frame 索引。它只索引 `LogicalLineStore` 中的 frame logical line 版本，不是第二份 payload store，也不是 `CommittedHistoryIndex` 的替代品。

## 5. 权威边界

### owner

`termx-core-v2` 是唯一 owner：

- shared vterm 解释 PTY bytes 和终端控制语义。
- EventRouter 只从同一 write transaction 的 ordered semantic ops、mode、screen cells 和 frame boundary 生成 domain event。
- `HistoryTrack` 维护 ordinary committed history、mutable frontier、screen app session、current frame 和 frame journal。
- protocol adapter 只把 `HistoryTrack` 投影成 `history.window` / `history.copy`。
- TUI-v3 和后续重新设计的客户端只消费协议结果。

### 禁止路径

这些路径仍然禁止：

```text
process name -> screen app mode
TUI live rows -> history rows
renderer output -> copy/history source
snapshot scrollback -> committed history truth
vterm internal scrollback rows -> ordinary transcript payload
alt-screen final frame -> primary committed history by default
```

### 允许路径

允许的是同源终端语义进入 core-v2 domain：

```text
PTY bytes
  -> shared vterm write transaction
  -> ordered semantic ops + final primary/alt screen cells
  -> screen app classifier / frame publish event
  -> HistoryTrack LogicalLineStore + ScreenFrameJournal
  -> history.window latest/older
```

## 6. screen app 识别

识别必须基于终端语义，不基于应用名。第一版建议使用 conservative classifier：宁可少归档一些 frame，也不要把普通 shell 输出误当 screen app。

### 6.1 强信号

任一强信号可以进入 screen app session：

- `DECSET 1049/1047/47` 进入 alt-screen。
- `DECSET 2026` synchronized output 后，在同一事务内出现 home/top positioning、ED/EL、scroll region、RI、整屏或大块 repaint。
- primary screen 出现 `CUP/HVP 1;1` 或等价 home、随后 `ED0/ED2` 或覆盖大块 screen 的 repaint。
- 启用 mouse tracking 后持续使用 cursor addressing 更新 screen。
- 隐藏 cursor、启用 application key/cursor/keypad、bracketed paste、focus tracking，再配合绝对定位/清屏/局部 repaint。

### 6.2 弱信号

弱信号只能辅助确认，不能单独触发：

- 单次 `CUP` 或 `EL`。
- 单次 styled blank / background fill。
- 单次 terminal report request。
- prompt 行的 inline editing。

### 6.3 退出条件

session close 条件：

- foreground app 语义退出：core-v2 不能假设知道 shell 内部前台子进程 PID，因此第一版必须从终端语义上识别“screen app 输出结束，后续 batch 回到普通 append 模式”的边界。
- terminal lifecycle exit。它只是最后兜底，不能作为 shell 内部前台程序退出的唯一信号。
- alt-screen reset 回到 primary，且没有随后被同一前台程序发布的 primary frame 接管。
- primary screen app 明确恢复普通 prompt append 模式，并且后续输出只表现为普通 append transcript。

第一版建议新增 `ScreenAppCloseBoundary`：

- 对 primary screen app，遇到不带 fullscreen/synchronized/absolute cursor repaint 信号的普通 append batch，且该 batch 从 current frame 下方或 shell prompt 位置开始写入时，先 close primary session，再按普通 transcript 处理该 batch。
- 对 alt-screen app，`DECRST 1049/1047/47` 关闭 alt session；如果随后立刻出现 primary synchronized/fullscreen frame，则开启或继续 primary session，但不能把 alt frame 拼入 primary frame。
- 对 terminal lifecycle exit，关闭所有 session；只有 active primary current frame 可以 final commit，alt current frame 只按 alt policy 保留或丢弃。

必须补 harness 覆盖 `codex frame -> shell prompt -> ls output`：Codex 最终 primary frame 被提交一次，后续 prompt 和 `ls` 走 ordinary committed history，不进入 frame journal。

## 7. 数据模型

### 7.1 HistoryTrack 扩展

`HistoryTrack` 增加 screen app session 运行态：

```text
HistoryTrack
  LogicalLineStore
  CommittedHistoryIndex
  MutableFrontier
  ScreenAppSessionState
    active session id
    source: primary | alt
    current frame id
    frame journal index
    publish sequence
    retention counters
```

`ScreenAppSessionState` 不保存第二份文本 payload。frame row 内容仍在 `LogicalLineStore`。

### 7.2 frame logical line

frame logical line 是 logical line，不是 visual row truth。它需要额外 metadata：

```text
LineKind: screen-frame | alt-screen-frame
FrameID
SessionID
FrameRole: current | archived
FrameSource: synchronized-output | primary-fullscreen-repaint | alt-screen | exit-flush
ScreenRow
ScreenCols
FixedGrid: true
Committed: false
```

约束：

- frame line 是 `LogicalLineStore` 中的 `FrameLine` 子类型。它复用 logical line id、cells、style、link 和 observer pin 机制，但不使用普通 transcript reflow、committed cursor 或 resize reclaim 语义。
- current frame line 和 archived frame line 都不进入 `CommittedHistoryIndex`。
- frame line 可以包含 styled blank footprint、tail fill、OSC8 link、宽字符 cell 等 cell metadata。
- fixed-grid frame 不按普通 text reflow；不同 cols 下展示时按 frame 原始 screen cols clip/pad。
- `ScreenRow` / `ScreenCols` 只服务 frame segment 内部 ordering 和 fixed-grid projection，不是 ordinary history cursor，也不能参与 committed boundary。
- frozen snapshot 如果引用旧 frame，frame replacement 必须 copy-on-write，不能让旧 copy 会话看到新内容。

### 7.3 ScreenFrameJournal

`ScreenFrameJournal` 是索引，不是 store：

```text
FrameRecord
  FrameID
  SessionID
  Sequence
  Source
  Role
  LineIDs
  ContentHash
  ScreenSize
  PublishedAt
  ArchiveReason
```

规则：

- archive current frame 时，只把该 frame 的 `FrameRecord` 从 current role 改成 archived role，并把 line membership 标记为 archived。
- replace current frame 时，新 frame 分配新 `FrameID` 和 line ids。
- 若新 frame hash 与 current frame hash 相同，不归档、不替换，只更新必要 timestamp 或 generation。
- journal older 顺序是 newest archived frame 到 oldest archived frame。
- retention 删除 archived frame 时，从 journal 移除索引；如果没有 frozen snapshot 或 observer epoch 引用，对应 logical line payload 可交给 storage compaction。
- frozen snapshot 必须能 pin 住它可见的 current frame 和 archived frame line ids。snapshot release 前，retention 只能把这些 frame 从 active journal 中隐藏，不能释放 payload；如果实现选择不 pin payload，则所有引用该 frame 的 token 必须明确 stale，不能 silent fallback。

## 8. ingest 行为

### 8.1 普通模式

普通模式不变：

```text
write text -> logical line frontier
LF/IND/scroll-out -> commit sealed line
older -> CommittedHistoryIndex
```

### 8.2 screen app 模式

进入 screen app session 后，当前 screen 的 repaint 不直接增加 ordinary committed depth：

```text
terminal semantic ops
  -> mutate vterm screen
  -> at publish boundary build final frame rows
  -> archive previous current frame if needed
  -> publish new current frame
```

发布边界包括：

- ESU (`DECRST 2026`)。
- alt-screen frame publish boundary。
- primary fullscreen repaint transaction 结束。
- process exit flush。

### 8.3 scroll-out 的处理

screen app session 内的 primary `ScrollbackAppend` 不能再简单等同 ordinary transcript commit。

规则：

- 如果当前 batch 被 classifier 判定为普通 append 输出，scroll-out 继续按 ordinary commit。
- 如果当前 batch 属于 screen app repaint，scroll-out 只是证明 screen ownership 变化；默认进入 frame publish / frame journal 逻辑，不把 vterm scrollback row 当 ordinary payload。
- 如果 screen app 明确输出普通日志段，再回到 repaint，可以由 session 内的 `TranscriptSubspan` 后续扩展表达；第一版不做自动猜测。

这个规则避免 Codex `/resume` 大块恢复时只保留当前 viewport，而把前面的恢复内容错误裁掉或错误混进旧 shell history。恢复内容应作为 screen app session frame/journal 浏览；最终退出时只提交最终 frame。

### 8.4 frame replacement

发布新 frame 时：

1. 从 shared vterm 当前 primary/alt screen cells 构造 fixed-grid frame logical lines。
2. 计算 frame hash。
3. 如果 hash 与 current frame 相同，忽略该 publish。
4. 如果存在旧 current frame，按 retention policy 归档为 archived frame。
5. 新 frame 作为 current frame 进入 `LogicalLineStore` 和 `ScreenAppSessionState`。
6. bump history generation，使 `history.window latest` 和 active frozen token 失效或更新。

### 8.5 process exit

process exit 是强 close boundary：

1. 如果有 pending synchronized frame，先 flush 成 current frame。
2. 如果 active session source 是 primary，且有 primary current frame，把它转成普通 committed logical lines。
3. 如果 active session source 是 alt，不执行 ordinary committed commit；alt current frame 只按 `CurrentOnly` 或 `Journaled` policy 保留，绝不写 primary committed history。
4. archived frames 默认不进入 `CommittedHistoryIndex`；它们只作为可浏览 session journal 保留到 retention。
5. 清理 active session state。
6. 写入 lifecycle marker 或 exit marker 时，必须与 primary final frame commit 保持同一 transaction 顺序。

最终效果：运行中可以翻 session frame journal；primary screen app 关闭后普通历史里只看到最终画面一次，不会看到每次 repaint；alt-screen 程序退出后不会污染 primary committed history。

## 9. history.window contract

当前 `HistoryWindow` 只表达 committed rows + latest current frame，不足以表达 frame journal older。需要把 older cursor 扩展为分段 cursor。

### 9.1 分段顺序

latest window 的逻辑顺序：

```text
ordinary committed tail before session
current frame
```

从 latest 往 older 翻页：

```text
current frame
newer archived frames
older archived frames
ordinary committed history before session
```

也就是说，用户向上滚动时先看到同一 screen app session 中较早的 frame，再回到进入 app 之前的 shell history。

### 9.2 cursor

目标模型需要 internal domain cursor：

```text
HistoryWindowCursor
  Segment: current-frame | archived-frame | committed
  SessionID
  FrameID
  LogicalLineID
  RowOffset
  Direction
  Generation
```

第一版实现可以沿用现有 `before_line_id/before_row_in_line`，但前提是 segment 判定完全在 core-v2 内完成：`screen-frame`、`archived-screen-frame`、committed line id 都来自同一个 `LogicalLineStore`，line id 全局唯一，core 在 `HistoryTrack` / frozen snapshot 里按 journal membership 组装 older 顺序。TUI 只能回传 core 给出的 cursor，不能根据 row kind 自己推断下一段。

这种做法不是把 segment 塞给 TUI；它只是把 line id 当作 core-owned boundary key。后续如果需要跨进程长期稳定 cursor、newer 双向分页、retention stale 诊断或多 session frame journal，再升级为显式 segment cursor 或 opaque cursor blob。

建议 wire 增量：

```text
cursor_segment = committed | current_frame | archived_frame
cursor_session_id
cursor_frame_id
cursor_line_id
cursor_row_in_line
```

也可以直接使用一个 core-v2 opaque cursor blob，但 blob 必须由 core 生成和校验，且 protocol/TUI/App harness 要证明跨 segment older/newer 不依赖本地推断。`token` 只 pin frozen snapshot，不替代 cursor segment。

### 9.3 row ownership

需要保留或扩展 row ownership：

```text
committed
live-tail-frame
archived-frame
alt-screen-frame
```

ownership 只告诉 client 如何渲染和锚定，不授权 client 推断 history truth。

### 9.4 copy/search

copy/search 必须在 core-v2 frozen/tokenized source 上工作：

- 范围跨 current frame、archived frame、committed history 时，core 按 segment cursor 顺序组装文本。
- frame fixed-grid 行按 cell text 和 style footprint 转文本；不按当前 pane cols 重排。
- frozen snapshot 应扩展为 ordered segments：committed segment、archived frame segments、current frame segment。每个 segment pin 住自身 line ids 和 frame metadata。
- archived frame 被 retention 删除后，已有 frozen token 要么继续引用 copy-on-write payload，要么返回明确 stale，不允许 silent fallback 到 TUI cache。
- selection/copy 跨 fixed-grid frame 和 ordinary logical line 时，frame segment 使用原始 cell columns，ordinary segment 使用 logical-line text/range；两者不能共享同一种 reflow cursor。

## 10. alt-screen 策略

alt-screen 有两类常见用途：

- htop/vim 等完整 screen app：用户可能希望运行中 history/copy 能看到当前或近期 frame，但这些 frame 不应写 primary committed history。
- Codex `/resume` picker、modal、临时列表：它只用于选择，退出后不应污染普通 history，也不应和后续 primary restored frame 拼接。

因此 alt-screen 不应只有一个策略。建议引入 frame archive policy：

```text
CurrentOnly
  只作为 latest current frame 可见，退出后丢弃，不归档，不提交。

Journaled
  current frame 被替换时进入 frame journal，older 可浏览，但不进 ordinary committed history。

CommitFinalOnExit
  只适用于 primary screen app final frame；alt-screen 默认不使用。
```

第一版建议：

- alt-screen picker 默认 `CurrentOnly`。
- 长时间运行且持续 repaint、启用 mouse/cursor hide/application modes 的 alt-screen 可升级为 `Journaled`。
- alt-screen reset 时不 force commit 到 primary history。
- 如果 alt-screen 后同一进程立刻发布 primary frame，primary frame 开启或继续 screen app session，但不能把 alt picker frame 和 primary restored frame 拼成一个 screen。

## 11. resize / attach / clear screen

### resize

- resize 不提交 ordinary history。
- resize 不把 archived frame 重排成新宽度。
- archived frame 带自己的 `ScreenCols`；history render 可 clip/pad。
- resize 发生在 pending transaction 中，ESU 按 resize 后 vterm screen 发布新 current frame。
- resize 后如果程序没有发布新 frame，latest 可继续返回旧 current frame，并通过 generation/token 表达 stale 或 unchanged。

### attach / reattach

- attach/reattach 只请求 authoritative latest window。
- attach/reattach 不创建 frame、不归档 frame、不提交 frame。

### clear screen / Ctrl+L

- `ED2` / Ctrl+L 类清屏只影响 current viewport/page-break，不物理删除 authoritative committed history。
- `ED3` clear scrollback 作为 soft boundary，不删除 ordinary committed index。
- 在 screen app session 内，clear screen 多半是 repaint 的一部分，应进入 frame replacement，不当成 ordinary history truncate。

## 12. retention 和性能

htop 这类程序可能每秒多次 repaint。frame journal 必须 bounded。

第一版建议：

- per session 最大 archived frame 数，例如 128。
- per terminal 最大 frame journal bytes，例如按配置默认 16-64 MiB。
- hash 去重：内容完全相同不归档。
- publish 节流：同一 session 高频 repaint 可按最小间隔或 semantic boundary 合并。
- 优先保留用户操作边界后的 frame：输入、命令提交、picker 选择、ESU、alt enter/exit、resize 后首帧。
- retention 删除 oldest archived frames，不影响 ordinary committed history。

如果未来要落盘，storage backend 仍只保存 `LogicalLineStore` payload 和 journal index；不能把 frame journal 变成另一套 row store。

## 13. harness 计划

### 13.1 domain harness

- 普通 shell 输出不进入 screen app session，LF 后 committed depth 增长。
- primary screen app 三次 repaint：latest 显示第三帧，older 先返回第二帧、第一帧，再返回进入 app 前的 shell history。
- repeated identical frame 不产生 archived frame。
- primary session close 后 final frame committed 一次，但 archived frames 仍作为 screen app session history 可浏览；它们不进入 ordinary committed depth。
- alt-screen session close 和 terminal lifecycle exit 都不把 alt current frame 写入 primary committed history。
- `codex frame -> shell prompt -> ls output`：screen app close boundary 先提交 primary final frame，再让 prompt/`ls` 走 ordinary committed history。
- screen app session 内 `ED2/ED3` 不删除 ordinary committed history。
- frozen snapshot 持有 archived frame 时触发 journal retention：payload 仍可 copy/older，或者 token 明确 stale；不能 silent fallback。
- fixed-grid `FrameLine` 与 ordinary logical line 混合 copy：frame 段按原始 cell columns，ordinary 段按 logical text/range。

### 13.2 shared-vterm semantic harness

- `DECSET 2026` 多次 BSU/ESU：每次 ESU 发布 frame，旧 frame 进入 journal。
- `RequiresFullReplace` + ordered semantic ops：full replace 只作为 live/stale boundary，frame/journal 由 semantic publish 决定。
- Codex `/resume` raw fixture：picker alt frame 不与 primary restored frame 拼接；primary restored frame older 可回到同 session archived frame。
- htop-like alt-screen repaint：frame hash 去重和 retention 生效，不写 primary committed history。
- Codex 退出到 shell prompt raw fixture：不依赖 terminal lifecycle exit 识别 session close。

### 13.3 protocol harness

- `history.window latest` 返回 current frame rows 和 segment-aware cursor。
- older cursor 先走 archived-frame segment，再走 committed segment；segment 边界必须由 core 生成和校验。
- 第一版可以使用 core-owned `before_line_id/before_row_in_line` 作为 boundary key；如果后续无法稳定表达 frame segment，协议扩展测试必须先失败并驱动 `.proto` / `internal/protocol` 更新。
- row ownership、line kind、fixed-grid metadata 端到端保留。
- stale token 不允许 fallback 到 local cache。
- frozen token pin 住 archived frame 后，`history.copy` 和 `history.release` 生命周期正确释放 observer。

### 13.4 TUI/App harness

- copy/history 入口只消费 protocol rows。
- older 翻页看到 archived frame，不读取 live snapshot/VTerm/xterm scrollback。
- frame fixed-grid 在窄宽度下不按普通 logical text reflow。
- selection/copy 跨 current frame、archived frame、committed history 时文本顺序稳定。
- alt-screen picker current-only 退出后不会出现在 ordinary history 或 primary archived frame journal。

## 14. 迁移切片

建议后续按小切片推进：

1. `R201DM` 文档和 review：确认 screen app session history 模型。
2. domain model harness：新增 `ScreenAppSessionState`、`ScreenFrameJournal` 纯内存测试，不接真实 PTY。
3. protocol cursor contract：先扩展 domain 和 wire cursor，让 current/archived/committed segment 可由 core 稳定表达。
4. frozen snapshot pinning：让 current/archived frame line ids 与 observer lifecycle 纳入 token。
5. primary synchronized frame archive：ESU replace 前归档旧 current frame。
6. primary close/final commit：session close 时只提交最终 primary current frame一次，并验证 shell prompt 回归普通 history。
7. alt-screen policy：picker current-only 与 long-running journaled 分开，且 alt 退出永不写 primary committed history。
8. retention/storage：frame journal 去重、节流、容量上限和 compaction。
9. TUI/App protocol smoke：确认 copy/history 不使用本地 fallback。

## 15. 失败条件

实现后如果出现下面现象，说明模型仍错：

- screen app repaint 仍直接增长 ordinary committed depth。
- history older 从 current frame 直接跳到进入 app 前的 shell history，看不到本 session archived frame。
- alt-screen picker 和 primary restored frame 被拼成一个 screen。
- `/resume` 后只能看到当前 viewport，前一帧或同 session 旧 frame 全丢。
- process exit 后 ordinary history 出现每次 repaint，而不是最终 frame 一次。
- Ctrl+L / ED3 物理删除 authoritative committed history。
- TUI/App 需要从 live snapshot、xterm buffer 或 renderer rows 才能复制完整文本。
- htop 高频 repaint 导致 frame journal 无上限增长。

## 16. 当前结论

用户提出的方向是对的，但需要把“只在程序退出时 commit”拆成两层：

- 对 ordinary committed history：screen app 运行中不提交 repaint，退出时只提交最终 frame。
- 对 history/copy 可浏览内容：运行中不能只保留 latest current frame，还需要 core-owned frame journal，否则标准终端语义不会帮我们保存那些被 repaint 覆盖掉的旧 screen。

因此更合适的模型不是让 TUI 猜 Codex 输出，也不是把 vterm scrollback 当历史，而是在 core-v2 `HistoryTrack` 内建立 screen app session：current frame 可变、旧 frame 归档、ordinary history 独立、退出时 final commit。
