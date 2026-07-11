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
- `EndpointID`：客户端本地稳定 ID，用于持久化、路由和 UI 区分。它不是展示名，也不能替代 SSH host key 或 hub device fingerprint。
- `TerminalRef`：跨 endpoint 的 terminal 引用，形如 `{endpoint_id, terminal_id}`。裸 `TerminalID` 只允许在单 endpoint protocol adapter 内使用。
- `EndpointManager`：TUI/client 侧 registry、连接生命周期和服务路由器。
- `AttachmentManager`：daemon 侧已连接客户端管理器。它和 `EndpointManager` 语义不同，不在本文范围内实现。

## 目标

- TUI 能在同一个 terminal pool 中展示本地和远端 endpoint 的 terminal。
- Terminal picker 和必要的 chrome 区域能展示 terminal 所属机器/endpoint，避免同名 terminal 难以区分。
- endpoint 的显示名称、transport 参数和连接策略有明确连接注册来源。
- pane 与 floating window 能绑定到任意 endpoint 的 terminal。
- input、resize、owner transfer、live frame、copy mode 和 history request 都路由到 terminal 所属 endpoint。
- workbench/layout storage 继续可以托管在本地客户端域，但 terminal binding 必须保存 `TerminalRef`。
- endpoint 离线或认证失败时，保留 layout 中的连接意图，并只把相关 terminal 标记为 unresolved/offline。
- 现有本地 unix socket 使用路径迁移到 endpoint manager 后，单 endpoint 行为不变。

## 非目标

- 第一阶段已完成 local/SSH endpoint；hub/P2P 进入 ME010+ 分阶段落地。ME010 只定义身份、安全、中继和 registry contract，不接真实网络。
- 当前仍不实现手机 App 或跨账号分享。
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
- TUI v3 现有配置文件是 `$XDG_CONFIG_HOME/termx/tui-v3.yaml`，fallback 到 `~/.config/termx/tui-v3.yaml`；该文件只表达 TUI 偏好，不应承载 endpoint/connection 注册表。

底层已经有一个有用基础：`internal/protocol.Client` 基于 `shared/transport.Transport` 工作。也就是说 transport 抽象本身已有雏形，主要缺的是 endpoint identity、配置、状态隔离和 service routing。

## 架构方向

### Connection registry

endpoint/connection 是 CLI、TUI 和未来其他客户端共享的连接目标注册表，不属于 TUI chrome/theme/shortcuts 偏好。它应该独立于 `tui-v3.yaml`，使用单独文件：

```text
$XDG_CONFIG_HOME/termx/connections.yaml
~/.config/termx/connections.yaml
```

建议引入客户端本地 registry 结构：

```go
type ConnectionRegistry struct {
    Version     int
    Default     EndpointID
    Connections map[EndpointID]ConnectionConfig
}

type ConnectionConfig struct {
    ID           EndpointID
    Label        string
    Transport    TransportKind
    Address      string
    AuthRef      string
    ConnectMode  EndpointConnectMode
    Enabled      bool
    Socket       string
    RemoteSocket string
    HubDeviceID       string
    DeviceFingerprint string
    GrantRef          string
    RelayMode         RelayMode
}
```

`ID` 是持久化和路由主键；`Label` 只用于展示；`AuthRef` 只用于 SSH；`GrantRef` 指向 managed WebRTC 的本地 capability grant；`ConnectMode` 决定启动时是否主动连接；`Socket` / `RemoteSocket` 属于 dial identity，运行中不能热切换。

建议 schema 使用 map，而不是 list，把 endpoint id 固定为 key，避免列表重排影响持久化引用：

```yaml
version: 1
default: local

connections:
  local:
    label: "This Mac"
    enabled: true
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

  studio:
    label: "Studio Mac"
    enabled: true
    transport: hub-p2p
    connect_mode: on_demand
    hub_device_id: "studio"
    device_fingerprint: "SHA256:abc123..."
    grant_ref: "grant:studio"
    relay_mode: auto       # auto | direct | relay_only
```

连接策略：

- `auto`：TUI 启动时主动连接并进入 terminal pool 初始 refresh；失败只标记该 endpoint offline，不阻塞本地 UI。
- `on_demand`：启动时只加载 endpoint 元数据；terminal picker 展示为可连接，用户展开、搜索命中、restore 可见绑定或显式 attach 时再连接。
- `manual`：启动和 restore 都不自动连接；只有用户在 endpoint/picker 中执行 connect 后才 dial。layout 中引用该 endpoint 的 pane/floating 保持 unresolved，直到用户连接。

默认行为：

- 未配置任何 endpoint 时，内置一个 `local` endpoint，`label` 优先取本机 hostname，取不到时显示 `local`，`connect_mode = auto`。
- registry 中没有 `default` 时，选择第一个 enabled local endpoint；仍没有则选择第一个 enabled endpoint。
- `label` 可改名但不改变 endpoint 身份；`ID` 一旦被 workbench 引用就不能自动重命名。
- `connections.yaml` 不复用当前 TUI scalar parser。ME003 应单独实现 connection registry loader/harness，或使用完整 YAML 解析，但仍要保持未知字段报错。
- `tui-v3.yaml` 只继续保存 TUI 偏好；renderer/input router 不读取 `connections.yaml`，只消费 reducer view-model。

hub/P2P registry 规则：

- Hub assignment 和 service URL 只来自 Companion/Control Plane 的当前 resolve，不进入 `connections.yaml`，也不属于 dial identity。
- `hub_device_id` 只用于 managed cloud 发现/路由，不是安全信任锚点。
- `device_fingerprint` 是远端 daemon/device public key 的信任锚点。`label`、endpoint id、Hub assignment、relay 地址和 grant ref 都不能替代它。
- `grant_ref` 只引用本地凭据存储、系统 keychain 或后续明确的 hub grant store；`connections.yaml` 不保存原始 token、私钥、capability grant 或一次性 pairing code。
- managed 配对保持 daemon -> client 单向引导：daemon 生成 capability grant，导入 bundle 只传递 target DeviceID、device fingerprint 和 grant；客户端把 grant 写入本地凭据存储后只在 registry 中保存 `grant_ref`。
- capability grant 是 remote-issued bearer capability，必须带 scope、expiry、grant id 和 revoke 语义；remote 连接时校验 grant，hub/relay 不能签发或扩大权限。
- `relay_mode = auto` 可以先尝试 P2P 直连再使用受控 relay；`direct` 禁止 relay fallback；`relay_only` 用于受限网络或诊断。
- `address`、`socket`、`remote_socket` 不适用于 `hub-p2p`。hub transport 通过 hub 发现目标找到远端 termx daemon，并用 `device_fingerprint` 校验远端身份，不应退化成 SSH、local socket 或原始 shell。
- 修改 `hub_device_id`、`device_fingerprint`、`grant_ref` 或 `relay_mode` 都属于 dial identity 变化，已连接 session 必须标记 `reconnect required`，不能热切换。

### Registry reload 与运行时 session

`connections.yaml` 是连接期望状态；已经连上的 endpoint session 是运行时事实。配置 reload 只能产生 registry diff，不能直接把运行中的 protocol session 原地改成另一台机器。

变更处理规则：

| 变更 | 已连接 session 行为 |
| --- | --- |
| 修改 `label` | 立即更新 UI 展示名；terminal title、cwd、history 和 lifecycle 不变。 |
| `enabled: true -> false` | 不自动断开已连接 session；标记为 `disabled by config`，禁止自动恢复、自动连接和创建新 terminal。 |
| 修改 `connect_mode` | 只影响未来连接策略；已连接 session 不变。 |
| 修改 `address` / `transport` / `auth_ref` / `socket` / `remote_socket` | 不热切换；当前 session 继续使用旧 dial 参数，UI 标记 `reconnect required`，断开或用户显式 reconnect 后才使用新配置。 |
| 修改 `hub_device_id` / `device_fingerprint` / `grant_ref` / `relay_mode` | 不热切换；managed 目标、远端设备身份、授权能力引用和 relay 策略都属于 dial identity，必须显式 reconnect 后生效。Hub assignment 由下一次 Companion resolve 获取，不写入 registry。 |
| 删除 connection | 当前 session 可继续存在但标记为 `unregistered`；重启后 layout binding 保留 unresolved，不自动连接。 |
| 修改 connection ID | 等价于删除旧 connection 并新增新 connection；不得自动迁移 workbench refs。 |

重启 TUI 时：

- `enabled=false` 的 endpoint 不自动连接。
- layout 中引用 disabled/unregistered endpoint 的 pane/floating 不删除，只进入 unresolved/disabled 状态。
- 如果 endpoint 的 dial identity 变化，不能直接拿旧 `TerminalID` 去新地址 attach；需要用户显式 reconnect 或后续 daemon identity 校验确认。

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
- 安全身份：SSH host key、hub device fingerprint、hub discovery id 和 endpoint label 必须分离，避免“改名即信任”。
- 展示歧义：terminal picker、pane chrome 和 footer summary 不能只展示 terminal title；多 endpoint 下必须能看出所属机器。
- parser 复杂度：connection registry 需要动态 map，不能复用当前 `tui-v3.yaml` scalar parser 半支持；必须单独补 schema harness，避免配置静默无效。
- 配置漂移：workbench 引用的 endpoint 被删除时，应保留 unresolved binding，并给用户明确修复入口。
- storage 边界：不要把远端 daemon 的 workspace storage 和本地 client layout storage 混成一份 truth。
- transport fallback：SSH 失败不能自动退化成本地 shell 或另一个 endpoint。
- hub 身份混淆：`label`、endpoint id、hub URL、hub device id、relay 地址和 `grant_ref` 都不能替代 `device_fingerprint`；否则改名、换 hub 或换 grant 会被误当成信任迁移。
- grant 泄露：capability grant 是 bearer secret，必须高熵、可过期、可撤销，并保存在本地凭据存储；`connections.yaml` 只能保存 `grant_ref`。
- hub relay 越权：relay 只能承载受限 protocol/datachannel，不能保存 terminal lifecycle、history truth、workbench storage truth 或设备信任决策。
- hub revoke 漏洞：授权撤销、设备撤销或 grant ref 失效必须只让对应 endpoint offline/reconnect-required，不能保留旧 channel 当作有效连接。

## 分阶段实施

### ME001：文档和工作流收敛

清理旧 `workflow.md`，新增本文档，明确当前主线、范围、任务队列和测试准入。

### ME002：本地默认 endpoint 模型

引入 `EndpointID`、`TerminalRef` 和默认 `local` endpoint。先在 state/harness 层证明两个 endpoint 下同名 terminal 不冲突。本阶段可以仍只连接一个本地 daemon。

### ME003：Endpoint registry/config

增加 connection registry 读取和默认 local endpoint。CLI/TUI 启动时由 registry 生成 endpoint 列表，而不是直接 dial 固定 socket。本阶段要定义 `connections.yaml` 的 endpoint map、`label`、`connect_mode` 和默认 local 行为。

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

第一阶段采用 OpenSSH stdio proxy：

- 本地 transport 启动 `ssh -T`，远端只执行 `termx --socket <remote_socket> v3 stdio-proxy`，双方用长度前缀 frame 承载现有 termx protocol payload。
- `auth_ref = ssh:<alias>` 表示使用本机 OpenSSH config 中的 host alias；为空时使用 `address` 作为 SSH target。私钥、agent、ProxyJump 和用户名都交给 OpenSSH 配置，不在 `connections.yaml` 内保存密钥路径或密码。
- Host key 必须走 OpenSSH `known_hosts` 校验，默认 `StrictHostKeyChecking=yes` 和 `BatchMode=yes`；未知 host、host key 变化或认证失败都作为该 endpoint 的 transport 错误展示，不得自动改写 known_hosts。
- `remote_socket: auto` 表示在远端进程内使用远端 termx 默认 socket 解析策略；显式路径只作为远端 daemon socket，不参与本地 socket 解析。
- SSH 建连成功但远端缺少 `termx`、远端 daemon 无法启动或 socket 无法连接时，只把该 endpoint 标记为 offline 并保留 terminal/workbench 连接意图，不清空其他 endpoint。
- 失败不能 fallback 成原始 SSH shell/PTY，也不能把请求转发到 local endpoint。

### ME010：Hub/P2P identity contract

该历史切片已由当前 managed cloud contract 取代；Hub 服务端实现位于 private cloud，公开 registry 只保存 endpoint/device/capability identity。

本阶段不接真实网络，只要求：

- `connections.yaml` 可表达 `transport: hub-p2p` endpoint，并要求 `hub_device_id`、`device_fingerprint` 和 `grant_ref`；caller-selected `hub_url` 必须拒绝。
- `relay_mode` 支持 `auto`、`direct`、`relay_only`，默认 `auto`。
- `hub_device_id` 和 `relay_mode` 进入 dial identity，修改后标记 reconnect required；ME011 会继续把安全身份收敛到 `device_fingerprint`。
- TUI endpoint projection 可展示 hub endpoint，EndpointManager 在无 hub dialer 时返回局部未连接错误，不 fallback 到 local/SSH/旧 remote。

### ME011：Hub/P2P one-way grant contract

按单向配对模型收敛 hub 授权 contract。remote 执行 pair 时生成 capability grant，客户端扫码或导入后保存到本地凭据存储；配对不要求客户端公钥回传，也不要求 remote 维护客户端 allowlist。

本阶段要求：

- `hub_device_id` 只作为 hub 发现/路由 ID。
- `device_fingerprint` 是远端设备安全身份，必须进入 dial identity。
- `grant_ref` 指向本地凭据存储中的 remote-issued capability grant，必须进入 dial identity。
- `connections.yaml` 不保存 grant token；registry 缺少 `device_fingerprint` 或 `grant_ref` 必须失败。

### ME012：Hub/P2P transport

接入 `termx-hub/` 的发现、授权、revoke、NAT traversal 和 relay datachannel。真实 transport 只连接远端 termx daemon 的 protocol session，不能 fallback 成原始 shell、旧 remote UI 或本地 daemon。

## 必要 harness

- 两个 endpoint 都返回 `term-1`，terminal pool 能展示两个独立条目。
- pane attach 到 `local/term-1`，floating attach 到 `server-a/term-1`，owner 状态不互相 demote。
- 输入发送到 `server-a/term-1` 时，本地 `local/term-1` 的 fake service 没有收到 input。
- live invalidate 只取消同一 `TerminalRef` 的 attach/session。
- history token 来自 endpoint A 时，不能被 endpoint B 的 adapter 使用。
- workbench restore 遇到缺失 endpoint 时保留 binding，并进入 unresolved 状态。
- endpoint B list 失败时，endpoint A 的 terminal pool 结果仍可展示。
- `connections.yaml` 配置 `lab.label = "Lab Server"` 后，terminal picker 和非默认 endpoint 的 pane chrome 使用该 label。
- `connect_mode = manual` 的 endpoint 启动时不 dial；用户显式 connect 后才进入 terminal list。
- `connect_mode = on_demand` 的 endpoint 在 picker 展开或 restore 可见 binding 时才 dial，失败只影响该 endpoint。
- `transport: hub-p2p` 缺少 `hub_device_id`、`device_fingerprint` 或 `grant_ref` 时 registry 解析失败；出现 `hub_url` 同样失败。
- hub endpoint 修改 `label` 只更新展示；修改 `device_fingerprint`、`grant_ref` 或 `relay_mode` 必须标记 reconnect required。
- 无 hub dialer 时 hub endpoint service 请求只返回该 endpoint 的未连接错误，不调用 local/SSH service。

## 验收准入

- 文档-only 改动运行 `git diff --check`。
- TUI 状态或服务改动运行 `go test ./tui/... -count=1`。
- CLI attach/connection registry 改动运行 `go test ./cmd/termx -count=1`。
- protocol 改动运行 `go test ./internal/protocol/... -count=1`。
- transport 改动运行对应 package 的 `go test ... -count=1`。
- 任意提交前运行 `git diff --check`。
