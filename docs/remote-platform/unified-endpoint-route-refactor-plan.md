# 统一 Endpoint、多 Route 与 Transport 竞速重构方案

状态：用户审核草案，已补充私有 TermX Cloud 专项审计、App/二维码统一、本地发现与客户端 `share` 设计，未进入实现

日期：2026-07-15

## 1. 背景

当前产品原则已经明确：用户管理的是 daemon endpoint，不是“SSH 机器”“Cloud 机器”或“直连机器”三套资源；local、SSH、直连 IP 和 managed WebRTC 只是到达同一个 daemon 的不同方式。

当前实现尚未完整落实这个原则：

- `shared/connection.Config` 把一个 endpoint 和一个 transport 绑定在同一个对象里。
- TUI `EndpointManager` 按 endpoint 配置中的唯一 transport 创建唯一 bundle。
- Cloud target device、daemon fingerprint、grant、SSH 参数和 local socket 被放在同一层 dial identity 中。
- Official App 同时存在共享 TypeScript connection orchestrator、TypeScript session manager 和 Android Kotlin managed Cloud `ConnectionStore` 等多套连接状态机。
- 当前 termx protocol Hello 不返回可验证 daemon identity，因此客户端无法安全确认 SSH、指定 IP 和 managed Cloud 是否到达同一个 daemon。

最终需要把“目标身份”“可用连接方式”“一次运行时连接”和“WebRTC 内部网络路径”拆成独立领域概念。

## 2. 重构目标

1. 一个客户端只保存一个逻辑 daemon endpoint，但该 endpoint 可以同时配置多条 route。
2. 同一个 daemon 可以同时开放 local、SSH、指定 IP 和 managed Cloud ingress。
3. TUI 与 App 使用同一套 endpoint、route、竞速、错误和运行时状态语义。
4. 未配置 route 优先级时，所有可用 route 默认并发竞速。
5. 配置 route 优先级时，按优先级分组并采用有界 staggered race。
6. 首个完成身份验证、授权和 protocol Hello 的 route 成为唯一活动 protocol session。
7. transport 切换不改变 `EndpointID`、`TerminalRef`、terminal lifecycle 或 history truth。
8. Cloud Companion 只作为 managed WebRTC route provider，不拥有 endpoint registry、跨 transport 选择或 terminal protocol session。
9. local、SSH 和指定 IP 不依赖账号、订阅、Hub 或 Relay；Cloud 故障只降低 managed route 可用性。
10. Official App 启动后先从本地 registry 恢复所有 endpoint；未登录 Cloud 或 Cloud 离线时，本地直连和 SSH endpoint 仍可发现、展示和连接。
11. 扫码、Cloud directory、LAN discovery 和手工配置统一进入同一个 `EndpointAssembler`，按已验证的 daemon fingerprint 合并，而不是形成互不相认的机器列表。
12. 同一 daemon 后续新增、删除或重新导入 route 时，只更新该 endpoint 的 route 集合，不复制 terminal、history、授权或用户展示状态。
13. TUI/CLI 可以通过显式 `termx endpoint share` 把已整理好的 portable route 配置和 selection policy 迁移到 App，避免在手机上重复录入 SSH/direct 参数。

## 3. 非目标

- 不把 route 竞速扩展成通用插件系统。
- 第一阶段不长期保留多个已授权 protocol session 作为热备。
- 第一阶段不实现无中断 live reroute；route 变化通过关闭旧 session、重连和重新 attach 完成。
- 不让 Cloud 服务读取 CapabilityGrant、terminal metadata、history、文件信息或 terminal payload。
- 不使用 hostname、IP、endpoint label、SSH alias 或 Hub device id 作为 daemon 安全身份。
- 不为指定 IP 连接新增用户名密码或长期共享 token 授权体系。
- daemon bootstrap 二维码不携带 SSH 密码、SSH 私钥、Cloud access/refresh token、Hub/Relay 地址或用户配置的 route priority；客户端显式 `share` 的本地策略迁移使用第 5.11 节独立 contract。
- 不让 Cloud 登录状态决定本地 endpoint 是否存在，也不把 `local/cloud/local_cloud` 保留为持久化机器类型。

## 4. 术语与领域模型

### 4.1 Endpoint

`Endpoint` 表达客户端要访问的一个逻辑 daemon。

```text
Endpoint
  EndpointID          客户端本地稳定引用
  Label               纯展示名称
  DaemonIdentity      DeviceID + pinned DeviceFingerprint
  ConnectMode         auto | on_demand | manual
  SelectionPolicy     route 竞速策略
  Routes              一组 AccessRoute
```

约束：

- `EndpointID` 只用于客户端持久化、`TerminalRef`、路由和 UI，不是安全身份。
- `DeviceFingerprint` 是跨 transport 合并同一 daemon 的安全锚点。
- `DeviceID` 是 daemon 稳定目录/发现 ID，但不能替代 fingerprint 校验。
- route 返回的 daemon identity 与 endpoint pin 不一致时必须失败，禁止自动覆盖 pin。

### 4.2 AccessRoute

`AccessRoute` 表达一种到达 endpoint 的配置。

首期 route kind：

- `local-unix`：本机 Unix socket。
- `ssh-stdio`：OpenSSH 登录远端后运行固定 `termx daemon stdio-proxy`。
- `direct-tls`：用户指定 IP/DNS + TLS 1.3 + daemon capability authentication。
- `managed-webrtc`：Cloud Companion + Hub signaling + WebRTC DataChannel。

每条 route 有独立的：

- `RouteID`。
- transport-specific address/config。
- `AuthRef`。
- enabled/manual-only 状态。
- 可选 priority。
- dial phase、错误、最近成功时间和观测路径。

route 不拥有 terminal lifecycle、history、workbench 或 endpoint identity。

### 4.3 Transport

`Transport` 是某次 route attempt 建立的运行时载体：

- Unix frame transport。
- SSH stdio frame transport。
- TLS/TCP frame transport。
- WebRTC DataChannel frame transport。

transport 建立不等于 endpoint 已连接。只有完成 daemon identity、authorization 和 termx protocol Hello 后，才能形成 `ReadySession`。

### 4.4 Path

`Path` 只描述某个 transport 内部实际经过的网络路径。

首期只有 `managed-webrtc` 使用：

- `direct`。
- `single_relay`。
- 后续显式阶段才可能有 `relay_mesh`。

SSH、local Unix 和 direct TLS 不使用 `direct/single_relay` 伪装自己的 route kind。

### 4.5 EndpointSession

`EndpointSession` 是 endpoint 当前唯一活动的已认证 termx protocol bundle。

```text
EndpointSession
  EndpointID
  RouteID
  SessionGeneration
  VerifiedDaemonIdentity
  AuthorizationSummary
  ProtocolBundle
  ObservedPath
  Lifecycle
```

同一客户端可以在竞速期间并发持有多个未决 transport，但第一阶段每个 endpoint 最终只保留一个活动 protocol session。

## 5. 建议配置 Schema

### 5.1 Endpoint registry

桌面 `connections.yaml` 建议升级为破坏性的 v2 schema：

```yaml
version: 2
default: studio

endpoints:
  studio:
    label: Studio
    device_id: device-xxx
    device_fingerprint: SHA256:xxx
    connect_mode: on_demand
    selection:
      hedge_delay: 300ms
    routes:
      lan:
        kind: direct-tls
        priority: 10
        address: 192.168.1.20:41120
        auth_ref: grant:studio

      ssh:
        kind: ssh-stdio
        priority: 20
        address: studio-host
        auth_ref: ssh:studio
        remote_socket: auto

      cloud:
        kind: managed-webrtc
        priority: 30
        target_device_id: device-xxx
        auth_ref: grant:studio
        relay_mode: auto
```

移动端不要求使用 YAML，但 native endpoint store 必须表达同一 versioned domain schema，并通过共享 contract fixture 验证。

### 5.2 扫码、Cloud 和手工配置只是 Endpoint 获取来源

客户端最终只应有一份 `SavedEndpointRegistry`。扫码、Cloud 目录、手工 direct/SSH 配置和后续局域网发现只是向 registry 或临时 discovery projection 提供候选信息：

```text
SavedEndpointRegistry
CloudDiscoveryProjection
BootstrapImportResult
ManualRouteDraft
        |
        v
EndpointAssembler
        |
        v
一个 DaemonIdentity 对应一个 Endpoint，Endpoint 下包含多条 Route
```

约束：

- `source=cloud/local/manual` 只能作为 provenance，不能成为 endpoint kind。
- `local/cloud/local_cloud` 不能作为持久连接真值；UI 应从当前 route 集合推导可用能力。
- Cloud 目录可以展示尚未保存的 discovery candidate，但只有用户扫码、手工添加、完成配对或显式固定后才进入本地 registry。
- Cloud logout 只让 managed route 失去账号准入，不删除已保存 Endpoint、CapabilityGrant、direct route 或 SSH route。
- App 离线启动时先读取本地 Endpoint registry；不能因为 Cloud 目录刷新失败而隐藏非 Cloud endpoint。

### 5.3 当前两套二维码协议为什么不能继续共存

当前实现实际存在两条不兼容路径：

- `shared/remoteauth.PairingBundle v1` 只包含 `DeviceID + DeviceFingerprint + CapabilityGrant`，不包含任何 route；CLI 和 Android importer 导入后却直接把它解释成 `hub-p2p/managed Cloud` endpoint。该 grant 还是未绑定客户端密钥的 bearer secret，二维码被复制后权限也被复制。
- `clients/ui/src/state/pairingPayload.ts` 的 schema v4 包含本地 Hub URL 和一次性 pairing session secret，但没有新的 DeviceIdentity/CapabilityGrant 真值，并继续依赖旧 Hub claim/session-token 路径。
- Cloud `ManagedDevice` 目录只返回 DeviceID 和展示 metadata，不返回 fingerprint；共享 UI 因此只能按裸 machine ID 把 Cloud 与本地记录拼接。

这三点共同造成了用户看到的割裂：扫码不是在给同一 daemon 增加访问方式，而是在进入完全不同的连接系统。

迁移时不应给两套旧 payload 再增加桥接或 fallback。当前仍处于私有开发阶段，应由一个破坏性的 bootstrap v2 contract 替换，并删除：

- `PairingBundle v1` 的“导入即 Cloud endpoint”语义。
- schema v4 local Hub URL/session secret parser。
- `/api/v1/pairing/claims` 及旧 session token 持久化路径。
- `MachineAccessClass` 与 `local_cloud` 合并分类。

### 5.4 EndpointBootstrapBundle v2

二维码、owner-only 文件和近端文本导入使用同一个 daemon-signed bundle。建议使用 deterministic protobuf，再以 base64url 放入 `termx://bootstrap?payload=...`，避免继续维护多套手写 JSON parser。

```text
EndpointBootstrapBundleV2
  SchemaVersion
  BundleID
  EndpointIdentity
    DeviceID
    DevicePublicKey
    DeviceFingerprint
    SuggestedLabel
  Routes[]
  AuthorizationBootstrap?
    PairingTicket?          默认；一次性、短时、仅用于换取客户端绑定授权
    BoundGrant?             仅用于双向离线扫码的响应包
  IssuedAt
  ExpiresAt
  BundleSignature          DeviceIdentity 对全部字段的签名
```

`PairingTicket` 至少包含 `TicketID + ScopeCeiling + ExpiresAt + Nonce + MaxRedemptions=1`，并由 daemon DeviceIdentity 签名。它只能打开受限的 pairing handshake，不能直接访问 terminal、history 或 file protocol。

客户端为每个 Endpoint 生成独立的 `ClientAccessIdentity`，private key 只进入平台 secure store。不要直接复用 Cloud `ClientInstallationIdentity`，否则 Cloud 账号身份和多个 daemon 的端到端授权会共享可关联公钥。

默认配对流程：

1. App 本地验证 bundle、daemon identity、route hint 和 ticket 签名，不把 ticket 发给 Cloud。
2. App 经任一可达 route 建立只到 daemon 的安全 channel，并证明新生成的 `ClientAccessIdentity` private key possession。
3. daemon 原子验证 ticket 未过期、未使用且 scope 未超限，把 `TicketID` 标记为 consumed，并签发带 `SubjectKeyFingerprint` 的 `CapabilityGrant v2`。
4. App 把 grant 与 client key ref 原子写入 secure store，registry 只保存 credential ref。
5. 后续 direct TLS/managed WebRTC session 必须同时提交 grant 和 client-key channel-bound signature；复制 grant 文本本身不能建立 session。

这里的“离线配对”指不依赖 TermX Cloud，只要手机能通过 LAN direct、SSH 或已有 managed DataChannel 到达 daemon 即可。若要求双方完全无网络，使用双向二维码：App 先展示 `ClientAccessIdentity` public key，daemon 再生成只绑定该 key 的 `BoundGrant` 响应包；禁止退回长期 bearer grant 单码导入。

首期 portable route hint：

```text
DirectTLSRouteHint
  RouteID
  Addresses[]              IP/DNS + port
  ServerName?              仅作 TLS routing，不是信任锚点

SSHStdioRouteHint
  RouteID
  Host
  Port
  UserHint?
  HostKeyFingerprints[]
  RemoteSocket

ManagedWebRTCRouteHint
  RouteID
  TargetDeviceID
  AccountProfileHint?      只帮助选择本机账号 profile
```

bundle 规则：

- DeviceIdentity 对 canonical bundle 签名；导入端验证 public key、fingerprint、DeviceID、bundle signature，以及 PairingTicket/BoundGrant issuer 全部一致。
- direct 地址、SSH host 和 Cloud target 都不是 daemon 信任锚点；最终连接仍必须完成第 8 节 identity proof。
- bundle 不携带 route priority。优先级是客户端本地策略，扫码不得替用户改变竞速顺序。
- bundle 不携带 Hub URL、Relay URL、edge token、refresh secret、Cloud account session、SSH password、SSH private key 或 Android/Desktop 本地 credential ref。
- portable bundle 不包含 `local-unix` route，因为另一台设备无法使用 daemon 主机上的 Unix socket。
- 默认二维码只携带短时一次性 `PairingTicket`，不携带长期 bearer grant；ticket 即使进入临时 secure store，也必须在成功兑换或过期后删除。
- 双向离线响应包可以携带已绑定指定 `ClientAccessIdentity` 的 grant；导入端必须先证明自己持有对应 private key。
- 同一个 bundle 可以同时带 direct、SSH 和 managed route，也可以只带 identity + ticket，用于给已由 Cloud 发现的 endpoint 补授权。
- bundle 中未出现某条 route 不表示删除；二维码是增量 bootstrap，不是客户端 registry 的全量覆盖真值。

### 5.5 Endpoint 合并算法

`EndpointAssembler` 使用经过验证的 daemon identity 合并，不能使用 label、hostname、IP、SSH alias、Cloud account 或 Hub URL。

建议顺序：

1. 完整验证 bootstrap bundle 或 Cloud signed directory projection，任何 secret 尚未写入。
2. 以 `DeviceFingerprint` 查询本地 identity index。
3. 已存在相同 fingerprint 时复用原 `EndpointID`，并要求 DeviceID 一致。
4. 相同 DeviceID 但 fingerprint 不同，或相同 fingerprint 携带不同 DeviceID，进入 `identity_conflict`，禁止自动合并或覆盖 pin。
5. 没有现存 Endpoint 时创建新的客户端本地 `EndpointID`；suggested label 只用于初始展示，之后不覆盖用户改名。
6. 按 `RouteID + RouteKind` upsert route。相同 RouteID 改成另一种 kind 属于冲突；地址变化可以作为同 route 的已签名配置更新。
7. 导入 ticket 时先验证 scope ceiling/expiry；兑换成功后原子写 `ClientAccessIdentity` key ref、绑定 grant 和 registry credential ref。新 grant 扩大现有 scope 时必须让用户明确确认，不能静默提权。
8. daemon bootstrap、Cloud discovery 和 LAN discovery 不修改已有 route priority、manual override、connect mode 和用户禁用状态。只有用户显式确认的客户端 `share` 导入可以更新本地 selection policy。

导入顺序必须满足交换律：

```text
先登录 Cloud，再扫二维码
==
先扫二维码，再登录 Cloud
==
先手工加 SSH，再补扫身份/授权
```

最终结果都应是同一个 Endpoint，具有相同 identity、route 集合和 credential refs。

### 5.6 Route 与授权分离

客户端绑定的 CapabilityGrant 是 transport-neutral daemon capability，可以由同一 Endpoint 的 `direct-tls` 和 `managed-webrtc` route 共同引用；route 地址变化不应重新签发授权。授权要求 `grant + ClientAccessIdentity proof`，不再把“拿到 bearer 文本”视为客户端身份。

SSH 不同：

- SSH server host key、用户名和 client credential 属于 `ssh-stdio` route。
- SSH private key/password 只进入手机或桌面的本地 secure credential store，二维码不得携带。
- 扫描带 SSH hint 的 bundle 后，如果本机没有可用 `SSHCredentialRef`，该 route 保存为 `credential_required`，不参与自动竞速。
- 用户手工添加 SSH 时先创建 `ManualRouteDraft`。在 SSH host key 和 daemon DeviceIdentity 都验证完成、用户接受 pin 后，才提交或合并到正式 Endpoint。
- 为“一次扫码即可连接”的本地体验，应优先让 daemon QR 包含 `direct-tls` 地址和一次性 `PairingTicket`；SSH 是需要已有密钥或额外凭据步骤的高级 route。

### 5.7 Cloud 目录如何与本地 Endpoint 合并

Hub 已持有 daemon public key 投影，因此 `ManagedDevice` 必须增加 `device_fingerprint`；同账号目录返回 fingerprint 不会扩大 terminal capability，只让客户端安全识别同一个 daemon。

Cloud 同步规则：

- Cloud directory 为同一 fingerprint 增加或刷新一条 `managed-webrtc` route，不创建第二个 machine。
- Cloud directory 不提供 PairingTicket 或 CapabilityGrant。没有本地绑定 grant 时 endpoint 可以展示为 `authorization_required`，但不能开始 terminal protocol。
- Cloud route 只保存 `TargetDeviceID + account profile ref`；Hub assignment 和 ICE/Relay 信息继续在每次 managed attempt 中短期 resolve。
- Cloud 设备 revoke/logout 只移除或禁用 managed route。daemon-local CapabilityGrant 是否仍有效，继续由 daemon 本地 revoke truth 决定。
- 仅由 Cloud 临时发现且用户从未保存/配对的 candidate 可以随 logout 消失；已经保存或含其他 route 的 Endpoint 必须保留。

### 5.8 Official App 目标交互

首页只显示 daemon Endpoint，不再显示“Cloud 机器”和“本地机器”两套列表。

每个 Endpoint 可展示：

- 当前是否已授权。
- 可用 route 图标/状态：LAN direct、SSH、Cloud。
- 当前连接实际使用的 route；managed route 再展示 direct/single-relay path。
- route race 期间正在尝试的方式。

新增入口统一为：

- 扫描 daemon QR：创建 Endpoint 或向已有 Endpoint 增加 route/授权。
- 从 TUI/CLI 接收 share：导入已经整理好的 route、SSH 参数和可选 selection policy。
- 手工添加 direct route：先验证 daemon identity，再提交。
- 手工添加 SSH route：配置 host、host-key pin 和本地 credential ref。
- 登录 TermX Cloud：增加账号 discovery overlay 和 managed route，不改变已有本地配置。

扫描已有 Cloud endpoint 的 QR 时，交互应表达“为这个 daemon 增加 LAN/SSH route 和授权”，不能再创建第二张机器卡片。扫描到相同 DeviceID 但 fingerprint 不同必须显示安全冲突，不能让用户通过普通确认按钮覆盖。

### 5.9 一个扫码入口，明确 intent 与安全域

App 可以只展示一个扫码入口，但扫码 payload 必须先按显式 scheme/version/intent 分发，禁止继续采用“先尝试 Cloud parser，失败后再尝试 local parser”的猜测式解析：

```text
termx://endpoint/bootstrap?...   daemon identity、route、pairing ticket
termx://endpoint/share?...       TUI/CLI one-time share session offer
termx://cloud/activate?...       Cloud account/device activation
```

不同 intent 的结果不同：

- endpoint bootstrap 只写本地 Endpoint registry、secure credential store 和 pairing exchange，不创建 Cloud account session。
- endpoint share 只连接用户明确启动的一次性客户端迁移 session；route/policy 在加密 channel 内接收，不能把 share offer 当成 daemon 签名。
- Cloud activation 只建立本机 account profile/session，再由 directory overlay 提供 managed route；activation payload 本身不创建 daemon Endpoint，也不携带 CapabilityGrant。
- scanner 在解析后必须先显示明确动作“添加/授权 daemon”“接收 TUI 配置”或“登录 TermX Cloud”，不能把不同授权合并成一个确认按钮。
- 未知 scheme/version/intent 直接拒绝，不做 legacy fallback；二维码内容不得上传给 Cloud 或第三方扫码服务解析。

因此统一的是入口、Endpoint 投影和后续连接体验，不是把 Cloud account credential 与 daemon capability 混成一种 token。

### 5.10 本地发现与 DHCP 地址变化

二维码里的 IP/DNS 只是 direct route 的 seed，不是长期网络真值。手机和 daemon 可能换 Wi-Fi、IPv4 地址、IPv6 temporary address 或端口，因此 App/TUI 需要独立的 `LocalDiscoveryProvider`：

```text
LocalDiscoveryCandidate
  ClaimedDeviceID
  ClaimedFingerprint
  Address
  Port
  ProtocolVersion
  AnnouncementExpiry
  AnnouncementSignature?
```

规则：

- 优先使用平台 mDNS/Bonjour primitive 发布 TermX service；announcement 可以由 DeviceIdentity 签名，但无论是否签名，地址最终都必须通过 TLS + daemon identity handshake 验证。
- 已保存 endpoint 只接受 fingerprint 与 pin 一致的 discovery candidate；mDNS label、来源 IP 和 DeviceID 单独出现都不能触发合并或换 pin。
- 未配对 daemon 可以显示为短期“附近设备”candidate，但在 PairingTicket、双向扫码或其他明确授权完成前不能访问 terminal。
- discovery 地址只保存在内存 TTL cache，参与对应 `direct-tls` route 的 address race；不要为每次广播、IP 抖动或网络切换写 endpoint registry。
- 可以低频保存最近成功地址作为下次冷启动 seed，但写入要 debounce/桶化；失败 seed 不删除 endpoint，也不阻止重新 discovery。
- 切换 Wi-Fi、VPN 或蜂窝网络时只使旧 candidate 过期并重新规划 route，不改变 EndpointID、授权或用户 priority。

这样“本地连接”不再是一张独立机器记录，而是同一 direct route 的动态地址来源；Cloud directory、二维码 seed 和 LAN discovery 最终都汇入同一个 EndpointSessionManager。

### 5.11 `termx endpoint share`：TUI/CLI 到 App 的配置迁移

daemon 生成的 `EndpointBootstrapBundle` 与客户端生成的 share 不是同一种真值：

- bootstrap 由 daemon DeviceIdentity 签名，只表达 daemon identity、portable ingress hint 和 pairing authorization，不拥有客户端 priority。
- share 由用户在已经配置好的 TUI/CLI 中显式发起，表达“把我的客户端 route 配置和本地策略迁移到另一台客户端”。

建议 canonical 命令：

```text
termx endpoint share <endpoint>
termx endpoint share <endpoint> --routes ssh,direct
termx endpoint share <endpoint> --without-policy
termx endpoint share <endpoint> --config-only
```

TUI endpoint action 调用同一个 share service，不再维护第二套导出格式。默认分享所有 portable route 和 selection policy；用户可以在预览中取消某条 route、priority 或授权。

share payload 使用独立的 versioned contract：

```text
ClientEndpointShareBundleV1
  TransferID
  DaemonIdentity
    DeviceID
    DeviceFingerprint
  SuggestedLabel?
  Routes[]
  SelectionPolicy?
  AuthorizationResult?
  CredentialDescriptors[]
  IssuedAt
  ExpiresAt
```

可迁移内容：

- `direct-tls`：seed addresses、port、server name 和 route enabled/manual-only 配置。
- `ssh-stdio`：host、port、user、ProxyJump、remote socket、host-key pins、auth method hint 和 priority。
- `managed-webrtc`：TargetDeviceID 与本机 account profile hint；App 仍需在自己的 Cloud session 中匹配账号。
- endpoint label、connect mode、route priority/disabled/manual-only 等客户端本地策略；导入已有 Endpoint 时必须先展示 diff，由用户选择是否覆盖。

不迁移的内容：

- `local-unix` route、当前 winner、最近错误、延迟统计、临时 LAN candidate 和 runtime session。
- 源客户端 `EndpointID` 与 credential ref；目标 App 复用自己已有 EndpointID，或生成新的本地 ID。
- Cloud access/refresh token、HubDirectory、Hub/Relay 地址、RelayLease 和 browser Cookie。Cloud credential 绑定 App installation，必须在 App 自己登录。
- 已有 CapabilityGrant。grant 已绑定源客户端 `ClientAccessIdentity`，复制后既不应可用，也不得降级为 bearer grant。

#### 一次扫码的本地 share session

`termx endpoint share` 默认启动一个短期、单次消费的 LAN TLS share listener，并显示 `termx://endpoint/share` QR。二维码只包含 `TransferID + listener addresses + ephemeral TLS certificate pin + one-time session secret + expiry`，不包含 endpoint 配置或 SSH credential body。

App 扫码后的流程：

1. App 验证 scheme/version/expiry，连接 share listener 并 pin 二维码中的 ephemeral TLS certificate。
2. App 发送自己的 `ClientAccessIdentity` public key 和 possession proof；TUI 显示 App 名称与短 fingerprint，用户确认接收方。
3. TUI 通过已认证 daemon session 请求一份绑定该 App key 的新 grant；只有 local owner 或显式 `ManageClientAccess` capability 可以签发，普通 terminal/file grant 不能转授权。
4. TUI 在 TLS channel 中发送 share bundle；App 展示 endpoint、route、policy 和凭据差异后再提交。
5. listener、session secret 和临时 key 立即销毁；重放、第二个接收方或过期连接全部拒绝。

若 daemon 当前不可达或发起方没有 `ManageClientAccess`，share 仍可用 `--config-only` 迁移 route/policy，但 App 将 endpoint 标记为 `authorization_required`，后续通过 daemon QR 或明确配对补授权。

#### SSH credential 的可选转移

默认 share 只迁移 SSH 连接参数，App 导入后选择本地 key、agent 或输入密码。这样已经消除了手机上最繁琐的 host、port、user、ProxyJump、socket、host-key pin 和 priority 编辑。

后续可以增加显式 `--include-ssh-credential`，但必须满足：

- 只允许在上述已确认接收方的实时 TLS share session 中传递，静态 QR、日志、shell argv 和普通导出文件都禁止携带 credential body。
- 每个 password/exportable private key 单独展示风险并确认；App 收到后立即写入 Keystore/secure store，再清理内存 buffer。
- agent、hardware-backed key、不可导出 Keystore key 和系统 credential ref 永不导出，只发送 `credential_required` descriptor。
- Cloud token 即使在加密 share session 中也禁止转移；它必须绑定目标 App 自己的 installation identity。

App 仍保留手工新增和编辑 route 的能力，但它是补充入口。主路径应是 TUI/CLI 整理配置后执行一次 share，App 扫码、审阅并导入。

## 6. Route 选择与竞速

### 6.1 Eligible route

开始连接前，`RouteSelectionPlanner` 先过滤：

- route 是否 enabled。
- 当前平台是否支持该 route kind。
- route 配置是否完整。
- 必要 credential reference 是否存在。
- 当前连接 intent 是否允许使用该 route。
- 用户是否显式锁定某条 route。

过滤只产生连接计划，不做网络 IO。

### 6.2 显式 route

用户通过 CLI、TUI 或 App 显式选择 route 时，只连接该 route：

```text
termx endpoint connect studio --route ssh
```

显式 route override 在本次 session 内保持 sticky。重连只使用该 route，直到用户解除 override 或重新执行自动连接。

### 6.3 无 priority：全量竞速

当 endpoint 的全部 eligible route 都没有配置 priority 时：

- 所有 route 在 `t=0` 同时启动。
- 首个产生 `ReadySession` 的 route 胜出。
- 其他 attempt 立即取消和清理。

不能因为某条 route 通常更便宜或更接近，就在没有用户配置时隐式改变默认竞速语义。

### 6.4 有 priority：分组 staggered race

只要任一 enabled route 配置 priority，就要求该 endpoint 下所有自动 eligible route 都配置 priority；混合配置直接报错。

规则：

- 数字越小优先级越高。
- 相同 priority 的 route 同时竞速。
- 下一 priority 组在前一组尚未产生 `ReadySession` 且经过 `hedge_delay` 后启动。
- 不要求前一组完整失败或超时，避免高优先级 route 卡住全部连接。
- 如果 `hedge_delay = 0`，所有组同时启动，但同一完成时刻使用 priority 作为稳定 tie-breaker。

示例：

```text
t=0ms     direct-tls(priority=10)
t=300ms   ssh(priority=20)
t=600ms   managed-webrtc(priority=30)
```

### 6.5 胜出边界

route 只有完成以下全部阶段才能胜出：

```text
transport connected
-> daemon identity verified
-> authorization accepted
-> termx protocol Hello succeeded
-> authorization scope 满足 ConnectIntent
-> ReadySession
```

以下状态都不能提前胜出：

- TCP socket connected。
- SSH 子进程已启动。
- TLS handshake completed。
- WebRTC ICE connected。
- DataChannel open。
- Hub signaling answer 已返回。

### 6.6 竞速取消与资源回收

每轮竞速生成唯一 `AttemptID` 和 `SessionGeneration`。

首个 `ReadySession` 通过单次 compare-and-swap 成为 winner，随后：

- cancel 所有 loser context。
- 关闭 loser transport。
- 结束 SSH 子进程和远端 stdio proxy。
- 关闭 loser WebRTC PeerConnection/DataChannel。
- 关闭未使用的 managed signaling session。
- 释放未使用 Relay reservation/lease 或明确报告 cancelled outcome。
- 禁止 loser 创建 protocol attachment、history token 或长期 event subscription。

route dialer 必须完整支持 context cancellation，不能只依赖上层丢弃返回值。

### 6.7 重连

- 自动连接产生的 session 断开后，重新执行完整 eligible route 竞速。
- 用户显式 route override 产生的 session 断开后，只重试该 route。
- route identity mismatch 进入 quarantine，不继续参与自动竞速，直到配置或安全 pin 被用户明确修复。
- auth expired、host key mismatch、grant revoked 等非网络错误进入 route-scoped fail-closed 状态，不进行无界重试。

## 7. 两层选路关系

需要严格区分两个不同层级：

```text
外层 Endpoint route race
  local-unix vs ssh-stdio vs direct-tls vs managed-webrtc

内层 managed-webrtc path selection
  WebRTC direct candidate vs single Relay candidate
```

外层由客户端 `EndpointSessionManager` 统一拥有。

内层由公开 managed WebRTC adapter 根据 Companion 提供的服务准入和 route policy 执行；Cloud Companion 不知道 SSH、local 或 direct TLS 是否存在，也不能替外层选择 transport。

## 8. Daemon Identity 与授权

### 8.1 Identity

daemon `DeviceIdentity` 必须从 Cloud 专用概念提升为 daemon 全局稳定身份：

- daemon 启动时加载一次。
- local、SSH、direct TLS 和 managed WebRTC 使用同一 public identity。
- Cloud enrollment 只登记该 identity，不创建第二份 identity。
- fingerprint 变化属于安全事件，不能通过重新 resolve、修改 label 或连接成功自动接受。

protocol/transport 需要增加可验证 daemon identity challenge，使 SSH 和 direct IP route 能证明自己与 Cloud route 到达同一 daemon。

### 8.2 Route authorization

不同 route 可以使用不同授权来源：

- `local-unix`：当前用户本地 socket 权限。
- `ssh-stdio`：OpenSSH host key + 用户认证 + 远端 socket 权限。
- `direct-tls`：DeviceIdentity + client-bound CapabilityGrant + ClientAccessIdentity proof。
- `managed-webrtc`：DTLS-bound DeviceIdentity + client-bound CapabilityGrant + ClientAccessIdentity proof。

Authorization scope 是 session 属性，不是 endpoint 展示属性。竞速 winner 必须满足当前 `ConnectIntent` 所要求的 capability。

`ManageClientAccess` 是单独的高权限 capability，只允许签发、列出或撤销其他客户端的绑定 grant；它不能由 `AllowDaemon`、terminal scope 或 file scope 隐式推出。local owner 可以通过 OS ownership 获得该管理边界，远程客户端必须被显式授予。

## 9. 指定 IP + 鉴权

指定 IP 建议使用 `direct-tls`，不新增用户名密码或长期 API token：

1. daemon 显式启用 TLS 1.3 listener，默认关闭公网监听。
2. TLS certificate 可以短期轮换，但 daemon 必须在认证握手中签名当前 TLS certificate fingerprint。
3. client 先验证 pinned DeviceFingerprint 和实际 TLS peer certificate binding。
4. identity 通过后，client 才提交 client-bound CapabilityGrant 和 `ClientAccessIdentity` channel-bound signature。
5. daemon 验证 grant issuer/subject、client key proof、expiry、revoke 和 scope 后调用 core-v2 `ServeScopedTransport`。

该模型与 managed WebRTC 的 DTLS DataChannel 授权语义一致，区别只在 transport/channel binding。

如果用户需要用户名密码、SSH key、agent 或 ProxyJump，应使用 SSH route，而不是给 direct TLS 再造一套账号系统。

## 10. 客户端运行时

### 10.1 公共 planner

引入纯领域 `RouteSelectionPlanner`：

- 输入 endpoint routes、priority、platform capability、manual override 和 `ConnectIntent`。
- 输出 race groups、start delay 和稳定 tie-breaker。
- 不做网络 IO，不读取 secure store，不修改 UI state。

Go TUI、CLI 与 Android native 使用同一组机器可读 fixture 验证规划结果。

### 10.2 TUI

TUI `EndpointManager` 应拆成：

- endpoint registry projection。
- route selection/session owner。
- endpoint-aware service router。

service router 继续按 `EndpointID` 路由 terminal/history/input/file；它不应该知道 route priority、Cloud account 或 SSH 参数。

TUI reducer 投影：

- endpoint aggregate state。
- 每条 route 的 phase/error。
- 当前 active route。
- managed route 的 observed path。
- race 中正在参与的 route。

### 10.3 App

Official App native 层拥有：

- `SavedEndpointRegistry`、daemon fingerprint 索引和 endpoint/route store。
- `EndpointBootstrapBundle` 的本地解析、签名验证与 `EndpointAssembler` 合并事务。
- `ShareSessionOffer` 的 TLS pin、receiver identity proof、share bundle 校验和安全凭据落盘。
- credential reference resolution。
- route dialer。
- route race/session lifecycle。
- Android foreground/background/network recovery。

共享 TypeScript UI 只负责：

- 消费脱敏后的 endpoint、route、授权状态和 runtime projection；不得接触原始 CapabilityGrant、SSH 私钥或密码。
- endpoint 和 route 展示，不再按 `local/cloud/local_cloud` 拆机器列表。
- 用户发起 connect/reconnect/select route。
- 用户发起扫码、接收 TUI share、手工 direct/SSH route 新建和凭据补全；原生层完成验证与持久化后再回传差异预览或结果。
- 消费 runtime event。
- 使用唯一活动 protocol session。

App 启动顺序固定为：

1. 先加载本地 `SavedEndpointRegistry`，立即展示可离线使用的 local/direct/SSH endpoint。
2. 再启动可选的 Cloud account session；Cloud directory 只向 `EndpointAssembler` 提交带 fingerprint 的 managed route overlay。
3. 启动 LAN discovery；网络恢复、扫码导入、手工新增、discovery candidate 和 Cloud directory 更新都走同一合并/投影边界，再触发 route eligibility/race 重算。

Android 首期必须补齐 direct TLS connector；SSH connector 应使用经过验证的 SSH client library，并由原生层管理 host-key pin 和 credential ref，不在 TypeScript 中实现 SSH 协议。

需要删除共享 UI 中拥有网络竞速真值的旧 local/hub orchestrator、`machineStore` 的来源分类合并，以及 Kotlin Cloud-only `ConnectionStore`、`ManagedPairingImporter`“扫码即 Cloud endpoint”和 TypeScript native session manager 的重复职责。

## 11. Daemon 多 Ingress 装配

同一个 daemon 进程可以并行运行：

```text
local Unix listener -----------------------> core-v2 full local scope
OpenSSH stdio proxy -> local Unix listener -> core-v2 remote OS-user scope
direct TLS listener -> remote auth --------> core-v2 capability scope
managed WebRTC answerer -> remote auth ----> core-v2 capability scope
```

所有 ingress 最终进入同一个 core-v2 server，因此看到的是同一 terminal lifecycle、history、file truth 和 daemon-local TerminalID。

## 12. Cloud Companion 初步收缩方向

Cloud Companion 只保留：

- account/device edge session 与安全刷新。
- Hub directory 和 managed device discovery。
- managed target resolve。
- signaling stream。
- Relay lease/route plan。
- 脱敏 path quality 和 connection outcome。

Cloud Companion 不应拥有：

- 客户端本地 `EndpointID` registry。
- SSH/local/direct TLS route 配置。
- 外层 route priority 或竞速。
- CapabilityGrant 或 DeviceIdentity private key。
- termx protocol client、terminal inventory 或 reconnect state machine。

后续建议从 Cloud IPC 中删除客户端本地 `EndpointID`，改用短期 `AttemptID + TargetDeviceID`；`ResolveEndpoint` 重命名为更准确的 managed target/session contract。

私有 Control Plane、Hub、Relay、Companion 的认证、离线能力、持久化恢复、写放大和故障窗口需要在本方案后续章节单独审计。

## 13. 迁移切片建议

### CONN001：模型与 contract

- 冻结 Endpoint/Route/Session/Path/ConnectIntent 模型。
- 建立 connections v2 schema。
- 定义确定性编码并由 DeviceIdentity 签名的 `EndpointBootstrapBundle v2`。
- 定义一次性 `PairingTicket`、每 Endpoint `ClientAccessIdentity` 和带 `SubjectKeyFingerprint` 的 `CapabilityGrant v2`；删除新配对继续签发 bearer-only grant 的路径。
- Cloud `ManagedDevice` projection 增加 `device_fingerprint`，建立 Cloud/QR/manual discovery 的统一 `EndpointAssembler`。
- 定义带 TTL 的 `LocalDiscoveryCandidate` fixture；动态地址不进入 endpoint identity 或高频持久化真值。
- 定义 `ClientEndpointShareBundle v1`、`ShareSessionOffer` 和跨 Go/Kotlin 的导入差异 fixture；share 与 daemon bootstrap 使用不同 intent。
- 建立 Go/Kotlin/TypeScript fixture。
- 更新现有 endpoint/transport 文档真值。

### CONN002：Daemon identity

- daemon 启动统一加载 DeviceIdentity。
- 增加跨 transport identity challenge/proof。
- 增加只允许 pairing exchange 的受限 handshake；ticket consume 与 bound grant 签发必须是 daemon-local 原子事务。
- 证明 local、SSH、direct TLS、managed WebRTC 返回同一 identity。
- SSH 手工草稿只有在 host key 与 daemon identity 都被验证后，才可归并到已有 endpoint。

### CONN003：Route planner 与 TUI/CLI session manager

- 实现 priority/staggered/default race。
- 先接入本地 registry、`EndpointAssembler` 和 route eligibility；Cloud 不在启动关键路径上。
- local/SSH 先接入新 session manager。
- 实现 `termx endpoint share <endpoint>` 与 TUI endpoint share action，共用一个短期 LAN TLS share service。
- share 请求新 App 授权时必须经过 daemon `ManageClientAccess`；源客户端已有 grant 不得直接导出。
- service router 继续保持 endpoint-aware。

### CONN004：Managed Cloud route adapter

- managed Cloud 改成普通 route dialer。
- Cloud directory 按已验证 fingerprint 向现有 endpoint 叠加 managed route，不按裸 DeviceID 或来源类型创建第二台机器。
- 外层 race 不进入 Companion。
- 清理 Cloud IPC 中客户端本地 endpoint identity。

### CONN005：Direct TLS

- 实现 TLS frame transport。
- 实现 certificate-bound DeviceIdentity 和 capability handshake。
- daemon 同时开启 local/direct/cloud ingress。

### CONN006：Official App

- Android native 建立统一 endpoint route manager、bootstrap verifier、per-endpoint `ClientAccessIdentity`、secure credential store 和 `EndpointAssembler`。
- 接入 direct TLS 与 SSH route connector；缺少 SSH credential 时保留 route 并标记 `credential_required`，不做 Cloud fallback。
- 使用平台 LAN discovery primitive，为 pinned endpoint 提供短期 direct address candidate；网络变化只刷新 candidate。
- 实现 `termx://endpoint/share` receiver、接收方 proof、导入 diff、route/policy 选择和 credential-required 状态。
- App 未登录或无法访问 Cloud 时仍从本地 registry 展示并连接扫码/手工导入的 endpoint。
- TUI/App planner contract 对齐。
- 共享 TypeScript UI 退出网络 owner 职责。

### CONN007：删除旧路径与真实验收

- 删除 local/hub URL race、旧 session token 和重复 connection store。
- 删除 `PairingBundle v1`“导入即 managed Cloud”、UI schema v4 `termx_pair`、旧 Hub pairing claim 和二维码来源分叉。
- 删除 `local/cloud/local_cloud` 作为持久连接 truth 的分类。
- 删除按裸 machine/device ID 合并的路径；所有自动合并必须基于已验证 fingerprint。
- 完成真实 SSH、direct TLS、managed direct/single Relay 竞速与切换验收。

## 14. 必要 Harness

- 一个 endpoint 配置 SSH、direct TLS、managed WebRTC，只显示一个 machine/endpoint。
- App 未登录 Cloud 时，扫描 direct TLS + PairingTicket bundle 后可落入本地 registry、直接向 daemon 兑换绑定 grant 并完成连接。
- App 冷启动且 Cloud 不可达时，已保存的 direct TLS/SSH endpoint 仍立即可见并可连接。
- daemon DHCP 地址改变后，App 通过 LAN discovery 找到同一 fingerprint 并连接；registry 中 EndpointID、grant ref、priority 和用户 label 不变。
- 伪造 mDNS label/DeviceID 或错误 fingerprint 的 announcement 不能更新 route pin，也不能进入 terminal handshake 后的 ReadySession。
- 先导入二维码再登录 Cloud，与先登录 Cloud 再导入二维码，最终得到完全相同的 endpoint identity 和 route 集合。
- 同一 daemon 分别扫描 direct、SSH 和 managed route bundle，始终只得到一个 endpoint；未配置 priority 时三条 eligible route 参加同一轮竞速。
- 二维码缺少已有 route 时不得删除该 route；二维码也不得覆盖本地 priority、manual-only、disabled 或用户 label。
- 相同 DeviceID 但不同 fingerprint、或相同 fingerprint 但不同 DeviceID，必须进入 identity conflict/quarantine，禁止静默合并。
- Cloud logout、账号撤销或 directory 暂时不可用时，只使 managed route unavailable；direct TLS、SSH 和本地授权记录保持不变。
- Cloud `ManagedDevice.device_fingerprint` 必须与 route handshake、CapabilityGrant issuer 和 endpoint pin 一致。
- 同一 PairingTicket 并发兑换只能有一个成功；daemon 在响应丢失后可幂等返回同一 bound grant，不得签发第二份权限，也不得要求 Cloud 在线。
- 截获 PairingTicket 在过期或消费后不能重放；仅截获 bound grant、但没有对应 `ClientAccessIdentity` private key 时也不能完成 capability handshake。
- SSH bundle 不含密码/私钥；缺少本地 credential ref 时 route 显示 `credential_required` 且不参与自动竞速。
- 手工 SSH route 在 host key 和 daemon identity 验证前只保存为 draft，不得依据 hostname、IP 或别名归并 endpoint。
- bootstrap parser 拒绝未签名、签名错误、过期、未知字段超限和包含禁止 secret 类型的 bundle；ticket、bound grant 和 client key 只进入原生安全边界。
- scanner 按显式 intent 分发 Cloud activation、endpoint bootstrap 与 endpoint share；模糊 payload 和通过 parser fallback 才能识别的 payload 必须拒绝。
- TUI 分享包含 SSH/direct/managed portable route 和 priority 后，App 只创建或更新同 fingerprint 的一个 Endpoint，导入结果与手机手工录入等价。
- share 导入已有 Endpoint 时必须展示 route/policy diff；未经确认不得覆盖 App 本地 priority、disabled、manual-only 或用户 label。
- `local-unix`、runtime winner/error/latency、Cloud token、Hub/Relay 地址、RelayLease、源 EndpointID 和源 credential ref 不出现在 share bundle。
- share QR 只含短期 TLS session offer；静态解析二维码不能得到 endpoint 配置、SSH credential 或 CapabilityGrant。
- 普通 terminal/file grant 无法通过 share 给 App 签发新授权；local owner 或 `ManageClientAccess` 成功时，App 获得绑定自己 key 的新 grant，而不是源 grant 副本。
- `--config-only` 在 daemon 离线时仍可迁移 route 与 policy，App 明确显示 `authorization_required` 或 `credential_required`，禁止 Cloud fallback。
- 可导出 SSH credential 只有在双方确认后的实时 TLS channel 中、逐项确认后才能传递；agent/hardware/Keystore key 始终保持不可导出。
- share session 过期、第二次消费、错误 TLS pin、错误接收方 proof 和用户拒绝接收方 fingerprint 都必须 fail closed 并清理 listener。
- 三条 route 注入不同延迟，TUI 与 App 选择相同 winner。
- 未配置 priority 时所有 eligible route 同时开始。
- 配置 priority 后按分组和 `hedge_delay` 启动。
- TCP/SSH/WebRTC 只完成底层连接但 identity/auth/protocol 未完成时不能胜出。
- loser SSH process、PeerConnection、Relay reservation 和 protocol transport 全部释放。
- route identity mismatch 不覆盖 endpoint pin，并从自动竞速中 quarantine。
- active transport 断开后自动重新竞速。
- 用户显式 route override 后只重连该 route。
- route 切换后 `TerminalRef` 不变，旧 generation 的 live/history/effect 回包全部拒绝。
- 两个客户端分别经 SSH 和 Cloud 操作同一 terminal，看到同一 core lifecycle/history truth。
- Cloud/Control Plane 中断时 local、SSH、direct TLS 不受影响。
- CapabilityGrant 撤销后 direct TLS 与 managed WebRTC 都 fail closed，SSH 仍只服从 SSH/OS 权限。

## 15. 当前审核决策

已经确认：

- 同一逻辑 endpoint 可以持有多条 route。
- 未配置 priority 时默认全量竞速。
- 配置 priority 时使用 priority-aware staggered race。
- winner 必须完成 identity、authorization 和 protocol Hello。
- TUI 与 App 使用同一选择语义。

私有 Cloud 专项审计已确认：

- 现有 credential 分层和 Cloud/DataChannel 安全边界总体正确，应保留。
- 登录、refresh 和 enrollment 都存在“先消费一次性凭据，后完成持久化和响应”的故障窗口，需要优先重构为事务化幂等交换。
- Hub policy 每 5 分钟全量重签和落盘，但有效离线窗口仍只有 30 分钟，且后台刷新失败会使整个 dev runtime 退出。
- Presence 声明了 30 秒 heartbeat，实现却没有 heartbeat contract，而是每 5 分钟重做 challenge/proof/HTTP stream，并反复更新 SQLite online 状态。
- managed route 取消能关闭 signaling，但不能立即释放已领取的 Relay 区域并发预算。
- Relay usage 在 durable outbox 落盘前已清空内存计数，落盘失败可丢用量；outbox 又对每个事件执行全文件重写和 fsync。
- 客户端每次重新登录都生成新 client device，会持续增加设备行、policy 记录和重复审计事件。
- Desktop Companion 每个 Hub 请求都读 OS credential store，且 account/device refresh 共用一把全局锁，需要改为内存会话缓存和按 session kind 的 singleflight。

详细结论和建议见第 16 节及后续章节。

## 16. 私有 TermX Cloud 专项审计

### 16.1 审计范围

本次核对了以下当前实现：

- `private/cloud/control-plane/`：设备安全目录、edge credential、policy、Relay lease 和 usage ledger。
- `private/cloud/hub/`：离线 edge authorization、Presence、signaling 和 verified snapshot store。
- `private/cloud/companion/`：OS credential session、refresh、Hub adapter 和本地 IPC connection。
- `private/cloud/devcloud/`：当前单区域 staging 装配、登录/enrollment flow、Relay 用量和 Web Controller 联动。
- `private/cloud/relay/`：RelayLease 离线验签、TURN credential、allocation 和 usage outbox。
- `private/cloud/mobile/android/`：Official Android account session、Keystore 和 refresh。
- `private/cloud/web-controller/`：浏览器账号、设备码批准、节点展示和 SQLite 写入。
- `remote/client/` 与 `remote/daemon/`：managed WebRTC dial、DataChannel auth、Presence agent 和 route 关闭边界。

### 16.2 应保留的安全边界

以下设计是正确的，重构时不应倒退：

- `EdgeAccessToken`、`RefreshSecret`、`DeviceIdentity`、`CapabilityGrant` 和 `RelayLease` 是不同 credential type，具有不同 audience 和 custody。
- Hub 用本地已验签 policy 与 edge token 取交集，cache miss 和 revoke 直接 fail closed，请求热路径不回查 Control Plane。
- daemon Presence 需要 fresh challenge 和 DeviceIdentity proof，仅有 daemon bearer token 不足以伪造 Presence。
- CapabilityGrant 只在 direct TLS 或 DTLS DataChannel 的端到端安全 channel 内由 owning daemon 验证，Hub/Relay/Control Plane 无权判断 terminal scope。
- RelayLease 绑定 account、managed session、client、daemon、region、route 和 quota，Relay 可离线验签。
- 浏览器 session 使用 HttpOnly + SameSite=Strict Cookie，变更请求还要求 same-origin 和 CSRF token。
- 已建立的 DataChannel 不经 Hub 转发 payload，Cloud 控制面故障不应主动中断已就绪 terminal session。

### 16.3 风险排序

| 优先级 | 当前实现 | 影响 |
| --- | --- | --- |
| P0 | login/refresh/enrollment 先消费一次性状态，再做设备目录、policy、session 和响应 | 中间失败或响应丢失后，用户已批准但拿不到凭据，可被迫重新登录或 enrollment |
| P0 | Relay `DrainUsage` 先清空 pending bytes，后写 durable outbox | outbox 落盘失败时用量消失；付费上线前不可接受 |
| P1 | policy 每 5 分钟无条件新增 revision、全量重签、fsync，有效窗口 30 分钟 | 写放大、同步故障使全部新 Cloud 连接在 30 分钟后失败 |
| P1 | policy 刷新失败会向 runtime error channel 报错并结束 worker | 一次后台持久化/签名故障可触发整个 dev cloud 重启 |
| P1 | Presence 每 5 分钟强制重连，却没有实际 heartbeat 协议 | 每个 daemon 每天约 288 次 challenge/proof/stream 重建，增加网络故障面和 CPU 开销 |
| P1 | Presence open/close 每次都更新 SQLite `nodes.online` | 在线瞬时态被错当持久真值，至少每 5 分钟两次写，重启还会全表置离线 |
| P1 | Relay lease 只在 5 分钟到期时释放区域并发预算 | Cloud route 在外层竞速中落败时，仍可暂时挤占后续连接的 Relay 配额 |
| P1 | 每次账号重新登录都生成新 client device ID | 设备列表膨胀、每次登录写目录/policy/SQLite/audit，且 client 仍只是 bearer token 持有者 |
| P1 | Relay usage outbox 每秒排水，Enqueue 和每个 Ack 都全文件 fsync | 持续 Relay 流量会产生高频存储写和 O(N) 队列重写 |
| P1 | Companion 每次 Hub 操作都读 OS credential store，account/device refresh 共用全局锁 | Keychain/Keystore 短暂异常会放大为所有 Cloud 请求失败，两种 session 互相阻塞 |
| P2 | CLI 的 browser/device-code 方法当前都只打印 URL；客户端每 1 秒轮询完成状态 | 方法语义不真实，故障提示弱，大量并发登录时产生无必要请求 |
| P2 | `/api/status` 固定返回 Control Plane/Hub ready | 用户和运维无法区分 Web、Control Plane、Hub、Relay 和 Companion 的真实故障 |

## 17. 认证与一次性凭据重构

### 17.1 统一 CredentialExchange 真值

login completion、refresh rotation 和 daemon enrollment completion 应共享同一个事务语义，而不是三套“删除旧记录后继续执行”的流程。

```text
CredentialExchange
  ExchangeID
  Kind                  login | refresh | daemon_enrollment
  SubjectBinding        account/device/session family
  ProofDigest
  State                 pending | approved | committed | deliverable | expired | revoked
  ResultRevision
  DeliveryReceipt       短期加密的同一结果
  DeliveryGraceUntil
```

建议事务顺序：

1. 验证 flow/refresh/proof 并锁定对应 exchange/session family。
2. 在 Control Plane 主数据库的同一事务中提交设备归属、auth epoch、refresh child hash、security revision 和 projection outbox。
3. 产生的 access/refresh response 使用短期 sealed delivery receipt 保存，只能被同一 exchange 重试读取。
4. 如果新设备需要 Hub policy 先可见，exchange 保持 `committed`，等目标 Hub 确认最低 security revision 后才进入 `deliverable`。
5. 响应丢失后，客户端使用原 exchange key 重试，服务端返回完全相同的结果，不再生成第二个 child token。
6. delivery grace 过期后删除结果密文，长期只保留 hash、family、expiry 和审计 metadata。

不应为解决响应丢失而长期允许旧 refresh secret 重放。幂等宽限必须绑定同一 session family、同一 exchange 和短期结果收据。

### 17.2 当前三个 P0 故障窗口

#### Login

`private/cloud/devcloud/control.go` 的 `handleCompleteLogin` 在开始设备注册和 session 签发前就删除已批准 flow。后续 directory、policy snapshot、Web projection、refresh store 或响应任一失败，客户端都无法继续原流程。

重构后：浏览器批准只使 exchange 进入 `approved`；设备注册、policy revision 和 session family 在事务中提交；同一 `FlowID` 可幂等取回已提交结果。

#### Refresh

`refreshStore.Rotate` 当前已删除旧 hash 并持久化新 hash，之后才检查设备状态和签发 access token。签发失败时，旧 secret 已失效，新 secret 已存在但没有交付。

重构后：旧 refresh row 锁定、设备/revoke 重验、child hash、access token claims 和 delivery receipt 在同一事务中提交。并发轮换只能观察到同一个 child result。

#### Daemon enrollment

`handleBeginEnrollment` 在 challenge 生成前就把一次性 code 标记为 claimed；`handleCompleteEnrollment` 在校验 proof 和签发 device session 前就删除 flow。

重构后：一次性 code 到 challenge 的转换必须原子提交；proof 失败可终止 exchange，服务端内部或响应故障不得把已正确证明的流程变成不可恢复状态。

### 17.3 稳定 ClientInstallationIdentity

当前 client login 每次生成随机 `client-*` ID，没有客户端公钥连续性。建议为 Desktop Companion 和 Official App 各自生成一次稳定的 `ClientInstallationIdentity`：

- private key 仅保存在 OS credential store/Android Keystore。
- `ClientDeviceID` 从 public key 或稳定 installation record 派生，重新登录不新建设备。
- login approval 页展示 display name、platform 和短 fingerprint，用户批准后 Control Plane 绑定该公钥。
- Hub 请求可增加基于 installation key 的 connection/request proof，使被窃取的 edge bearer 不能单独使用。
- logout 只删除当前 account session；“移除设备”才 revoke installation identity 和全部 refresh family。
- Web `node.enrolled` audit 只在首次建立设备归属时写入，metadata 没变化时不写 UPDATE 或重复 audit。

daemon 仍使用已有 `DeviceIdentity`；两种 identity 不应混为同一个领域类型。

### 17.4 Web 账号验证

当前 password + bcrypt + SQLite 可作为 staging 验收，不应默认当作生产账号平台。

生产建议：

- 优先接入成熟 OIDC 或 passkey/WebAuthn identity provider，不在 TermX 内继续扩展自建密码体系。
- login/register/device-code/enrollment 入口按 IP、account 和 flow family 做有界限流，但错误仍不泄漏账号、code 或设备是否存在。
- 保留 HttpOnly、SameSite=Strict、same-origin 和 CSRF 现有边界。
- 设备批准页必须明确展示请求设备和操作类型，不允许仅凭一个短码盲目批准。

### 17.5 Daemon-local 配对与授权交换

二维码配对不进入 Cloud `CredentialExchange`，也不写 Control Plane 数据库。它属于 owning daemon 的本地安全事务：

```text
PairingExchange
  TicketID
  TicketDigest
  ScopeCeiling
  SubjectKeyFingerprint?
  State                 issued | consumed | expired | revoked
  ResultGrantDigest?
  DeliveryGraceUntil
```

约束：

- 默认只持久化 ticket digest、状态、过期时间和 result digest，不保存二维码明文。
- 消费 ticket、绑定 client key、签发 grant 和写 delivery receipt 必须原子完成；响应丢失后同一 ticket + 同一 client key 可在短宽限内取回同一结果。
- 不同 client key 重放已消费 ticket 必须拒绝；配对失败不能打开 terminal protocol。
- 这是低频安全写入，一次成功配对只允许一个事务写；普通连接、重连和 capability 验证不得写数据库。
- 过期/已消费 ticket 使用批量 compaction 清理，不为倒计时或每次校验更新 `last_seen`。
- daemon-local revoke truth 继续独立于 Cloud；Cloud logout 不撤销本地 grant，用户显式从 daemon 移除客户端才写 revoke。
- TUI share 为 App 请求 target-bound grant 时复用同一 PairingExchange 事务与幂等交付边界；`ManageClientAccess` 校验失败时只能返回 config-only 结果。

## 18. 离线能力与故障隔离

### 18.1 离线不是单一状态

| 故障域 | 应继续工作 | 允许失败的操作 |
| --- | --- | --- |
| Web Controller 不可用 | 已有 edge session、Presence、Hub directory、managed direct/Relay | 账号登录、管理、付费、设备批准 |
| Control Plane/DB 不可用 | 在 edge token 和已验签 policy 有效窗口内，Hub 的列表、resolve、Presence 重建、direct；Relay 受独立 budget 窗口限制 | 新登录、refresh、enrollment、订阅变更和新的全局撤销 |
| Hub 不可用 | 已就绪 DataChannel 及 local/SSH/direct-TLS route | 新 managed Cloud resolve/signaling |
| Relay 不可用 | managed direct candidate 和非 Cloud route | 显式 relay-only；auto 只能在同一 managed attempt 允许的候选中失败 |
| Cloud Companion 不可用 | 已就绪的公开进程 DataChannel 和其他 route | 新 managed route，不得影响 local/SSH/direct-TLS |
| 客户端无互联网 | local/SSH/LAN direct-TLS 按实际网络可达性参与外层竞速 | managed Cloud route |

### 18.2 拆分 IdentityPolicy 与 RelayBudget

当前一个 `MaxStaleness=30m` 同时控制设备 ownership、client/daemon admission、Presence 和付费 Relay budget，把安全撤销、离线可用性和成本风险绑成了一个开关。

建议拆成两类签名投影：

```text
IdentityPolicy
  account/device ownership
  principal kind
  public key
  auth/revoke epoch
  managed-direct allow
  security revision

RelayBudget
  entitlement revision
  region/pool
  max lease duration/bytes/bitrate/concurrency
  shorter expiry
```

建议基线：

- 保留当前 8 小时 edge access token，`IdentityPolicy` 有效期可设为 24 小时，则 Control Plane 中断时新 managed direct 的有效离线上限仍受 token 的 8 小时限制。
- `RelayBudget` 保持更短窗口，例如 30 分钟，避免 Control Plane 长时间故障时无界消耗付费 Relay。
- 撤销延迟与离线时长不可能同时为零。默认最坏撤销传播上限应明确等于有效 admission 窗口；更高安全等级可显式选择较短离线窗口。
- Control Plane 可在可用时立即推送签名 revoke/auth-epoch delta，但不应在同步链路故障时回退到 Hub 逐请求查库。

### 18.3 Policy 发布不应靠 5 分钟全量改 revision 续命

新 snapshot envelope 建议区分：

- `SecurityRevision`：只在账号、设备、公钥、revoke 或 entitlement 的语义真值改变时递增。
- `ContentDigest`：对 canonical projection 求摘要，防止同 revision 内容被替换。
- `EnvelopeSequence/IssuedAt/ExpiresAt`：只用于对未变内容进行低频续期。

发布规则：

- 业务真值改变时立即发布。
- 内容未变时只在接近 expiry 时续期，并加 jitter，不每 5 分钟全量重写。
- Hub 对同 digest 的更新 envelope 允许新的 sequence，但拒绝 revision/digest 回滚或同 revision 不同 digest。
- snapshot store 只在实际接受新 envelope 时落盘，不做 no-op fsync。
- 后台同步失败进入 degraded 状态并指数退避重试；Hub 继续使用最后已验签投影直到它真正过期，不因一次刷新失败退出整个 runtime。

### 18.4 Hub assignment 可维护性

当前 `HubDirectoryVersion` 在 devcloud 中固定为 `1`，Desktop 和 Android 又在已有 session 时无条件拒绝 HubID 变化。这能防止未授权跳转，但也会把合法的单区域 Hub URL/instance 替换变成强制退出重登。

本阶段不建设多区域调度，但应定义最小的签名 `HubAssignment`：

- monotonic assignment version。
- 当前 HubID/origin/region。
- 可选的短期 previous assignment grace，仅用于可控维护切换。
- Companion/App 只接受签名且 version 递增的切换，不接受请求返回的任意 URL。

## 19. Presence 重构

### 19.1 当前问题

`PresenceReady.HeartbeatSeconds=30` 已返回给 daemon，但 `remote/daemon.Agent` 没有发送 heartbeat。Hub 把 Presence 硬过期设为 5 分钟，`RunContinuously` 于是每轮都重做：

```text
BeginPresence
-> fresh challenge
-> DeviceIdentity signature
-> OpenPresence HTTP stream
-> 5 分钟到期
-> 关闭并重连
```

这不是 heartbeat，而是周期性全量重新认证。

### 19.2 目标 PresenceLease

建议改为两层生命周期：

```text
PresenceSession hard lifetime
  <= min(edge token expiry, IdentityPolicy expiry)
  只在首次建立、流真正断开或 hard renewal 时重做 challenge/proof

PresenceLivenessLease
  heartbeat interval: 30s
  in-memory deadline: 90s
  heartbeat 成功只更新 Hub 内存 deadline
```

当前 HTTP response stream 是 Hub -> daemon 单向事件流，因此需要一个明确的双向语义：

- 最小实现可增加独立 `RenewPresence` RPC，由 daemon 对 `PresenceSessionID + counter + sent_at` 签名，Companion 只转发。
- Hub 按 device/session/counter 拒绝重放，成功时只修改内存 deadline。
- 后续如改用全双工 HTTP/2/WebSocket，仍保留同一签名 heartbeat 语义，不应依赖“TCP 还没关”作为存活证明。
- heartbeat 连续丢失后 Hub 在 90 秒内移除 Presence 并关闭未完成 signaling；客户端后续用 fresh proof 重建。
- policy revoke/auth epoch 更新到达 Hub 时立即关闭对应 Presence，不等 heartbeat timeout。

### 19.3 Online 不落库

Presence 是 Hub runtime truth，`nodes.online` 不应是 SQLite 真值。

建议：

- Web 节点列表查询持久设备目录后，在返回前与 Hub Presence 快照合并。
- 移除 Presence open/close 对 `SetCloudDaemonOnline` 的同步写入。
- 启动时不再执行全表 `MarkDaemonNodesOffline`；Hub 空的 runtime state 自然表示当前无在线 Presence。
- 如产品需要“最后在线”，只持久化 `last_seen_at`，按 15-30 分钟桶化合并写，不在每个 heartbeat 或 stream 重建时写。
- online/offline 变化事件可用于 WebSocket/UI 推送，但不成为第二份 Presence truth。

## 20. 持久化与写入收敛

### 20.1 写入预算表

| 领域 | 当前写入 | 目标写入 |
| --- | --- | --- |
| Hub policy | 每 5 分钟全量 JSON/token + fsync + rename | 语义变更时发布；接近 expiry 才 jittered renewal；digest no-op 不写 |
| Presence | 每 5 分钟 online=true/false 两次 SQLite UPDATE | 稳态 0 次 DB 写；可选 `last_seen` 低频合并 |
| Refresh session | 每次 issue/rotate/revoke 重写所有 hash 记录 | 以 hash/family 为 key 的行级事务；TTL 索引批量清理 |
| Security directory | 每次 account/user/device 变更重写全量 snapshot | 生产使用规范化表 + security revision + transaction outbox；JSON 仅留 staging harness |
| Client login | 每次新 device + policy + node upsert + audit | 稳定 installation device；首次 insert，metadata 实际改变才 update，audit 只记语义事件 |
| Daemon pairing | 当前二维码直接复制长期 bearer grant | ticket consume + bound grant 签发一次原子写；普通连接 0 写；过期记录批量清理 |
| LAN discovery | 旧 machine address 可能直接持久化或反复更新 | candidate 仅内存 TTL；可选 last-success seed 低频 debounce 写 |
| Relay usage outbox | 每秒 Enqueue 全量 fsync，每个 Ack 再全量 fsync | append-only/WAL 或 SQLite outbox；按时间/数量批量 append 和 batch ack |
| Companion credential | 每个 Hub 请求读 OS credential store | 启动/首次使用时读，进程内缓存有效 session；refresh/logout 时原子替换或清理 |

### 20.2 Relay usage P0 修正

Relay 计量需要一个可提交的 drain 边界：

```text
PrepareUsageBatch
  冻结本批 counter/sequence，后续流量进入下一批

PersistUsageBatch
  durable append，成功前本批仍可恢复

CommitUsageBatch
  只在 outbox 成功后从 authority 移除本批
```

要求：

- outbox 写失败不得清空 pending bytes 或递增不可恢复的 sequence。
- Control Plane ledger 的 `(relay_id, lease_id, sequence)` 幂等记录和 aggregate 必须在同一持久事务中提交，不能只放内存。
- response/ACK 丢失后 Relay 重发同一 batch，Control Plane 返回 duplicate success。
- 持续流量按 5-10 秒、数量阈值或 termination 触发批量，不固定每秒执行两次全量 fsync。
- 内存和磁盘队列都必须有容量上限、水位指标和明确的拒绝新 Relay allocation 策略，不得静默丢计量。

### 20.3 投影不和安全主事务耦合

Web node table、audit 和 Hub policy 都是主安全真值的 projection。当前 devcloud 在一个请求内交替更新 JSON directory、Hub snapshot、SQLite node 和 refresh file，容易出现部分成功。

生产边界应改为：

- Control Plane 主数据库事务持有 account/device/session family/security revision 真值。
- 同一事务写 projection outbox。
- Hub policy publisher 和 Web read-model worker 分别幂等消费 outbox。
- Web projection 失败只表示展示延迟，不回滚已完成的安全撤销或设备注册。
- Hub policy 未达到 exchange 所需最低 revision 时，不交付一个立即不可用的新 edge session。

## 21. Companion、TUI 与 App 交互收敛

### 21.1 Companion session runtime

- Service 内建立 account/device 分离的内存 session cache，每份会话只有一个 owner，替换和关闭时清理 secret bytes。
- OS credential store 只在进程启动/首次加载、refresh 成功、login/enrollment 完成和 logout 时访问，不作为每个 Hub request 的热路径依赖。
- account 和 device 使用独立 singleflight/mutex，不因账号刷新阻塞 daemon Presence renewal。
- 主动刷新窗口保留“刷新失败但旧 token 仍有效时继续使用”的现有正确语义，并加入 jitter 避免群体同时刷新。
- refresh 网络 timeout 与 route dial timeout 分离，避免一次 Control Plane 慢请求占满整个 route attempt 的时间预算。
- `Status` 区分 `ready`、`ready_offline`、`refresh_degraded`、`login_required`、`companion_unavailable`，但不暴露 token、Hub 内部地址或原始 adapter error。

### 21.2 登录和设备引导

交互应统一为“当前阶段 + 下一个可执行动作”，不只显示泛化 temporary error。

- CLI/TUI `browser` 方法：默认通过平台 opener 打开已验证 HTTPS URL，同时保留 URL 和 code 供手工备用。
- CLI/TUI `device-code` 方法：不自动打开浏览器，只展示短码、地址、到期和等待状态。两种 method 必须有真实行为差异。
- Official App 同时保留两条路：同机使用系统浏览器/Custom Tab 批准；跨设备使用 Web 生成 QR，App 扫码后 Web 显示设备 metadata 再批准。
- Cloud activation QR 与 daemon endpoint bootstrap QR 共用 scanner 外壳，但使用不同显式 intent 和 parser；Cloud activation 成功后只刷新 account discovery overlay。
- 不在 App WebView 内收集账号密码，不把 browser session Cookie 交给原生模块。
- 当前 1 秒 polling 改为 20-30 秒可取消 long-poll，或有界退避 + jitter；pending 是稳定状态，不返回泛化 503。
- flow 取消要有显式 server-side cancel，尽快释放内存容量，TTL 只作为最终保险。
- 错误映射固定到恢复动作：`login_required`、`approval_expired`、`device_revoked`、`policy_stale`、`hub_unavailable`、`relay_quota`、`companion_missing`、`identity_mismatch`。TUI 和 App 必须消费同一 contract fixture。

### 21.3 真实健康状态

`/api/status` 不应固定返回全部 ready。建议返回脱敏的独立组件状态：

- Web/BFF 是否可接受用户管理请求。
- Control Plane DB 是否可写。
- policy publisher 最后成功时间和当前 backlog。
- Hub 当前 verified policy age，以及 direct/Presence admission 是否仍在有效窗口。
- Relay 是否可接受新 lease/allocation，usage outbox 是否超水位。
- Companion 本地 session 是否仍可离线使用。

UI 可将组件状态收敛为用户可执行结果，但 diagnostics 和 metrics 必须保留真实故障域。

## 22. Managed Cloud Route Attempt 取消契约

外层 route race 接入 Cloud 后，每个 managed route 尝试必须有独立 `AttemptID`，并与客户端本地 `EndpointID` 解耦。

```text
PrepareManagedAttempt(AttemptID, TargetDeviceID, policy)
OpenSignaling(AttemptID, offer)
AcquireRelay(AttemptID)              仅在实际需要时
CancelManagedAttempt(AttemptID)
ReportManagedOutcome(AttemptID, outcome)
```

`CancelManagedAttempt` 必须：

- 幂等，只允许 owning account/client principal 取消。
- 关闭对应 Hub signaling session 和未交付事件。
- 立即删除 Hub 的 managed attempt/Relay budget reservation，不等 5 分钟 lease expiry。
- 关闭已建立的 loser PeerConnection 后，Relay 通过 allocation close 释放 TURN concurrency。
- 对已产生流量签发最终 usage event；未分配或零流量 attempt 不计费。
- 不影响同 endpoint 其他 route attempt 或其他已就绪 managed session。

Cloud route 的内部 direct/single-relay 候选仍由 managed adapter 管理；外层 planner 只看到该 route attempt 是否最终生成 `ReadySession`。

## 23. 验收门禁与可观测性

### 23.1 P0 Harness

- login 在事务提交后、HTTP 响应前断开，用同一 FlowID 重试返回同一 device/session/refresh child。
- refresh 在 child hash 提交后丢失响应，使用旧 secret 的宽限重试只取回同一 sealed result；宽限窗口后重放失败。
- 两个并发 refresh 请求只产生一个 child session，不会一个成功、一个把 family 再次旋转。
- enrollment 在 challenge 生成或最终 session 交付的任意注入故障后，不吞掉已正确使用的 code/proof。
- Relay outbox 写失败后 pending bytes 仍在；重试持久化同一 sequence/body，Control Plane 响应丢失后 duplicate apply 不重复计费。

### 23.2 离线与故障 Harness

- Control Plane/DB 停止后，在有效 edge token + IdentityPolicy 窗口内，新的 Hub list/resolve/Presence rebuild/managed direct 仍成功。
- 同样故障下，新 login/refresh/enrollment 明确失败，不伪造本地成功。
- RelayBudget 过期后新 Relay 失败，但 managed direct 不因此失败。
- Hub/Companion 在 DataChannel `ReadySession` 建立后停止，已有 terminal protocol 仍继续；新 managed route 失败并由外层竞速选择其他 route。
- policy renewal 失败不退出 Hub/runtime，指数退避后恢复；只在已验签 policy 真正过期时停止新 admission。

### 23.3 Presence 与写入 Harness

- daemon 稳定运行 24 小时期间不重建 PresenceSession，heartbeat 只修改 Hub 内存 deadline。
- heartbeat 中断后在配置的 liveness window 内移除 Presence，重放 counter 不能延长 deadline。
- Presence 建立、heartbeat、断开和重建过程中 SQLite 写入数为 0；可选 `last_seen` 只按时间桶写一次。
- 无安全语义变更时，policy publisher 不递增 `SecurityRevision`，不每 5 分钟落盘。
- 同一 ClientInstallationIdentity 重新登录 100 次，设备行仍为 1，不产生 100 条 `node.enrolled` audit。
- 外层 route race 取消 managed Relay loser 后，Hub budget reservation 和 TURN allocation 在限定时间内都回到基线。

### 23.4 必要指标

- `cloud_policy_age_seconds{kind=identity|relay_budget}`
- `cloud_policy_publish_total{result,reason}`
- `credential_exchange_total{kind,result}` 与 `credential_exchange_replay_total{result}`
- `cloud_refresh_total{kind,result}` 与 refresh latency
- `hub_presence_sessions`、`hub_presence_reconnect_total{reason}`、heartbeat timeout
- `cloud_store_writes_total{store,operation}` 与 fsync latency
- `relay_usage_outbox_events/bytes/oldest_age_seconds`
- `managed_attempt_total{route,result}` 与 loser cancel latency
- `companion_session_state{kind,state}`

指标 label 不得包含 account ID、device ID、IP、terminal ID、credential body 或用户输入的 endpoint label。

## 24. 建议实现切片

以下切片只是审核后的候选队列；在用户确认前不写入 `workflow.md`，不与当前 `CLOUD018` 混合实现。

### CLOUDR001：幂等交换 contract 与故障注入 harness

- 定义 `CredentialExchange`、session family、delivery receipt 和 stable error code。
- 先建 login/refresh/enrollment 响应丢失与并发重试 harness。

### CLOUDR002：Control Plane 事务真值

- 把 security directory、refresh family、exchange 和 projection outbox 收敛到同一事务 store contract。
- staging 先可使用 SQLite 真实跨进程持久化，不再以多个全量 JSON 文件组合模拟事务。

### CLOUDR003：Stable ClientInstallationIdentity

- Desktop Companion 和 Android 生成稳定 installation key。
- login approval、client device directory、edge token 和 Hub proof 改为公钥绑定。
- 删除每次登录新建随机 client device 的路径。

### CLOUDR004：Policy 分层与离线窗口

- 拆分 IdentityPolicy 和 RelayBudget。
- 引入 semantic revision、content digest、renewal sequence 和 jittered publisher。
- 刷新失败进入 degraded/retry，不停整个 runtime。

### CLOUDR005：Presence heartbeat 与在线投影

- 实现签名 heartbeat + 内存 liveness lease。
- 删除 5 分钟强制重建和 SQLite online 热路径写入。
- Web 读模型在查询时合并 Hub Presence。

### CLOUDR006：Managed attempt 取消与 Relay 计量

- 增加 `CancelManagedAttempt` 和 Relay reservation 即时释放。
- 把 usage drain 改为 prepare/persist/commit，outbox 改为批量 WAL/SQLite。
- Control Plane ledger 持久化幂等键和 aggregate。

### CLOUDR007：Companion/App/TUI session 与交互

- 实现 session cache、per-kind singleflight、refresh jitter 和 degraded status。
- 统一 browser/device-code/QR/long-poll 交互与错误 fixture。
- 确保 Companion/Hub 退出不关闭已就绪 DataChannel。

### CLOUDR008：统一 Endpoint Route 接入与真实验收

- managed Cloud 作为普通 route dialer 接入第 6 节外层竞速。
- Cloud directory 输出 daemon fingerprint，并通过 `EndpointAssembler` 与扫码、手工 direct/SSH、本地 discovery 合并。
- Official App 冷启动先加载本地 registry；Cloud 登录和 directory 更新只叠加 managed route，不创建 Cloud-only machine truth。
- 统一采用 `EndpointBootstrapBundle v2`，删除 v1 扫码即 Cloud 和 schema v4 local Hub/session-secret 路径。
- 接入 `termx endpoint share`，允许 TUI/CLI 把 portable route 与 selection policy 一次迁移到 App；Cloud credential 继续由 App 独立登录。
- 验收 Cloud loser 取消、Control Plane 离线、Hub/Relay 故障和 SSH/direct/local winner。
- 删除 Cloud-only endpoint/session owner 和重复 App/TUI connection state machine。

## 25. 建议审核结论

建议按以下原则通过方案：

1. 同一 daemon 是一个 Endpoint，local、SSH、direct TLS 和 managed Cloud 是多条 Route，默认竞速，显式 priority 时分组 hedge。
2. Cloud 不获得外层 route 选择权，只实现一条可取消的 managed route attempt。
3. 在扩展 route race 前，先修复 login/refresh/enrollment 的事务幂等性和 Relay usage 丢失窗口。
4. 保留现有 credential 分层、Hub 离线验证和 CapabilityGrant 端到端边界，但把二维码默认授权从 bearer grant 升级为一次性 PairingTicket，并把长期 grant 绑定到客户端密钥。
5. 用 8 小时 edge token 和独立 IdentityPolicy 支持有界 Control Plane 离线，RelayBudget 保持更短窗口；撤销上限作为明确产品安全参数。
6. Presence 稳态只是 Hub 内存 heartbeat lease，不是每 5 分钟重新 enrollment/认证，也不在每次存活变化时写数据库。
7. 客户端使用稳定 ClientInstallationIdentity，不在每次登录创建新 cloud device。
8. policy、refresh、security directory 和 Relay usage 的生产持久化使用行级事务/outbox/WAL，不使用高频全量 JSON fsync。
9. TUI、CLI 和 Official App 共享同一 route planner、Cloud error、login/enrollment 和 reconnect contract fixture。
10. 已就绪 DataChannel 的生命周期不与 Companion、Hub、Web 或 Control Plane 的后续可用性绑定。
11. 扫码、Cloud directory、LAN discovery 和手工 SSH/direct 配置只是 endpoint 获取来源；统一由签名 bootstrap、daemon fingerprint 和 `EndpointAssembler` 合并，不能继续形成 Cloud、本地和 SSH 三套机器真值。
12. Official App 的本地 endpoint registry 是基础能力，Cloud 是可选 route overlay；退出账号或 Cloud 故障不得隐藏或破坏本地直连、SSH route 与端到端授权。
13. 增加 `termx endpoint share` 作为 TUI/CLI 到 App 的主配置迁移入口；它可以迁移 portable route 和用户确认的 priority，但二维码只建立一次性 TLS share session，Cloud token 与源客户端 grant 永不复制。
