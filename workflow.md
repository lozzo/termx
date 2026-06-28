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
| R324. SK terminal history renderer 接入 | 完成 | `termx-core-v2/terminal.go`、`termx-core-v2/history/`、必要时 live semantic result 最小联动 | Terminal 持有新的 HistoryLogicalRenderer 与 HistoryStore；write/resize/close 只交 semantic transaction 或 CloseReason 给 renderer；移除 ErrHistoryNotRebuilt 临时返回并恢复 TerminalHistoryWindow/Copy/Freeze/Release |
| R325. SK protocol/CLI authoritative history 恢复 | 完成 | `termx-core-v2/protocol_service.go`、`termx-core-v2/history/`、`termx-cli/` | protocol history.window/history.copy/history.release 重新返回 authoritative HistoryStore 数据，保留 row kind、segment、logical line id、generation、token、cursor boundary，并修复 CLI history-dump 临时失败；若 CLI 暴露 history payload 缺陷，最小回到 history owner 修复 |
| R326. SK tmux/vterm harness 对齐 | 完成 | `termx-core-v2/`、必要时最小触及 `scripts/` 或 `termx-vterm/` | 用真实 PTY/tmux 对比 harness 覆盖 primary screen app 上滑历史、长同步输出、current frame 之外 older paging、alt enter archive、final screen-frame、resize；若 vterm transaction 缺少 ordered proof，先补 vterm contract/harness 再回 core |
| R327. SK Codex resume 重绘历史与 CJK 回归 | 完成 | `workflow.md`、`termx-core-v2/`、必要时最小触及 `termx-vterm/` | 总结并复现 `/resume` 选择新 session 后 clear+redraw 应重建 authoritative history、二次 `/resume` 应形成新的 session 历史边界、中文宽字符不应被 continuation cell 投影成额外空格，并验证 vterm semantic event 覆盖；先写 harness，再修模型，不写程序名特殊分支 |
| R328. SK clear/repaint session boundary 与 tmux 对齐 | 完成 | `workflow.md`、`termx-core-v2/`、必要时最小触及 `termx-vterm/` | 复现 Codex 类 primary screen app 在切换 session 时 clear 旧画面并 redraw 新历史后，authoritative history 出现旧新混杂或截断的问题；用 tmux 受控序列确认 ED2/ED3/clear+redraw 对 scrollback 的可观察语义，明确 clear 是否只清 mutable current、是否应清除/裁剪 sealed timeline、以及 redraw 是否应作为新 session projection 重写；先补 harness，再修 owner 边界，不写程序名特殊分支 |
| R329. SK vterm 产出全覆盖与 scrollback proof 消费 | 完成 | `workflow.md`、`termx-core-v2/`、必要时最小触及 `termx-vterm/` | 对齐 vterm 解析 PTY 后的虚拟屏幕与 scrollback 产出：凡进入 primary scrollback 的内容都必须作为 ordered proof 或明确边界进入 authoritative history；`il/dl/su/sd/ri/ris/tab/scroll-region` 等 vterm control 不得被 history 静默忽略；先补覆盖 harness，再修消费链 |
| R330. SK Codex resume 编号输出诊断脚本 | 完成 | `workflow.md`、`scripts/` | 新增受控 Python 输出脚本，用 `Sxx 001..100` 这种编号行模拟 Codex `/resume` clear+redraw、多次切换和 CJK 文本，方便人工或 tmux/TermX history dump 对照缺行、混行和中文空格问题 |
| R331. SK screen frame 后普通 prompt 顺序修复 | 完成 | `workflow.md`、`termx-core-v2/` | 复现编号脚本结束后 shell prompt 被插入 `S03` current frame 中间的问题；修复 primary screen app current frame 到普通 stream 的 session 边界和 sealed timeline sequence，使 prompt 排在已关闭 frame 之后 |
| R332. SK frozen history 全量分页与普通 CJK 列坐标修复 | 完成 | `workflow.md`、`termx-core-v2/` | 用编号脚本和 authoritative history dump 复验 Ghostty/tmux 观察；修复 frozen token 只保存 latest 页导致 older 重复最新页的问题，并修复 ordinary stream 按 slice index 写入宽字符导致中文之间出现真实空格的问题 |
| R333. SK Codex 类历史分页 cursor 契约修复 | 完成 | `workflow.md`、`termx-core-v2/`、`internal/protocol/`、`termx-tui-v3/`、按需 `termx-cli/` | 修复 Codex 类 primary screen app clear+redraw 后 copy/history 上滑仍截断或混排的问题；把 older 分页 cursor 的 projection absolute row index 与 logical line 内 row 区分开，TUI 不再用本地 row-in-line 重建 core cursor，并放宽 older prepend boundary 校验与 SourceLines identity |
| R334. SK primary frame 启动时不重复 sealed shell tail | 完成 | `workflow.md`、`termx-core-v2/` | 复现普通 shell 已有 5 条 sealed 输出后启动 Codex 类 primary screen app，latest/history 中 shell tail 被 current primary frame 再投影一次的问题；修复首次/增量 synchronized primary frame 只拥有本 transaction 触达的 rows，不把已 sealed 普通屏幕行纳入 current frame |
| R335. SK full-replace primary frame 启动行归属修复 | 完成 | `workflow.md`、`termx-core-v2/`、`termx-vterm/` | 修复 Codex 类程序启动时 vterm 因大面积 direct damage 产出 full-replace primary frame，core 把整屏 shell tail 再接管为 current frame，导致普通 shell 历史从 5 行变 10 行的问题；vterm 需要透出 direct damage 触达行，core 只按 transaction proof 接管 current frame rows |
| R336. SK ED2 clear-time primary frame scrollback 保留 | 完成 | `workflow.md`、`termx-core-v2/`、按需 `termx-vterm/` | 修复 Codex 内 `/clear` 后当前屏只显示新 Codex 内容，但 history/copy 上滑看不到清屏前 primary frame 的问题；ED2 只能清当前 viewport，不等同 ED3 clear scrollback，core 需要在不重复 sealed shell tail 的前提下保留 primary current frame 离开 viewport 的 clear-time proof |
| R337. SK ordinary prompt 收口使用当前屏幕 proof | 完成 | `workflow.md`、`termx-core-v2/` | 修复 Codex/Claude 类 primary screen app 退出时一闪而过的 transient 行被旧 current frame seal 进 history、同时 shell prompt 恢复后看不到 iTerm2 式旧历史的问题；普通输出恢复前关闭 primary frame 时应使用本 transaction 的 vterm 当前屏幕 proof，并排除本次普通输出触达行 |
| R338. SK pseudo-TUI ED2 stale scroll-out seal 清理 | 完成 | `workflow.md`、`termx-core-v2/` | 修复 primary frame 曾经发生 payload scroll-out 后，后续没有 clear-time proof 的 ED2 repaint 仍靠旧状态把 current tail seal 进 history 的问题；ED2 只清 current ownership，旧屏进入 history 必须来自同一 transaction 的 vterm proof |
| R339. SK iTerm2 式 Codex `/clear` 历史保留 | 完成 | `workflow.md`、`termx-core-v2/`、`termx-vterm/` | 明确 TermX 与 tmux/Ghostty 不同：Codex primary screen app 内部 `/clear` 只清 live viewport，不清 authoritative history；进入 history/copy 后仍能向上看到 clear 前 shell 历史和 Codex 旧 frame |
| R340. SK clear-scrollback 软页边界 | 完成 | `workflow.md`、`termx-core-v2/`、按需 `termx-vterm/` | 按“新建一页，不撕掉前页”的 iTerm2 式无限历史目标处理 ED3/clear-scrollback：它只能失效当前窗口边界，不能物理删除 core-v2 logical-line history；ED2/ED3 组合后 history/copy 仍能向上看到旧页 |
| R341. SK cursor-backed sealed timeline store | 完成 | `workflow.md`、`termx-core-v2/history/`、按需 `termx-core-v2/terminal.go` | 为 10 万行普通输出先落地 cursor-backed store：sealed logical-line payload 可驻留 backend，latest/older/copy 按 cursor/window 读取，不为窗口分页先 materialize 全量 rows；current frame spill 留后续切片 |
| R342. SK 二进制历史 payload 后端 | 完成 | `workflow.md`、`termx-core-v2/history/` | 删除 R341 临时 JSONL 文件后端，改成长度前缀二进制 append-only payload 文件；文件后端不得依赖 `encoding/json`，为后续 mmap/zero-copy/index 文件预留稳定 record layout |
| R343. SK history store Apply 热路径去全量扫描 | 完成 | `workflow.md`、`termx-core-v2/history/` | 修复 10 万行普通输出被 authoritative history store 拖慢的问题；HistoryStore Apply 不得在每个 mutation 后全量 reindex 已有 lines/timeline/frame records，计数器只能随当前 mutation 增量维护，恢复路径才允许全量扫描 |
| R344. SK TUI live refresh latest-only backlog 合并 | 完成 | `workflow.md`、`termx-tui-v3/services/`、按需 `termx-tui-v3/app/` | 修复压力输出期间 TUI 按积压 changed 事件逐帧追屏的问题；普通 `terminal.changed` 只表示 live surface 失效，必须在 service/app 边界合并为 latest-only refresh，不能按固定事件数量分批拉 snapshot 和渲染中间屏 |
| R345. SK TUI live refresh 全链路 perf 抓取与饥饿修复 | 完成 | `workflow.md`、`termx-tui-v3/services/`、`termx-tui-v3/app/`、按需 `termx-core-v2/` | 建立压力输出全链路 perf harness，抓 core changed/protocol refresh/snapshot/app render/FrameSink 写帧计数；修复普通 changed backlog 被无限吞并导致持续输出时 latest screen 刷新不足的问题 |
| R346. SK 生产历史 backend 接入与 live 批次交互上限 | 完成 | `workflow.md`、`termx-core-v2/`、`termx-cli/` | 修复真实 daemon 仍使用内存 history store 导致 10 万行后 RSS 暴涨的问题，并把 core live ingest 单批上限降到 PTY 读粒度级别，让 latest screen 在压力输出期间持续刷新而不是 1MB 一跳 |

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

- `R346` 已完成：定位用户看到的两个现象分别来自两条链路。TUI 低帧率不是程序整体执行慢，而是 core live ingest queue 曾按 1MB 合并 PTY 输出，protocol/TUI 又按 4096 个普通 changed 做 latest-only 合并，导致压力输出时 latest screen 只能大块跳变；现在 live ingest 单批降到 PTY read 粒度，core protocol 和 TUI service 的普通 changed 合并窗口降到 64，仍保留 resize/exit/read-error/跨 terminal 等语义边界。RSS 到 2.8G 的主因是生产 `Terminal` 仍用 `NewInMemoryHistoryStore`，R341/R342 的 file backend 只存在于 history 包测试；现在 core-v2 server 支持 `WithHistoryStorageDir`/`WithHistoryStoreFactory`，默认 daemon/v3 daemon 将 sealed logical-line payload 写到 XDG state 下 `history-v2`，backend 创建失败时注册 terminal 直接失败而不静默回退内存。新增测试覆盖生产 terminal 写出 `.history-lines.bin`、backend 失败不 fallback、terminal id 路径转义、默认 daemon 配置 history dir，以及大 backlog 分段让出 refresh。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-tui-v3 && go test ./... -count=1`、`cd termx-cli && go test ./cmd/termx -count=1`、`git diff --check`。
- `R345` 已完成：全链路抓取确认 live 刷新不足不只在 TUI 渲染端，core protocol 仍有两条旧热路径会把普通 `terminal.changed` 当内容帧逐个推送：channel 0 event stream 和 attach channel full-replace `screen.update` stream。现在 core protocol 对普通 live invalidation 做 bounded latest 合并，保留 resize/exit/metadata/read-error/跨 terminal 边界；TUI protocol adapter 同样改成 bounded yield，避免 R344 的“吞到 channel 为空”在持续输出时饿死刷新。新增 `perftrace` 计数点覆盖 `core.terminal.changed`、`core.protocol.event`、`core.protocol.attach_screen_update`、`tui.protocol.live_event`、`tui.live_event`、`tui.live_surface`、`tui.frame`；bench 显示 10 万 ordinary changed 在 core protocol 和 TUI service 边界都收敛为 25 次 live invalidation。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-tui-v3 && go test ./... -count=1`、`git diff --check`，并额外运行 `go test ./termx-core-v2 -run '^$' -bench 'BenchmarkProtocolLiveInvalidationDrain100k' -benchtime=1x` 与 `go test ./termx-tui-v3/services -run '^$' -bench 'BenchmarkProtocolTerminalServiceAdapterLiveEventDrain100k' -benchtime=1x`。
- `R344` 已完成：定位压力输出期间 TUI “一帧一帧追屏”的直接原因是 protocol terminal adapter 在普通 `terminal.changed` backlog 中按固定 `maxProtocolLiveRefreshDrain=64` 分批吐出 refresh invalidation；这会让 app 层反复拉 latest snapshot 并写出中间屏，视觉上从 100 跳到 300/500/1000 逐段追，而不是把 changed 当成 latest-only invalidation。现在 service 层会吞并当前已经排队的同 terminal 普通 refresh backlog，直到 backlog 为空或遇到 resize/exit/read-error/metadata 等语义边界才吐一次 refresh；新增测试覆盖 512 个 changed 只产生一个 refresh，并验证 semantic boundary 不被吞掉。准入已通过 `cd termx-tui-v3 && go test ./... -count=1`、`git diff --check`。
- `R343` 已完成：定位 `time python scripts/generate_terminal_stress.py --lines 100000` 卡顿的直接 core-v2 根因之一是 `inMemoryHistoryStore.applyMutation` 每处理一个 mutation 后调用 `reindexCounters()`，全量扫描已有 logical lines、timeline records 和 frame records；10 万行普通输出会把 authoritative history 写入拖成近似 O(n²)，同步压住真实 PTY 输出链路。现在 `Apply` 热路径只观察当前 mutation 携带的 line/record/frame/session id 增量推进辅助计数器，`reindexCounters()` 仅保留给 recovery 构造路径；新增 guard 测试禁止 `Apply`/`applyMutation`/store upsert/flushStorage 热 helper 重新调用全量 reindex，并用连续单行 Apply 验证增量计数器和 latest projection。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R342` 已完成：删除 R341 临时 JSONL payload store，改为二进制 append-only record。文件 record 使用固定 magic/version/header、line id 与长度前缀；payload 自身用小端整数和字符串长度编码 logical line、cells、style，并把连续相同 width/style/link 的 cells 合并成 run，避免 JSON 反射解析和文本膨胀。新增 guard 测试禁止 file backend import `encoding/json` 或回到 `.jsonl` 路径。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R338` 已完成：继续沿 `335af9ac4b7e9e44703540c93e9655a224d3f73d` 方向推进，保留当前 copy/history 交互基础，只收敛 core-v2 pseudo-TUI 历史边界。renderer 已删除跨 transaction 的 stale scroll-out 记忆：ED2 repaint 不再因为更早发生过 payload scroll-out 就把当前尾屏 seal 进 authoritative history；旧屏内容进入历史只能来自同一 transaction 的 clear-time scroll-out proof。新增 R338 renderer harness 覆盖“先 payload scroll-out、后无 clear-time proof ED2 repaint”场景，验证只 clear current ownership 并 publish 最新 frame，不 close stale tail。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R337` 已完成：对照 iTerm2/tmux 后确认这不是 Codex 程序名问题，而是普通 shell 输出恢复时的 frame/session boundary。tmux 的 `scroll-on-clear` 会在整屏 clear 时把屏幕内容滚入 pane history；iTerm2 对“不用 alt-screen、把光标移到 prompt 上方重绘”的程序也会先把可见屏幕保存到 scrollback，并且 ED2 与 ED3 分开处理。我们原来在 `ClosePrimaryFrameBeforeStream` 上直接 seal renderer 里的旧 `PrimaryCurrent`，导致 Codex 退出时一闪而过的 `Shutting down...` transient 行被写进 history，而 prompt transaction 已经带着的最终可见屏幕 proof 没被使用。现在 frame reducer 新增 `ClosePrimaryCurrentFromFrameExcludingRows`，只用同一 transaction 的 `PrimaryFrame` proof 收束 current frame 已拥有的 rows，并排除本次 ordinary stream 触达行；renderer 在普通输出恢复前优先走该路径，prompt 行仍由 stream reducer 消费，已 sealed shell tail 不会被复制进 final screen-frame。新增 R337 reducer/renderer/server harness。准入已通过 `cd termx-core-v2 && go test ./... -count=1`。
- `R336` 已完成：用户复现的 Codex 内 `/clear` 后当前屏只显示新内容但 history 上滑看不到清屏前内容，根因是 R333/R329 为避免 transient UI 重复，把 ED2 clear-time scroll-out proof 一律跳过；这把 ED2 错当成了“只清 current frame、不进入 scrollback”的 repaint boundary。按 iTerm2/tmux 语义，ED2 只清 viewport，不等同 ED3 clear scrollback；若清屏前有 primary current frame，vterm 的 clear-time scroll-out proof 表示该 frame 真实离开 viewport，应进入 scrollable authoritative history。现在 classifier 仅在 `state.HasPrimaryCurrent && ED2` 时打开 `ConsumeClearTimeScrollOutProof`，renderer 会 seal 该 proof 并只 clear current frame ownership，不再同时 close 同一旧 frame，避免 proof + frame 双写；没有 primary frame ownership 的 ordinary shell clear-time proof 仍跳过，避免重复已 sealed shell tail。准入已通过 `cd termx-core-v2 && go test ./... -count=1`。
- `R335` 已完成：用户复现的“普通 shell 5 行，进入 Codex 后 history 变 10 行”不是 TUI 本地重复，而是 core authoritative history 在 full-replace primary frame 启动路径中把 vterm 整屏 `PrimaryFrame` side proof 全量接管为 current frame。上一轮 R334 只覆盖 synchronized-output touched rows，未覆盖 vterm 因 `broad_direct_cell_damage`/`repeated_direct_damage` 产出 `RequiresFullReplace` 且缺少 ordered content ops 的路径。现在 vterm `WriteDamage` 记录并排序 `DirectDamageTouchedRows`，`TerminalSemanticTransaction.PrimaryFrameTouchedRows` 透传该 proof；core classifier 在已有 sealed timeline 时只允许带 touched-row proof 的 full-replace frame 按行接管 current frame，renderer 用同一 `ReplacePrimaryTouchedRows` 路径发布新 UI 行，不再把已 sealed shell tail 复制为 `current-primary-frame`。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-vterm && go test ./... -count=1`。
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
- `R324` 已完成：Terminal 现在持有新的 `HistoryLogicalRenderer` 与 `HistoryStore`，write 路径只把 live surface 产出的 vterm semantic transaction 交给 renderer，resize 只作为 non-history boundary，process exit/remove/shutdown 通过 `CloseReason` 收口 open line/current frame；`TerminalHistoryWindow` / `Copy` / `Freeze` / `Release` 已恢复为 core-v2 authoritative store 入口，不再返回 `ErrHistoryNotRebuilt`，也不从 live snapshot 或 raw PTY fallback。新增 R324 terminal/protocol harness 覆盖普通 authoritative window/copy/release、primary synchronized scroll-out + current frame、alt transient 不进 primary timeline、resize 不删历史、process exit 和 terminal remove lifecycle close。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R325` 已完成：protocol history.window/history.copy/history.release 通过 R324 authoritative store 返回真实 rows/copy/token release，并在 harness 中锁定 row kind、row segment、logical line id、generation、cursor boundary 和 copy token。CLI `termx v3 history-dump` 在新 store 分页下恢复可用，`--limit 1 --all` 现在按 older older latest 输出；本轮从 CLI dump 暴露出新 FrameReducer 未裁尾部默认空白 frame rows 的 payload 缺陷，已回到 `termx-core-v2/history/` owner 修复：frame rows 只裁掉尾部纯 default blank rows，保留中间空行和 styled blank 内容。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-cli && go test ./cmd/termx -count=1`、`git diff --check`。
- `R326` 已完成：新增 renderer 级 shared `historyIDAllocator`，让 StreamLineReducer 与 FrameReducer 在同一个 terminal history renderer 内共享 logical line id 和 timeline record id，修复 scroll-out/open-line 与 frame row id 冲突导致 authoritative payload 被覆盖的问题。新增 R326 tmux/vterm harness：用真实 tmux capture 作为可观察 PTY 基准，并用 core-v2 authoritative history window 验证长 synchronized output、current frame 之外 older paging、alt enter archive、纯 alt transient 不进 primary history、process exit final screen-frame 和 resize 后 archived frame 固定生成宽度。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R327` 已完成：新增 Terminal 级和 history domain 级回归，覆盖 `/resume` 类 synchronized clear+redraw 的 authoritative history、二次 redraw 中相同文本 scroll-out 仍作为新 PTY 事件记录、非 resize full-replace 带 primary frame 时发布 current frame、latest limit 截在 current frame 内时 older 先返回 frame 前半部分、CJK width=0 continuation 不再投影成中文之间的真实空格。实现上移除 scroll-out payload signature 去重，改为按 transaction event 记录；history cell 转换跳过无文本 width=0 continuation，并按 grapheme width 展开 scroll-out runs；older window 改为基于完整 live projection 回翻；classifier 对非 resize `RequiresFullReplace` + `PrimaryFrame` 走 primary frame session。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R328` 已完成：tmux 受控序列确认 `ED2` 是“清当前屏并把可见内容按顺序送入 scrollback”，`ED3` 是明确 clear scrollback/history 边界。vterm 现在把 ED2 clear-time rows 挂在 `ed` semantic op 上，core 按 ordered event 在 redraw 前处理 clear boundary；ordinary stream 清屏前会 seal open line，primary frame redraw 只替换 current frame，不把 redraw 同时写成 ordinary timeline；ED3 同时清 store 与 renderer mutable/session 状态，避免二次 `/resume` 沿用旧 session。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-vterm && go test ./... -count=1`、`git diff --check`。
- `R329` 已完成：按 vterm 解析 PTY 后的实际产出补齐 history 消费缺口。core classifier 现在读取 authoritative history ownership 边界，区分 ordinary stream 已 seal 内容和 primary current frame 内容：普通 ED2 不重复消费 scrollback proof，primary frame ED2 必须把 vterm scrollback proof 写入 committed history 并清 current ownership。StreamLineReducer 现在消费 `il/dl/su/sd/ri/decstbm/ht/cbt/ris` 等 vterm controls，明确忽略只影响终端能力/状态查询且不产生历史 payload 的 controls，并处理 vterm `TailFill`。RIS 会 seal ordinary open line 并清 current frame/alt transient。新增 R329 domain 与 terminal harness 覆盖 ED2 primary scrollback proof、line controls、scroll region RI、RIS 和 TailFill。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-vterm && go test ./... -count=1`、`git diff --check`。
- `R330` 已完成：新增 `scripts/emit_codex_resume_numbered.py`，默认输出三段编号内容：普通 `S01 001..100`、第一次 clear+redraw `S02 001..100`、第二次 clear+redraw `S03 001..100`；可切换 `ED2`/`ED3`/`RIS`/无清屏、同步输出模式和 CJK marker，并可生成 expected manifest 用于和 TermX/tmux/history dump 对照缺行、混行、截断和中文空格。
- `R331` 已完成：截图中的“shell prompt 插在 `S03` 历史中间”根因不是 manifest，而是 history projection 仍保留 `S03` primary current frame，后续普通 prompt 作为 open line 被投影到 current frame 前；同时 ordinary timeline record 缺少全局 sequence，会被排到 frame record 前。现在 ordinary record、scroll-out record 和 frame record 共享 renderer timeline sequence；普通输出恢复时若仍有 primary current frame，会先关闭 frame 再消费 ordinary stream，prompt 因此排在 `S03 REDRAW_END` 之后。
- `R332` 已完成：真实 tmux 基准确认编号脚本顺序本身正常；复测 current TermX authoritative dump 定位到两个 core 问题：protocol latest 建立的 frozen token 只保存 latest 页，导致 `history-dump --all --limit 40` 的 older 分页只能重复最新页；ordinary stream reducer 内部按 slice index 写入 vterm display column，宽字符后续列被补成真实空格，形成 `中 文 编 号`。现在 frozen token 保存完整 live projection，latest 只裁当前页，older 从同一 frozen projection 向前翻；普通流写入按 display column 计算 cell index，宽字符 continuation 不再落成空格。受控复测 `scripts/emit_codex_resume_numbered.py --lines 100 --sessions 3 --clear ed2 --redraw-mode all --cjk-every 10` 后，authoritative dump 共有 9 个 window，能看到 `S01 001/100`、`S03 026/100`、`TERMX_DONE_MARKER`，且 `bad_cjk=0`。
- `termx-core-v2/docs/history-rebuild-goal-prompt.md` 是给 `/goal` 使用的 R303-R310 连续推进 prompt；它要求每轮只处理最早未完成切片，完成 harness、实现、准入、`workflow.md` 更新和中文提交后再进入下一切片。
