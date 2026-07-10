# TermX Remote Platform 安全与协议规范

状态：RP003 已实现基线

版本：v1 draft

日期：2026-07-11

## 1. 安全目标

TermX 远程平台必须同时满足两个独立授权问题：

1. 云服务准入：某个账号或设备是否可以使用官方 Hub/Relay。
2. terminal 授权：某个客户端是否可以访问某个 daemon 及其哪些 terminal 能力。

第一个问题由私有 Control Plane、Hub 和 Relay 处理；第二个问题只由 owning daemon 处理。两者不得复用 token、字段、缓存或判断结果。

## 2. 威胁模型

假设攻击者可以：

- 观察、重放或篡改客户端与 Control Plane/Hub 之间的网络请求。
- 控制恶意 Hub 节点、Relay 节点或云服务内部日志读取权限。
- 猜测 DeviceID、ManagedSessionID 和 signaling correlation ID。
- 获取过期、撤销或属于其他 session 的服务票据。
- 控制恶意、被替换或被攻破的本地 Cloud Companion，并返回错误 DeviceID、SDP/ICE、RelayLease 或 route plan。
- 诱导客户端连接到相同 label 的冒充 daemon。
- 重放以前 DataChannel 内捕获的授权 frame。

假设基础：

- 客户端和 Control Plane/Hub 使用正确验证的 TLS。
- WebRTC DataChannel 使用端到端 DTLS；Relay 不终止 DTLS。
- daemon DeviceIdentity 私钥与客户端安全存储未被主机级攻击者攻破。
- 公开算法实现使用成熟密码学库，不自行实现 primitive。

不承诺防御已完全控制 daemon 或客户端主机的攻击者；此时应通过设备撤销、grant 撤销和账号 session 失效限制后续影响。

## 3. 五类凭据

### 3.1 AccountAccessToken

用途：用户或客户端访问 Control Plane API。

属性：

- issuer：Control Plane identity provider。
- audience：Control Plane API。
- subject：account/user/client session。
- 短期 access token，使用 refresh/session 机制续期。

禁止：

- 发送给 daemon 作为 terminal authorization。
- 直接发送给 Hub/Relay 代替 admission/lease。
- 被 core-v2 或 RemoteSessionAcceptor 解析。

### 3.2 DeviceIdentity

用途：daemon 证明自己是已配对的安全设备。

v1 使用 daemon 本地生成的 Ed25519 长期密钥对：

- private key 永不离开 daemon 安全存储。
- public key 可以注册到 Control Plane 并包含在端到端 `DeviceHello` 中。
- `DeviceFingerprint` 是规范化 public key 的稳定摘要。
- DeviceID 是目录/路由 ID，不能替代 fingerprint 验证。

### 3.3 CapabilityGrant

用途：授权 grant 持有者访问 daemon 的指定 scope。

Grant 由 daemon DeviceIdentity 签名，至少包含：

```text
version
grant_id
issuer_device_id
issuer_device_fingerprint
scope_kind
scope_value
issued_at
not_before
expires_at
revocation_id
nonce
signature
```

scope v1 仅允许：

- daemon-wide protocol scope；
- single-terminal scope；
- machine-events scope。

Grant 是 bearer capability：持有原始 grant 即代表被授权。为降低重放面，它必须仅存于客户端安全凭据存储，并只在 DTLS DataChannel 内用于 challenge proof。普通 endpoint 配置只保存 `grant_ref`。

禁止：

- 放入 SDP、ICE candidate、Hub admission、HTTP Authorization、gRPC metadata、URL、二维码解析服务或云端数据库。
- 由 Hub/Web Controller 验签后向 daemon 声明“已授权”。
- 用账号订阅状态扩大 grant scope。

### 3.4 HubAdmissionTicket

用途：允许一个 daemon 或 client 在短时间内使用指定 Hub 的 signaling 服务。

由 Control Plane 服务签名，至少包含：

```text
version
ticket_id
issuer
audience_hub_id
principal_kind        # daemon | client
account_id
device_id
managed_session_id
target_device_id      # client ticket 必填
allowed_operations    # presence | offer | answer | candidate
issued_at
expires_at
key_id
signature
```

约束：

- 短 TTL，目标 Hub 和 ManagedSession 绑定。
- Hub 使用缓存的 Control Plane 公钥离线验签。
- ticket 不包含 terminal ID、scope 或 CapabilityGrant。
- daemon 与 client ticket 权限不同；client 不能注册任意 device presence。

### 3.5 RelayLease

用途：允许一个指定 WebRTC session 在有限时间和配额内使用 Managed Relay。

由 Control Plane entitlement service 签名，至少包含：

```text
version
lease_id
issuer
audience_relay_pool
account_id
managed_session_id
client_device_id
target_device_id
region
path_kind             # single_relay | relay_mesh
route_id              # relay_mesh 时必填
route_version         # relay_mesh 时必填
client_edge_relay_id  # relay_mesh 时必填
daemon_edge_relay_id  # relay_mesh 时必填
max_internal_transit
not_before
expires_at
max_bytes
max_bitrate_kbps
max_concurrency
credential_binding_id
key_id
signature
```

约束：

- session-specific、region/route-specific、短 TTL。
- `single_relay` 只授权一个 Relay；`relay_mesh` 只授权 lease 中的两个 Edge Relay、route version 和受限 internal transit 数。
- Relay/TURN 可以从私有派生密钥和 `credential_binding_id` 验证 principal-specific 短期 credential，但不得把派生 seed 或长期 shared secret 下发给任何 endpoint。
- Client 与 daemon 只获得各自 Edge Relay credential；Relay 间 tunnel 使用独立服务身份，不能把 endpoint credential 复用为内部节点身份。
- lease entitlement 不表达 terminal scope。
- refresh 必须重新经过 entitlement/quota 判断，不能无限续用旧 lease。

## 4. 信任边界

```text
AccountAccessToken  -> local Cloud Companion custody, Control Plane audience only
HubAdmissionTicket  -> Hub only
RelayLease          -> Relay/TURN only
DeviceIdentity proof <-> Client and daemon E2E only
CapabilityGrant       -> Client secure store and daemon E2E only
```

任何服务接收到不属于自己的 credential type 都必须拒绝并避免记录 credential body。公开类型应使用不同 envelope tag 和 audience，防止“看起来都是 token”导致误用。桌面 Cloud Companion 可以保管 AccountAccessToken 并短期转交 caller-specific admission/lease credential，但禁止接收 CapabilityGrant 或 DeviceIdentity private key。

## 5. 端到端 DataChannel 授权协议

### 5.1 前置条件

- WebRTC PeerConnection 已完成 ICE 和 DTLS。
- DataChannel 必须 reliable、ordered，并使用约定 label `termx-protocol`。
- 客户端已从 endpoint 配置获得 expected DeviceID、pinned DeviceFingerprint 和 `grant_ref`。
- daemon 未因为 Hub 声明或 admission ticket 预先创建 core-v2 protocol session。

### 5.2 协议帧

授权帧使用独立 versioned envelope，不能与 termx protocol frame 混淆：

```text
AuthEnvelope {
    protocol: "termx-remote-auth"
    version: 1
    message_type
    auth_session_id
    payload
}
```

DataChannel 中每条授权消息编码为：

```text
ASCII "TXRA" || deterministic protobuf(AuthEnvelope)
```

- protobuf schema 固定在 `termx-proto/remoteauthpb/remote_auth.proto`。
- 单帧最大 64 KiB；错误 magic、错误版本、空/多义 payload 和任意层级 unknown field 一律拒绝。
- canonical input 不使用 JSON、map 或本地结构体字段拼接；Go/Kotlin/Swift 必须使用 schema 中独立的 canonical message 和 deterministic protobuf bytes。
- `CapabilityAccepted` 后立即切换为原有 termx protocol frame；不得继续解析 `AuthEnvelope`，也不得为旧 auth 格式保留 fallback。

v1 消息：

- `DeviceHello`
- `CapabilityOpen`
- `CapabilityAccepted`
- `CapabilityRejected`

### 5.3 DeviceHello

daemon 在 DataChannel 打开后首先发送：

```text
DeviceHello {
    auth_session_id
    device_id
    device_public_key
    device_fingerprint
    server_nonce
    daemon_dtls_certificate_fingerprint
    issued_at
    signature
}
```

`signature` 使用 Ed25519 覆盖 `DeviceHelloSignatureInput` 的 deterministic protobuf bytes：

```text
DeviceHelloSignatureInput {
    protocol
    version
    auth_session_id
    device_id
    device_public_key
    device_fingerprint
    server_nonce
    daemon_dtls_certificate_fingerprint
    issued_at_unix_nano
}
```

公开 WebRTC adapter 必须从已建立连接的真实 `DTLSTransport` 取得 fingerprint：daemon 读取本端 DTLS 参数，client 对 Pion 返回的对端原始证书计算 SHA-256。不能只信任 Hub/Companion 转发的 SDP fingerprint 字段。

客户端验证：

1. message version 和时间窗口；
2. public key 计算出的 fingerprint 与 message 一致；
3. fingerprint 与 endpoint pin 一致；
4. DeviceID 与 expected target 一致；
5. DeviceIdentity signature 有效；
6. message 中 daemon DTLS fingerprint 与当前连接实际 peer certificate fingerprint 一致。

第 6 步将长期 DeviceIdentity 绑定到本次 DTLS channel，防止恶意 Hub 终止两条 WebRTC 连接并透明转发授权消息。任一验证失败立即关闭 DataChannel，错误分类为 `device_identity_mismatch`，不得自动覆盖 pin。

### 5.4 CapabilityOpen

客户端从安全存储读取原始 grant，先本地校验格式、签名、issuer fingerprint 和 expiry，再发送：

```text
CapabilityOpen {
    auth_session_id
    grant
    client_nonce
    proof
}
```

`proof` 使用成熟 HMAC-SHA-256 实现。HMAC key 是去除首尾空白后的原始 grant UTF-8 bytes，message 是下面 `CapabilityProofInput` 的 deterministic protobuf bytes：

```text
CapabilityProofInput {
    protocol = "termx-remote-auth"
    version = 1
    auth_session_id
    server_nonce
    client_nonce
    daemon_dtls_certificate_fingerprint
    grant_sha256
}
```

这不把 bearer grant 变成客户端身份凭据；它只证明发送者当前持有完整 grant，并将授权 frame 绑定到本次 channel/challenge，避免直接重放历史 `CapabilityOpen`。

### 5.5 daemon 验证顺序

daemon 必须按以下顺序 fail closed：

1. auth session、nonce 和本次 DTLS fingerprint 一致且未使用。
2. grant envelope 和算法版本受支持。
3. grant 签名由当前 DeviceIdentity 验证通过。
4. issuer DeviceID/fingerprint 指向当前 daemon。
5. `not_before`、`expires_at` 有效。
6. `revocation_id` 未撤销。
7. HMAC proof 有效。
8. scope 可以无歧义映射为 core-v2 `TransportScope`。

验证成功后发送 `CapabilityAccepted`，其中只返回 grant ID、规范化 scope summary 和 auth session ID；随后才把同一个 DataChannel 交给 termx protocol framing。

验证失败发送稳定错误类别后关闭 channel。错误 detail 不得泄漏 grant 是否属于其他已存在 terminal。

### 5.6 Protocol 切换

`CapabilityAccepted` 是协议切换点：

```text
remote auth frames -> CapabilityAccepted -> termx protocol frames
```

- 切换前出现 termx protocol frame：拒绝并关闭。
- 切换后出现 remote auth frame：拒绝并关闭。
- 每个 DataChannel 只对应一个 capability scope；scope 变化必须建立新 channel/session。
- reconnect 必须重新执行完整设备证明和 challenge，不复用上次 accepted 状态。
- `termx-shared/remoteauth/handshake_test.go` 固化 v1 grant、DeviceHello signing bytes 和 CapabilityOpen proof 的跨平台十六进制向量；其他语言实现必须逐字节匹配。

## 6. 配对与 grant 交付

### 6.1 单向配对

daemon owner 生成 grant，客户端通过本地二维码、受控文件或明确的近端通道导入。二维码 payload 可以包含：

- endpoint metadata；
- DeviceID 与 DeviceFingerprint；
- Hub/Control Plane locator；
- 原始 CapabilityGrant。

二维码解析必须在客户端本地完成，不得上传第三方或 TermX 云端解析。导入后原始 grant 立即写入 Keychain/Keystore/file credential store，配置只记录 `grant_ref`。

### 6.2 云端辅助配对

Team/Pro 可以提供配对审批和加密投递便利，但必须满足：

- Control Plane 不获得可解密的原始 grant。
- grant 使用客户端目标密钥端到端封装后才可暂存。
- 服务端只保存一次性 encrypted envelope、expiry 和审计 metadata。
- v1 若未建立可靠的客户端公钥模型，则不实现云端 grant 投递，继续使用本地扫码/导入。

不得为了“先做出来”让 Web Controller 暂存明文 grant。

## 7. Hub admission 协议

### 7.1 daemon presence

daemon 使用 `principal_kind=daemon` 的短期 ticket 打开 signaling stream。Hub 验证：

- issuer、signature、audience、expiry；
- ticket DeviceID 与 presence DeviceID 一致；
- operation 包含 presence/answer/candidate；
- 同 ticket/session 的 replay 和并发策略。

presence 只包含 DeviceID、protocol version、candidate capability 和短 TTL heartbeat，不包含 terminal inventory。

### 7.2 client offer

client 使用绑定 target DeviceID 和 ManagedSessionID 的 ticket 发 offer/candidate。Hub 只将消息路由到对应 daemon presence，并返回 opaque signaling result。

禁止字段：

- `session_token` 表达 grant；
- `Authorization: Bearer <CapabilityGrant>`；
- terminal ID/list；
- core-v2 scope；
- subscription plan detail。

### 7.3 signaling 日志

允许记录：ticket ID hash、ManagedSessionID、SignalingSessionID、Hub ID、DeviceID hash、SDP size、candidate type、耗时和错误类别。

禁止记录：Authorization body、完整 SDP 默认内容、ICE credential、grant、设备私钥或 DataChannel payload。调试采样完整 SDP 必须由受控短期开关显式启用并自动脱敏 credential。

## 8. Relay/TURN 安全

### 8.1 租约验证

Relay 必须验证 lease signature、audience、region、session、expiry 和限制。TURN username/credential 必须与 LeaseID 和 expiry 绑定，服务端可以从私有派生密钥验证，但客户端不能获得派生主密钥。

### 8.2 限流与终止

Relay 可以按 lease 执行：

- 最大并发 allocation/channel；
- 会话总字节；
- 上下行速率；
- expiry；
- account/device 风控 denylist。

终止只影响 Relay path。Relay 不发送“terminal unauthorized”，也不调用 daemon revoke API。

### 8.3 UsageEvent

```text
UsageEvent {
    event_id
    lease_id
    managed_session_id
    relay_id
    route_id
    path_kind
    hop_id
    sequence
    interval_start
    interval_end
    bytes_up
    bytes_down
    active_seconds
    termination_reason
    signature
}
```

要求：

- `(relay_id, lease_id, sequence)` 幂等。
- 单调 sequence，允许延迟补报。
- Control Plane 拒绝重复、回退或越界事件。
- Relay Mesh 可以有多个 hop-level event，但结算必须按 `route_id + managed_session_id` 聚合为一次用户 session，不能按 hop 重复计算同一字节。
- usage 不包含 IP 以外不必要的 payload metadata；IP 保留策略单独受隐私政策约束。

## 9. 撤销与失效

| 对象 | Owner | 失效方式 | 影响 |
| --- | --- | --- | --- |
| AccountAccessToken | Control Plane | session revoke/expiry | 无法获取新云服务票据 |
| DeviceIdentity | daemon + Control Plane directory | device reset/revoke | 旧 fingerprint 必须重新配对 |
| CapabilityGrant | daemon | revoke ID/expiry | 新 protocol session 被拒绝 |
| HubAdmissionTicket | Control Plane/Hub | 短 TTL/replay deny | 对应 signaling session 失败 |
| RelayLease | Control Plane/Relay | expiry/quota/revoke | Relay path 拒绝或结束 |

订阅取消不等于 grant 撤销；grant 撤销也不等于账号封禁。Control Plane 可以停止签发新的 paid RelayLease，但不得写 daemon grant revoke store。

## 10. 密钥与算法治理

- DeviceIdentity v1 使用 Ed25519。
- 摘要和 HMAC v1 使用 SHA-256/HMAC-SHA-256。
- Control Plane ticket/lease 签名算法在 RP004 固定；必须支持 `key_id`、轮换、重叠验证窗口和紧急撤销。
- 所有 canonical encoding 必须在公开 contract 中固定，并有跨 Go/Kotlin/Swift/TypeScript fixture。
- token 比较、proof 比较和 signature 验证使用库提供的常量时间能力。
- 不接受 `alg=none`、调用方自选算法或未识别 critical field。

## 11. 安全日志与指标

所有 credential 默认标记 secret，结构化 logger 必须拒绝序列化：

- AccountAccessToken body；
- CapabilityGrant body；
- HubAdmissionTicket body；
- RelayLease credential material；
- TURN password；
- device private key。

可记录 hash/reference ID 和错误类别。安全指标包括 ticket 验签失败、replay、fingerprint mismatch、grant revoke/expiry、Relay quota deny 和异常 usage sequence，但不包含 terminal 内容。

## 12. 必须具备的安全 harness

- Hub request/response fixture 扫描，证明不存在 grant/terminal/scope 字段。
- malicious Hub harness：替换 offer target 后，客户端因 DeviceFingerprint/DTLS binding 失败拒绝。
- malicious companion harness：替换 target、SDP/ICE、lease 或 route 后仍不能绕过 pinned DeviceFingerprint、DTLS binding、RelayLease audience 和 daemon capability 验证。
- replay harness：历史 `CapabilityOpen` 在新 nonce/channel 上失败。
- scope harness：single-terminal grant 无法 List/Attach 其他 terminal。
- revoke/expiry harness：daemon 拒绝且不创建 core-v2 session。
- relay harness：无 lease、错 region、过期、超额和重复 usage event 均 fail closed。
- relay-mesh harness：错 Edge、错 route version、超出 transit 数、未授权邻接和 hop event 重复均 fail closed；session 账单只聚合一次。
- subscription harness：套餐失效不影响 local/SSH，也不改变已有 grant 的 daemon 验证结果。
- cross-platform canonical fixture：Go/Kotlin/Swift 对 DeviceHello、grant 和 proof 计算一致。
- log redaction harness：所有 credential 类型不能进入默认日志输出。

## 13. 安全验收不变量

- Hub、Relay 和 Control Plane 的内存、日志、请求 schema 与数据库中都不存在原始 CapabilityGrant。
- daemon 只在本次 DataChannel 的设备证明和 challenge 通过后创建 scoped protocol session。
- client 只信任 pinned DeviceFingerprint，不信任 Hub 返回的 label、DeviceID 或 online 状态。
- Hub admission 与 Relay lease 都是短期、受 audience 约束、不可替代 terminal capability。
- Relay 不能解密 DataChannel，也不能改变 scope。
- 旧 bearer grant-in-signaling、非空 Bearer 即通过、长期 agent token 和共享 TURN credential 全部删除且没有兼容 fallback。
