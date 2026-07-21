# 统一 Endpoint / Route 技术基线

状态：RTC001 Proto contract 基线

活动切片、顺序、范围和测试准入只以仓库根目录 `workflow.md` 为准。本文只说明 Endpoint/Route 的稳定模型，不维护第二份任务队列。

## 1. 产品结论

- 一个 daemon 在客户端只对应一个 Endpoint。
- local Unix、Direct WebRTC TCP、SSH WebRTC TCP 和 managed WebRTC 是到达同一 Endpoint 的 Route。
- Endpoint identity 只能由经过验证的 DeviceIdentity/fingerprint 归并；IP、域名、SSH host、Cloud DeviceID 和 label 都不是身份真值。
- Direct 与 SSH 不依赖账号、订阅、Hub 或 Relay。
- Muxvia Cloud 是同一个 App 内的可选 managed Route，不是独立 App 版本。
- 所有远程 Route 最终进入可靠有序 WebRTC DataChannel，并复用同一 remote auth、Hello、Proto command/event 和 resource lifecycle。

## 2. Proto 真值

`proto/remoteauthpb/remote_auth.proto` 是 Endpoint/Route 跨边界 schema truth。当前 contract 为：

```text
EndpointRegistryV1
  schema_version
  default_endpoint_id
  endpoints[] EndpointConfigV1

EndpointConfigV1
  schema_version
  endpoint_id
  label / label_source
  identity
  connect_mode
  enabled
  selection_policy
  routes[] EndpointRouteConfigV1

EndpointRouteConfigV1
  schema_version
  route_id
  enabled / manual_only / priority
  credential_ref
  source / policy_source
  oneof route
    local_unix
    direct_webrtc_tcp
    ssh_webrtc_tcp
    managed_webrtc
```

旧的 route kind enum、`direct-tls`、`ssh-stdio` 和扁平 kind-specific 字段已经退出 contract，不允许恢复 alias 或兼容 parser。

### 2.1 Local Unix

```text
LocalUnixRouteConfig
  socket
```

Local Unix 只用于同一主机的 Go/native CLI 和 TUI，不进入二维码、Endpoint share 或远程平台 binding。

### 2.2 Direct WebRTC TCP

```text
DirectWebRTCTCPRouteConfig
  signaling_addresses[]
  ice_tcp_addresses[]
  advertised_addresses[]
  server_name?
```

- `signaling_addresses` 定位 daemon embedded signaling。
- `ice_tcp_addresses` 定位 daemon ICE-TCP listener。
- `advertised_addresses` 是用户为 LAN、FRP 或其它 TCP 映射提供的 locator override。
- 地址覆盖不能改变 Endpoint identity、DTLS binding、CapabilityGrant scope 或 PairingTicket。

### 2.3 SSH WebRTC TCP

```text
SSHWebRTCTCPRouteConfig
  host / port / user
  host_key_fingerprints[]
  proxy_jump?
  credential_descriptor?
  remote_signaling_address
  remote_ice_tcp_address
  ssh_credential_ref
```

- Go Client Engine 负责 SSH host-key 校验、credential resolution 和 `direct-tcpip` tunnel。
- Pion ICE-TCP 通过 SSH-backed dialer 访问 daemon loopback listener。
- `credential_descriptor` 只描述目标平台所需凭据类别，不携带 password、private key、Cloud token 或源平台 credential ref。
- 旧 OpenSSH 子进程与 `muxvia daemon stdio-proxy` 已删除，不是该 Route 的实现或 fallback。

### 2.4 Managed WebRTC

```text
ManagedWebRTCRouteConfig
  target_device_id
  account_profile_ref
  relay_mode
```

- Cloud signaling、ICE-UDP 和 TURN 只提供可达性。
- Cloud 登录和订阅只决定 managed Route eligibility。
- Endpoint identity、CapabilityGrant、terminal scope 和 protocol session 仍由公开 Go Client Engine 与 owning daemon 验证。

## 3. Credential 边界

`EndpointCredentialDescriptor` 是 portable contract，只包含：

```text
descriptor_id
kind
exportable
```

- registry 中的 `credential_ref` 只引用当前平台 secure store。
- share/import 必须要求目标平台重新解析或创建 credential。
- SSH private key、password、Cloud token、CapabilityGrant body 和 ClientAccessIdentity private key 都不能进入 Endpoint Proto、二维码或普通配置文件。

## 4. Go 领域职责

| Owner | 负责 | 不负责 |
| --- | --- | --- |
| `client/endpoint` | registry projection、strict parser、assembler、planner、identity conflict、portable contract validation | 网络 IO、credential body、session winner、重连 |
| `client/runtime` | attempt、PeerConnector、ReadyPeerSession、通用 pairing admission/DTLS binding、winner、generation、lease、cancel、replacement | 复制 Route 配置、signaling、UI state、terminal truth |
| `client/adapter` | 执行 planner 已选定的单条 Route | 自行选择 fallback、创建第二份 Endpoint/session truth |
| `remote/` | daemon embedded signaling、ICE mux、Pion/DataChannel 和 remote auth 接线 | Cloud 账号、订阅、terminal capability policy |
| `client/binding` | Proto bytes、opaque handle、异步 operation/event、close/release | Endpoint、Route、credential 或重连状态机 |

Go 内部可以使用便于 validation/planning 的领域投影，但跨 JNI/C ABI、进程、插件、第三方客户端或未来 WASM 的字段必须来自生成 Proto，不能手写镜像 DTO。

## 5. Parser 与 assembler

- 当前 `endpoints.yaml` 是 Go Client Engine 的本地可读投影，schema version 为 `3`。
- parser 使用 known-fields 模式；旧 version、旧 route kind、旧字段、未知字段和混合 kind-specific 字段全部 fail closed。
- assembler 只按完整 DeviceIdentity 合并来源。
- 相同 RouteID 改变 Route 类型属于冲突。
- label、地址或 Cloud projection 不能替换已 pin fingerprint。
- 外部 bootstrap 默认只能增加可达 Route，不得静默覆盖客户端 priority、manual-only 或 connect mode。

## 6. Planner

- planner 输入是规范化 Endpoint、connect intent、generation、平台支持 Route 和本地可用 credential ref。
- planner 不读取 secure store body、不 dial、不选择 winner、不修改 registry。
- planner 输出不可变 attempt group 和稳定过滤诊断。
- 平台明确支持的 Direct、SSH 和 Cloud connector 使用同一 `PeerConnector.Connect -> ReadyPeerSession` contract，可以进入同一 attempt group。
- Route connector 在当前平台未装配或 credential primitive 缺失时必须显式 unavailable，不能把 Route 配置降级解释成旧 transport。
- Direct 与 SSH 已收口为同一 ReadyPeerSession；Cloud 最终装配时机由 `workflow.md` 推进。

## 7. Portable bundle

`EndpointBootstrapBundleV2` 和 `ClientEndpointShareBundleV1` 都携带 `EndpointRouteConfigV1`：

- bootstrap 允许 daemon 提供签名 Route hint 和一次性 PairingTicket。
- share 只在一次性 TLS share session 内传输用户确认后的 portable config。
- share offer 只携带 listener locator、临时证书 pin、一次性 secret、transfer ID 和有效期；接收端使用临时 Ed25519 key 完成 nonce challenge receiver proof 后，发送端才单次释放 bundle。
- CLI/TUI 通过同一 `endpoints.yaml` 使用 `muxvia endpoint share ID`；Android 通过同一 Go binding 先接收 preview token、展示 Route/policy diff，再原子 commit。
- 当前 bundle 固定为 config-only，导入结果不能被解释为已持有 CapabilityGrant 或目标平台 credential。
- local Unix、源 credential ref、Cloud token、runtime winner、session、grant body 和 UI state 都不能进入 bundle。
- deterministic protobuf、unknown-field rejection、大小限制、签名和过期检查必须在导入前完成。
- `client/runtime.PairingService` 统一校验 attempt、Endpoint pin、实际 DTLS fingerprint binding 和 PairingTicket handshake；Route connector 只建立 peer，并保证成功或失败后 exact-close。

## 8. 失败语义

- schema/version 不支持：`unsupported_version`。
- identity pin 冲突：`identity_conflict`。
- 相同 RouteID 类型冲突：`route_conflict`。
- 本地 credential 缺失：`credential_required`。
- Route connector 尚未交付或当前平台不支持：`route_unavailable`。
- Cloud 缺失、未登录或无订阅只能过滤 managed Route，不得删除或阻断 Direct/SSH。
- 禁止用旧 parser、旧 proxy、local fallback、Cloud fallback 或静默字段修正掩盖错误。

## 9. 验证基线

RTC001 至少证明：

- generated Go/TypeScript 与 Proto 同步。
- public descriptor baseline 经显式 schema review 更新。
- Route `oneof`、字段号和 version 由 contract test 固定。
- deterministic round-trip、unknown-field rejection 和 portable validation 通过。
- registry parser/encode、assembler、planner fixtures 使用新 Route 语义。
- 全仓扫描不存在旧 Route enum/name。

真实 connector 和 Android APK 用户链路的完成证据只记录在 `workflow.md` 对应切片；本文不重复维护阶段状态。
