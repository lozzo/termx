# 工作流：多 endpoint / 多 transport 管理主线

## 当前目标

- 当前分支从多 endpoint / 多 transport 主线临时切入插件系统第一阶段基础落地。
- 插件控制面设计以 `termx-tui-v3/docs/client-session-plugin-control-design.md` 为准。
- 多 endpoint / 多 transport 详细技术规划仍以 `termx-tui-v3/docs/multi-endpoint-transport-plan.md` 为准。
- 旧 screen app 无限历史清场与重建记录不再保留在本活动文件中；需要追溯时查看 git 历史和对应文档。

## 基准文档

- `termx-tui-v3/docs/client-session-plugin-control-design.md`：当前插件 action / hook / message trace 技术规划。
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
- `termx-hub/`，仅限 ME010+ hub/P2P 身份、安全、中继、发现和 transport contract 需要时触及；不得恢复旧 remote UI/app 路径。
- `scripts/`、`Makefile`、`go.work`、`go.work.sum`，仅当测试或入口联动需要时最小化触及。

## 冻结范围

- `termx-remote/`
- `termx-remote-v2/`
- `termx-app/`
- `remote-ui/`
- `web-control/`

以上目录默认冻结。`termx-hub/` 当前已为 ME010+ hub/P2P 主线受限解冻，但不得牵连 `termx-app/`、`remote-ui/`、`web-control/` 或旧 remote fallback。

## 硬语义规则

- `TerminalID` 只在单个 daemon/endpoint 内唯一；TUI/client 侧跨 endpoint 状态必须使用 `EndpointID + TerminalID` 的 `TerminalRef`。
- ME009 起，用户可见 terminal 名称是 first-party 新建 terminal 的默认 canonical key；协议字段仍暂名为 `TerminalID`，但 create 必须优先发送 terminal name，并在单 endpoint 内拒绝重名。
- 后续 identity 收敛切片才能重命名协议字段或删除旧 ID 兼容；当前不得把随机 ID 继续用于 first-party 新建 terminal 的 history/layout/storage key。
- Endpoint 表达“当前客户端要连接的 daemon 目标”；Transport 表达“到达该 endpoint 的方式”，例如 local unix socket、SSH、hub/P2P。
- daemon 侧“有哪些客户端连接到我”与 TUI/client 侧“我连接了哪些 daemon endpoint”是两个不同管理器，不得混成一个模型。
- connection registry 是 CLI/TUI 共享的 endpoint 注册表，默认文件为 `$XDG_CONFIG_HOME/termx/connections.yaml` 或 `~/.config/termx/connections.yaml`；不得塞进 TUI-only 的 `tui-v3.yaml`。
- TUI 不拥有 terminal lifecycle、committed history 或 history truth；history/live/input/resize 必须路由到 owning endpoint 的 daemon。
- Workbench/layout storage 可以继续保存在本地客户端域，但其中的 terminal 连接意图必须持久化 `TerminalRef`，不能只持久化裸 `TerminalID`。
- endpoint label、transport address、hub identity、SSH host key 和 auth ref 不得互相替代；展示名不能作为安全身份。
- endpoint 离线、认证失败、transport 断开只能影响该 endpoint；不得清空其他 endpoint 的 terminal pool、layout、copy/history 状态。
- cancel token、live channel、surface/session key、input serial key、history token 和 copy token 都必须按 endpoint 作用域隔离。
- 现有 `internal/protocol` 仍以单 daemon 会话为边界；多 endpoint 路由应在 client/TUI 侧 manager 完成，不要求单个 protocol client 同时承载多个 daemon。
- SSH 第一阶段只作为连接远端 termx daemon 的 transport；不得悄悄 fallback 成原始 shell/PTY。
- hub/P2P endpoint 的展示名、endpoint id、hub URL、hub device id、device fingerprint、grant ref 和 relay 策略必须分离；`device_fingerprint` 才是远端设备安全身份，`hub_device_id` 只用于发现/路由，label 只用于 UI。
- hub/P2P 配对保持 remote -> client 单向引导：remote 生成 capability grant，客户端扫码或导入后保存到本地凭据存储；remote 不要求客户端公钥回传或写入 allowlist。
- hub/P2P relay 只能承载受限 protocol/datachannel，不能成为 terminal lifecycle、history、workbench storage 或设备信任 truth。
- hub/P2P 连接失败、授权失效、设备撤销或 relay 不可用时，只影响对应 endpoint，不得 fallback 到 local、SSH、旧 remote app 或原始 shell。
- `connections.yaml` 不保存 hub 原始 token、capability grant 或私钥；`grant_ref` 只能引用本地凭据存储、系统 keychain 或后续明确的 hub grant store。

## 任务队列

| ID | 状态 | 范围 | 验收 |
| --- | --- | --- | --- |
| ME001 | 完成 | 清理 `workflow.md`，新增多 endpoint / transport 规划文档 | 文档说明术语、边界、阶段、风险和测试准入 |
| ME002 | 完成 | 引入 `EndpointID` / `TerminalRef` 状态模型，默认 endpoint 为 `local` | 本地单 endpoint 行为不变；同名 terminal 在不同 endpoint 不冲突 |
| ME003 | 完成 | 设计并实现 client 侧 connection registry 基础结构 | 可列出 local endpoint；配置缺失时有稳定默认；endpoint 名称和连接策略来自 `connections.yaml` |
| ME004 | 完成 | Terminal picker / Terminal Pool 支持 endpoint 聚合和局部失败 | picker 展示机器名称、endpoint 状态和 terminal；单 endpoint 失败不影响其他 endpoint |
| ME005 | 完成 | live/input/resize/owner/copy/history 路由按 `TerminalRef` 隔离 | owner 转移、输入、history token 不跨 endpoint 串扰 |
| ME006 | 完成 | workbench storage 持久化 endpoint-aware terminal binding | 旧 snapshot 默认映射到 `local`；缺失 endpoint 保留 unresolved binding |
| ME007 | 完成 | local unix socket 作为标准 endpoint transport | 当前本地 attach 路径迁移到 endpoint manager 后行为不变 |
| ME008 | 完成 | SSH transport 连接远端 termx daemon | 明确认证、host key、远端 socket 发现和失败展示 |
| ME009 | 完成 | Terminal picker 单一 create 入口与 terminal name identity 第一阶段 | picker 只展示一个 create 行；create prompt 记住上次 endpoint/command/workdir；新建 terminal 在单 endpoint 内按名称唯一，默认以名称作为 daemon-local key |
| ME010 | 完成 | hub/P2P 身份、安全、中继策略与 connection registry contract | `connections.yaml` 可表达 hub endpoint；label 不作为安全身份；hub 发现目标/relay 变化触发 reconnect；无真实 dialer 时不 fallback |
| ME011 | 完成 | hub/P2P 单向配对与 capability grant contract | `hub_device_id` 只做发现；`device_fingerprint` 是远端安全身份；`grant_ref` 指向 remote-issued grant；fingerprint/grant 变化触发 reconnect |
| PL001 | 完成 | 插件 action / hook / message trace 基础模型 | `termx-shared/plugin` 提供纯模型与 harness：host-derived identity、trace/depth、hook scope/filter/delivery、self-caused 与 dedupe 防循环；不接 runner、protocol 或真实 UI |
| PL002 | 完成 | 插件 manifest / catalog / grant 纯模型与 harness | `termx-shared/plugin` 定义 manifest、contribution catalog、event catalog、capability grant 与 namespace 校验；host 从 manifest+grant 推导 action/hook，不信任 runner 自报 |
| PL003 | 完成 | TUI client action registry，把快捷键背后操作收敛到 ActionID | TUI 内建操作注册为 `termx.client.*` action；快捷键只绑定 ActionID，同一 action 可绑定多组 key sequence |
| PL004 | 完成 | client session control protocol 草案落地 | `internal/protocol` 定义 client session register/list/call/response 基础消息；daemon 只做 broker，不解释 UI state |
| PL005 | 完成 | daemon broker + TUI mailbox | 支持 daemon 按 SessionID 或授权广播向 TUI 投递 action，并接收 typed response/error |
| PL006 | 待开始 | hook source adapter | 接入 TUI panel/float/tab after-event 与 daemon terminal lifecycle/PTY activity 元数据 hook；不做 before/veto hook |
| PL007 | 待开始 | one-shot external stdio JSON runner | 通过 stdio JSON 协议执行 one-shot 插件；PL007 前不引入 Lua/JS/Risor/WASM 核心依赖 |
| SI001 | 暂停 | TUI 同步输入组交互与 input 多播 | 用户确认先切入插件消息流基础模型；恢复后继续 `Ctrl-P i/v/u` 同步输入组 |
| ME012 | 待开始 | hub/P2P transport dialer 与跨设备发现 | 接入 `termx-hub/` 发现/授权/relay；P2P 或 relay datachannel 只连接远端 termx daemon；局部失败不影响其他 endpoint |

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
- `termx-shared/connection/` 改动：`cd termx-shared && go test ./connection -count=1`
- `termx-shared/plugin/` 改动：`cd termx-shared && go test ./plugin -count=1`
- `termx-shared/transport/` 改动：运行对应 package 的 `go test ... -count=1`
- 任意提交前都必须运行 `git diff --check`

## 当前状态

- ME001 已完成：工作流已收敛为多 endpoint / 多 transport 主线，详细规划落到 `termx-tui-v3/docs/multi-endpoint-transport-plan.md`。
- ME002 已完成：TUI state 已有 `EndpointID` / `TerminalRef` 基础模型，默认 `local` endpoint 保持现有本地行为，同名 terminal 可在不同 endpoint 下共存。
- ME003 已完成：`termx-shared/connection` 提供独立 `connections.yaml` registry loader，缺省返回稳定 `local` endpoint，并定义 label 热更新与 dial identity 变更需要 reconnect 的基础判断。
- ME004 已完成：TUI state 增加 reducer-owned endpoint 展示投影，Terminal picker / Terminal Manager 可按 endpoint 分组展示机器名称、transport、connect mode、状态和局部错误；endpoint-scoped list 失败不会清空其他 endpoint 的 terminal rows。
- ME005 已完成：live surface/session/input channel、Terminal Pool 操作、resize owner、copy/history pending/window/release 均带 `TerminalRef` 路由；同名 terminal 的 input serial、live refresh、history window 和 copy session 不跨 endpoint 串扰。
- ME006 已完成：workbench snapshot 持久化 `EndpointID + TerminalID` 连接意图，旧 binding 缺 endpoint 时默认恢复为 `local`；缺失、disabled 或 manual endpoint 的 binding 保留在 pane/floating layout 中并标记 unresolved，不自动 attach。
- ME007 已完成：TUI runtime 启动时加载 `connections.yaml` endpoint registry，local unix socket 作为 `EndpointManager` 的标准 local transport bundle 接入；terminal/core/live 请求进入 per-endpoint adapter 前剥离 `EndpointID`，回包后补回 `EndpointID`，当前本地 attach、list、live、history 行为保持不变；显式 `--socket` 仍覆盖 registry local socket。
- ME008 已完成：新增 OpenSSH stdio-proxy transport，`auth_ref=ssh:<alias>` 使用本机 SSH config，host key 由 known_hosts 严格校验，`remote_socket=auto` 在远端解析默认 socket；EndpointManager 可 lazy dial SSH endpoint，Terminal Manager 对 auto/on_demand endpoint 执行聚合刷新，失败只标记对应 endpoint offline 且不 fallback 成 shell 或 local endpoint。
- ME009 已完成：picker 只保留单一 create 行，create prompt 用 server 下拉选择 endpoint，并记住上次 endpoint/command/workdir；TUI/CLI/protocol first-party create 优先以 terminal name 作为 daemon-local key，core-v2 在 create 与 rename 时拒绝同 daemon 重名。
- ME010 已完成：`connections.yaml` 可表达 hub/P2P endpoint，hub URL、`hub_device_id` 与 relay 策略分离；label 只影响展示，hub 发现目标/relay 变化触发 reconnect；无真实 hub dialer 时 EndpointManager 只返回该 endpoint 的未连接错误，不 fallback。
- ME011 已完成：按用户确认的单向配对模型收敛 hub 安全 contract，remote 生成 capability grant 给客户端；`hub_device_id` 只做发现/路由，`device_fingerprint` 作为远端设备安全身份，`grant_ref` 指向本地保存的 grant，真实 `termx-hub/` transport dialer 和跨设备发现进入 ME012。
- PL001 已完成：`termx-shared/plugin` 已落 action / hook / message trace 纯模型与 harness；覆盖 host-derived identity、opaque trace token、depth/budget、hook scope/filter/delivery、self-caused 与 dedupe 防循环，不接 external runner、WASM、protocol、core terminal truth 或 TUI 真实 UI。
- PL002 已完成：`termx-shared/plugin` 已落 manifest / catalog / grant 纯模型与 harness；host 按 manifest+event catalog+grant 解析 action、hook、keybinding，限制第三方 `termx.*`、builtin runner、wildcard hook、delivery policy override、跨 owner keybinding、one-shot hooks/keybindings 和 client kind 越界。
- PL003 已完成：TUI input 层新增 client-local `termx.client.*` action registry，公开 `BindingCatalog` 只暴露 ActionID/ActionArgs；现有快捷键经 action registry 适配回原 semantic intent，新增 `Ctrl+1..9` 直接触发 tab activate，与 `Ctrl+T` 后数字共用同一 action。
- PL004 已完成：`internal/protocol` 已定义 client session register/list 与 client control call/respond 的 typed protocol contract；`client.control.call` 请求不携带 Source，broker 必须由 runner session/grant 派生 `ClientControlInvocation`，TerminalRef 只保留 `EndpointID + TerminalID`，本切片未实现 daemon broker 或 TUI mailbox。
- PL005 已完成：daemon client session broker 已接入 protocol register/list/watch/unwatch/call/respond；TUI services 提供 mailbox adapter，daemon 按 SessionID 或显式 BroadcastAllowed action catalog 路由，Source 由 host 派生，TerminalRef 只透传给目标 client，不作为 broker broadcast scope。
- SI001 暂停：按用户确认先切入插件消息流基础模型；恢复时继续 TUI 本地同步输入组，且同步状态仍属于当前 TUI reducer-owned 输入路由状态。
