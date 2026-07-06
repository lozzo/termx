# 工作流：多 endpoint / 多 transport 管理主线

## 当前目标

- 当前分支切换到 TUI/client 侧多 endpoint 与多 transport 管理设计和落地。
- 详细技术规划以 `termx-tui-v3/docs/multi-endpoint-transport-plan.md` 为准。
- 旧 screen app 无限历史清场与重建记录不再保留在本活动文件中；需要追溯时查看 git 历史和对应文档。

## 基准文档

- `termx-tui-v3/docs/multi-endpoint-transport-plan.md`：当前多 endpoint / 多 transport 技术规划。
- `termx-tui-v3/docs/architecture.md`：TUI v3 架构基准。
- `termx-core-v2/docs/architecture.md`：core-v2 架构基准。
- `internal/protocol/` 与 `termx-shared/transport/`：现有协议和 transport 抽象基线。

## 当前允许主动修改范围

- `workflow.md`
- `AGENTS.md`
- `termx-tui-v3/`
- `termx-cli/`
- `termx-shared/`
- `internal/protocol/`
- `termx-proto/`
- `termx-core-v2/`，仅当 endpoint 能力、terminal 生命周期或 history contract 需要时最小化触及。
- `scripts/`、`Makefile`、`go.work`、`go.work.sum`，仅当测试或入口联动需要时最小化触及。

## 冻结范围

- `termx-remote/`
- `termx-remote-v2/`
- `termx-app/`
- `remote-ui/`
- `web-control/`
- `termx-hub/`

以上目录默认冻结。只有当任务队列明确进入 hub/P2P 或远程 UI 切片，并且先更新本工作流范围说明后，才允许解冻。

## 硬语义规则

- `TerminalID` 只在单个 daemon/endpoint 内唯一；TUI/client 侧跨 endpoint 状态必须使用 `EndpointID + TerminalID` 的 `TerminalRef`。
- Endpoint 表达“当前客户端要连接的 daemon 目标”；Transport 表达“到达该 endpoint 的方式”，例如 local unix socket、SSH、hub/P2P。
- daemon 侧“有哪些客户端连接到我”与 TUI/client 侧“我连接了哪些 daemon endpoint”是两个不同管理器，不得混成一个模型。
- TUI 不拥有 terminal lifecycle、committed history 或 history truth；history/live/input/resize 必须路由到 owning endpoint 的 daemon。
- Workbench/layout storage 可以继续保存在本地客户端域，但其中的 terminal 连接意图必须持久化 `TerminalRef`，不能只持久化裸 `TerminalID`。
- endpoint label、transport address、hub identity、SSH host key 和 auth ref 不得互相替代；展示名不能作为安全身份。
- endpoint 离线、认证失败、transport 断开只能影响该 endpoint；不得清空其他 endpoint 的 terminal pool、layout、copy/history 状态。
- cancel token、live channel、surface/session key、input serial key、history token 和 copy token 都必须按 endpoint 作用域隔离。
- 现有 `internal/protocol` 仍以单 daemon 会话为边界；多 endpoint 路由应在 client/TUI 侧 manager 完成，不要求单个 protocol client 同时承载多个 daemon。
- SSH 第一阶段只作为连接远端 termx daemon 的 transport；不得悄悄 fallback 成原始 shell/PTY。

## 任务队列

| ID | 状态 | 范围 | 验收 |
| --- | --- | --- | --- |
| ME001 | 完成 | 清理 `workflow.md`，新增多 endpoint / transport 规划文档 | 文档说明术语、边界、阶段、风险和测试准入 |
| ME002 | 完成 | 引入 `EndpointID` / `TerminalRef` 状态模型，默认 endpoint 为 `local` | 本地单 endpoint 行为不变；同名 terminal 在不同 endpoint 不冲突 |
| ME003 | 待开始 | 设计并实现 client 侧 endpoint registry/config 基础结构 | 可列出 local endpoint；配置缺失时有稳定默认；endpoint 名称和连接策略来自 `tui-v3.yaml` |
| ME004 | 待开始 | Terminal picker / Terminal Pool 支持 endpoint 聚合和局部失败 | picker 展示机器名称、endpoint 状态和 terminal；单 endpoint 失败不影响其他 endpoint |
| ME005 | 待开始 | live/input/resize/owner/copy/history 路由按 `TerminalRef` 隔离 | owner 转移、输入、history token 不跨 endpoint 串扰 |
| ME006 | 待开始 | workbench storage 持久化 endpoint-aware terminal binding | 旧 snapshot 默认映射到 `local`；缺失 endpoint 保留 unresolved binding |
| ME007 | 待开始 | local unix socket 作为标准 endpoint transport | 当前本地 attach 路径迁移到 endpoint manager 后行为不变 |
| ME008 | 待开始 | SSH transport 连接远端 termx daemon | 明确认证、host key、远端 socket 发现和失败展示 |
| ME009 | 阻塞 | hub/P2P transport 与跨设备发现 | 先解冻 `termx-hub/` 并补充 hub 身份、安全和中继策略 |

## 执行规则

1. 每轮先读取 `workflow.md`。
2. 开始实现前检查 `git status --short --branch`，不得混入用户或其他代理的未提交改动。
3. 按任务队列顺序推进最早的 `待开始` 或 `进行中` 切片；遇到 `阻塞` 不得跳过。
4. 切片开始时先把状态改为 `进行中`，可与首个实现提交同提交。
5. 只执行当前切片，不跨切片扩展范围。
6. 先补模型和 harness，再接真实 protocol、transport 或 CLI 入口。
7. 完成后更新任务状态、当前说明和必要文档。
8. 每个有效变动都必须提交，提交信息使用中文；用户明确要求不提交时除外。

## 测试准入

- 文档-only 改动：`git diff --check`
- `termx-tui-v3/` 改动：`cd termx-tui-v3 && go test ./... -count=1`
- `termx-cli/` 改动：`cd termx-cli && go test ./cmd/termx -count=1`
- `internal/protocol/` 改动：`go test ./internal/protocol/... -count=1`
- `termx-shared/transport/` 改动：运行对应 package 的 `go test ... -count=1`
- 任意提交前都必须运行 `git diff --check`

## 当前状态

- ME001 已完成：工作流已收敛为多 endpoint / 多 transport 主线，详细规划落到 `termx-tui-v3/docs/multi-endpoint-transport-plan.md`。
- ME002 已完成：TUI state 已有 `EndpointID` / `TerminalRef` 基础模型，默认 `local` endpoint 保持现有本地行为，同名 terminal 可在不同 endpoint 下共存。
- 下一切片是 ME003：实现 endpoint registry/config 基础结构，定义 `tui-v3.yaml` 中 endpoint 名称、transport 参数和 `auto` / `on_demand` / `manual` 连接策略的解析边界。
