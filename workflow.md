# 工作流：termx remote 迁移主线

本文件是当前分支唯一有效的活动驱动文件。后续分析、实现、测试、提交都先看这里；如果本文件与旧说明、聊天记录、旧代码行为冲突，以本文件为准。

本文件已经从旧 history/copy 长队列压缩为 remote 迁移队列。旧队列和完成记录不再在这里重复备份，按 git 历史追溯；本文件只保留当前阶段需要执行和约束的内容。

## 0. 怎么读

开始任何工作前只看这几段：

- `1. 当前目标`
- `3. 工作范围`
- `4. 不可违反的语义`
- `5. 任务队列`
- `6. 测试准入`

如果用户请求和任务队列冲突，先更新本文件，再改代码；不要靠口头约定跳过范围和顺序。

## 1. 当前目标

### 1.1 一句话目标

把 `termx remote ...` 从 legacy/fallback 路径迁到 core-v2 protocol/service extension，同时保持默认本地 CLI 入口继续走 `termx-core-v2/` 与 `termx-tui-v3/`。

### 1.2 这轮只关心什么

当前阶段只迁 remote 的后端控制链路：

1. 明确 remote 现有 method、service、storage、transport 和 CLI fallback 边界。
2. 把 remote/protocol contract 迁到 core-v2 domain 结构，而不是为旧 core 保留兼容层。
3. 用 core-v2 adapter 接上 `termx-remote` public service，而不是把默认入口退回旧 core。
4. 分步迁移 `remote.status`、`remote.local.*`、`remote.pair.start`。
5. 保留 remote terminal/storage routing 的 core-v2 truth，不新增第二份 terminal truth。

### 1.3 不在当前阶段做什么

- 不迁 `remote-ui/`、`web-control/`、`termx-hub/`。
- 不把 `termx-remote-v2/` 纳入实现范围；它当前只作为未跟踪实验目录存在。
- 不继续修补旧 `termx-core/` 或 `tuiv2/`。
- 不对旧 core 协议、旧 storage 格式、旧 daemon method 或旧 remote fallback 做任何兼容；冲突时直接迁到 core-v2 contract。
- 不借 remote 迁移重开 history/copy/floating 的长尾问题；除非当前切片直接触发回归。

## 2. 技术设计基准

- core-v2：`termx-core-v2/docs/architecture.md`
- tui-v3：`termx-tui-v3/docs/architecture.md`
- CLI 切换审计：`termx-cli/docs/v2-v3-switch-audit.md`
- remote public service：`termx-remote/`
- protocol contract：`internal/protocol/`

如果实现发现设计文档过期，必须和当前切片一起更新；不要代码先跑偏，文档以后再补。

## 3. 工作范围

### 3.1 当前主线允许主动修改

- `workflow.md`
- `AGENTS.md`
- `termx-cli/cmd/termx/remote_*.go`
- `termx-cli/docs/v2-v3-switch-audit.md`
- `termx-core-v2/`
- `termx-remote/`
- `internal/protocol/`

### 3.2 受限联动范围

只有当前切片确实需要时，才允许最小化触及：

- `termx-cli/cmd/termx/` 内非 `remote_*.go` 的必要 glue 或测试
- `termx-tui-v3/` 中受 protocol/service adapter 影响的 smoke 或 harness
- `termx-proto/`，仅当 remote protocol payload 必须调整
- `termx-shared/`，仅当 transport/session contract 必须调整
- `termx-testkit/`
- `scripts/`
- `Makefile`
- `go.work`
- `go.work.sum`
- 必要顶层说明文档

### 3.3 只读参考范围

默认不得修改：

- `termx-core/`
- `tuiv2/`
- `termx-remote-v2/`

### 3.4 冻结范围

不得主动触碰，除非本文件先明确解冻：

- `remote-ui/`
- `web-control/`
- `termx-hub/`
- `termx-app/`
- `bin/`
- `.claude/`
- 顶层可执行产物和测试产物
- 未在本文件列出的目录

## 4. 不可违反的语义

### 4.1 默认本地入口不能回退

- 默认 `termx`、`daemon`、`attach`、`new`、`ls`、`kill`、`rm` 仍必须走 `termx-core-v2/` 和 `termx-tui-v3/`。
- 旧 `termx-core/` 和 `tuiv2/` 只能通过 `termx legacy ...` 或当前尚未迁完的 remote fallback 隔离文件间接存在。
- 默认入口依赖守卫不能放松，不能新增默认源文件对旧 core/TUI 的 import。
- remote/protocol 迁移后不得保留旧 core wire format、storage format、method adapter、双 handler、fallback 读写或兼容 shim。

### 4.2 remote 不能拥有第二份 terminal truth

- terminal lifecycle、PTY size、attachment、events、history 和 storage 必须来自 core-v2 daemon/protocol。
- remote 只负责授权、配对、transport/session 和请求路由。
- remote storage 只能走 core-v2 storage API，不得把 TUI workbench、terminal lifecycle、copy/history 交互态写成 remote 私有 truth。

### 4.3 remote migration 必须分层

- 先审计 method/contract，再接 core-v2 extension hook，再接 service adapter，再启用 CLI flow。
- remote management request 必须通过清晰 adapter 路由到 core-v2 public/protocol 方法。
- 新 protocol structure 必须直接表达 core-v2 的 terminal、attachment、history、storage 和 event 模型；不得先模拟旧 core 协议再翻译到 core-v2。
- 不得直接读写 TUI reducer state、renderer、TerminalHost 或旧 core runtime。
- 不得用 storage scrub、定时刷新、重复 attach、局部 fallback 分支掩盖状态错乱。
- 禁止补丁式迁移：不能为了让某个 remote 命令暂时可用而叠加临时 if、重复同步、隐式状态修正或旧路径兜底；每个切片必须先明确 domain owner、truth source、消息链路和失败条件，再用契约测试或 harness 锁住语义。

### 4.4 tui-v3 和 history/copy 基线不能被破坏

- tui-v3 不以 Bubble Tea 作为主运行时。
- renderer 只消费 view-model，不读 core client、history source、runtime service 或 protocol client。
- copy/history 仍只消费 core-v2 authoritative history，不从 live/snapshot/VTerm scrollback fallback。
- panel/pane 只表达工作台槽位和连接意图，不表达 terminal running/exited 或 copy/history 交互态。

### 4.5 实现纪律

- 先写 domain model、小 harness 或契约测试，再接真实 protocol、terminal 或 CLI。
- 代码必须按正确模型写完整；如果方案依赖“再刷一次状态”“失败就 fallback”“先 scrub storage”“兼容旧内部格式”才能成立，默认不合格，需要回到状态归属和 contract 重做。
- 关键代码写简短中文注释，只解释不自明的边界或约束。
- 手工编辑文件必须使用 `apply_patch`。
- 不得覆盖用户或其他代理的未提交改动。
- 不得 amend commit，除非用户明确要求。

## 5. 任务队列

状态只能使用：`待开始`、`进行中`、`完成`、`阻塞`。同一时间只能有一个切片处于 `进行中`。

自动执行时只看下面这张表，按顺序取最早未完成切片：

| 切片 | 状态 | 范围 | 白话说明 |
| --- | --- | --- | --- |
| 背景里程碑：local v2/v3 切换与 history/copy/floating 收口 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`termx-cli/`、`internal/protocol/`、相关文档 | 默认本地入口、TerminalView/Attachment、authoritative history、copy/history、floating、owner/attachment 计数已经收口；细节按 git 历史追溯 |
| R173. SK remote 迁移工作流重置 | 完成 | `AGENTS.md`、`workflow.md` | 已收紧 AGENTS 的 remote 迁移边界，压缩 workflow 为 remote 主线，并保留测试/提交准入 |
| R173B. SK 禁止补丁式实现准入 | 完成 | `AGENTS.md`、`workflow.md` | 已把禁止补丁式实现写成硬语义：先定位 truth/source/message chain，再按模型和 harness 实现 |
| R173C. SK core-v2 protocol-only 准入 | 完成 | `AGENTS.md`、`workflow.md` | 已明确 remote/protocol 迁移不对旧 core 做任何兼容，协议结构必须迁到 core-v2 domain contract |
| R174. SK remote method contract audit | 待开始 | `termx-cli/docs/v2-v3-switch-audit.md`、`termx-cli/cmd/termx/remote_*.go`、`termx-remote/`、`termx-core-v2/`、`internal/protocol/` | 梳理 `remote.status`、`remote.pair.start`、`remote.local.*`、terminal/storage routing 现在走哪里；输出 core-v2 contract 目标，标出要删除的旧 core fallback，不设计兼容层 |
| R175. SK core-v2 remote extension hook | 待开始 | `termx-core-v2/`、`internal/protocol/`、`termx-cli/` 按需 | 在 core-v2 daemon/protocol 中提供 remote method 注册/路由 hook，用 fake handler 证明非 legacy method 能进入 core-v2 |
| R176. SK remote service core-v2 adapter | 待开始 | `termx-core-v2/`、`termx-remote/`、`termx-cli/` 按需 | 实现 core-v2 daemon/storage adapter，满足 `termx-remote` service 需要的 terminal management、storage、events、transport 入口 |
| R177. SK remote status core-v2 path | 待开始 | `termx-cli/cmd/termx/remote_*.go`、`termx-core-v2/`、`termx-remote/` | 让 `termx remote status` 默认打到 core-v2 remote service，不再以 legacy daemon 作为成功路径 |
| R178. SK remote local enable/status/disable core-v2 path | 待开始 | 同上，按需 `termx-shared/` | 迁移 `remote.local.enable/status/disable`，保留 local runtime 状态语义和 storage truth |
| R179. SK remote pair start core-v2 path | 待开始 | 同上，按需 transport/session contract | 迁移 `remote.pair.start`，让 pair flow 通过 core-v2 adapter 获取 terminal/storage/transport 能力 |
| R180. SK remote terminal/storage routing smoke | 待开始 | `termx-remote/`、`termx-core-v2/`、`termx-cli/`、`termx-testkit/` 按需 | 补最小端到端 smoke：remote service 通过 core-v2 操作 terminal 管理与 storage，不持有第二份 truth |
| R181. SK remote legacy fallback cleanup | 待开始 | `termx-cli/cmd/termx/remote_*.go`、`go.work`、`go.work.sum`、相关文档 | 清理 remote fallback 对旧 core 的默认依赖证据，保留必要 legacy 隔离说明 |

## 6. 测试准入

每个有效切片提交前，至少跑和改动范围相符的测试：

- 文档-only 改动：`git diff --check`
- core-v2 改动：`cd termx-core-v2 && go test ./... -count=1`
- remote 改动：`cd termx-remote && go test ./... -count=1`
- CLI remote 改动：`cd termx-cli && go test ./cmd/termx -count=1`
- protocol 改动：`cd internal && go test ./protocol/... -count=1`
- proto 改动：`cd termx-proto && go test ./... -count=1`
- 默认入口或依赖边界改动：`cd termx-cli && go test ./cmd/termx -run TestDefaultRuntimeSourceDoesNotImportLegacyCoreOrTUI -count=1`
- 跨模块迁移改动：按需加跑 `make test-v2-migration`
- CLI 编译入口相关改动：按需确认 `go run ./termx-cli/cmd/termx --help` 能编译运行

如果测试无法运行，最终说明必须写清原因。

## 7. 自动推进和提交规则

- 每次开始工作先读本文件，再跑 `git status --short --branch`。
- 只执行任务队列里最早未完成的切片。
- 如果最早未完成切片是 `阻塞`，必须停下说明原因，不能跳到后面。
- 如果最早未完成切片是 `待开始`，先把它标成 `进行中`，再开始做。
- 一个小阶段收口后，更新本文件状态和当前状态说明，跑准入，提交一个 `SK:` 中文提交。
- 不要长期堆未提交改动，也不要把多个小阶段硬塞进一个提交。
- 不得 amend commit，除非用户明确要求。
- 不得覆盖用户或其他代理的未提交改动；如果冲突，先停下说明。

## 8. 当前状态

- 当前分支已经完成本地默认入口 v2/v3 切换；最近收口提交为 `c9d133d4 SK: 修复启动重复附件计数`。
- `R173` 已完成：`AGENTS.md` 明确 remote 迁移不能退回旧 daemon/TUI，`workflow.md` 已压缩为 remote 迁移队列。
- `R173B` 已完成：禁止补丁式实现成为 AGENTS 与 workflow 的硬准入，后续 remote 切片必须先讲清状态归属和 contract。
- `R173C` 已完成：remote/protocol 迁移只面向 core-v2 contract，不对旧 core 保留兼容层或 fallback。
- `termx remote ...` 仍是下一阶段迁移主线，旧 fallback 只能作为待迁边界存在，不能作为默认本地切换完成证据。
- `termx-remote-v2/` 当前是未跟踪目录，本工作流默认不触碰。
- 当前已知环境缺口：本机没有 `protoc` 与 `protoc-gen-go`；只有需要重新生成 proto 时才构成阻塞。
