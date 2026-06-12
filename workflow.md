# 工作流：termx TerminalView/Attachment 主线

本文件是当前分支唯一有效的活动驱动文件。后续分析、实现、测试、提交都先看这里；如果本文件与旧说明、聊天记录、旧代码行为冲突，以本文件为准。

本文件只保留 5 类东西：

- 当前目标
- 允许改哪里
- 不能改错的语义
- 当前任务顺序
- 测试和提交规则

架构细节不在这里展开，分别以 `termx-core-v2/docs/architecture.md`、`termx-tui-v3/docs/architecture.md`、`termx-tui-v3/docs/ui-interaction-spec.md`、`termx-tui-v3/docs/render-architecture.md`、`termx-cli/docs/v2-v3-switch-audit.md` 为准。

## 0. 怎么读这个文件

如果只是准备开始干活，先看这 4 段：

- `1. 当前目标`
- `4. 不可违反的语义`
- `5. 任务队列`
- `7. 测试准入`

如果你发现“用户想做的事”和“当前任务顺序”冲突，先改本文件，再改代码，不要靠口头约定跳过去。

## 1. 当前目标

### 1.1 一句话目标

把 `TerminalView/Attachment` 做成一等模型：同一个 terminal 可以同时被多个 pane 或 floating 连接，但 terminal process、live surface、authoritative history 仍然只有一份。

### 1.2 这轮只关心什么

这轮只收口一件事：**让 history / copy 主链尽快达到可用状态**。

这里的“可用”只看这一条主验收链：

1. 用户 attach 到一个真实 terminal。
2. 进入 copy mode。
3. 连续 `PageUp` 能拿到 older 历史。
4. pane resize 后，当前 frozen history 只做本地重排，不回 core 重新取一份 latest。
5. 搜索能在当前 frozen history 上命中。
6. 选中和复制得到的文本仍按 logical line 正确组装。

### 1.3 主验收链的通过标准

- 进入 copy mode 后，看到的是 authoritative history，不是 live fallback。
- `PageUp` 会继续拉 older，而不是只在本地假滚。
- resize 后，当前历史内容不丢、不跳回 pending、不被 live 内容替换。
- 搜索、cursor、selection 在 older prepend 和本地 reflow 后仍指向原来的内容。
- copy 出来的文本不因为 visual wrap / local reflow 被错误插入换行。

### 1.4 当前已经直接可用的能力

- core-v2 的 history truth 已经从 live surface 剥离，主线是 logical line。
- copy mode 已经走 frozen snapshot，不再跟着后续 live 输出漂移。
- latest / older 分页主链已经打通，older 继续带 token / boundary 回 core。
- TUI 已经能对 frozen logical lines 做本地 reflow，resize 不再默认回 core 重拿 latest。
- 搜索已经按 logical-line reflow 语义工作，不再只限于单个 visual row。
- copy / yank 已经按 logical line 组装，不再把同一条 reflow 后的 logical line 错拆成多行。
- boundary overlap / clipped partial / same-line merge 这些会直接影响 copy 正确性的基础语义已经补到主链。
- 大部分 stale response guard 已经补上，旧 latest/older 不会轻易覆盖当前 frozen history。

### 1.5 当前还阻塞“可用”的点

- 还缺一个统一的高层可用性验收闭环，当前主要是很多 targeted test，退出标准不清楚。
- 还没有把“真实 attach -> copy mode -> older -> resize -> search -> selection -> copy”收成一组明确通过的 runtime/CLI 黑盒。
- `215E1` 当前没有清晰的结束标准，导致工作容易继续滑向长尾边界审计。

## 2. 技术设计基准

- core-v2：`termx-core-v2/docs/architecture.md`
- tui-v3：`termx-tui-v3/docs/architecture.md`
- UI 交互：`termx-tui-v3/docs/ui-interaction-spec.md`
- render framework：`termx-tui-v3/docs/render-architecture.md`
- CLI 切换审计：`termx-cli/docs/v2-v3-switch-audit.md`

实现时如果发现这些设计文档需要变，就和当前切片一起改；不要代码先跑偏，文档以后再补。

## 3. 工作范围

### 3.1 当前主线允许主动修改

- `termx-core-v2/`
- `termx-tui-v3/`
- `termx-cli/`
- `internal/protocol/`
- `termx-proto/`
- 根目录直接相关文件：`workflow.md`、`AGENTS.md`、`go.work`、`go.work.sum`、`Makefile`、必要顶层说明文档

### 3.2 受限联动范围

只有当前切片确实需要时，才允许最小化触及：

- `termx-vterm/`
- `termx-shared/`
- `termx-testkit/`
- `termx-remote/`
- `scripts/`

### 3.3 只读参考范围

默认不得修改：

- `termx-core/`
- `tuiv2/`

### 3.4 冻结范围

不得主动触碰：

- `remote-ui/`
- `termx-app/`
- `web-control/`
- `termx-hub/`
- `bin/`
- `.claude/`
- 顶层可执行产物和测试产物
- 未在本文件列出的目录

## 4. 不可违反的语义

### 4.1 历史 truth 只能是 logical line

- core-v2 的历史基本单位必须是 logical line，不是 visual row，不是 snapshot scrollback，不是 grid viewport。
- `LogicalLineStore` 是唯一历史 truth。
- `HistoryWindow` 只是按当前 `cols` 投影出来的窗口，不是新的历史来源。
- attach、reattach、bootstrap、recovery、full replace、clear screen、resize 都不能凭空创造 committed history。
- alt-screen 不写入 primary history；process exit 必须 force commit primary mutable frontier。

### 4.2 tui-v3 只消费 authoritative history

- `termx-tui-v3` 不拥有 committed history truth。
- `HistoryStore` 只保存 core-v2 返回的 authoritative logical-line 历史快照、pending 状态和 exhausted 信息。
- `CopyModeStore` 只保存交互态：active view、terminal id、cursor、selection、frozen token 和当前本地投影 cols/rows。
- copy mode 缺冻结快照时，只能显示 pending、empty 或 error，不能从 live surface、snapshot 或本地 VTerm scrollback fallback。

### 4.3 resize 必须有 owner/follower 语义

- 同一 terminal 同时只能有一个有效 resize owner 修改 PTY size。
- follower 或 observer view 只能显示当前 terminal projection，不能因为自己 content rect 变化就覆盖 PTY size。
- owner transfer 必须走协议、effect、message 和 reducer 路径，不能在 UI 层本地偷偷改 truth。

### 4.4 render 和 runtime 的边界不能乱

- `RenderResult` 是唯一主输出；plain 文本、测试快照和 ANSI frame 都只是适配层。
- renderer 只消费 view-model 和 layout plan，不直接读 service、host 或 core client。
- `service` 不得直接修改 reducer-owned state；所有状态变化都必须走 message/effect 回主循环。
- `termx-tui-v3` 主线不得引入 Bubble Tea `Program`、`tea.Model`、`tea.Msg`、`bubbles` 等 contract。

### 4.5 实现纪律

- 先写 domain model 和最小 harness，再接真实 protocol、terminal 或 CLI。
- 大目标必须拆成可独立验证的小阶段，每个小阶段单独用 `SK:` 中文提交。
- 如果做着做着发现边界错了，先收口成文档/架构修正，再继续实现。
- 关键代码要加简短中文注释，说明真正不自明的逻辑。

## 5. 任务队列

状态只能使用：`待开始`、`进行中`、`完成`、`阻塞`。同一时间只能有一个切片处于 `进行中`。

自动执行时只看下面这张表，按顺序取最早未完成切片：

| 切片 | 状态 | 范围 | 白话说明 |
| --- | --- | --- | --- |
| 背景里程碑：0-215H3 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`termx-cli/`、`internal/protocol/`、`termx-proto/`、相关文档 | 默认入口、runtime、styled render framework、TerminalView/Attachment 基线、resize ownership、history MVP H1-H3 都已经收口；更细的历史细节需要时去看 git 提交和架构文档 |
| 215D1. SK floating group commands | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/` | 已补齐 floating group commands |
| 215F. SK shortcut integration and tmux harness | 完成 | `termx-cli/`、`termx-tui-v3/`、`termx-core-v2/`、`internal/protocol/`、`Makefile` 按需 | 已补 runtime 黑盒证据与 tmux smoke |
| 215E1-A. SK history copy 可用性主链验收 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`internal/protocol/`、相关文档 | 已补高层 runtime 黑盒，证明 attach -> copy mode -> older -> resize 本地重排 -> search -> selection -> copy 主链成立 |
| 215E1-B. SK history copy 主链缺口修补 | 完成 | 同上 | 已收掉 attach / resize / 本地 reflow 这类真实会卡主链可用性的缺口，不再继续扩到长尾黑盒 |
| 215E1-C. SK history copy 收口与回归 | 完成 | 同上 | 已用高层 runtime 黑盒和 core/protocol/tui-v3 联合模块测试证明主链可用，停止继续补长尾语义 |
| 215E2. SK clipboard paste 主链 | 完成 | `termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/services/`、`termx-cli/`、相关文档 | 已把显示态 `p/P PASTE` 接上真实主链：`p` paste 上一次 copy，`P` paste system clipboard，都会退出 copy mode 并发到 active terminal |
| 215E3. SK clipboard history overlay | 待开始 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/` | 再补完整的 clipboard history overlay |

当前下一步：

- `215E1` 已经收口
- 下一步切到 `215E2`
- 不再继续主动补新的 history 长尾黑盒，除非它直接回归当前主链

## 6. 必做证据

### 6.1 `215E1-A` 的唯一主验收链

必须证明这条链真实成立：

1. attach 到一个真实 terminal。
2. 进入 copy mode，看到 authoritative history pending -> latest。
3. 连续 `PageUp`，older 真正从 core 返回并 prepend。
4. resize 后，当前 frozen history 仍在，本地 reflow 生效，不回 core 重拿 latest。
5. 搜索能命中当前 frozen history。
6. 选中和复制得到的文本仍按 logical line 正确组装。

### 6.2 模块级最低守卫

在主验收链之外，当前阶段只保留这些最低守卫：

- core-v2：`\n`、`\r`、erase、auto-wrap、alt-screen、process exit、resize 的 history 语义不能回退。
- protocol：latest / older、token / generation / cursor / boundary stale guard 不能回退。
- tui-v3：copy mode 不得 fallback 到 live；本地 reflow、search、selection、copy 不能回退。

### 6.3 非阻塞长尾

下面这些不再作为当前阶段继续主动扩张的目标；除非它们直接阻塞主验收链，否则先停：

- same-terminal reconnect / terminal-pool reconnect / floating reconnect 的对称黑盒再细化
- delayed stale latest / older / error 的继续分叉覆盖
- workbench reload / delayed attach / lifecycle 组合路径的更多审计
- boundary overlap / clipped marker / grapheme 投影的再细化证明
- 更多“第 N 层关键语义”式的追加审计

## 7. 测试准入

每个有效切片提交前，至少跑和改动范围相符的测试：

- `215E1-A`：
  - 1 组高层 runtime / protocol 黑盒，覆盖主验收链
  - 必要的模块守卫测试
- core-v2 改动：`cd termx-core-v2 && go test ./... -count=1`
- tui-v3 改动：`cd termx-tui-v3 && go test ./... -count=1`
- protocol 改动：`cd internal && go test ./protocol/... -count=1`；如果动到 `termx-proto/`，再跑 `cd termx-proto && go test ./... -count=1`
- CLI 改动：`cd termx-cli && go test ./... -count=1`
- 默认入口、跨模块或迁移相关改动：按需加跑 `make test-v2-migration`
- 默认入口相关改动：还要确认 `go run ./termx-cli/cmd/termx --help` 能编译运行
- 文档-only 改动：至少跑 `git diff --check`

## 8. 自动推进和提交规则

- 每次开始工作先读本文件，再跑 `git status --short --branch`。
- 只执行任务队列里最早未完成的切片。
- 如果最早未完成切片是 `阻塞`，必须停下说明原因，不能跳到后面。
- 如果最早未完成切片是 `待开始`，先把它标成 `进行中`，再开始做。
- 一个小阶段收口后，立刻更新本文件状态、跑准入、提交一个 `SK:` 中文提交。
- 不要长期堆未提交改动，也不要把多个小阶段硬塞进一个提交。
- 不得 amend commit，除非用户明确要求。
- 不得覆盖用户或其他代理的未提交改动；如果冲突，先停下说明。

## 9. 当前状态

- 默认本地入口已经切到 `termx-core-v2/` 和 `termx-tui-v3/`；旧本地路径只允许走 `termx legacy ...`。
- `AppRuntime` 已是事件驱动批处理循环；真实 CLI attach 不再有外层 `16ms` 轮询；resize latest-wins 和 owner 转移链路已经收口。
- `215H1`、`215H2`、`215H3` 已完成，live/history 边界、core-v2 authoritative history stale guard、tui-v3 active-view history binding 都已经落地。
- 非 history 快捷键主线 `215D1`、`215F` 已完成。
- `215E1` 代码主线已经具备：
  - frozen snapshot
  - latest / older 分页
  - TUI 本地 reflow
  - logical-line search / selection / copy
  - 关键 stale guard
- `215E1-A` 已补 runtime 高层验收：真实 attach、进入 copy mode、older prepend、resize 本地重排、search、selection、copy 已经能在同一条黑盒链里通过。
- `215E1-B` 已收掉一个真实 runtime 缺口：手工 `LiveResizeMsg` 现在会同步当前 owner view 的 desired size，不再被 attach correction 的 stale recovery 拉回旧 content rect。
- `215E1-B` 本轮还收回了一个 attach guard 回归，并把两条过时断言对齐到当前 frozen snapshot 语义：显式 `ViewID` 的首次 attach 不再被误丢；pane size 后 copy mode 继续本地 reflow 当前 frozen history，不再期待第二个 latest。
- `215E1-C` 已完成：当前已有高层 runtime 主验收链，且 `go test ./termx-core-v2/... ./internal/protocol/... ./termx-tui-v3/... -count=1` 已通过，说明从 core 到 protocol 到 tui-v3 的 history/copy 主链当前处于可用状态。
- `215E2` 已完成：display / copy mode 下的 `p/P` 已接上 paste 主链；`p` 走最近一次 copy buffer，`P` 走 system clipboard read，都会退出 copy mode 并把文本发到 active terminal；如果 live surface 开着 bracketed paste，会按 `\x1b[200~...\x1b[201~` 包装。
- 当前下一步切到 `215E3`：开始收口 clipboard history overlay。
- 已知环境缺口：本机当前没有 `protoc` 与 `protoc-gen-go`；只有在需要重新生成 proto 时才构成阻塞。
