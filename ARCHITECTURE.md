# AnyTTY Cloud 当前架构

## 1. 文档地位

本文是当前开发协议和实现边界的唯一总览。连接安全、性能审查和故障矩阵见 [CONNECTION_ARCHITECTURE.md](CONNECTION_ARCHITECTURE.md)。

仓库不维护开发期协议兼容层。协议、记录或测试与本文不一致时，必须删除旧实现并一次性升级开发数据，不能通过双协议分支继续保留。

## 2. 产品边界

AnyTTY Cloud 由三个运行角色组成：

- Controller：账号、订阅、设备注册、Edge 配置、Directory fallback、运营管理和用量结算。
- Edge：面向 daemon 和客户端的公网入口、内存 Presence、WebRTC 信令、TURN Relay 及 durable reservation journal。
- daemon：终端能力的最终所有者，持有 DeviceIdentity、AccessStore 和与客户端的端到端授权状态。

客户端不把 Controller 或 Edge 当作终端授权方。terminal、file、command 和 CapabilityGrant 只在客户端与 daemon 的端到端通道中处理。

## 3. 核心决策

1. daemon 只在首次注册时直接访问 Controller。
2. 注册成功后，daemon 持久化 Controller 签名的 `DaemonBindingClaims` 和与其摘要绑定的 `EdgeLocator`。
3. daemon 启动和重连直接访问记录中的 Edge。
4. pairing offer 携带 daemon 签名的短期 pairing route grant 和同一 Edge locator，客户端不经 Controller 即可配对。
5. 已授权客户端优先使用 credential 中缓存的 Edge locator。
6. 只有 locator 缺失、明确过期或 Edge 传输不可达时，客户端才访问 Controller Directory。
7. 每个 ClientGateway Relay session 必须通过当前 ready EdgeControl generation 向 Controller 提交强事务 reservation；Edge 不从 binding、locator 或本地状态创建商业授权。
8. 同一 EdgeControl 双向流承担状态投影、策略与证书控制、主动关闭命令，以及有界关联的 Relay reserve、renew、settle 和 query。

## 4. 拓扑

```text
                           account / operator / fallback
Client ----------------------------------------------------> Controller
   |                                                            ^
   | cached locator, ClientGateway                               | mTLS EdgeControl
   v                                                            |
 Edge <----------------------------------------------------------+
   ^
   | persisted locator, AgentGateway
   |
daemon

Client <========== WebRTC + DTLS + DataChannel ==========> daemon
```

Controller 不运输 SDP、ICE、terminal、file 或 CapabilityGrant。Edge 只运输 WebRTC 建连所需信令，不能读取端到端 terminal 数据。

## 5. 首次注册

1. daemon 使用一次性 enrollment code、DeviceIdentity public key 和指纹请求 challenge。
2. daemon 用 DeviceIdentity 私钥签 challenge。
3. Controller 在同一事务中消费 code 并创建持久 daemon identity。
4. Controller 按可用性和负载选择 owning Edge。
5. Controller 投影公开 `EdgeLocator`，对其确定性编码计算 SHA-256。
6. Controller 签发 `DaemonBindingClaims`，绑定 daemon、账号、设备公钥、owning Edge、locator 摘要、revision 和有效期；binding 不携带 Relay authority。
7. daemon 原子保存 version 2 enrollment record。

version 2 记录只包含：

- `daemon_id`、`account_id`。
- 完整签名 binding envelope。
- 完整 Edge locator protobuf。
- enrollment 时间。

加载记录时必须拒绝未知字段、错误版本、损坏 protobuf、身份不一致、Edge 不一致及 locator 摘要不一致。记录中不保存 Controller 地址、运行时 generation、session、TURN credential 或私钥。

## 6. daemon 日常连接

daemon 从记录解码 binding 和 locator，使用 locator 的独立 CA pool、SNI 和 endpoint 连接 Edge。`AgentHello` 携带：

- 签名 binding。
- binding payload 摘要、daemon ID、boot ID 和 connection ID 的 DeviceIdentity proof。
- 当前协议版本和软件版本。

Edge 使用从 EdgeControl 获得的 Controller verification key 验签，检查 target Edge、时间窗、revision 和 DeviceIdentity proof，然后把认证 claims 与当前 AgentGateway generation 一起放入内存状态。

AgentGateway 断开后，daemon 对同一 Edge 指数退避重连，不访问 Controller。当前实现没有跨 Edge 自动迁移；原 Edge 永久不可达时需要重新对齐 locator。

## 7. pairing

daemon 生成紧凑 `PairingClaimOffer`，其中只包含：

- 128-bit 一次性 claim、daemon ID、device public key 和过期时间。
- 首次连接 owning Edge 所需的 edge ID、endpoint、server name。
- Edge CA 根证书 DER 的 SHA-256 指纹，不包含 CA PEM。

客户端使用 CA 指纹校验 Edge 在 TLS handshake 中发送的完整证书链，然后直接建立 ClientGateway pairing stream。Edge 将 daemon identity、客户端公钥、claim 摘要、产品、session 和 generation 与当前在线 AgentGateway binding 对齐，并向 owning daemon 发起实时 `AgentAuthorize`。claim 本体只在通过 DTLS 建立的端到端通道中提交，Edge 和 Controller 都不能据此生成 terminal 权限。

daemon 原子把 claim 绑定到新的 ClientAccessIdentity，并通过 `PairingAccepted` 返回 CapabilityGrant、CloudRouteGrant 和完整 Edge locator。客户端验证 daemon identity 与签名后才写入 secure credential。

## 8. 已授权客户端连接

```text
secure credential
  -> 校验 locator 结构和 CloudRouteGrant envelope
  -> TLS 连接 Edge
  -> ClientGateway Hello + ClientAccessIdentity proof
  -> Edge 验 daemon grant 和当前 daemon Presence
  -> daemon AccessStore 实时预检
  -> offer / answer / ICE
  -> DTLS DataChannel
  -> daemon identity + CapabilityGrant 鉴权
  -> protocol Hello
  -> ReadyPeerSession
```

缓存命中时 Controller RPC 数为零。只有以下条件允许 Directory fallback：

- credential 没有 locator。
- Edge dial/TLS/HTTP2 在发送 ClientHello 前失败，并被包装为结构化 locator-unreachable 错误。
- Edge 明确返回位置不存在。

授权失败、daemon 拒绝、配额、Relay、协议或 DataChannel 错误不得触发 fallback。新的 locator 只有在端到端认证和 protocol Hello 完成后才写回 credential；写缓存失败不关闭已经成功的 session。

## 9. Relay reservation 和续期

Relay 与连接存活是不同状态。heartbeat 只证明传输活着；Controller 已提交的 reservation 决定 TURN allocation 是否仍可转发。`reservation_id` 同时是请求、grant、唯一 settlement 和 ACK identity，一个 ClientGateway session 固定占一个套餐并发 slot。

Edge 在发送 reserve 前写 `REQUESTED`，收到 grant 后写 `HELD_UNEXPOSED`，并在 ICE credential 离开 Edge 前最后写入 `EXPOSED`。续期先 durable 写 `RENEW_PENDING(sequence)`，丢失响应时以同一 sequence 重放；续期只推进短期 expiry，不增加 slot、hold 或字节预算。

同一 reservation 最多容纳四个 physical TURN allocation，它们只属于 Edge 本地 group，共享字节和速率 limiter。关闭单个 allocation 只累加 group counter；session 正常关闭先停止 admission，待 pending、active、closing 全部静止后写一个 aggregate settlement。Controller ACK 后才删除 journal；进程崩溃后的 `EXPOSED` 或 `CLOSING` 记录按 `RECOVERY_MAX` 重放，且不恢复 Relay authority。

Controller 不 ready 时，AUTO 只尝试 P2P，RELAY_ONLY 明确不可用，不能从 locator、binding、delegation 或本地续期获得新的 Relay authority。

## 10. 真值与持久化

Controller PostgreSQL 中只有绑定真实订阅周期的 `usage_periods` 和长期保留终态的 `relay_reservations` 是 Relay 授权与结算真值；运营 aggregate 仅可作为同事务可重建投影。实时 Presence 只存在于 Controller Directory 内存，由 Edge snapshot/delta 重建。

Edge 内存保存当前 AgentGateway、ClientGateway、Relay group/allocation、认证 claims、correlation 和有界 mailbox。Edge 磁盘只保存运行配置、证书、可验证配置缓存和未 ACK 的 reservation journal。

daemon 磁盘保存 DeviceIdentity、AccessStore 和 enrollment record。客户端 secure store 保存 ClientAccessIdentity、CapabilityGrant、CloudRouteGrant 和公开 locator。

任何一方都不得在日志中记录私钥、enrollment/claim token、完整 signed envelope、CapabilityGrant、TURN credential、SDP、ICE candidate、terminal 或 file payload。

## 11. EdgeControl

EdgeControl 使用 mTLS 双向流。协议版本 5 的 payload 包含：

- Hello/Welcome 和 binding verification keys。
- 原子 snapshot、增量、heartbeat 和 resync。
- desired config 与证书热更新。
- 精确 generation 的 daemon/session 关闭命令与结果。
- Relay reserve、renew、settle、query response，并按 reservation ID 有界关联。

Controller 断开不影响已有 P2P/DataChannel，但禁止新的 Relay reservation 和 renewal；现存 allocation 只能使用已提交 grant 的剩余短期 authority。

## 12. 资源和并发

- 公网 Edge gRPC 单消息上限 1 MiB，单连接并发 stream 上限 256。
- Runtime actor 对 Agent、session、pending signaling、Relay、mailbox 和 delta buffer 设硬上限。
- SDP、candidate、grant 和 offer 在创建 PeerConnection 或 Relay allocation 前校验大小和数量。
- 每个双向 stream 只有一个 writer；所有 envelope 使用严格单调 `stream_seq`。
- generation fence 防止旧连接、旧命令或迟到清理删除新状态。
- 队列满时快速失败，不能无限阻塞或无界增长。

## 13. 故障语义

| 故障 | 当前行为 |
| --- | --- |
| Controller 停止，Edge 仍运行 | daemon 可重连同一 Edge；已授权客户端和 pairing 可直连；AUTO 只走 P2P，RELAY_ONLY 不可用 |
| Controller 与 EdgeControl 断开 | Edge `ready=false`，数据面 listener 和既有认证状态继续运行 |
| public Controller 被墙，国内 Edge 可达 | 缓存连接不受影响；仅 locator fallback 失败 |
| Edge 重启且 Controller 不可达 | verification key 尚未持久化，新的 daemon admission 会 fail closed |
| owning Edge 不可达 | 客户端只对传输/位置错误 fallback；daemon 当前不做自动跨 Edge 迁移 |
| locator 缓存写失败 | 已完成端到端认证的 session 保持成功 |
| settlement/ACK 丢失 | 唯一 aggregate fact 保留在 journal，恢复后按同一 reservation ID 幂等重放 |

## 14. 安全门禁

- Controller enrollment code 一次性消费。
- 所有身份 proof 使用 Ed25519 和 domain separation。
- binding、route grant 和 pairing grant 都覆盖各自目标和时间窗。
- binding 间接签名 daemon 保存的完整 Edge locator；短期 pairing grant 绑定 offer 中的 locator 摘要。
- 长期 CloudRouteGrant 不绑定固定 locator，确保 Controller 认证 fallback 后可以迁移 Edge 而不永久回源。
- 私有 Edge CA 不与系统 root pool 混用。
- Directory 在返回位置前验证 daemon grant 和 ClientAccessIdentity proof。
- Edge 在 daemon binding 过期后拒绝新的 session 和 Relay。
- terminal 权限必须再次通过 DataChannel 内 daemon 身份和 CapabilityGrant 校验。

尚未达到上线门禁的项目记录在连接审查文档中。开发阶段不能用兼容分支规避这些门禁。

## 15. 验证基线

必须持续通过：

- `go test ./...`
- `./scripts/check-generated-code.sh`
- Controller 停止后的 cached P2P/pairing、AUTO P2P-only 和 RELAY_ONLY fail-closed 集成测试。
- binding/locator 任意篡改拒绝测试。
- 协议 descriptor contract 测试，确保被删除的 RPC 和 payload 不会重新出现。

任何协议变更必须同时更新 proto、生成代码、descriptor baseline、实现、集成测试和本文；仓库不接受只为旧开发数据保留的解析或 fallback 分支。
