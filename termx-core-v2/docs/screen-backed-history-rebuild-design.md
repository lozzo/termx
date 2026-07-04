# screen-backed history rebuild design note

状态：R430 后本文是当前 screen-backed infinite history production 基准。R419-R427
完成 domain model、projection 与验收；R428 已把 Terminal 默认入口切到共享
`ScreenHistoryBuffer`；R429 已把 sealed physical rows 接入 `.screen-rows.bin`
production backend；R430 冻结旧 mutation-backed logical-line renderer/store 为显式
factory 和迁移 harness，不再允许默认 daemon 回退。

本文是 R419 勘察与迁移基线。目标是把 infinite history 的正文真值从
append-only logical-line reducer 前移到 authoritative screen model：PTY bytes
先经 vterm 语义事务更新 physical rows/cells，再由 projection 在
HistoryWindow/Copy/Search 查询阶段生成 logical lines。

## 1. 当前链路勘察

### 1.1 PTY bytes 到 history window

当前 `termx-core-v2/terminal.go` 的输出路径已经拆成两条消费者：

- live path：`watchOutput` 把 PTY chunk 投给 `terminalLiveIngestQueue`，
  `ingestProcessLiveOutput -> applyLiveOutput -> live.SurfaceTrack.Write` 维护最新
  native screen 并发布 live invalidation。该路径只服务实时显示和 response owner。
- history path：同一 PTY chunk 投给 history tap queue，
  `ingestHistorySemanticOutput -> SemanticTap.ApplyPTYWrite` 得到
  `TerminalSemanticTransaction`，再经 `SemanticTapResult.HistoryJournal()` 或
  `HistoryJournalFromDecision` 裁剪成 compact `HistoryJournal`。
- backlog path：`terminalHistoryIngestQueue` 只保存 `HistoryJournal` 副本，按 seq
  批量交给 `HistoryJournalRenderer.ApplyJournal`，再合并
  `HistoryMutationBatch` 后调用 `HistoryStore.Apply`。
- query path：`Terminal.HistoryWindow/HistoryCopy/HistoryFreeze/HistoryRelease` 只读
  `HistoryStore`，protocol/TUI 继续消费 core-v2 authoritative window。

这个链路已经避免 raw PTY replay 和 live snapshot 反推 history，但 history 正文仍在
`journal -> StreamLineReducer/FrameReducer -> LogicalLine/FrameJournal` 中生成。

### 1.2 vterm semantic transaction

`termx-vterm/vterm/semantic_source.go` 已经提供 screen-backed rebuild 需要的主要输入：

- ordered `Ops`：write、cursor move、erase、insert/delete、scroll/copy rect、mode、
  resize 等 terminal semantic op。
- `PrimaryScrollOut`：同一 transaction 中 primary viewport 离屏 row 的 proof。
- `PrimaryFrame` / `AltFrame`：用于复杂 repaint 或 alt transient 的当前屏 side proof。
- `PrimaryFrameTouchedRows`：非 full-replace 优先从 ordered ops 推导，full-replace 才保留
  direct damage rows。
- alt/sync/clear/resize flags：`AltEntered/AltExited`、`SynchronizedBegin/Active/End`、
  `ClearScrollback`、`RequiresFullReplace`。

这些字段应继续保留。`ScreenHistoryBuffer.ApplyTransaction` 消费同一 transaction，
不能从 `Raw`、live `SurfaceTrack`、TUI rows 或 parser fallback 建第二份 truth。
R431 后 `ScreenOpCopyRect` 也按 in-place physical cell mutation 处理：只更新目标 physical row 的 cells/version，不 seal、不 append logical history，也不改变未触及 rows 的 RowID。

### 1.3 classifier / journal / renderer

`history/classifier.go` 和 `Terminal.historyDecisionForTransaction` 目前把 transaction 分到
ordinary stream、primary frame session、alt transient 或 boundary-only。它还承担了很多正文
正确性判断，例如 touched-row ownership、scroll-out proof 是否消费、ordinary 输出恢复前是否
close primary frame。

`history/journal.go` 当前把 decision 编码为四类正文/边界命令：

- `OrdinaryLineBatch`：包含 sealed logical lines、open-line update 和 open-line commands。
- `Boundary`：ED2/ED3/RIS/resize/alt/sync。
- `ScrollOutProof`：把 vterm proof 交给 stream reducer seal。
- `FrameEvent`：replace/archive/clear/final primary 或 alt frame。

`history/journal_renderer.go` 仍是正文 history reducer：ordinary batch 走
`StreamLineReducer`，frame event 走 `FrameReducer`，最终写出 `LogicalLine`、
`HistoryRecord`、`MutableFrame` 和 `SealedFrame` mutation。

新架构中 journal 可以暂时保留为 backlog 和 boundary/meta queue，但正文 mutation owner 应迁到
`ScreenHistoryBuffer`。classifier 只判断 mode/boundary/sync/alt，不再决定正文是否正确。

### 1.4 HistoryStore / TUI history boundary

`history/in_memory_store.go` 现在维护 logical line payload、sealed timeline、open line、
frame journal、frame records、frozen projection 和 backend projection index。latest window 已经
有 `ScreenRow` 锚点，能避免 current primary frame 入口塞入过多 sealed tail，但它仍是在
logical/frame record 层拼 projection，并没有 physical RowID/Version/seal-once invariant。

`termx-tui-v3/docs/architecture.md` 的边界仍然正确：TUI `HistoryStore + CopyModeStore` 只能消费
core authoritative `HistoryWindow`，copy-history render 不能读取 live surface、local VTerm
scrollback、snapshot/grid viewport 或本地 row ownership。

## 2. 仍在把 pseudo-TUI frame 转成 logical history 的位置

这些位置是后续替换目标：

- `Terminal.historyDecisionForTransaction`：判断 primary pseudo-TUI，并设置
  `PublishPrimaryFrame` / `PublishPrimaryFrameTouchedRowsOnly` / scroll-out flags。
- `HistoryJournalFromDecision`：把 `PrimaryFrame` 和 `PrimaryFrameTouchedRows` 生成
  `HistoryJournalFrameReplacePrimary`。
- `HistoryJournalRenderer.applyBoundaryFrameEvent`：把 frame event 交给
  `FrameReducer.ReplacePrimaryTouchedRows` 或 `ReplacePrimaryCurrent`。
- `FrameReducer` 及 store frame journal：把 touched rows/current frame 转为
  logical screen-frame rows；这些 rows 后续可能 close/archive 到 sealed timeline。
- `HistoryJournalRenderer.applyScrollOutProof`：把 proof 直接 seal 成 logical line。
- `journalOrdinaryRecorder` / `StreamLineReducer`：普通输出和部分 CR/EL/CUP 编辑仍在
  ingest 阶段形成 logical/open line，而不是先修改 physical row。

这些路径不是要一次删除，而是按 slice 逐步让 pseudo-TUI primary path 先切到 screen buffer。

## 3. 保留接口与替换实现

必须保留的外部接口：

- `history.TerminalSemanticTransaction` 及 vterm semantic op contract。
- `Terminal.HistoryWindow`、`HistoryCopy`、`HistoryFreeze`、`HistoryRelease`。
- protocol/TUI 的 authoritative `HistoryWindow`、row kind、token、cursor、generation 边界。
- live `SurfaceTrack` 快路径和 response owner 边界；live rows 仍不能成为 copy/history truth。
- file-backed history backend 的窗口读取能力，但 payload schema 后续可破坏性调整。

需要替换的内部实现：

- 新增 `ScreenHistoryBuffer`，作为 history ingest 的 screen/cell/row state machine owner。
- `ScreenHistoryBuffer` 维护 main/alt grid、stable RowID、Version、Cursor、Margins、
  row owner、seal-once set 和 committed physical rows。
- logical line 只由 projection layer 从 committed physical rows + current screen rows 生成，
  projection 必须保留 `RowIDs` 并用 RowID 去重。
- `HistoryStore` 可先新增 screen-backed implementation，后续再收缩旧 logical/frame store。
- journal 长期收缩为 boundary/meta/event journal；正文 history truth 不再由
  `StreamLineReducer`/`FrameReducer` 持有。

## 4. 迁移基线测试

现有回归不能破：

- `r328_clear_repaint_regression_test.go`：ED2/ED3、clear repaint、多 session history、CJK。
- `r331_screen_frame_to_prompt_order_test.go`：primary frame close 后 prompt 顺序。
- `r334_shell_tail_primary_frame_dup_test.go`：sealed shell tail 不被 current/final frame 重复接管，
  full-replace、scroll-out proof、真实 vterm synchronized output、R415/R417/R419 分类边界。
- TUI architecture guard：copy/history render 只来自 authoritative rows，不混入 live surface。

新增 screen model 测试必须覆盖附件要求的普通 shell、Codex-style pseudo-TUI、progress bar、
EL/DCH/ICH、scroll region、alt-screen、clear repaint、wide char 和 idempotency。

## 5. 分阶段落地

1. R420：只引入 `ScreenHistoryBuffer` domain model 与 synthetic op harness，不接 terminal/TUI。
2. R421：补齐基础 VT 控制序列、scroll region、alt-screen、wide char 和 idempotency invariant。
3. R422：新增 physical-row 到 logical-line projection，锁定 RowID dedupe 与 latest window 拼接。
4. R423：新增 ANSI harness/fixtures，能 dump screen/committed/logical 三份状态。
5. R424：把 primary pseudo-TUI path 先切到 screen buffer，普通 shell append path 可暂留旧逻辑。
6. R425：HistoryWindow/Copy/Frozen projection 改为 screen-backed projection。
7. R426：收缩旧 journal 正文 reducer，只保留 boundary/meta/event backlog。
8. R427：多角色 review、完整准入、性能与 1M stress 回归。
9. R428：Terminal 默认 history path 创建同一个 `ScreenHistoryBuffer`，transaction
   renderer、journal renderer 和 `ScreenBackedHistoryStore` 共享 physical row truth；
   配置 `WithHistoryStorageDir` 不再把默认 daemon 带回旧 logical-line file store。
10. R429：sealed physical rows 接入 production `ScreenPhysicalRowBackend`；恢复阶段只恢复
    RowID/row count/applied seq 等 row store 元数据，window 分页按 row range 读取，不把
    无限历史重新 materialize 到 `ScreenHistoryBuffer.Committed`。
11. R430：旧 `HistoryLogicalRenderer`、`NewInMemoryHistoryStore`、logical-line file backend
    冻结为显式 `WithHistoryStoreFactory` / legacy harness 路径；当前生产入口不得调用它们
    作为默认 truth 或 fallback。

第一轮实现不能暂缓 RowID、seal-once、identity-based projection、current screen 与 committed history
去重，也不能让 pseudo-TUI repaint 继续直接 append committed logical rows。
