# 多 endpoint / 多 transport 管理规划

## 背景

当前 TUI v3 默认只连接一个本地 daemon。`TerminalID`、terminal pool、live attach、copy/history 和 workbench binding 都隐含在“唯一 daemon”下面成立。下一阶段希望一个 TUI/client 能同时管理多个 daemon endpoint，例如本机 daemon、SSH 到远端服务器上的 daemon，或未来通过 hub/P2P 找到的同账号设备。

这件事和 daemon 侧 client manager 是两个问题：

- daemon 侧 client manager：管理“有哪些客户端正在连接我”，用于断开慢消费者、异常客户端或抢夺控制权。
- TUI/client 侧 endpoint manager：管理“我正在连接哪些 daemon endpoint”，用于把本地和远端 terminal 汇总到同一个工作台。

本文只规划 TUI/client 侧 endpoint manager 和多 transport 路由。daemon 侧 client manager 作为独立后续主题处理。

## 术语

- `Endpoint`：当前 TUI/client 可连接的一个 daemon 目标。一个 endpoint 对应一个 daemon 的 terminal、history、live 和 storage API 边界。
- `Transport`：连接 endpoint 的方式，例如 local unix socket、SSH tunnel 或未来 hub/P2P。
- `EndpointID`：客户端本地稳定 ID，用于持久化、路由和 UI 区分。它不是展示名，也不能替代 SSH host key 或 hub identity。
- `TerminalRef`：跨 endpoint 的 terminal 引用，形如 `{endpoint_id, terminal_id}`。裸 `TerminalID` 只允许在单 endpoint protocol adapter 内使用。
- `EndpointManager`：TUI/client 侧 registry、连接生命周期和服务路由器。
- `AttachmentManager`：daemon 侧已连接客户端管理器。它和 `EndpointManager` 语义不同，不在本文范围内实现。

## 目标

- TUI 能在同一个 terminal pool 中展示本地和远端 endpoint 的 terminal。
- Terminal picker 和必要的 chrome 区域能展示 terminal 所属机器/endpoint，避免同名 terminal 难以区分。
- endpoint 的显示名称、transport 参数和连接策略有明确配置来源。
- pane 与 floating window 能绑定到任意 endpoint 的 terminal。
- input、resize、owner transfer、live frame、copy mode 和 history request 都路由到 terminal 所属 endpoint。
- workbench/layout storage 继续可以托管在本地客户端域，但 terminal binding 必须保存 `TerminalRef`。
- endpoint 离线或认证失败时，保留 layout 中的连接意图，并只把相关 terminal 标记为 unresolved/offline。
- 现有本地 unix socket 使用路径迁移到 endpoint manager 后，单 endpoint 行为不变。

## 非目标

- 第一阶段不实现手机 App、hub/P2P 或跨账号分享。
- 第一阶段不实现 daemon 侧 client manager。
- 不让 TUI 成为 history truth owner；TUI 仍只消费 core-v2 提供的 authoritative history window。
- SSH transport 第一阶段只连接远端 termx daemon，不隐式 fallback 成原始 SSH shell。
- 不用 endpoint label、主机名或配置文件 key 代替安全身份。

## 当前代码形态

TUI v3 目前的关键假设是“一个 runtime 只连接一个 daemon”：

- `services.CoreClient` 和 `services.TerminalService` 接口大多只接收裸 `TerminalID`。
- `ProtocolCoreClientAdapter` 和 `ProtocolTerminalServiceAdapter` 各自持有一个 protocol client。
- `TerminalPoolStore` 是一个全局列表，`TerminalPoolItem` 只有 `TerminalID`。
- `TerminalViewBinding` 持久化 pane/floating 到 terminal 的连接关系，但只保存 `TerminalID`。
- live attach、resize owner、copy/history token、terminal pool refresh、输入 serial key 都没有 endpoint 作用域。
- CLI attach 路径目前 dial 一个默认本地 socket，并额外创建 workbench/clipboard event sessions。
- TUI v3 现有配置文件是 `$XDG_CONFIG_HOME/termx/tui-v3.yaml`，fallback 到 `~/.config/termx/tui-v3.yaml`；当前简化 parser 只支持 scalar 值，不支持 endpoint map/list。

底层已经有一个有用基础：`internal/protocol.Client` 基于 `termx-shared/transport.Transport` 工作。也就是说 transport 抽象本身已有雏形，主要缺的是 endpoint identity、配置、状态隔离和 service routing。

## 架构方向

### Endpoint 配置

建议引入客户端本地配置结构：

```go
type EndpointConfig struct {
    ID            EndpointID
    Label         string
    TransportKind TransportKind
    Address       string
    AuthRef       string
    ConnectMode   EndpointConnectMode
    Enabled       bool
    Default       bool
    Capabilities  EndpointCapabilities
}
```

`ID` 是持久化和路由主键；`Label` 只用于展示；`AuthRef` 指向本地凭据或 SSH 配置；`ConnectMode` 决定启动时是否主动连接；`Capabilities` 表达 endpoint 支持的协议能力，不能靠猜测 endpoint 类型替代。

配置文件沿用 TUI v3 的配置位置：

```text
$XDG_CONFIG_HOME/termx/tui-v3.yaml
~/.config/termx/tui-v3.yaml
```

建议 schema 使用 map，而不是 list，把 endpoint id 固定为 key，避免列表重排影响持久化引用：

```yaml
version: 1

endpoints:
  local:
    label: "This Mac"
    enabled: true
    default: true
    transport: local
    connect_mode: auto       # auto | on_demand | manual
    socket: auto             # auto 表示沿用当前 resolveV3Socket 策略

  lab:
    label: "Lab Server"
    enabled: true
    transport: ssh
    connect_mode: on_demand
    address: "lab.example.com"
    auth_ref: "ssh:lab"
    remote_socket: auto

  prod:
    label: "Prod Box"
    enabled: true
    transport: ssh
    connect_mode: manual
    address: "prod.example.com"
    auth_ref: "ssh:prod-readonly"
    remote_socket: "/run/user/1000/termx-v2-wire1.sock"
```

连接策略：

- `auto`：TUI 启动时主动连接并进入 terminal pool 初始 refresh；失败只标记该 endpoint offline，不阻塞本地 UI。
- `on_demand`：启动时只加载 endpoint 元数据；terminal picker 展示为可连接，用户展开、搜索命中、restore 可见绑定或显式 attach 时再连接。
- `manual`：启动和 restore 都不自动连接；只有用户在 endpoint/picker 中执行 connect 后才 dial。layout 中引用该 endpoint 的 pane/floating 保持 unresolved，直到用户连接。

默认行为：

- 未配置任何 endpoint 时，内置一个 `local` endpoint，`label` 优先取本机 hostname，取不到时显示 `local`，`connect_mode = auto`。
- 配置中没有 `default: true` 时，选择第一个 enabled local endpoint；仍没有则选择第一个 enabled endpoint。
- `label` 可改名但不改变 endpoint 身份；`ID` 一旦被 workbench 引用就不能自动重命名。
- 当前 config parser 不支持动态 endpoint map。ME003 必须补 parser/harness，或切换到完整 YAML 解析，但仍要保持未知字段报错。

### EndpointManager

`EndpointManager` 管理多个 per-endpoint connection bundle。因为当前 protocol session 的 event stream 有边界限制，一个 endpoint 可以继续持有多个 protocol session，例如 terminal/control、workbench storage watch、clipboard storage watch、live/history 等。manager 的职责是注册、连接、重连、状态汇总和按 `EndpointID` 找到正确 service。

推荐边界：

```go
type EndpointManager interface {
    ListEndpoints(ctx context.Context) ([]EndpointStatus, error)
    TerminalService(endpointID EndpointID) (services.TerminalService, error)
    CoreClient(endpointID EndpointID) (services.CoreClient, error)
}
```

也可以在上层提供 endpoint-aware service：

```go
type MultiEndpointTerminalService interface {
    List(ctx context.Context) ([]EndpointTerminalPool, error)
    Attach(ctx context.Context, ref TerminalRef, req AttachRequest) (AttachResult, error)
    SendInput(ctx context.Context, ref TerminalRef, input InputPayload) error
}
```

关键原则是：现有 per-endpoint protocol adapter 不需要知道所有 endpoint。router 在进入 adapter 前剥离 `EndpointID`，在返回结果后补回 `EndpointID`。

### TerminalRef 模型

跨 endpoint 状态必须使用 `TerminalRef`：

```go
type TerminalRef struct {
    EndpointID EndpointID
    TerminalID string
}
```

需要逐步替换或包裹的状态包括：

- `TerminalPoolItem`
- `TerminalViewBinding`
- live attach / surface / frame ready message
- resize owner 和 active owner 查询
- input serial key
- copy/history request、token、generation 和 cache key
- terminal lifecycle / remove / kill / restart 操作

裸 `TerminalID` 只能存在于 protocol adapter 内部和 daemon 返回的原始 payload 中。

### Terminal picker 与 chrome 展示

Terminal picker 是用户最容易感知多 endpoint 的入口，必须把机器/endpoint 信息作为一等字段展示，而不是只在 terminal 名称里拼字符串。

picker 展示模型建议包含：

```go
type EndpointPickerGroup struct {
    EndpointID   EndpointID
    Label        string
    Status       EndpointStatusKind
    Transport    TransportKind
    ConnectMode  EndpointConnectMode
    TerminalRows []TerminalPickerRow
}

type TerminalPickerRow struct {
    Ref        TerminalRef
    Title      string
    Command    string
    CWD        string
    Lifecycle  string
    OwnerState string
}
```

展示规则：

- 多 endpoint 配置存在时，terminal picker 默认按 endpoint 分组，组标题显示 `Label`、transport、连接状态和 terminal 数量。
- 只有一个 local endpoint 时，可以保持当前紧凑展示，但 row 的 view-model 仍必须携带 `TerminalRef`。
- 搜索索引同时包含 endpoint id、label、transport host、terminal title、command 和 cwd。
- 同名 terminal 必须显示 endpoint badge，例如 `Lab Server / shell`，不能只显示 `shell`。
- offline/manual endpoint 也可以作为组显示，提供 connect action；它们没有 terminal list 时不伪造 terminal。
- endpoint 的安全身份只在详情/诊断中显示，例如 SSH host key fingerprint 或 hub device id；普通 picker label 不承担安全校验。

chrome 展示规则：

- pane/floating 绑定非默认 endpoint 时，标题或状态区域应显示 endpoint badge，例如 `Lab Server` 或配置的短标签。
- 多 endpoint 配置存在时，即使当前 terminal 来自默认 endpoint，也可以在 terminal picker、focused pane metadata 或 footer summary 中显示 endpoint，以减少歧义。
- endpoint badge 来自 `EndpointStatus` / view-model，不允许 renderer 读取配置文件或 protocol client。
- endpoint offline 只改变该 binding 的状态标识，不删除 pane/floating。

### Workbench storage

layout storage 仍可作为当前客户端本地 truth source，保存 pane/floating 到 terminal 的连接意图。变化是 binding 必须持久化 endpoint：

```json
{
  "terminal_ref": {
    "endpoint_id": "local",
    "terminal_id": "term-1"
  }
}
```

迁移策略：

- 旧 snapshot 中只有 `terminal_id` 时默认映射到 `endpoint_id = "local"`。
- endpoint 缺失或离线时，不删除 binding，UI 标记为 unresolved/offline。
- remote endpoint 的 terminal lifecycle truth 仍在远端 daemon；本地 storage 只保存连接意图。

## 主要风险和坑

- `TerminalID` 冲突：不同 endpoint 都可能有 `term-1`，所有 map key、cache key 和 UI selection 都要 endpoint scoped。
- owner 串扰：pane 和 floating window 抢 owner 只能影响同一个 `TerminalRef`，不能跨 endpoint demote。
- token 串扰：history/copy/live token 必须绑定 endpoint，endpoint A 的 token 不能发到 endpoint B。
- cancel 串扰：live invalidate、refresh debounce、attach cancellation 不能只按 terminal ID cancel。
- 输入串扰：`SerialKey` 必须包含 endpoint，否则远端同名 terminal 可能复用本地输入序列。
- 局部失败：terminal pool 聚合要能展示 endpoint A 正常、endpoint B offline，不能把一个错误提升为全局空列表。
- reconnect epoch：endpoint 重连后旧 channel、surface revision、history token 和 inflight request 需要失效。
- 安全身份：SSH host key、hub device identity 和 endpoint label 必须分离，避免“改名即信任”。
- 展示歧义：terminal picker、pane chrome 和 footer summary 不能只展示 terminal title；多 endpoint 下必须能看出所属机器。
- parser 复杂度：现有 `tui-v3.yaml` parser 不支持动态 map/list，endpoint config 落地时必须补 schema harness，避免半支持导致配置静默无效。
- 配置漂移：workbench 引用的 endpoint 被删除时，应保留 unresolved binding，并给用户明确修复入口。
- storage 边界：不要把远端 daemon 的 workspace storage 和本地 client layout storage 混成一份 truth。
- transport fallback：SSH 失败不能自动退化成本地 shell 或另一个 endpoint。

## 分阶段实施

### ME001：文档和工作流收敛

清理旧 `workflow.md`，新增本文档，明确当前主线、范围、任务队列和测试准入。

### ME002：本地默认 endpoint 模型

引入 `EndpointID`、`TerminalRef` 和默认 `local` endpoint。先在 state/harness 层证明两个 endpoint 下同名 terminal 不冲突。本阶段可以仍只连接一个本地 daemon。

### ME003：Endpoint registry/config

增加 endpoint 配置读取和默认 local endpoint。CLI/TUI 启动时由 registry 生成 endpoint 列表，而不是直接 dial 固定 socket。本阶段要定义 `tui-v3.yaml` 的 endpoint map、`label`、`connect_mode` 和默认 local 行为。

### ME004：Terminal pool 聚合

Terminal pool 和 terminal picker 从单 service list 改为按 endpoint 聚合。UI 至少能显示 endpoint label/status/connect action，并支持局部失败。

### ME005：live/input/owner/copy/history 路由

把 live attach、input、resize owner、copy/history 请求和 token cache 全部改为 `TerminalRef` 作用域。

### ME006：Workbench storage endpoint-aware schema

workbench snapshot 保存 endpoint-aware binding。旧 snapshot 默认迁移到 `local`。缺失 endpoint 时保留 unresolved binding。

### ME007：Local transport 标准化

把当前 unix socket attach 路径纳入 endpoint manager，确保本地单 endpoint 行为不变。

### ME008：SSH transport

实现 SSH 到远端 termx daemon 的 transport。需要明确认证来源、host key 验证、远端 socket 发现、超时和错误展示。

### ME009：Hub/P2P transport

在解冻 `termx-hub/` 后设计 hub identity、发现、中继、NAT、授权和 revoke 策略。该阶段当前阻塞。

## 必要 harness

- 两个 endpoint 都返回 `term-1`，terminal pool 能展示两个独立条目。
- pane attach 到 `local/term-1`，floating attach 到 `server-a/term-1`，owner 状态不互相 demote。
- 输入发送到 `server-a/term-1` 时，本地 `local/term-1` 的 fake service 没有收到 input。
- live invalidate 只取消同一 `TerminalRef` 的 attach/session。
- history token 来自 endpoint A 时，不能被 endpoint B 的 adapter 使用。
- workbench restore 遇到缺失 endpoint 时保留 binding，并进入 unresolved 状态。
- endpoint B list 失败时，endpoint A 的 terminal pool 结果仍可展示。
- `tui-v3.yaml` 配置 `lab.label = "Lab Server"` 后，terminal picker 和非默认 endpoint 的 pane chrome 使用该 label。
- `connect_mode = manual` 的 endpoint 启动时不 dial；用户显式 connect 后才进入 terminal list。
- `connect_mode = on_demand` 的 endpoint 在 picker 展开或 restore 可见 binding 时才 dial，失败只影响该 endpoint。

## 验收准入

- 文档-only 改动运行 `git diff --check`。
- TUI 状态或服务改动运行 `cd termx-tui-v3 && go test ./... -count=1`。
- CLI attach/config 改动运行 `cd termx-cli && go test ./cmd/termx -count=1`。
- protocol 改动运行 `go test ./internal/protocol/... -count=1`。
- transport 改动运行对应 package 的 `go test ... -count=1`。
- 任意提交前运行 `git diff --check`。
