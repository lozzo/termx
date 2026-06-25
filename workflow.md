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

- 当前 screen app 无限历史定案：`termx-core-v2/docs/screen-app-infinite-history-final-plan.md`
  - 其中 `2.1 架构图与接口绑定` 是后续代码边界准入；R302-R310 必须按图上的 interface 和 owner 落地，不能绕过图里的边界直接补旧实现。
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
- `CommittedHistoryIndex`、`MutableFrontier`、segment cursor、storage backend、cache、adapter、TUI/App projection 都不能演变成第二份历史 truth。
- `persisted` 或落盘不表示不可修改；是否可修改由 session/segment/finalization 语义决定。

### 4.2 terminal 语义来源

- 不能把 raw PTY bytes parser 作为 terminal 语义 owner；raw parser 不能 fallback 出第二套历史。
- core-v2 应消费 termx-vterm 解释过程中的语义 transaction，而不是消费最终屏幕快照。
- vterm 的当前屏幕不是无限历史来源；它最多证明“终端语义解释过程可以产生可记录事件”。
- 与 tmux 一样，只有程序真实输出到 PTY 的内容才能进入历史；没有输出过的程序内部状态不在目标内。

### 4.3 普通输出

- 普通 shell/程序 stdout 一旦形成完整 logical line，就可以 commit 到 history。
- 普通输出不应长时间持有 screen app session。
- 进程退出必须 force commit primary mutable frontier。

### 4.4 screen app session

- pseudo-TUI 类程序在 primary screen 上反复改写时，必须有一个可变 session。
- session 内允许程序修改从 session 开始到当前的可变内容。
- session 暴露给历史模式前，必须先通过 segment cursor/archive 形成一致视图，不能展示半更新状态。
- session 最终退出或模式切换时，按分类决定 commit：普通 primary screen app 可以 commit final screen-frame；纯 alt-screen transient 不写 primary history。

### 4.5 alt-screen

- alt-screen 不写入 primary history。
- htop/vim 这类纯 alt-screen transient：运行期间可在当前屏幕选择；退出时不把屏幕内容 commit 到 primary history。
- Codex/Claude Code 这类 primary screen app 临时进入 alt-screen 选择器时：进入 alt 前必须 archive/hide 当前 primary frame；alt 内容作为 live transient 展示和选择；退出 alt 后如果出现新的 primary 输出，必须作为新的 primary frame publish，可以接回同一 session journal，但不得复活 pre-alt current frame，也不得凭空 commit alt 屏幕。
- 如果程序运行期间用户进入历史模式，当前 session 的文本内容必须作为历史视图的一部分，但仍需标记为 mutable/frozen projection，而不是当成 committed truth。

### 4.6 final screen-frame

- primary screen app 退出时允许把最后一屏作为固定宽度 screen-frame commit。
- final screen-frame 是为了保存“屏幕程序最终展示的内容”，不是普通 logical line reflow。
- final screen-frame 后续不随 resize 重排。

### 4.7 resize

- resize 不得重写 committed history。
- 普通 logical line 可以在展示层按新宽度重新 wrap。
- mutable screen app session 在 resize 时必须保留可变语义，不能凭空产生 committed history。
- final screen-frame 必须固定生成时的列宽。
- Codex/Claude Code 运行中 resize 是硬 harness，不能靠程序名特殊适配。

### 4.8 色彩和主题

- 历史应保存终端语义属性，不应把“当时主题下的默认前景/背景 RGB”提前烘焙成不可变颜色。
- SGR 明确指定的颜色可以作为具体颜色或调色板索引保存。
- default fg/bg、bold、dim、inverse 等应尽量保存为语义属性，由查看历史时的主题解析。
- 如果终端应用写入明确 RGB 背景，则该 RGB 属于内容属性，不能被后续主题替换。

### 4.9 TUI/App 边界

- tui-v3 不拥有 committed history truth，只消费 core-v2 authoritative `HistoryWindow`。
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
| R302. SK terminal semantic transaction 接口 | 待开始 | `termx-core-v2/`、`termx-vterm/` | 先立 vterm 到 core-v2 的语义事件接口：普通写入、cursor move、erase、scroll、alt enter/leave、resize、SGR 属性、flush/transaction boundary |
| R303. SK history projector domain harness | 待开始 | `termx-core-v2/` | 用 fake semantic transaction 驱动 projector，锁定普通输出 commit、mutable session、segment cursor、final screen-frame、alt transient 的基本语义 |
| R304. SK 普通输出最小实现 | 待开始 | `termx-core-v2/`、按需 `termx-vterm/` | shell/stdout 输出完整 logical line 后直接进入 committed history；process exit force commit primary mutable frontier |
| R305. SK primary screen app session 最小实现 | 待开始 | `termx-core-v2/`、按需 `termx-vterm/` | 支持 Codex/Claude Code 这类 primary screen app 在退出前修改 session 内文本；历史模式看到一致的当前 session projection |
| R306. SK alt-screen transient 与 primary archive | 待开始 | `termx-core-v2/`、按需 `termx-vterm/` | htop/vim 纯 alt 不进 primary history；primary app 临时进 alt 前 archive/hide 当前 frame，退出 alt 后的新 primary 输出作为新 frame publish，不能复活 pre-alt current frame |
| R307. SK resize 与 final screen-frame harness | 待开始 | `termx-core-v2/`、按需 `termx-vterm/` | 覆盖运行中 resize、final screen-frame 固定宽度、committed logical line 不被 resize 重写 |
| R308. SK 色彩属性与主题解析边界 | 待开始 | `termx-core-v2/`、按需 `termx-tui-v3/` | 保存 default fg/bg 语义属性而不是提前烘焙主题 RGB；明确 RGB/256 色/默认色在历史里的存储和渲染规则 |
| R309. SK storage backend 无限历史接口 | 待开始 | `termx-core-v2/` | 以 append/update transaction、segment cursor、index/window 为边界接文件存储；不得让文件格式成为第二份历史模型 |
| R310. SK protocol/TUI history window 接入 | 待开始 | `termx-core-v2/`、`termx-tui-v3/`、按需 `internal/protocol/`、`termx-proto/` | tui-v3 copy/history 只消费 authoritative history window；live display 和 history surface 分层 |

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

- `R301` 已完成：新增 `termx-core-v2/docs/r301-history-cleanup-audit.md` 记录清场审计结论；真实 PTY 输出路径确认经 `live.SurfaceTrack.WriteWithResult` 进入 shared-vterm semantic batch，`historyANSIParser` 仅保留为 `Ingest` / `IngestBatch` legacy skeleton。`terminal_semantic_ingest.go` 删除 semantic batch 的 raw parser fallback 分支；新增 `TestR301HistoryCleanupGuard` 防止 production Go 源码重新按程序名分支、semantic batch projector 调 `ingestOutputLocked(batch.Raw)` 或真实 PTY chunk 直接 `historyQueue.Enqueue(text)`。准入已通过 `cd termx-core-v2 && go test ./... -count=1`。
