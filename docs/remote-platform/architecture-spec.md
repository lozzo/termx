# TermX Remote Platform 架构规范

状态：RP006 统一公开客户端实现基线

版本：v1 draft

日期：2026-07-11

## 1. 架构目标

本规范将 TermX 远程连接拆成可独立演进的四个平面：

1. 公开 terminal data plane：core-v2、termx protocol 和客户端 endpoint session。
2. 公开 end-to-end authorization plane：设备证明和 capability handshake。
3. 私有 managed control plane：账号、设备目录、entitlement、Hub admission 和 Relay lease。
4. 私有 connectivity data plane：Hub signaling 与 Relay/TURN 转发。

拆分目标是保证：

- 无云时 local/SSH 完整工作。
- 云端故障或订阅变化不会改变 daemon 的 terminal authorization。
- Hub/Relay 可以商业化、扩容和替换，而不污染 core/TUI/App 领域。
- TUI 和 App 共用 endpoint 与远程协议，不再各自维护一套业务连接逻辑。
- 旧代码可以作为实现参考，但不能形成双真值或兼容链路。

## 2. 总体拓扑

可渲染的完整网络图和 direct/Relay 时序见 `network-topology.md`。下图保留最小文本拓扑，便于纯终端阅读。

```text
                           private managed services
                    +--------------------------------+
                    |                                |
                    |  Control Plane                 |
                    |  account/device/entitlement    |
                    |  admission/relay lease/usage   |
                    |          | signed tickets       |
                    |          v                      |
                    |  Hub -------------- Relay/TURN |
                    | presence + signaling  bytes    |
                    +-----^--------------------^------+
                          | SDP/ICE             | encrypted DTLS
                          |                     |
+-------------------------+---------------------+------------------+
|                         public clients                           |
|  TUI / CLI / App -> client/runtime -> WebRTC Transport Adapter  |
|         |                 |                                      |
|         |                 +-> local / SSH adapters                |
+---------|---------------------------------------------------------+
          | termx protocol after E2E capability handshake
          v
+------------------------------------------------------------------+
| public daemon                                                    |
| DeviceIdentity -> RemoteSessionAcceptor -> core-v2 scoped session|
| terminal lifecycle/history/input truth remains here              |
+------------------------------------------------------------------+
```

Hub 只传递 signaling metadata。Relay 只转发已经由 WebRTC DTLS 加密的字节。两者均不能读取 termx protocol frame 或 capability grant。

## 3. 领域与真值归属

| 领域 | Domain owner | Truth source | 禁止拥有 |
| --- | --- | --- | --- |
| Terminal lifecycle/history | core-v2 daemon | owning daemon storage/runtime | 云账号、Hub、Relay、App/TUI 本地投影 |
| Endpoint registry | client | `connections.yaml` 与可选云 metadata projection | daemon client registry、Relay node |
| Client endpoint runtime | `client/runtime` | per-endpoint race、winner、generation、ReadySession | terminal lifecycle truth、UI reducer state |
| Device identity | daemon | daemon 本地长期密钥 | Hub 注册 token、设备 label |
| Terminal capability | daemon | daemon 签发、验证和撤销记录 | Control Plane subscription、Hub session |
| Account/session | Control Plane | 私有账号数据库 | terminal capability |
| Device directory | Control Plane | 最小 device metadata/presence projection | terminal inventory、terminal content |
| Hub presence/signaling | Hub | 短 TTL session state | terminal list、capability scope、长期设备私钥 |
| Relay entitlement | Control Plane | subscription + policy -> short lease | daemon terminal authorization |
| Relay forwarding/usage | Relay | active lease + session counters | 解密后的 DataChannel 内容 |
| Workbench/layout/copy UI | TUI/App | client local state | committed history truth |

## 4. 核心模型

### 4.1 Endpoint 与 Transport

`Endpoint` 表达一个 daemon 目标，其稳定引用是客户端域的 `EndpointID`。跨 endpoint terminal 必须继续使用：

```text
TerminalRef = EndpointID + daemon-local TerminalID
```

`Transport` 表达到达 endpoint 的方法：

- `local`：本地 unix socket。
- `ssh`：通过 OpenSSH stdio proxy 到远端 daemon socket。
- `webrtc`：通过 Hub signaling 协商的 DataChannel。

`direct`、`single_relay` 和 `relay_mesh` 不是 transport kind，而是 `webrtc` transport 的 `ObservedPath`。其中 `relay_mesh` 表示两端各自就近接入 Edge Relay，再经受控 inter-region backbone 转发。详细阶段和限制见 `global-acceleration-spec.md`。因此以下状态保持不变：

- endpoint identity；
- device fingerprint；
- capability grant；
- terminal protocol；
- `TerminalRef`；
- TUI/App workbench binding。

### 4.2 Device

Device 是运行 daemon 的长期安全主体：

- `DeviceID`：路由与目录标识，不单独作为安全证明。
- `DevicePublicKey`：daemon 长期公开密钥。
- `DeviceFingerprint`：公开密钥的稳定规范化摘要，是客户端 pin 的安全身份。
- `DeviceLabel`：用户展示 metadata，不参与认证。

Hub presence 只能声明某个 `DeviceID` 当前在哪个 Hub session 上线；客户端必须通过 DataChannel 内设备证明确认其公钥和已 pin fingerprint。

### 4.3 EdgeManagedSession

连接热路径中的托管 session 由 Hub 创建和持有，而不是由 Control Plane 逐连接创建：

```text
EdgeManagedSession {
    session_id
    account_id
    client_device_id
    target_device_id
    hub_id
    relay_policy
    created_at
    expires_at
}
```

它绑定已离线验证的 client edge token、Hub 本地授权投影和 active target presence，用于 signaling、route intent 与异步 usage correlation。Control Plane 可接收脱敏 audit/usage projection，但不参与创建。它不能包含 terminal ID、grant 内容、terminal scope 或 protocol payload。

### 4.4 ProtocolSession

ProtocolSession 只在客户端和 daemon 之间存在：

1. transport 已建立；
2. 端到端设备证明通过；
3. capability handshake 通过；
4. daemon 将 scope 映射成 core-v2 `TransportScope`；
5. 才允许 protocol Hello/List/Attach 等 frame。

Hub session、WebRTC PeerConnection 和 ProtocolSession 生命周期相关但不是同一个对象。任何一个对象不得用另一个对象的 ID 代替自身授权。

## 5. 组件职责

### 5.1 Public daemon

负责：

- 持有 DeviceIdentity 和 device fingerprint。
- 生成、验证和撤销 CapabilityGrant。
- 建立公开 RemoteSessionAcceptor。
- 在 DataChannel 内证明设备身份并验证 capability challenge。
- 将接受后的 scope 映射到 core-v2 `ServeScopedTransport`。
- 通过公开 cloud connector interface 注册 presence 和接收 signaling；桌面官方服务由独立 Cloud Companion IPC adapter 实现。

不负责：

- 判断订阅套餐、Relay 余额或账号计费。
- 把 terminal inventory 注册到 Hub。
- 将原始 capability grant 发给 Hub 或 Control Plane。

### 5.2 Public TUI/CLI/App client

负责：

- 统一加载 EndpointSpec、transport config 和 `grant_ref`。
- 通过平台安全存储读取原始 grant。
- 调用公开 ControlPlaneClient/HubClient interface 获取短期票据并交换 signaling；桌面官方服务通过 Cloud Companion IPC adapter 实现这些 interface。
- 创建平台特定 WebRTC primitive。
- pin 并验证 daemon DeviceIdentity。
- 在 DataChannel 内完成 capability handshake。
- 建立标准 termx protocol client bundle。
- 向 UI 投影 connecting/probing/direct/single-relay/relay-mesh/offline/auth-expired 等状态。
- 在显式 SmartRoute 下只执行 Companion 返回的短期 ICE plan，并在授权前核对实际 candidate path。

不负责：

- 解释套餐数据库或自行伪造 Relay entitlement。
- 从 Hub terminal list 建立 terminal pool。
- 在 capability handshake 前发送 termx protocol 请求。

### 5.3 Private Cloud Companion

桌面/headless 官方 cloud 使用可选闭源 `termx-cloud` sidecar；移动端官方构建使用实现同一 contract 的私有模块。详细分发和 IPC 见 `distribution-and-cloud-companion-spec.md`。

负责：

- 持有本地 AccountAccessToken/account session。
- 调用官方 Control Plane/Hub，转发 presence、SDP/ICE 和稳定错误。
- 获取 caller-specific Hub admission、RelayLease/TURN credential 和 route plan。
- 转发不含 terminal/grant 的网络质量 summary。

不负责：

- 创建或终止 WebRTC DataChannel。
- 接收 DeviceIdentity private key、CapabilityGrant 或 terminal protocol frame。
- 改变 daemon scope、endpoint identity 或 terminal lifecycle。
- 在缺失或失败时触发 local/SSH/旧 remote fallback。

### 5.4 Private Control Plane

负责：

- 用户、organization、登录 session 和 account token。
- device registration、ownership、最小目录和 presence projection。
- 套餐、entitlement、quota、账单和风控。
- 签名 edge token、HubDirectory，以及带单调 revision 的设备/订阅/撤销投影。
- 向区域 Hub 同步签名 RelayPolicy/RelayBudget 和受限 regional issuer 授权；不参与每次 RelayLease 请求。
- 接收、去重和结算 Relay usage event。
- 配对审批和审计 metadata，但不保存原始 capability grant。

不负责：

- 转发 SDP/ICE 或 terminal protocol。
- 判断某个 terminal request 是否在 capability scope 内。
- 保存 terminal list、history、输入或屏幕内容。

### 5.5 Private Hub

负责：

- 离线验证签名 edge token，并将其与本地 DeviceOwnership/Pairing/Revocation/Subscription 投影取交集。
- 在 direct 热路径本地创建短期 EdgeManagedSession；cache miss、过期或 revision 不完整时 fail closed，不同步查询 Control Plane。
- 维护短 TTL device presence 与 session routing。
- 转发 offer、answer 和 ICE candidate。
- 分配 signaling correlation ID、超时和错误。
- 在需要时返回允许使用的 Relay endpoints metadata。
- 在签名区域预算内使用独立 regional key 签发短期、session-specific RelayLease；不得持有 Control Plane root key。

不负责：

- 接收 CapabilityGrant 或把它当 `session_token`。
- 验证 terminal scope、terminal ID、grant expiry 或 revoke。
- 持久化 terminal inventory。
- 直接查询套餐数据库；Control Plane 同步只允许后台 snapshot/delta 流，不能成为连接请求 fallback。
- 代理 termx protocol frame。

### 5.6 Private Relay/TURN

负责：

- 验证 session-specific RelayLease 或由其派生的短期 TURN credential。
- 限制 region、session、并发、速率、字节数和 expiry。
- 转发 DTLS/SRTP/WebRTC 数据。
- 生成幂等、可补报的 usage event。
- 先把 signed lease + signed usage event 写入 durable outbox，再异步向 Control Plane at-least-once 补报。

不负责：

- 终止 DataChannel DTLS 或读取 termx frame。
- 读取 capability grant。
- 扩大或缩小 daemon scope。

### 5.7 Web Controller

Web Controller 是私有 Control Plane 的管理 UI/API surface，不是独立安全域。它可以提供账号、设备、套餐、Relay 使用量、组织、审批和审计管理，但不能成为 client 与 daemon 之间的 protocol gateway。

私有 archive 中旧 `web-control/` 以 agent 数、server 数和 heartbeat kick 为中心的 schema 不是目标模型。新模型应围绕 Account、Organization、Device、Entitlement、ManagedSession、Admission、RelayLease 和 UsageEvent 重建。

## 6. 公开接口边界

public namespace 提供 interface、Cloud Companion IPC contract 和 wire DTO；桌面 private companion 或移动端 private module 实现 official cloud adapter，私有服务实现网络端 contract。

### 6.1 CloudCompanionClient

桌面公开进程通过固定用途的 versioned local IPC 使用 official cloud：

```text
Hello(protocol range, caller role, capabilities) -> selected version
GetStatus() -> install/session/capability summary
ResolveEndpoint(target device id) -> managed endpoint metadata
OpenPresence(signed device proof) -> presence/signaling stream
CreateSignalingSession(offer/ICE) -> answer/ICE/error stream
AcquireRelayLease(session, route preference) -> caller-specific lease credential
ReportPathQuality(redacted summary) -> ack
```

IPC 禁止包含 CapabilityGrant、DeviceIdentity private key、terminal inventory/content 或 DataChannel frame。完整安装、版本和错误 contract 见 `distribution-and-cloud-companion-spec.md`。

### 6.2 ControlPlaneClient

概念接口：

```text
RegisterDevice(device proof, metadata) -> device registration
ResolveEndpoint(target device id) -> hub assignment + target presence metadata
IssueHubAdmission(session intent) -> short-lived signed admission
IssueRelayLease(session id, region preference) -> lease or entitlement error
ReportClientOutcome(session id, path, error class) -> accepted
```

客户端只能看到最小错误类别，例如 unauthenticated、device-not-found、entitlement-denied、quota-exhausted、region-unavailable。数据库字段和套餐实现不得泄漏进公开 contract。

### 6.3 HubClient

概念接口：

```text
OpenAgent(admission, device presence) -> signaling stream
CreateOffer(admission, target device id, SDP) -> signaling session
PollOrStreamAnswer(signaling session) -> SDP/ICE/error
SendCandidate(signaling session, ICE candidate) -> ack
CloseSignaling(signaling session, reason) -> ack
```

所有 payload 都禁止包含 CapabilityGrant、terminal ID、terminal inventory 或 core-v2 scope。

### 6.4 RelayLeaseProvider

Relay credential 获取与 Hub signaling 解耦：

```text
AcquireRelayLease(managed session, requested region) -> RelayLease
RefreshRelayLease(existing lease id) -> RelayLease
```

基础策略下，客户端在 relay policy 不允许或 direct 已达到质量门槛时不请求 lease；SmartRoute 可以在 direct 可达但质量较差时请求允许的 route class。Hub 不得把长期共享 TURN 密钥下发给客户端。

### 6.5 SmartRoutePlanProvider

SmartRoute 决策与 WebRTC 执行分离：

```text
PlanManagedRoute(endpoint, managed session, target, SMART_ROUTE)
    -> plan id + direct|single-relay + selection reason + short ICE material
```

候选质量、成本、entitlement、评分权重和 hysteresis state 只属于私有 Route Planner。公开客户端必须二次校验计划绑定、有效期和 ICE policy，禁止接受 Relay Mesh、未选中 TURN 或隐式 fallback。GA002 只在初次连接/重连时取计划；会话内 ICE restart 仍未实现。

### 6.6 RemoteSessionAcceptor

公开 daemon interface 接收已经建立的可靠有序 DataChannel，先运行端到端授权状态机，再将成功 session 交给 core-v2。它不接受来自 Hub 的预验证 grant 标记。

## 7. 连接时序

### 7.1 Local

```text
Client -> local socket -> protocol Hello -> core-v2
```

不涉及账号、Hub、WebRTC 或 Relay。

### 7.2 SSH

```text
Client -> OpenSSH/host-key -> remote daemon stdio proxy
       -> protocol Hello -> core-v2
```

SSH 自身负责 transport authentication。该路径不需要云端 capability grant；远端 daemon 可以按 SSH 接入策略映射 scope。

### 7.3 Managed WebRTC direct

```text
Client/Daemon -> Control Plane: short Hub admissions
Client/Daemon -> Hub: presence + SDP/ICE signaling
Client <--------------------------> Daemon: WebRTC direct DTLS
Client <--------------------------> Daemon: device/capability handshake
Client <--------------------------> Daemon: termx protocol
```

Control Plane 不转发 SDP，Hub 不看到 capability。基础策略下 direct 达到质量门槛后不申请 Relay lease；启用 SmartRoute 时，direct 即使可达也必须与允许的 Relay candidate 比较稳定性和成本，不能把“打洞成功”直接当成最佳路径。

GA002 的客户端先经 Companion v3 获取 direct 或 single-relay 短期计划，再用原始 `SMART_ROUTE` intent 进行 signaling。实际 ICE path 与计划不一致时，在 DeviceIdentity/capability handshake 前关闭当前连接；公开客户端不能自行改取其他 RelayLease。

### 7.4 Managed WebRTC Relay

```text
path policy selects a relayed candidate
Client -> Control Plane: request RelayLease(route class)
Client/Daemon -> Edge Relay(s): lease-derived credentials
Client <----- encrypted DTLS via single Relay or Relay Mesh -----> Daemon
Client <-------------- capability + protocol --------------------> Daemon
Relay(s) -> Control Plane: signed idempotent usage events
```

Relay lease 失败只终止对应 relayed candidate。若 direct 或其他已授权 route 仍可用则可以继续；不得切换到 SSH/local、旧 remote path 或未授权 route class。Relay Mesh 只能按 `global-acceleration-spec.md` 分阶段启用，不进入基础 Relay 首版。

## 8. 状态机与失败边界

客户端 WebRTC endpoint 至少有以下状态：

```text
idle
resolving_endpoint
obtaining_admission
signaling
connecting_direct
probing_paths
selecting_route
obtaining_relay_lease
connecting_relay
authenticating_device
authorizing_capability
connected_direct | connected_single_relay | connected_relay_mesh
offline(error_class)
```

关键失败条件：

| 失败 | Owner | 行为 |
| --- | --- | --- |
| 云账号过期 | Control Plane access | 无法获取新票据；不影响 local/SSH 和已有 daemon capability |
| Hub admission 过期 | Hub session | 重新获取短票据；不得复用长期 agent token |
| Device fingerprint 不匹配 | Client E2E auth | 立即拒绝，要求显式重新配对 |
| Capability 过期/撤销 | Daemon | 拒绝 protocol session；Hub 不解释原因细节 |
| Relay entitlement 拒绝 | Control Plane | 不创建 Relay path；可继续仍有效的 direct 尝试 |
| Relay 配额耗尽 | Relay/Control Plane | 按租约终止或拒绝续租；不修改 terminal scope |
| Hub/Relay 单点故障 | Managed connectivity | 仅该 endpoint offline；其他 endpoint 保持 |
| Protocol 错误 | Client/daemon | 关闭 ProtocolSession；不自动重配对或扩大 scope |

## 9. TUI 与 App 收敛

TUI 和 App 必须共享：

- EndpointSpec 与 TerminalRef 语义。
- DeviceIdentity pin、grant metadata 和 grant reference 语义。
- ControlPlaneClient、HubClient、RelayLeaseProvider 的 contract fixtures。
- RemoteSession handshake message 与错误类别。
- protocol client adapter 和 endpoint state machine 行为测试。

允许平台差异：

- Go/Pion 与 Android/iOS WebRTC primitive。
- Keychain/Keystore 存储实现。
- 前后台、网络切换和系统通知生命周期。
- UI 导航与渲染。

不允许平台差异：

- capability 是否经 Hub 传递。
- session token 的含义。
- terminal inventory 来源。
- direct/Relay 是否使用不同 protocol。
- scope、expiry、fingerprint 和 revoke 的解释。

## 10. 数据最小化

### 10.1 Control Plane 可保存

- account、organization 和 billing metadata。
- account/device cloud session 由 companion/platform secret store 持有；普通 `termx` 配置不保存 token。
- device ID、public key/fingerprint、owner、label 和最后在线时间。
- Hub/region assignment、ManagedSession metadata 和 outcome。
- entitlement、RelayLease metadata 和聚合 usage。
- 配对审批、grant reference hash/status 和审计事件。

### 10.2 禁止保存或转发

- 原始 CapabilityGrant。
- terminal inventory、TerminalID、命令、cwd、history、screen 或输入。
- DataChannel 明文或 termx protocol frame。
- daemon device private key。
- 可长期复用的 TURN shared secret。

诊断日志需要 correlation ID 时，只使用 ManagedSessionID、SignalingSessionID、RelayLeaseID 和脱敏 DeviceID，不记录 credential body。

## 11. 可扩展性与部署

- Control Plane 是持久状态 owner，可按账号/组织分区。
- Hub 是短 TTL 状态服务，按 region 横向扩展；票据应支持离线验签，避免每次 signaling 查询主数据库。
- Relay 是高带宽数据面，按 region 独立扩容和计费；usage event 异步上报。
- Global Accelerator 启用后，Relay 还可以作为 Client/Daemon Edge；私有 Route Planner 只基于质量、容量、成本和 policy 选择受限 backbone route，不参与 terminal authorization。
- Hub 与 Relay 可以同区域部署，但必须保持逻辑职责和凭据隔离。
- presence、signaling 和 lease 过期都必须有明确 TTL；禁止永久 agent token 驱动长期隐式会话。

## 12. 架构验收

- public core/TUI/App/CLI 构建不依赖私有 Hub/Web Controller 源码。
- public build 不依赖 private companion；fake companion 可以驱动完整 cloud contract harness，缺失 companion 不影响 local/SSH。
- fake Control Plane + fake Hub 可以驱动完整 client contract harness。
- 真实 direct 和 Relay harness 都在 DataChannel 建立后运行同一 E2E handshake 和 protocol 测试。
- 启用全球加速后，direct、single-relay 和 relay-mesh harness 使用同一 endpoint、grant、DataChannel auth 和 termx protocol fixture；只允许 `ObservedPath`、route diagnostics 和 usage 不同。
- Hub 请求 schema 不含 grant、terminal 或 scope 字段。
- daemon 不向 Hub 注册 terminal inventory。
- Control Plane 的 subscription/entitlement 代码无法 import core-v2 scope package。
- local/SSH 测试在完全不启动私有服务时通过。
