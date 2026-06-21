# 工作流：termx remote 迁移主线

本文件是当前分支唯一有效的活动驱动文件。后续分析、实现、测试、提交都先看这里；如果本文件与旧说明、聊天记录、旧代码行为冲突，以本文件为准。

本文件已经从旧 history/copy 长队列压缩为 remote 迁移队列。旧队列和完成记录不再在这里重复备份，按 git 历史追溯；本文件只保留当前阶段需要执行和约束的内容。

## 0. 怎么读

开始任何工作前只看这几段：

- `1. 当前目标`
- `3. 工作范围`
- `4. 不可违反的语义`
- `5. 扫描结论和任务队列`
- `6. 测试准入`

如果用户请求和任务队列冲突，先更新本文件，再改代码；不要靠口头约定跳过范围和顺序。

## 1. 当前目标

### 1.1 一句话目标

把 `termx remote ...` 从 legacy/fallback 路径迁到 core-v2 protocol/service extension，同时保持默认本地 CLI 入口继续走 `termx-core-v2/` 与 `termx-tui-v3/`。

### 1.2 这轮只关心什么

当前阶段只迁 remote 的后端控制链路：

1. 明确 remote 现有 method、service、storage、transport 和 CLI fallback 边界。
2. 把 `termx-proto` wire contract 与 `internal/protocol` Go contract 迁到 core-v2 domain 结构，而不是为旧 core 保留兼容层。
3. 在 core-v2 补齐 remote 所需的 typed protocol、transport scope、terminal create、storage/events 和 service hook。
4. 用 core-v2 adapter 接上 `termx-remote` public service，而不是把默认入口退回旧 core。
5. 分步迁移 `remote.status`、`remote.local.*`、`remote.pair.start` 和 remote terminal/storage routing。
6. 切换 CLI remote client/config 到 core-v2 默认 daemon，并清理旧 fallback 边界。
7. 保留 remote terminal/storage routing 的 core-v2 truth，不新增第二份 terminal truth。

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
- wire protocol contract：`termx-proto/`
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
- `termx-proto/`
- `internal/protocol/`

### 3.2 受限联动范围

只有当前切片确实需要时，才允许最小化触及：

- `termx-cli/cmd/termx/` 内非 `remote_*.go` 的必要 glue 或测试
- `termx-tui-v3/` 中受 protocol/service adapter 影响的 smoke 或 harness
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
- 跨进程、daemon client 或 remote service 需要共享的 payload 字段必须先进入 `termx-proto/wirepb/terminal.proto`，再由 `internal/protocol` 映射为 core-v2 Go domain；不能只在 CLI adapter 或反射 helper 里临时承接。

### 4.2 remote 不能拥有第二份 terminal truth

- terminal lifecycle、PTY size、attachment、events、history 和 storage 必须来自 core-v2 daemon/protocol。
- remote 只负责授权、配对、transport/session 和请求路由。
- remote storage 只能走 core-v2 storage API，不得把 TUI workbench、terminal lifecycle、copy/history 交互态写成 remote 私有 truth。

### 4.3 remote migration 必须分层

- 先审计 method/contract，再接 core-v2 extension hook，再接 service adapter，再启用 CLI flow。
- remote management request 必须通过清晰 adapter 路由到 core-v2 public/protocol 方法。
- 新 protocol structure 必须直接表达 core-v2 的 terminal、attachment、history、storage 和 event 模型；不得先模拟旧 core 协议再翻译到 core-v2。
- `.proto` 是 wire contract，不是旧 core contract；字段命名、optional/repeated 语义、时间单位、枚举和 scope 必须向 core-v2 domain 对齐。
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

## 5. 扫描结论和任务队列

### 5.1 代码扫描结论

这次扫描到的当前边界如下，后续切片必须按这些边界拆，不得用补丁式兼容绕过去：

- `termx-cli/cmd/termx/remote_client.go` 现在调用 `dialOrStartClient(resolveSocket(...))`，会自动启动 `legacy daemon`；迁移后必须改为 core-v2 socket/client 路径，不能继续连旧 daemon。
- `termx-cli/cmd/termx/remote_runtime.go` 同时承担旧 core adapter、remote method handler、storage/events/transport adapter，并直接 import `termx-core`；迁移目标是删除这个旧 core adapter，改成 core-v2 domain adapter。
- `termx-cli/cmd/termx/remote_protocol_codec.go` 是旧 adapter 私有 remote codec；迁移后 remote method codec 应收口到 `internal/protocol` typed contract，不在 CLI 里保留第二套 remote codec。
- `termx-cli/cmd/termx/remote_config.go` 只因默认 config path 依赖 `tuiv2/shared`；这不是 protocol truth，但会阻止 remote 文件脱离旧 TUI 依赖，必须独立清理。
- `termx-remote.Service` 的边界是合理的：它只需要 daemon 提供 terminal management、storage、events、transport/session，不应拥有 terminal lifecycle/history/storage truth。
- `termx-remote` 的 `runtimepb` 是 remote runtime/localweb/WebRTC API，可保留；不能把它当成 core daemon protocol truth，也不能让 core-v2 模拟旧 core protocol 后再翻译。
- `internal/protocol` 已有 remote wire protobuf，但当前用反射/getter/wirepb 指针承接 remote params/results；迁移目标是新增显式 `protocol.Remote*` domain structs，移除反射和 legacy getter 兼容。
- `termx-core-v2` 已有 storage/events 和 protocol dispatch，但没有 remote service hook，也没有 public `ServeTransport/ServeScopedTransport`；remote WebRTC datachannel 需要 core-v2 自己的 transport scope API。
- `termx-core-v2` 的 `ProcessSpec` 当前只包含 `TerminalID/Command/Size`，而 `protocol.CreateParams` 和 remote terminal management 会传 `Dir/Env/Scrollback*`；这些字段必须进入 core-v2 create/process contract，不能在 remote adapter 里丢弃。
- `termx-proto/wirepb/terminal.proto` 是 remote/core-v2 跨进程 wire contract 的入口。当前已包含 `CreateParams` 的 `dir/env/scrollback_*` 和 `RemoteStatus/RemotePairStart*/RemoteLocal*`，但后续必须先审计这些字段是否完整表达 core-v2 domain；需要共享的新字段必须写入 `.proto` 并同步生成 `terminal.pb.go`，不能只补在 Go 侧反射/adapter。

### 5.2 任务队列

状态只能使用：`待开始`、`进行中`、`完成`、`阻塞`。同一时间只能有一个切片处于 `进行中`。

自动执行时只看下面这张表，按顺序取最早未完成切片：

| 切片 | 状态 | 范围 | 白话说明 |
| --- | --- | --- | --- |
| 背景里程碑：local v2/v3 切换与 history/copy/floating 收口 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`termx-cli/`、`internal/protocol/`、相关文档 | 默认本地入口、TerminalView/Attachment、authoritative history、copy/history、floating、owner/attachment 计数已经收口；细节按 git 历史追溯 |
| R173. SK remote 迁移工作流重置 | 完成 | `AGENTS.md`、`workflow.md` | 已收紧 AGENTS 的 remote 迁移边界，压缩 workflow 为 remote 主线，并保留测试/提交准入 |
| R173B. SK 禁止补丁式实现准入 | 完成 | `AGENTS.md`、`workflow.md` | 已把禁止补丁式实现写成硬语义：先定位 truth/source/message chain，再按模型和 harness 实现 |
| R173C. SK core-v2 protocol-only 准入 | 完成 | `AGENTS.md`、`workflow.md` | 已明确 remote/protocol 迁移不对旧 core 做任何兼容，协议结构必须迁到 core-v2 domain contract |
| R174. SK remote 迁移代码扫描与队列设计 | 完成 | `workflow.md`、只读扫描 `termx-cli/`、`termx-remote/`、`termx-core-v2/`、`internal/protocol/`、`termx-proto/`、旧 `termx-core/` | 已定位 CLI legacy daemon 入口、旧 core adapter、remote service 边界、core-v2 缺口和后续切片顺序 |
| R175. SK termx-proto core-v2 wire contract | 待开始 | `termx-proto/`、`internal/protocol/` 只读参照、`termx-core-v2/` 只读参照、`termx-remote/` 只读参照 | 先审计并对齐 `.proto`：remote/status/local/pair、create/process、terminal info、storage/events、transport scope 需要共享的字段必须进入 `terminal.proto`；如改 `.proto` 必须同步生成 `terminal.pb.go`，环境缺 `protoc` 时本切片标阻塞 |
| R176. SK remote protocol typed contract | 待开始 | `internal/protocol/`、按需 `termx-proto/` | 新增显式 `protocol.RemoteStatus`、`RemotePairStartParams/Result`、`RemoteLocalEnableParams`、`RemoteLocalStatus`；`Encode/DecodeMethod*` 不再用反射/getter/wirepb 指针作为 remote domain；补 protocol tests |
| R177. SK core-v2 create/process remote contract | 待开始 | `termx-core-v2/`、`internal/protocol/`、按需 `termx-proto/` | 让 core-v2 terminal create/process contract 承载 `Dir`、`Env`、必要 CWD metadata 和 remote create 需要的 scrollback 参数；wire 字段缺失时先回到 `termx-proto`，不得在 remote adapter 里静默丢字段 |
| R178. SK core-v2 transport scope API | 待开始 | `termx-core-v2/`、`internal/protocol/`、`termx-proto/` 按需、`termx-shared/` 按需 | 给 core-v2 提供 public `ServeTransport` / `ServeScopedTransport` / `TransportScope` 能力，供 remote WebRTC datachannel 接入；scope 只能过滤/约束 protocol session，不能创建第二份 terminal truth |
| R179. SK core-v2 remote method hook | 待开始 | `termx-core-v2/`、`internal/protocol/` | 在 core-v2 protocol dispatch 中接入 typed remote service hook，用 fake service 证明 `remote.status`、`remote.pair.start`、`remote.local.*` 进入 core-v2；不照搬旧 `ProtocolMethodHandler` wire bytes contract |
| R180. SK core-v2 remote daemon adapter | 待开始 | `termx-cli/cmd/termx/remote_*.go`、`termx-core-v2/`、`termx-remote/` | 用 core-v2 server/domain 实现 `termx-remote.Service` 需要的 Daemon/StorageDaemon/ScopedDaemon adapter；删除或改写旧 `remote_runtime.go` 对 `termx-core` 的 import |
| R181. SK core-v2 daemon remote lifecycle | 待开始 | `termx-cli/cmd/termx/`、`termx-core-v2/`、`termx-remote/` | 默认 `termx daemon` 装配 remote config/service/start/close/local auto-enable；remote runtime 生命周期跟随 core-v2 daemon，不回退 legacy daemon |
| R182. SK remote client switch to core-v2 daemon | 待开始 | `termx-cli/cmd/termx/remote_*.go`、`termx-cli/cmd/termx/*test.go` | `termx remote status/info/open/enable/disable/pair` 使用 `resolveV3Socket` + `dialOrStartV3Client`，不再调用 `dialOrStartClient` 或启动 legacy daemon |
| R183. SK remote config path 脱离 tuiv2 | 待开始 | `termx-cli/cmd/termx/remote_config.go`、按需共享 config helper | 移除 remote config 对 `tuiv2/shared` 的依赖；默认 config path 与当前 v3 config policy 明确，remote 文件不再 import 旧 TUI |
| R184. SK remote status/local/pair core-v2 smoke | 待开始 | `termx-cli/`、`termx-core-v2/`、`termx-remote/` | 补 CLI/core-v2 smoke：`remote.status`、`remote.local.enable/status/disable`、`remote.pair.start` 都经过 core-v2 daemon 和 `termx-remote.Service`，不经过旧 core |
| R185. SK remote terminal management/storage/events routing | 待开始 | `termx-remote/`、`termx-core-v2/`、`termx-cli/`、`termx-testkit/` 按需 | 验证 remote runtime API 的 terminal list/create/get_directory/set_metadata/restart/remove、storage get/put/delete/list、events subscription 都路由到 core-v2 truth |
| R186. SK remote transport session core-v2 routing | 待开始 | `termx-core-v2/`、`termx-remote/`、`termx-shared/`、`termx-testkit/` 按需 | 验证 remote WebRTC/datachannel transport 通过 core-v2 `ServeScopedTransport` 进入 protocol session，terminal scope 与 machine-events-only scope 行为正确 |
| R187. SK remote legacy fallback cleanup | 待开始 | `termx-cli/cmd/termx/remote_*.go`、`termx-cli/cmd/termx/default_dependency_guard_test.go`、`go.work`、`go.work.sum`、相关文档 | 清理 remote fallback 对旧 core/tuiv2 的默认依赖证据；remote 源文件不再作为旧依赖豁免，旧 core 只允许显式 `legacy_*` 路径 |
| R188. SK remote migration docs finalization | 待开始 | `workflow.md`、`termx-cli/docs/v2-v3-switch-audit.md`、必要顶层文档 | 更新审计文档和当前状态，记录 remote 已迁 core-v2 contract、旧 fallback 删除边界和最终测试证据 |

## 6. 测试准入

每个有效切片提交前，至少跑和改动范围相符的测试：

- 文档-only 改动：`git diff --check`
- core-v2 改动：`cd termx-core-v2 && go test ./... -count=1`
- remote 改动：`cd termx-remote && go test ./... -count=1`
- CLI remote 改动：`cd termx-cli && go test ./cmd/termx -count=1`
- protocol 改动：`cd internal && go test ./protocol/... -count=1`
- proto 改动：`cd termx-proto && go test ./... -count=1`
- `.proto` 改动：必须同步更新对应生成文件；如果本机缺少 `protoc` 或 `protoc-gen-go`，不得手写生成文件，当前切片应标 `阻塞` 并写清缺口。
- 默认入口或依赖边界改动：`cd termx-cli && go test ./cmd/termx -run TestDefaultRuntimeSourceDoesNotImportLegacyCoreOrTUI -count=1`
- remote fallback/旧依赖清理改动：除 CLI 测试外，必须确认 `termx-cli/cmd/termx/remote_*.go` 不再 import `termx-core` 或 `tuiv2`，且默认依赖守卫不再把 remote 文件作为旧依赖豁免。
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
- `R174` 已完成：已扫描 CLI remote、`termx-remote.Service`、core-v2 protocol/server/storage/events、旧 core extension/transport 源边界，并把迁移队列细化到 R175-R188。
- `termx remote ...` 仍是下一阶段实现主线，旧 fallback 只能作为待删除边界存在，不能作为默认本地切换完成证据。
- 当前明确缺口：`remote_client.go` 仍连 legacy daemon；`remote_runtime.go` 仍 import 旧 `termx-core`；`remote_config.go` 仍 import `tuiv2/shared`；`internal/protocol` remote codec 仍用反射/wirepb；core-v2 仍缺 remote hook 与 public scoped transport API。
- `termx-remote-v2/` 当前是未跟踪目录，本工作流默认不触碰。
- 当前已知环境缺口：本机没有 `protoc` 与 `protoc-gen-go`；只有需要重新生成 proto 时才构成阻塞。
