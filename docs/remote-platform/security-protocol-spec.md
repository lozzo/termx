# TermX Remote Platform 安全与协议规范

状态：CONN002 client-bound authorization v2 基线

版本：v2

日期：2026-07-15

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

## 3. 凭据与身份类型

### 3.1 EdgeAccessToken 与 RefreshSecret

用途：account/client 或 daemon 以短期签名 edge token 访问指定 Hub；refresh secret 只用于向 Control Plane 低频轮换 edge token。

属性：

- issuer：Control Plane authority。
- audience：固定 Hub ID，并绑定 account、device、principal kind、auth epoch 和 HubDirectory。
- edge token TTL 当前为 8 小时；Hub 离线验签后仍须与本地 signed policy 取交集。
- refresh secret 是 256-bit 随机单次 bearer，只进入 OS credential store/Android Keystore；Control Plane 只持有 SHA-256 与 kind/account/device/expiry，轮换后旧 secret 重放失败。

禁止：

- 发送给 daemon 作为 terminal authorization。
- 把 refresh secret 发送给 Hub、Relay、WebView、日志或配置。
- 用 edge token 代替 RelayLease 或 daemon CapabilityGrant。
- 被 core-v2 或 RemoteSessionAcceptor 解析。

### 3.2 DeviceIdentity

用途：daemon 证明自己是已配对的安全设备。

每个 daemon 使用一份全局、稳定的 Ed25519 长期密钥对；local、Direct WebRTC TCP、SSH WebRTC TCP 和 managed WebRTC 不得各自生成身份：

- private key 永不离开 daemon 安全存储。
- DeviceIdentity 首次创建由跨进程文件锁串行化；同一安全域不能因两个 daemon 并发启动生成两把长期 key。
- public key 可以注册到 Control Plane 并包含在端到端 `DeviceHello` 中。
- `DeviceFingerprint` 是规范化 public key 的稳定摘要。
- DeviceID 是目录/路由 ID，不能替代 fingerprint 验证。

### 3.3 ClientAccessIdentity

用途：客户端证明自己持有某个 Endpoint 专用的授权私钥。

约束：

- 每个客户端、每个 Endpoint 独立生成 Ed25519 key pair；不能复用 Cloud account、安装身份或其他 daemon 的 key。
- private key 与绑定 grant 一起进入 OS secure store、Android Keystore 加密存储或 owner-only file credential store。
- 普通 endpoint registry 只保存 credential reference；二维码、Cloud、Hub、Relay、日志和 Web storage 都不能接收 private key。
- `SubjectKeyFingerprint` 是 public key 的稳定 fingerprint。复制 grant 文本但没有对应 private key，不能完成 capability handshake。

### 3.4 PairingTicket

用途：允许一个新 `ClientAccessIdentity` 在短时间内向 owning daemon 请求绑定 grant。

PairingTicket 由 DeviceIdentity 签名，至少包含：

```text
version
ticket_id
issuer_device_id
issuer_device_fingerprint
scope_ceiling
issued_at
not_before
expires_at
grant_lifetime_seconds
nonce
max_redemptions = 1
signature
```

Ticket 不能访问 terminal、history 或 file protocol，有效期最长 24 小时；其兑换得到的 grant 最长一年。ticket 只存在于 daemon 签名的 deterministic protobuf `EndpointBootstrapBundleV2`，没有第二套 JSON bundle 或独立 token envelope。daemon `AccessStore` 必须先持久化 canonical bundle digest，再对外返回；兑换时在一个原子 mutation 中绑定 client public key、生成 grant claims、保存结果摘要和撤销索引。

### 3.5 CapabilityGrant

用途：授权 grant 持有者访问 daemon 的指定 scope。

Grant 由 daemon DeviceIdentity 签名，至少包含：

```text
version
grant_id
issuer_device_id
issuer_device_fingerprint
subject_key_fingerprint
scope
issued_at
not_before
expires_at
revocation_id
nonce
signature
```

CapabilityGrant v2 的基础 scope 仅允许：

- daemon-wide protocol scope；
- single-terminal scope；
- machine-events scope。

文件权限必须逐项附着在 daemon-wide scope；`ManageClientAccess` 是独立 capability，不能由 daemon-wide 或文件权限隐式推出。

当前实现只接受 CapabilityGrant v2。Grant 必须绑定 `SubjectKeyFingerprint`，并且只能与对应 `ClientAccessIdentity` signature 一起，在完成 channel binding 的端到端认证握手内提交给 owning daemon；v1 bearer/HMAC envelope 和 bearer-only fallback 均 fail closed。

禁止：

- 放入 SDP、ICE candidate、Hub signaling envelope、HTTP Authorization、gRPC metadata、URL、二维码解析服务或云端数据库。
- 由 Hub/Web Controller 验签后向 daemon 声明“已授权”。
- 用账号订阅状态扩大 grant scope。

### 3.6 Hub 本地 Presence 与 EdgeManagedSession

用途：Hub 基于 EdgeAccessToken、本地 signed policy 和 active Presence ownership 创建短期 signaling 状态，不再使用 Control Plane Hub ticket。

约束：

- Presence challenge 由 Hub 创建、单次消费并限制 TTL；daemon 必须用 policy 中登记的 DeviceIdentity public key完成 Ed25519 proof。
- `EdgeManagedSession` 绑定 authenticated client connection、account、target daemon 与 active Presence。
- daemon answer 必须从接收 offer 的已认证 Presence 返回。
- cache miss、token/auth epoch 过期、错误 public key、challenge replay、撤销、投影断档或超过最大陈旧窗口均 fail closed，且禁止同步回源。

### 3.7 RelayLease

用途：允许一个指定 WebRTC session 在有限时间和配额内使用 Managed Relay。

由 Control Plane 授权的受限 regional issuer 在签名 RelayBudget 内签名；Control Plane root key 不复制到 Hub。至少包含：

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

### 3.8 Control Plane 服务凭据编码

Control Plane 固定 edge access、edge policy 与 Relay lease 的签名算法和 canonical encoding：

- 签名算法固定为 Ed25519，不携带 `alg` 字段，也不接受调用方选择算法。
- Edge access 和 edge policy 使用各自独立 domain separator 与 schema。
- Relay lease token 为 `TXRL1.<base64url(canonical-json)>.<base64url(signature)>`。
- signature input 是前两段的原始 ASCII bytes；base64url 不带 padding。
- canonical JSON 使用 UTF-8、无额外空白、整数 Unix 秒和 schema 定义顺序；未知字段、重复 operation、非 canonical operation 顺序和尾随 JSON 一律拒绝。
- `key_id` 选择离线验签公钥；Control Plane 同时发布新旧公钥形成重叠验证窗口，紧急撤销会立即拒绝该 key 签发且尚未过期的凭据。
- edge access、edge policy、`TXRL1` 与 usage 的 `TXUE1` domain separator 不可互换，不能跨用途验签。

固定测试向量由 `private/cloud/control-plane/servicecredential/credential_test.go` 维护；向量使用公开测试 seed 和虚构身份，不包含生产密钥或真实账号数据。
- Client 与 daemon 只获得各自 Edge Relay credential；Relay 间 tunnel 使用独立服务身份，不能把 endpoint credential 复用为内部节点身份。
- lease entitlement 不表达 terminal scope。
- refresh 必须重新经过 entitlement/quota 判断，不能无限续用旧 lease。

## 4. 信任边界

```text
EdgeAccessToken      -> local Cloud Companion/Android Keystore custody, Hub only
RefreshSecret        -> local credential store custody, Control Plane refresh endpoint only
RelayLease          -> Relay/TURN only
DeviceIdentity proof <-> Client and daemon E2E only
ClientAccessIdentity private key -> Client secure store and E2E proof only
PairingTicket        -> Owner-controlled QR/file and daemon PairingExchange only
CapabilityGrant       -> Client secure store and daemon E2E only
```

任何服务接收到不属于自己的 credential type 都必须拒绝并避免记录 credential body。公开类型应使用不同 envelope tag 和 audience，防止“看起来都是 token”导致误用。桌面 Cloud Companion 可以保管 EdgeAccessToken 与 RefreshSecret，但 refresh 只能交给 Control Plane，edge token 只能交给 Hub；Companion 禁止接收 CapabilityGrant 或 DeviceIdentity private key。

## 5. 端到端安全 Channel 授权协议

当前 wire contract 把 channel binding 抽象为 `kind + SHA-256 hash`，由 Direct/SSH/managed WebRTC 的 DTLS DataChannel 和 owner-only local Unix PairingExchange 共用同一 DeviceIdentity/client proof 状态机。真实 connector 与用户链路完成度只看 `workflow.md`，不能因为 helper 已存在就宣称 Route 已交付。

共同不变量：CapabilityGrant 和携带 PairingTicket 的 EndpointBootstrapBundle 只提交给 owning daemon；Control Plane、Companion、Hub、Relay 和 signaling 永远不得接收正文。

### 5.1 ChannelBinding

```text
ChannelBinding {
    kind              # direct_tls | dtls | local_unix
    binding_hash[32]
}
```

- `direct_tls`：对当前连接 daemon certificate DER 做 SHA-256；adapter 必须从实际 TLS connection state 读取。
- `dtls`：使用当前 WebRTC DTLS certificate 的规范化 SHA-256 digest；不能只信任 SDP、Hub 或 Companion 转发字段。
- `local_unix`：对 domain separator 与 canonical owner-only pairing socket path 做 SHA-256；它只允许 PairingExchange，不能进入普通 remote capability session。
- kind 与 hash 同时进入 daemon 和 client signature。错误 kind、全零 hash、配置地址或 signaling metadata 构造出的伪 binding 一律拒绝。

### 5.2 协议帧

```text
AuthEnvelope {
    protocol: "termx-remote-auth"
    version: 2
    auth_session_id
    exactly_one_payload
}
```

每条授权消息编码为 `ASCII "TXRA" || deterministic protobuf(AuthEnvelope)`：

- protobuf schema 固定在 `proto/remoteauthpb/remote_auth.proto`。
- 单帧最大 64 KiB；错误 magic、v1、空/多义 payload 和任意层级 unknown field 一律拒绝。oneof 在 protobuf unmarshal 前扫描原始 wire，重复同一 payload 或同时出现多个 payload 也必须拒绝，不能接受 generated parser 的“最后一个字段胜出”。
- canonical input 使用独立 protobuf message 和 deterministic bytes，不使用 JSON、map 或字段字符串拼接。
- v2 payload 包含 `DeviceHello`、`CapabilityOpen`、`PairingOpen`、`CapabilityAccepted`、`PairingAccepted` 和 `CapabilityRejected`。

### 5.3 DeviceHello

daemon 首先发送：

```text
DeviceHello {
    device_id
    device_public_key
    device_fingerprint
    server_nonce
    channel_binding
    issued_at_unix_nano
    signature
}
```

DeviceIdentity Ed25519 signature 覆盖 protocol/version、auth session、全部 identity 字段、server nonce、`ChannelBinding` 和 issued time 的 deterministic protobuf bytes。

客户端必须验证 message version/time、public key 与 fingerprint、endpoint pin、expected DeviceID、DeviceIdentity signature，以及 message binding 与当前 adapter 实测 binding 一致。任一失败立即关闭 transport，不得自动覆盖 pin 或尝试旧 auth。

### 5.4 CapabilityOpen

客户端从 secure store 原子读取 `ClientAccessIdentity + CapabilityGrant`，先验证 grant issuer、subject、scope 和有效期，再发送：

```text
CapabilityOpen {
    grant
    client_public_key
    client_nonce
    proof
}
```

`proof` 是 ClientAccessIdentity Ed25519 signature，覆盖下面 canonical input：

```text
ClientProofInput {
    protocol = "termx-remote-auth"
    version = 2
    auth_session_id
    server_nonce
    client_nonce
    channel_binding
    credential_sha256
    client_public_key
    open_kind = capability
}
```

daemon 必须确认 grant `SubjectKeyFingerprint` 等于 `client_public_key` fingerprint，再验证 signature。复制 grant、复制历史 open、替换 key 或跨 channel 转发都不能通过。

### 5.5 PairingOpen

PairingExchange 使用相同 challenge，但 `credential_sha256` 指向完整 canonical `EndpointBootstrapBundleV2` bytes，`open_kind = pairing`。`PairingOpen` 发送 `pairing_bundle + client_public_key + label + nonce + proof`，不再发送另一种 ticket 字符串。客户端必须先持久化 per-Endpoint ClientAccessIdentity，再发起兑换。

daemon `AccessStore` 是 ticket digest、key binding、可重建 grant claims、result/receipt digest、grant metadata 和 revoke 的唯一持久真值：

1. 首次兑换必须验签、未过期且 `MaxRedemptions=1`。
2. canonical bundle digest、key binding、grant claims 和 result digest 在一次原子文件替换中提交；state 由 DeviceIdentity 签名，不持久化二维码明文、grant body 或 receipt body。
3. 同一 ticket 与同一 key 只在 24 小时 delivery grace 内重建并返回逐字节相同的 grant/receipt，重试不写磁盘；超过 grace 后 fail closed。
4. 同一 ticket 的其他 key 返回 consumed；未消费的过期 ticket 返回 expired。
5. `PairingAccepted` 后 transport 立即关闭，不能切换到 terminal protocol。
6. 过期未消费 ticket、超过 delivery grace 的消费记录和长期过期 grant metadata 只在后续低频安全 mutation 中批量 compact；倒计时、普通连接、验证和 access list 不触发写入。
7. 同一 AccessStore 目录只能有一个进程 owner；第二 daemon 即使使用不同 socket 也必须启动失败。只读诊断 snapshot 不能替代 remote ingress owner。
8. grant/ticket 的有效期以 daemon 收到对应 `CapabilityOpen`/`PairingOpen` 的时间重新校验；发送 `DeviceHello` 的时刻不能冻结授权时间或让客户端跨过精确 expiry。
9. state 原子 rename 后的 chmod/目录 fsync 错误属于“已发布、耐久性未确认”；运行中 AccessStore 必须保留新内存真值并返回错误，不能回滚成与磁盘不同的 ticket/revoke 状态。
10. 客户端收到 `PairingAccepted` 后必须按响应实际接收时间再次验证新 grant；若锁等待或网络延迟已跨过 `expires_at`，不得把过期 grant 写入 secure store或替换旧 credential。

### 5.6 daemon capability 验证顺序

daemon 必须按以下顺序 fail closed：

1. auth session、双方 nonce、open kind 与当前 channel binding 一致。
2. 只接受 grant v2，DeviceIdentity signature 和 issuer 指向当前 daemon。
3. grant `not_before`、`expires_at` 有效，daemon-local `revocation_id` 未撤销。
4. client public key fingerprint 等于 `SubjectKeyFingerprint`。
5. ClientAccessIdentity Ed25519 proof 有效。
6. scope 可以无歧义映射为 core-v2 `TransportScope`；`ManageClientAccess` 单独映射。

签名正确但未登记在当前 AccessStore 的 grant 按 revoked/unknown fail closed，不能通过单独构造 `Revocations` 或 nil checker 绕过。所有远程 DTLS DataChannel handshake 在发送 `DeviceHello` 前都必须确认 AccessStore owner 可用；`local_unix` 只允许 owner pairing acceptor，generic session acceptor 在领域入口直接拒绝。

成功后 `CapabilityAccepted` 只返回 grant ID、subject fingerprint、规范化 scope summary 和 auth session。普通 capability 验证、access list 和 reconnect 只读内存状态，不刷新 last-seen、不清理记录、不写 Cloud/数据库或本地 AccessStore。

验证失败发送稳定错误类别后关闭 channel。错误 daemon fingerprint/issuer identity 在 Go/Android 均映射为 device identity mismatch；subject key mismatch 只表示 ClientAccessIdentity 不匹配。错误 detail 不得泄漏 grant 是否属于其他已存在 terminal。

### 5.7 Protocol 切换

`CapabilityAccepted` 是协议切换点：

```text
remote auth frames -> CapabilityAccepted -> termx protocol frames
```

- 切换前出现 termx protocol frame：拒绝并关闭。
- 切换后出现 remote auth frame：拒绝并关闭。
- 每个 transport 只对应一个 capability scope；scope 变化必须建立新 session。
- reconnect 必须重新执行 DeviceHello 和 client proof，不复用 accepted 状态。
- owner-only pairing listener 设置 pairing-only 模式，`CapabilityOpen` 在 accepted 或 core 切换前被拒绝。
- Go/Kotlin fixture 固化 v2 DeviceHello、ChannelBinding、ClientProof、canonical EndpointBootstrapBundleV2、错误 key/fingerprint、精确 expiry 边界、v1 拒绝和 scope summary；其他语言实现必须逐字节匹配。

## 6. 配对与 grant 交付

### 6.1 默认 PairingTicket 流程

daemon owner 通过认证后的 `remote.access.ticket.create` 管理 RPC 创建 ticket。静态 QR 使用 `termx://bootstrap?payload=<base64url deterministic protobuf>`，owner-only 文件保存同一 protobuf bytes；两者只允许包含：

- bundle version、显示 label；
- DeviceID 与 DeviceFingerprint；
- 短期一次性 PairingTicket。

不得包含长期 grant、ClientAccessIdentity private key、SSH 密码/私钥、Cloud token、Hub/Relay secret 或 route credential。二维码解析必须在客户端本地完成，不得上传第三方或 TermX 云端。

桌面导入顺序固定为：严格解析并验签 bundle，先要求 bundle 带有可移植 route 或本地已有同 identity endpoint，在跨进程 registry read-modify-write 锁内组装增量 candidate；随后在同一 credential ref 锁内持久化 ClientAccessIdentity、比较现有 grant 与 ticket scope ceiling，并且只在 scope 未扩大或用户显式确认后执行 PairingExchange；返回 grant 按响应接收时间通过 issuer/subject/scope/expiry 验证后与 canonical bundle digest 一起原子绑定，最后保存普通 registry。导入器不得因 bundle 无 route 凭空创建 managed Cloud route；registry 保存失败时保留可恢复的 secure credential，不删除已消费 ticket 对应的唯一 key。重试相同 bundle 时，若本地 digest 对应的 grant 仍有效，可直接完成 registry 恢复而不受 daemon delivery grace 限制；不同 bundle 仍必须重新兑换。并发 pair import 与 endpoint mutation 必须由同一 registry transaction lock 串行化，不能整文件覆盖丢失另一 Endpoint。registry 与 credential 锁等待、锁内 PairingExchange 和网络收发都必须传播根命令 context；`--timeout` 到期后释放锁并退出，不能无限阻塞后续客户端操作。新 grant 若扩大同一 credential ref 的现有 scope，CLI/App 必须在任何 daemon 兑换前取得显式用户确认，静默重试和二维码 metadata 不能自行提权。

### 6.2 离线与故障隔离

PairingTicket、DeviceIdentity、ClientAccessIdentity、AccessStore 和 capability handshake 都不依赖 TermX Cloud、账号订阅或 Control Plane 数据库。Direct、SSH 和 Android 的完整兑换入口按 `workflow.md` 推进，但必须复用同一 contract 和 daemon AccessStore，不能创建第二份授权真值。

Cloud/Companion/Hub/Relay 故障只能影响 managed route。已有 local/SSH/direct route 与未过期、未撤销的 bound grant 应继续工作；普通连接不因 Cloud 离线产生数据库写入或 fallback。

### 6.3 授权管理

local owner 拥有 `LocalOwner` 管理边界；远端 session 只有显式 `ManageClientAccess` 才能创建 ticket、列出脱敏记录或撤销 grant。`AllowDaemon`、terminal scope 和文件权限都不能隐式获得该能力。

access list 只返回 grant/revocation ID、subject fingerprint、client label、scope、issued/expiry/revoked time，不返回 ticket、grant body、client public key bytes 或私钥。撤销由 owning daemon 原子持久化并在重启后继续生效；删除客户端本地 credential 不等于撤销。

### 6.4 云端辅助配对

当前不实现 Cloud 明文 grant/ticket 托管。未来若提供审批或加密投递，服务端也只能保存目标 ClientAccessIdentity 加密的一次性 envelope、expiry 和审计 metadata；Control Plane/Web Controller 不得获得可解密 grant，账号授权不得扩大 daemon signed scope。

## 7. Hub 本地准入协议

### 7.1 daemon presence

daemon 使用 `principal_kind=daemon` 的短期 edge token 获取一次性 challenge，再以 DeviceIdentity proof 打开 signaling stream。Hub 验证：

- edge token issuer、signature、audience、expiry、account、device、principal 和 auth epoch；
- signed policy 中的 ownership、kind、public key、revoke 与 staleness；
- challenge/session/device/public key/signature/signed time 的完整绑定；
- challenge replay 和同 DeviceID active Presence 并发策略。

presence 只包含 DeviceID、protocol version、candidate capability 和短 TTL heartbeat，不包含 terminal inventory。

### 7.2 client offer

client 使用绑定 account/device/Hub 的 edge token请求 target DeviceID。Hub 与本地 policy 取交集后创建 EdgeManagedSession，只将消息路由到对应 daemon Presence，并返回 signaling result。

禁止字段：

- `session_token` 表达 grant；
- `Authorization: Bearer <CapabilityGrant>`；
- terminal ID/list；
- core-v2 scope；
- subscription plan detail。

### 7.3 signaling 日志

允许记录：edge token ID hash、ManagedSessionID、SignalingSessionID、Hub ID、DeviceID hash、SDP size、candidate type、耗时和错误类别。

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
    key_id
    signature
}
```

要求：

- `(relay_id, lease_id, sequence)` 幂等。
- 单调 sequence，允许延迟补报。
- Control Plane 拒绝重复、回退或越界事件。
- Relay Mesh 可以有多个 hop-level event，但结算必须按 `route_id + managed_session_id` 聚合为一次用户 session，不能按 hop 重复计算同一字节。
- usage 不包含 IP 以外不必要的 payload metadata；IP 保留策略单独受隐私政策约束。
- v1 signature input 为 `TXUE1.<base64url(canonical-json-without-signature)>` 的原始 ASCII bytes；event 的 `key_id` 必须与 Control Plane 注册的 Relay 部署身份匹配，签名算法固定为 Ed25519。

## 9. 撤销与失效

| 对象 | Owner | 失效方式 | 影响 |
| --- | --- | --- | --- |
| AccountAccessToken | Control Plane | session revoke/expiry | 无法获取新云服务票据 |
| DeviceIdentity | daemon + Control Plane directory | device reset/revoke | 旧 fingerprint 必须重新配对 |
| PairingTicket | daemon AccessStore | expiry/原子消费/delivery grace | 未绑定 key 不能再兑换；同 key 仅在 24 小时交付宽限内取回已提交结果 |
| CapabilityGrant | daemon | revoke ID/expiry | 新 protocol session 被拒绝 |
| EdgeAccessToken/Presence challenge | Control Plane/Hub | 短 TTL、auth epoch、challenge replay deny | 对应 Presence/signaling 失败 |
| RelayLease | Control Plane/Relay | expiry/quota/revoke | Relay path 拒绝或结束 |

订阅取消不等于 grant 撤销；grant 撤销也不等于账号封禁。Control Plane 可以停止签发新的 paid RelayLease，但不得写 daemon grant revoke store。

## 10. 密钥与算法治理

- DeviceIdentity、ClientAccessIdentity、CapabilityGrant v2、PairingTicket 和 challenge proof 使用 Ed25519。
- fingerprint、channel binding 和 credential digest 固定使用 SHA-256；remote auth v2 不使用 grant-as-HMAC-key。
- Control Plane ticket/lease 签名算法在 RP004 固定；必须支持 `key_id`、轮换、重叠验证窗口和紧急撤销。
- 所有 canonical encoding 必须在公开 contract 中固定，并有跨 Go/Kotlin/Swift/TypeScript fixture。
- token 比较、proof 比较和 signature 验证使用库提供的常量时间能力。
- 不接受 `alg=none`、调用方自选算法或未识别 critical field。

## 11. 安全日志与指标

所有 credential 默认标记 secret，结构化 logger 必须拒绝序列化：

- AccountAccessToken body；
- PairingTicket body；
- CapabilityGrant body；
- EdgeAccessToken、RefreshSecret 或 Presence challenge body；
- RelayLease credential material；
- TURN password；
- DeviceIdentity 与 ClientAccessIdentity private key。

可记录 hash/reference ID 和错误类别。安全指标包括 ticket 验签失败、replay、fingerprint mismatch、grant revoke/expiry、Relay quota deny 和异常 usage sequence，但不包含 terminal 内容。

## 12. 必须具备的安全 harness

- Hub request/response fixture 扫描，证明不存在 grant/terminal/scope 字段。
- malicious Hub harness：替换 offer target 后，客户端因 DeviceFingerprint/DTLS binding 失败拒绝。
- malicious companion harness：替换 target、SDP/ICE、lease 或 route 后仍不能绕过 pinned DeviceFingerprint、DTLS binding、RelayLease audience 和 daemon capability 验证。
- replay harness：历史 `CapabilityOpen` 在新 nonce/channel 上失败。
- client-bound harness：复制 grant、错误 client key、错误 daemon fingerprint 和错误 channel binding 全部失败。
- pairing harness：真实 daemon 覆盖 ticket 并发兑换、响应丢失幂等取回、open 收到时精确 expiry、错误 key、revoke、重启恢复，以及第二 daemon 共享 state 时因 owner lock 启动失败。
- write-budget harness：AccessStore state 不含 raw bundle/grant/receipt；普通 capability 验证与 access list 不替换文件，不写 Cloud/数据库，过期记录只随后续低频 mutation 批量 compact。
- transaction harness：未确认 scope expansion 不发起 PairingExchange；rename 后 durability 错误不回滚 AccessStore 内存；相同 bundle digest 可恢复 registry；并发 pair import 与 endpoint mutation 不丢配置。
- deadline harness：PairingAccepted 到达时 grant 已过期则不替换旧 credential；另一个进程持有 registry/credential lock 时，带根 timeout 的 CLI 按 deadline 退出；默认 daemon auto-start/Hello、PairingExchange connect 与 `PairingOpen` transport backpressure 分别停滞时，根 context 都会关闭未认证 transport、释放 registry/credential 锁并退出；DataChannel 的 in-flight `Send` 与并发 `Close` 必须按“先关闭底层 channel、再等待 send lock”收敛，不能形成 auth deadline 死锁。
- scope harness：single-terminal grant 无法 List/Attach 其他 terminal。
- revoke/expiry harness：daemon 拒绝且不创建 core-v2 session。
- relay harness：无 lease、错 region、过期、超额和重复 usage event 均 fail closed。
- relay-mesh harness：错 Edge、错 route version、超出 transit 数、未授权邻接和 hop event 重复均 fail closed；session 账单只聚合一次。
- subscription harness：套餐失效不影响 local/SSH，也不改变已有 grant 的 daemon 验证结果。
- cross-platform canonical fixture：Go/Kotlin 对 v2 DeviceHello、ChannelBinding、grant、EndpointBootstrapBundleV2 和 ClientProof 计算一致；Swift 接入时必须复用同一向量。
- log redaction harness：所有 credential 类型不能进入默认日志输出。

## 13. 安全验收不变量

- Hub、Relay 和 Control Plane 的内存、日志、请求 schema 与数据库中都不存在原始 PairingTicket、CapabilityGrant 或客户端私钥。
- daemon 只在本次远程 DTLS DataChannel 的设备证明、channel binding 和 capability challenge 全部通过后创建 scoped protocol session；connector 尚未完成时必须显式 unavailable，不得回退旧 transport。
- client 只信任 pinned DeviceFingerprint，不信任 Hub 返回的 label、DeviceID 或 online 状态。
- Edge access 与 Relay lease 都是短期、受 audience 约束、不可替代 terminal capability。
- Relay 不能解密 DataChannel，也不能改变 scope。
- daemon AccessStore 是 pairing/revoke 唯一持久真值且是 remote ingress 的必选依赖；普通连接、重连和 verification 不产生写入，Cloud 离线不影响其他 route。
- 旧 bearer grant-in-signaling、非空 Bearer 即通过、长期 agent token 和共享 TURN credential 全部删除且没有兼容 fallback。
