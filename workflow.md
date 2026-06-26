# 工作流：screen app 无限历史清场与重建主线

本文件是当前分支唯一有效的活动驱动文件。后续分析、实现、测试、提交都先看这里；如果本文件与旧说明、聊天记录、旧代码行为或局部假设冲突，以本文件为准。

本文件已经从旧 remote + app 迁移队列重置为 screen app 无限历史主线。旧队列、旧完成记录和旧补丁路径只通过 git 历史追溯，不再作为当前实现依据。

## 0. 怎么读

开始任何工作前只看这几段：

- `1. 当前目标`
- `2. 技术设计基准`
- `3. 工作范围`
- `4. 不可违反的语义`
- `5. 清场规则`
- `6. 任务队列`
- `7. 测试准入`
- `8. 提交规则`

如果用户请求和任务队列冲突，先更新本文件，再改代码；不要靠口头约定跳过范围和顺序。

## 1. 当前目标

### 1.1 一句话目标

实现接近 tmux/普通终端体验的无限历史：只要内容真实经过 PTY 并被终端语义解释到 primary/history 轨道，就应能在历史模式中回看、搜索和复制；实现方式必须以 core-v2 logical line 为唯一历史真值。

### 1.2 这轮只关心什么

当前阶段先清掉旧的补丁式历史实现，再立接口和 harness，最后按接口推进实现：

1. 删除或隔离旧的 screen app/history 补丁路径，避免后续 Agent 顺着旧逻辑继续打补丁。
2. 定义 core-v2 与 termx-vterm 之间的 terminal semantic transaction 接口。
3. 定义普通输出、primary screen app、alt-screen transient、final screen-frame、segment cursor、resize、色彩属性和文件存储的边界。
4. 先补小 harness 锁住边界，再接真实 PTY/vterm/protocol/TUI。
5. tui-v3 和 App 只消费 core-v2 authoritative history window，不从本地 VTerm scrollback、snapshot、DOM/canvas rows 或 live cache 推断历史真值。

### 1.3 不在当前阶段做什么

- 不修旧历史代码上的单点 bug。
- 不为旧 storage、旧 protocol、旧 snapshot/workbench schema 或旧运行时行为做兼容。
- 不恢复旧 `termx-core/` 或 `tuiv2/`。
- 不把 Codex、Claude Code、htop、vim 这类程序名写成特殊分支；只能通过终端语义和屏幕行为分类。
- 不保证读取程序没有输出到 PTY 的内部状态。目标是 tmux/终端级别的可观察历史，不是读取 Codex 内部 session 数据库。
- 不迁 `web-control/`、`termx-hub/`、`termx-remote-v2/`。
- 不继续推进旧 remote + app 队列里的 Web desktop 可见性问题；那条线已退出当前主线。

## 2. 技术设计基准

- 当前 history 语义设计基准：`termx-core-v2/docs/history-logical-renderer-design.md`
  - R318 后，history 被定义为消费 vterm terminal semantic transaction 的 logical-line renderer。
  - 新模型使用 `mutable` / `sealed` / `timeline` / `frame journal` 语义，不再把 `commit` / `committed` 作为领域概念。
  - 如果旧文档、旧代码类型名、历史聊天记录或旧测试描述仍使用 `commit/committed`，默认按新文档翻译为 sealed/timeline/lifecycle close 语义。
- 旧 screen app 无限历史定案：`termx-core-v2/docs/screen-app-infinite-history-final-plan.md`
  - 该文档保留 R300-R317 的问题背景、vterm transaction 边界和已完成切片记录；其中 `commit/committed` 术语和 projector/store 分层若与新文档冲突，以 `history-logical-renderer-design.md` 为准。
- core-v2 架构：`termx-core-v2/docs/architecture.md`
- tui-v3 架构：`termx-tui-v3/docs/architecture.md`
- vterm terminal 语义来源：`termx-vterm/`
- 旧 history 说明、session history 说明和同步输出说明只能作为问题背景；如果与本文件或定案冲突，以本文件和定案为准。

如果实现发现设计文档过期，必须和当前切片一起更新；不要代码先跑偏，文档以后再补。

## 3. 工作范围

### 3.1 当前主线允许主动修改

- `workflow.md`
- `AGENTS.md`
- `termx-core-v2/`
- `termx-core-v2/docs/screen-app-infinite-history-final-plan.md`
- `termx-vterm/`，仅限提供 terminal semantic transaction 所需的最小接口、事件和 harness

### 3.2 受限联动范围

只有当前切片确实需要时，才允许最小化触及：

- `termx-tui-v3/`，只接 authoritative history window、copy mode、scroll/selection/render harness
- `internal/protocol/` 与 `termx-proto/`，只在 `history.window`、history copy 或 semantic history contract 需要跨进程时修改
- `termx-cli/`，只在默认入口守卫、smoke 或必要 CLI glue 需要时修改
- `termx-shared/`
- `termx-testkit/`
- `scripts/`
- `Makefile`
- `go.work`
- `go.work.sum`
- 必要顶层说明文档

### 3.3 已删除旧目录

默认不得恢复：

- `termx-core/`
- `tuiv2/`

### 3.4 冻结范围

不得主动触碰，除非本文件先明确解冻：

- `termx-remote/`
- `termx-remote-v2/`
- `termx-app/`
- `remote-ui/`
- `web-control/`
- `termx-hub/`
- `bin/`
- `.claude/`
- 顶层可执行产物和测试产物
- 未在本文件列出的目录

## 4. 不可违反的语义

### 4.1 历史真值

- 历史 truth 的基本单位是 logical line，不是 visual row、wrapped row、snapshot scrollback、grid viewport 或 xterm buffer row。
- core-v2 的 logical-line history 是唯一历史数据模型。
- R318 后，历史领域语义使用 `mutable` / `sealed`：`mutable` 表示仍可被当前 open line 或 current frame 改写，`sealed` 表示已按 terminal/session 语义离开当前可变区域。
- `commit` / `committed` 不再是新模型的领域概念；旧代码名若尚未迁移，只能按 sealed/timeline/lifecycle close 语义理解。
- sealed timeline、open line、mutable/current frame、frame journal、segment cursor、storage backend、cache、adapter、TUI/App projection 都不能演变成第二份历史 truth。
- `persisted` 或落盘不表示 sealed，也不表示不可修改；是否可修改由 terminal/session 语义决定。

### 4.2 terminal 语义来源

- 不能把 raw PTY bytes parser 作为 terminal 语义 owner；raw parser 不能 fallback 出第二套历史。
- core-v2 应消费 termx-vterm 解释过程中的语义 transaction，而不是消费最终屏幕快照。
- vterm 的当前屏幕不是无限历史来源；它最多证明“终端语义解释过程可以产生可记录事件”。
- 与 tmux 一样，只有程序真实输出到 PTY 的内容才能进入历史；没有输出过的程序内部状态不在目标内。

### 4.3 普通输出

- 普通 shell/程序 stdout 一旦按终端语义形成完整 logical line，就可以 seal 进 history timeline。
- 普通输出不应长时间持有 screen app session。
- 进程退出必须 seal 普通输出 open line。

### 4.4 screen app session

- pseudo-TUI 类程序在 primary screen 上反复改写时，必须有一个可变 session。
- session 内允许程序修改从 session 开始到当前的可变内容。
- session 暴露给历史模式前，必须先通过 segment cursor/archive 形成一致视图，不能展示半更新状态。
- session 最终退出或模式切换时，按分类决定 seal 或丢弃：普通 primary screen app 可以 seal final screen-frame；纯 alt-screen transient 不写 primary history。

### 4.5 alt-screen

- alt-screen 不写入 primary history。
- htop/vim 这类纯 alt-screen transient：运行期间可在当前屏幕选择；退出时不把屏幕内容 commit 到 primary history。
- Codex/Claude Code 这类 primary screen app 临时进入 alt-screen 选择器时：进入 alt 前必须 archive/hide 当前 primary frame；alt 内容作为 live transient 展示和选择；退出 alt 后如果出现新的 primary 输出，必须作为新的 primary frame publish，可以接回同一 session journal，但不得复活 pre-alt current frame，也不得凭空 commit alt 屏幕。
- 如果程序运行期间用户进入历史模式，当前 session 的文本内容必须作为历史视图的一部分，但仍需标记为 mutable/frozen projection，而不是当成 sealed truth。

### 4.6 final screen-frame

- primary screen app 退出时允许把最后一屏作为固定宽度 screen-frame seal。
- final screen-frame 是为了保存“屏幕程序最终展示的内容”，不是普通 logical line reflow。
- final screen-frame 后续不随 resize 重排。

### 4.7 resize

- resize 不得重写 sealed history。
- 普通 logical line 可以在展示层按新宽度重新 wrap。
- mutable screen app session 在 resize 时必须保留可变语义，不能凭空产生 sealed history。
- final screen-frame 必须固定生成时的列宽。
- Codex/Claude Code 运行中 resize 是硬 harness，不能靠程序名特殊适配。

### 4.8 色彩和主题

- 历史应保存终端语义属性，不应把“当时主题下的默认前景/背景 RGB”提前烘焙成不可变颜色。
- SGR 明确指定的颜色可以作为具体颜色或调色板索引保存。
- default fg/bg、bold、dim、inverse 等应尽量保存为语义属性，由查看历史时的主题解析。
- 如果终端应用写入明确 RGB 背景，则该 RGB 属于内容属性，不能被后续主题替换。

### 4.9 TUI/App 边界

- tui-v3 不拥有 sealed history truth，只消费 core-v2 authoritative `HistoryWindow`。
- tui-v3 copy mode 不得从本地 VTerm scrollback、snapshot totals、row ownership、LoadedRows、wrapped 拼接结果推断历史。
- App/Web/native live display 可以有本地短缓存，但 copy/search/history truth 必须走 core-v2 logical-line window。
- renderer 只消费 view-model，不读 core client、history source、runtime service 或 protocol client。

## 5. 清场规则

这条主线允许先删后写，但删除必须有边界。R301 的“清场”是审计和最小隔离优先，
只删除会继续误导实现的补丁入口；大规模删除旧 raw parser / fallback 必须等 R302/R303
接口和 harness 站稳后再做。

- 可以删除旧补丁式 screen app/history 代码、raw parser fallback、程序名分支、snapshot/history 拼接 fallback、重复同步和隐式状态修正。
- 删除后必须保留可编译的最小骨架，或者在当前切片内补上替代 interface/harness。
- 不要为了保留旧测试而保留错误模型；旧测试若表达的是旧补丁语义，应删除或改写成新模型 harness。
- 任何“先兼容旧路径，后面再切”的方案默认不合格，除非本文件明确列为当前切片。
- 不得用 storage scrub、定时刷新、重复 attach、局部 fallback 分支掩盖状态错乱。
- 关键代码需要写简短中文注释，说明 domain owner、truth source、消息链路或失败条件。

## 6. 任务队列

状态只能使用：`待开始`、`进行中`、`完成`、`阻塞`。同一时间只能有一个切片处于 `进行中`。

自动执行时只看下面这张表，按顺序取最早未完成切片：

| 切片 | 状态 | 范围 | 白话说明 |
| --- | --- | --- | --- |
| R300. SK 无限历史工作流重置与定案纳入 | 完成 | `AGENTS.md`、`workflow.md`、`termx-core-v2/docs/screen-app-infinite-history-final-plan.md` | 旧 remote + app 队列退出当前主线；screen app 无限历史定案成为当前实现基准 |
| R301. SK 旧历史实现清场审计与最小隔离 | 完成 | `termx-core-v2/`、只读 `termx-vterm/`、相关 docs | 找出旧 screen app/history 补丁路径、raw parser fallback、snapshot 拼接和程序名特殊逻辑；只删除或隔离会继续误导后续实现的入口，保留可编译骨架和清场 harness，大规模删除等 R302/R303 接口站稳后再做 |
| R302. SK terminal semantic transaction 接口 | 完成 | `termx-core-v2/`、`termx-vterm/` | 先立 vterm 到 core-v2 的语义事件接口：普通写入、cursor move、erase、scroll、alt enter/leave、resize、SGR 属性、flush/transaction boundary |
| R303. SK history projector domain harness | 完成 | `termx-core-v2/` | 用 fake semantic transaction 驱动 projector，锁定普通输出 commit、mutable session、segment cursor、final screen-frame、alt transient 的基本语义 |
| R304. SK 普通输出最小实现 | 完成 | `termx-core-v2/`、按需 `termx-vterm/` | shell/stdout 输出完整 logical line 后直接进入 committed history；process exit force commit primary mutable frontier |
| R305. SK primary screen app session 最小实现 | 完成 | `termx-core-v2/`、按需 `termx-vterm/` | 支持 Codex/Claude Code 这类 primary screen app 在退出前修改 session 内文本；历史模式看到一致的当前 session projection |
| R306. SK alt-screen transient 与 primary archive | 完成 | `termx-core-v2/`、按需 `termx-vterm/` | htop/vim 纯 alt 不进 primary history；primary app 临时进 alt 前 archive/hide 当前 frame，退出 alt 后的新 primary 输出作为新 frame publish，不能复活 pre-alt current frame |
| R307. SK resize 与 final screen-frame harness | 完成 | `termx-core-v2/`、按需 `termx-vterm/` | 覆盖运行中 resize、final screen-frame 固定宽度、committed logical line 不被 resize 重写 |
| R308. SK 色彩属性与主题解析边界 | 完成 | `termx-core-v2/`、按需 `termx-tui-v3/` | 保存 default fg/bg 语义属性而不是提前烘焙主题 RGB；明确 RGB/256 色/默认色在历史里的存储和渲染规则 |
| R309. SK storage backend 无限历史接口 | 完成 | `termx-core-v2/` | 以 append/update transaction、segment cursor、index/window 为边界接文件存储；不得让文件格式成为第二份历史模型 |
| R310. SK protocol/TUI history window 接入 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、按需 `internal/protocol/`、`termx-proto/` | tui-v3 copy/history 只消费 authoritative history window；live display 和 history surface 分层 |
| R311. SK synchronized output scroll-out proof 修复 | 完成 | `termx-vterm/`、`termx-core-v2/` | 修复 primary screen app 一次性输出超过屏幕时只剩最后一屏的问题；vterm 必须把 scroll-out runs/cells 作为 semantic proof 交给 core，core 在 primary session 中保留这些真实经过 PTY 的历史 |
| R312. SK current frame 历史分页与冻结边界 | 完成 | `termx-core-v2/`、按需 `termx-tui-v3/` | 修复进入 copy/history 后 current primary frame 之外上滑为空的问题；latest token 必须冻结 current frame，older 必须能从 current/archive segment 接回 committed history |
| R313. SK older prepend window tail boundary 修复 | 完成 | `termx-core-v2/`、按需 `termx-tui-v3/` | 复现 tmux 可见完整历史但 TUI 上滑黑屏；修复 older response 的 tail boundary，使 TUI 不再把 current-frame older 页判成 stale |
| R314. SK authoritative history dump 诊断命令 | 完成 | `termx-cli/`、`termx-core-v2/`、按需 `internal/protocol/` | 新增命令行诊断入口，把指定 terminal 的 core-v2 authoritative history window 分页 dump 到文件，用来区分历史存储/分页错误和 TUI 展示错误 |
| R315. SK screen-frame trailing blank rows 裁剪 | 完成 | `termx-core-v2/` | 对比 tmux 与 authoritative dump，修复 archived/current screen-frame 将整屏尾部默认空白行当作历史 rows 返回，导致 TUI 上滑大片黑/空的问题 |
| R316. SK copy/history PageUp 翻页与 frame 空白展示修复 | 完成 | `termx-tui-v3/`、按需 `termx-cli/` | 用 tmux 自测和 authoritative dump 对比确认 core 已有 committed 历史；修复 TUI copy/history 连续 PageUp 仍停在最新 frame/空白区域的问题，并避免默认空白 frame row 被渲染成黑块 |
| R317. SK archived frame 与 committed 投影顺序修复 | 完成 | `termx-core-v2/` | authoritative dump 显示早期 archived primary frame 被直接追加到全部 committed 日志之后，导致历史顺序错乱；修复 latest/older 的 segment 投影，让 archive 通过 current-frame older cursor 返回并按事务顺序夹在 committed 历史中 |
| R318. SK history logical renderer 设计重写 | 完成 | `workflow.md`、`termx-core-v2/docs/history-logical-renderer-design.md`、相关旧设计文档标注 | 去掉 commit/committed 领域概念，把 history 明确定义为消费同一份 vterm terminal semantic transaction 的 logical-line renderer；列出对象、接口、timeline、frame journal、mutable/sealed、freeze/window/storage 边界 |
| R319. SK semantic 覆盖矩阵与旧 projector/store 清场 | 完成 | `workflow.md`、`termx-core-v2/docs/history-logical-renderer-design.md`、`termx-core-v2/history/`、`termx-core-v2/terminal.go`、相关 core-v2 harness | 补全 vterm semantic transaction 到 logical-line renderer 的覆盖矩阵；删除旧内存 projector/store 补丁实现和旧 harness；落地新 renderer/state/interface/type 边界，真实实现前 history.window 明确返回未重建 |
| R320. SK ordered semantic event contract 与 coverage harness | 完成 | `termx-core-v2/history/`、必要时只读或最小触及 `termx-vterm/` | 根据 semantic 覆盖矩阵定义 renderer 内部 ordered event 输入边界；明确 ops、scroll-out proof、primary/alt frame、alt enter/exit、resize/full-replace 如何进入同一消费链；若 vterm side proof 缺少顺序信息，用 harness 暴露 contract 缺口并标注后续联动 |
| R321. SK StreamLineReducer 最小实现 | 完成 | `termx-core-v2/history/` | 实现 ordinary stream 的 open line、row ownership 和 cursor model；按 semantic op 的 row/col/cursor/erase/insert/delete 处理 WriteSpan、CR、LF/IND/NEL、soft-wrap、BS/CUB/CUF/CHA/HPA、CUP/VPA/CUU/CUD、EL、ED/ClearRect、ECH/DCH/ICH，并让 PrimaryScrollOut proof exactly-once seal |
| R322. SK FrameReducer 最小实现 | 完成 | `termx-core-v2/history/` | 实现 primary current frame、archived primary frame 和 alt transient frame journal；PrimaryFrame 全量替换 current，alt enter archive/clear primary 并 publish alt，alt exit clear transient，resize 不 seal history，final frame 固定生成宽度 |
| R323. SK HistoryStore 内存实现与 window/copy/freeze | 完成 | `termx-core-v2/history/` | 实现新的内存 HistoryStore，只接收 HistoryMutationBatch，不解释 terminal ops；维护 LogicalLineStore、SealedTimeline、OpenLine、FrameJournal、FrozenHistorySnapshot，并实现 latest/older/freeze/copy/storage recovery |
| R324. SK terminal history renderer 接入 | 待开始 | `termx-core-v2/terminal.go`、`termx-core-v2/history/`、必要时 live semantic result 最小联动 | Terminal 持有新的 HistoryLogicalRenderer 与 HistoryStore；write/resize/close 只交 semantic transaction 或 CloseReason 给 renderer；移除 ErrHistoryNotRebuilt 临时返回并恢复 TerminalHistoryWindow/Copy/Freeze/Release |
| R325. SK protocol/CLI authoritative history 恢复 | 待开始 | `termx-core-v2/protocol_service.go`、必要时最小触及 `termx-cli/` | protocol history.window/history.copy/history.release 重新返回 authoritative HistoryStore 数据，保留 row kind、segment、logical line id、generation、token、cursor boundary，并修复 CLI history-dump 临时失败 |
| R326. SK tmux/vterm harness 对齐 | 待开始 | `termx-core-v2/`、必要时最小触及 `scripts/` 或 `termx-vterm/` | 用真实 PTY/tmux 对比 harness 覆盖 primary screen app 上滑历史、长同步输出、current frame 之外 older paging、alt enter archive、final screen-frame、resize；若 vterm transaction 缺少 ordered proof，先补 vterm contract/harness 再回 core |

## 7. 测试准入

每个切片提交前必须运行本切片相关命令。测试无法运行时，最终说明必须写清原因。

- 文档-only：`git diff --check`
- core-v2 改动：`cd termx-core-v2 && go test ./... -count=1`
- vterm 改动：`cd termx-vterm && go test ./... -count=1`
- tui-v3 改动：`cd termx-tui-v3 && go test ./... -count=1`
- protocol 改动：`go test ./internal/protocol/... -count=1`，如果修改 `.proto` 还必须同步生成物并运行相关生成/descriptor 测试
- CLI 守卫改动：运行对应 focused Go test；如果不确定，用 `cd termx-cli && go test ./cmd/termx -count=1`
- 任意提交前都要运行：`git diff --check`

## 8. 提交规则

- `/goal` 自动模式下，每个完成切片必须使用中文提交信息提交。
- 用户明确要求不要提交时，按用户最新指令执行，并在最终说明未提交。
- 不得 amend commit，除非用户明确要求。
- 不得使用 destructive git 命令。
- 不得覆盖用户或其他代理的未提交改动；发现冲突时停下说明。

## 9. 当前状态

- `R301` 已完成：新增 `termx-core-v2/docs/r301-history-cleanup-audit.md` 记录清场审计结论；本轮追加清场后，旧 `historyANSIParser`、semantic ingest、terminal history pipeline、history queue、history projector 和相关 production/test harness 均已删除。`TestR301HistoryCleanupGuard` 现在防止旧 history implementation 文件、程序名分支和真实 PTY chunk 直接 raw queue 路径重新出现。准入已通过 `cd termx-core-v2 && go test ./... -count=1`。
- `R302` 已完成：`termx-vterm/vterm/semantic_source.go` 新增 `TerminalSemanticSource`、`TerminalSemanticTransaction`、`TerminalSemanticSize`、frame/scroll-out contract 和 `SemanticSource` adapter；`ApplyPTYWrite` / `Resize` 产出 ordered ops、primary scroll-out proof、primary/alt frame、alt enter/exit、synchronized output、full replace/resize boundary。`termx-core-v2/terminal_semantic_contract.go` 目前只保留 vterm contract alias，不再保留旧 history adapter 或旧 semantic ingest 桥接。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-vterm && go test ./... -count=1`。
- `R303` 已完成：`termx-core-v2/history/memory.go` 新增纯内存 `HistoryProjector` / `InfiniteHistoryStore` domain 骨架，只消费 `TerminalSemanticTransaction`、`ScreenAppDecision`、`HistoryMutation` 和 lifecycle close；`termx-core-v2/history/projector_harness_test.go` 用 fake semantic transaction / fake classifier 覆盖普通 scroll-out commit、CR/CUP/EL frontier mutate、primary synchronized/current repaint current-only、alt transient no-commit、process exit force close、resize non-history boundary 和 frozen copy boundary。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R304` 已完成：`live.SurfaceTrack.WriteWithResult` 现在保留同一 vterm 产出的 `TerminalSemanticTransaction`；`Terminal` 持有 R303 内存 projector/store，普通输出只按 semantic ops 和 scroll-out proof 更新 logical-line history，CR/EL/CUP/BS 等只 mutate frontier，process exit 通过 `HistoryProjector.ForceClose(CloseReasonProcessExit)` 收口 primary mutable frontier。新增 `TerminalHistoryWindow` / `TerminalHistoryCopy` 仅供 core-v2 domain harness 使用；protocol `history.window` / `history.copy` 在 R310 前仍返回 `ErrHistoryNotRebuilt`。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R305` 已完成：terminal history classifier 现在把 synchronized primary transaction 按 terminal semantics 归入 primary screen session，并通过 R303 projector 发布 `current-primary-frame`；repaint 替换 current frame，不增加 ordinary committed depth。`TerminalHistoryFreeze` 仅用于 core-v2 harness 验证 frozen boundary 能指向 current primary frame segment；protocol/TUI 接入仍留给 R310。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R306` 已完成：terminal history classifier 现在按 `AltEntered` / `AltExited` / `AltFrame` terminal semantics 发布 transient alt frame；纯 alt frame 可在 latest 选择但不 ordinary commit，alt exit 释放 transient frame。primary current 进入 alt 前会 archive/hide，退出 alt 后的新 primary output 发布为新的 current frame id，不复活 pre-alt current ownership。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R307` 已完成：resize transaction 现在作为 non-history boundary 进入 projector，不能从 resized live snapshot 生成或重写 committed history；process exit 会把 active primary current frame 作为 final `screen-frame` commit 一次，committed projection 保留 fixed-grid kind 和生成时 `ScreenCols`。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R308` 已完成：新增 history view-time `HistoryTheme` / `ResolveCellStyleForTheme` 边界；default fg/bg 继续以空 token 保存在 payload，只有查看时按 theme 解析，`ansi:N`、`idx:N` 和 `#rrggbb` 明确颜色 token 原样保留。terminal harness 覆盖 default、16 色、256 色和 truecolor SGR 写入 history payload。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R309` 已完成：新增 R309 纯内存 `StorageBackend` 实现与 harness，`StorageTransaction` 明确承载 lines、tombstones、committed index、frontier ids 和 frame records；recover 重建完整 domain state，compact 只删除未被 committed/frontier/frame 引用的 payload，backend 不定义 line mutability。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R310` 已完成：protocol `history.window` / `history.copy` / `history.release` 现在接 core-v2 authoritative `HistoryWindow` / frozen token / copy / release 边界；window payload 保留 row kind、row segment、logical line id、generation、token 和 segment cursor。内存 store 补齐 committed segment older prepend 最小实现，tui-v3 protocol adapter 显式传递 `older` / `oldest` mode，继续只消费 authoritative window/copy，不从 live rows、snapshot、LoadedRows 或本地 row count 推断分页。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-tui-v3 && go test ./... -count=1`、`go test ./internal/protocol/... -count=1`、`git diff --check`。
- `R311` 已完成：tmux `capture-pane -S -` 对同类 synchronized output 能看到所有真实经过 PTY 的行；vterm dump 定位到 `WriteDamage.ScrollbackAppend` 已含 `Runs` 文本，但 `TerminalSemanticTransaction.PrimaryScrollOut` 曾只拷贝 `Cells` 导致 proof 变空，且 begin/payload/end 分片中间 payload 缺少 active sync mode 会被误判为 ordinary。现在 vterm 保留 scroll-out `Runs`，`TerminalModes.SynchronizedOutput` / `TerminalSemanticTransaction.SynchronizedActive` 暴露 mode 2026 的 active 状态；core primary session 路径消费同一 transaction 的 scroll-out proof 提交 logical lines，并保留最后一屏为 current primary frame。准入已通过 `cd termx-vterm && go test ./... -count=1`、`cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R312` 已完成：修复 Codex history latest 能看到 current frame 但上滑到 frame 之外为空的问题。core `OlderWindow` 现在支持从 `current-primary-frame` / `archived-primary-frame` cursor 接回 committed history；latest freeze token 保存当时 visible primary/alt frame payload，后续 repaint 不会改写 frozen latest window。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-tui-v3 && go test ./... -count=1`、`git diff --check`。
- `R313` 已完成：tmux harness 先挂 pipe 后触发输出，确认 raw PTY bytes 与 `capture-pane -S -` 都包含完整 `log01..log12` 与 current frame；根因是 core/protocol older 已返回 rows，但 prepend response 的 `Boundary.LastLineID` 使用 older page tail，TUI state 会把它判为 stale 并丢弃，导致上滑区域黑/空。现在 older prepend 保留 latest frozen tail boundary，并补齐 core domain、protocol 和 TUI state harness。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-tui-v3 && go test ./... -count=1`、`git diff --check`。
- `R314` 已完成：新增 `termx v3 history-dump <terminal-id> --out <file>`，默认按 core-v2 `history.window` latest + older cursor 分页 dump authoritative history，并在结束时 `history.release` frozen token；dump 文件记录 window token/boundary/cursor、row logical line id、row-in-line、segment、kind 和文本，用于区分 core 存储/分页问题与 TUI 展示问题。CLI harness 覆盖 committed tail + current primary frame + older prepend 场景。准入已通过 `cd termx-cli && go test ./cmd/termx -count=1`、`git diff --check`。
- `R315` 已完成：`/tmp/termx-history.dump` 显示 core authoritative history 有 3 个 windows、1413 rows、1226 committed rows 和 35 current frame rows；同一 `codex resume 019efc61-417a-7682-9ce6-fa8da10ad9e3` 的 tmux capture 只有 71 行可见正文。dump 中 `archived-primary-frame` 有 152 行，其中 90 行是 trailing default blank rows，说明黑/空区域来自 core frame projection 把整屏尾部默认空白当作 history rows 发给 TUI。现在 semantic screen-frame 进入 core history payload 时裁掉 trailing default blank rows，但保留中间空行和带显式样式/链接的空白行。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R316` 已完成：本轮先用 tmux 跑真实 `codex resume 019efc61-417a-7682-9ce6-fa8da10ad9e3` 并抓 `history-pageup-*.txt`，确认最新代码下 6 次 PageUp 均有 30-35 行正文，不再是黑/空屏；再用受控长历史 PTY harness 生成 260 行 committed 输出后进入 history，确认 TUI PageUp 能从 latest frame 翻进 older committed 行。根因在 TUI copy/history 把 PageUp/wheel 当 cursor 移动，容易停在最新 frame 内部；现在新增 `CopyModeStore.ScrollViewport`，PageUp/wheel/进入 pending scroll 均移动 viewport，cursor 只跟随保持可见。renderer 侧同时把 default blank frame cell 交给 viewport 背景，不再固化成终端 ANSI 背景；明确 styled blank/TailFill 仍保留。准入已通过 `cd termx-tui-v3 && go test ./... -count=1`、真实 tmux Codex resume harness、受控长历史 tmux harness。
- `R317` 已完成：分析 `/tmp/termx-history.dump` 确认 core authoritative dump 本身存在投影顺序问题：`archived-primary-frame` 来自早期 `/resume` 前 primary frame，却被 latest 直接追加在全部 committed 日志之后，造成历史看起来乱序。现在 core-v2 memory store 按 transaction sequence 组装 committed/archive/current timeline，latest 只返回 committed tail + active current/alt frame，不再把 archived frame 挂在最新尾部；older 从 current/alt cursor 回翻时按 timeline 返回 archive 再回 committed。新增 domain 和 terminal 链路 harness。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R318` 已完成：新增 `termx-core-v2/docs/history-logical-renderer-design.md` 作为新的 history 语义基准，明确 history 是消费同一份 vterm terminal semantic transaction 的 logical-line renderer；去掉 `commit/committed` 领域概念，改用 `mutable` / `sealed` / `timeline` / `frame journal` / `frozen projection` / storage residency 边界。`workflow.md` 已把该文档列为当前设计基准，旧 screen app final plan 与 core-v2 architecture 已标注为历史背景或待迁移旧词。准入已通过 `git diff --check`。
- `R319` 已完成：补全 `history-logical-renderer-design.md` 的 semantic 覆盖矩阵，并同步代码接口名；删除旧 `memoryHistoryProjector` / `memoryHistoryStore`、旧 projector harness 和 `frameRowsFromWriteOps` fallback；新增 `HistoryLogicalRenderer`、`StreamLineReducer`、`FrameReducer`、`HistoryState`、`SealedTimeline`、`FrameJournal`、`HistoryStore` 等边界。terminal/protocol 在新 renderer 接入前明确返回 `ErrHistoryNotRebuilt`，避免继续暴露旧错误历史。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`；子 Agent 只读审核无阻塞 findings。
- R319 后，旧 `HistoryTrack`、frontier/index/window/snapshot/storage/style、旧 raw parser、旧 projector、旧 terminal history pipeline、旧 `memoryHistoryProjector` / `memoryHistoryStore` 和相关 harness 均已删除。core/server/terminal/protocol 已切断旧 history storage、pipeline 和 window 投影入口；新的 `HistoryLogicalRenderer` / `HistoryStore` 真实实现接入前，`history.window` / `history.copy` / `history.release` 必须显式返回未重建，后续不得沿用已删除补丁路径，也不得通过 stub、fallback 或桥接代码恢复旧模型。
- `R320` 已完成：`termx-core-v2/history/events.go` 新增 `HistorySemanticEventsFromTransaction` 和 `HistorySemanticEventOrderSource`，把 `TerminalSemanticTransaction` 的 ordered ops、alt enter/exit、primary scroll-out proof、primary/alt frame、resize/full-replace 统一归一化为 renderer 内部 event 链。R320 harness 覆盖 WriteSpan row/col、CR、CUP、EL、ECH/DCH/ICH、ScrollRect、CopyRect、PrimaryScrollOut、PrimaryFrame、Alt enter/exit、AltFrame、Resize/full-replace 和真实 vterm transaction；当前 vterm 只给 `Ops` 提供精确 op 间顺序，scroll-out/frame/full-replace side proof 明确标记为 transaction-level order，后续若 reducer 需要 raw 中精确位置，必须最小联动 vterm 增补 ordered proof。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R321` 已完成：新增 `NewStreamLineReducer` 及内部 row ownership/cursor/open-line state；`WriteSpan` 按 row/col 覆盖对应 draft，CR/LF/IND/NEL/soft-wrap 和 BS/CUB/CUF/CHA/HPA/CUP/VPA/CUU/CUD 更新 cursor/row ownership，EL/ED/ClearRect/ECH/DCH/ICH 按 vterm semantic op 修改 mutable draft，ScrollRect/CopyRect 只移动或复制 mutable row ownership，离屏内容只由 `PrimaryScrollOut` proof seal。R321 harness 覆盖 `abc\rX\n`、EL mode 0/1/2、CUP 目标行写入、ED/ClearRect、ECH/DCH/ICH、soft-wrap+LF、ScrollRect/CopyRect 和重复 proof exactly-once。当前 vterm scroll-out proof 仍无独立 proof id，reducer 只能用 payload signature 防止同一 proof 重放；若后续需要区分相同文本的不同 proof，必须先最小联动 vterm 增补 ordered proof id。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R322` 已完成：新增 `NewFrameReducer` 和 primary/alt frame journal state；`ReplacePrimaryCurrent` 只用 vterm `TerminalSemanticFrame` 全量替换 mutable primary current，不从 write ops 重建 frame，也不把 repaint 追加进 timeline；`ArchivePrimaryCurrent` 在 alt enter 等边界把 current primary seal 为 archived frame 并清 current；`ReplaceAltCurrent`/`ClearAltCurrent` 只维护 transient alt frame，不写 primary timeline；`ApplyNonHistoryBoundary(FrameReasonResize)` 只发 non-history boundary，不 seal 或重写 frame；`ClosePrimaryCurrent` 生成 fixed-width final primary frame。R322 harness 覆盖 primary repaint current-only、纯 alt transient no primary timeline、archive 顺序、post-alt new frame id、resize boundary 和 final screen-frame 固定 cols。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R323` 已完成：新增 `NewInMemoryHistoryStore` / `NewInMemoryHistoryStoreFromRecovered`，新的内存 store 只应用 `HistoryMutationBatch`，维护 logical line payload、sealed timeline、open line、frame journal、frame records 和 frozen projection；latest window 返回 sealed timeline tail + active open/current primary/alt projection，older window 按 timeline cursor 回翻，archived frame 按 record sequence 出现在真实位置，不挂 latest 尾部；freeze 保存当时 mutable frame rows，后续 repaint 不改 frozen window；copy 从 frozen/live authoritative rows 取文本；recovery 从 storage backend 的 lines/timeline/frame records 重建投影。R323 projection 目前以 logical line 为 window row，visual wrap 留给后续 protocol/TUI 投影切片。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `termx-core-v2/docs/history-rebuild-goal-prompt.md` 是给 `/goal` 使用的 R303-R310 连续推进 prompt；它要求每轮只处理最早未完成切片，完成 harness、实现、准入、`workflow.md` 更新和中文提交后再进入下一切片。
