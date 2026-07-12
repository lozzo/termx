# Hub 边缘授权与 Control Plane 降载计划

状态：CLOUD009 进行中；2026-07-12

## 1. 目标

Control Plane 是账号、设备 ownership、订阅、计费和全局策略的持久真值，但不能成为每次 managed 连接的同步依赖。客户端启动、登录、token 刷新和 HubDirectory 刷新可以访问 Control Plane；daemon/client 注册到 Hub 后，presence、direct signaling、短期 EdgeManagedSession 和后续 Relay 准入由 Hub 的本地投影处理。

目标不是让客户端永远不知道 Control Plane，而是把高频连接流量与数据库读从 Control Plane 移除。Control Plane 不可用时，只要 Hub 已验证的本地快照仍在 `max_staleness` 内，已有 presence 和新的已授权 direct 连接都应继续工作。

## 2. 权威状态与凭据

- Control Plane 持久拥有 Account、DeviceOwnership、Pairing、Subscription、Revocation、HubDirectory 和策略 revision。
- 启动阶段签发的 edge token 使用非对称签名，至少绑定 `iss`、`aud`、`account_id`、`client_device_id`、`jti`、`iat`、`nbf`、`exp`、`auth_epoch` 和 key id。Hub 只持有验证公钥。
- edge token 只证明账号/client 会话，不能单独授权任意 target。Hub 的最终准入是 token claims 与本地 DeviceOwnership/Pairing/Revocation/Subscription 投影的交集，deny/revoke 优先。
- daemon presence 仍使用 fresh one-time challenge 和 DeviceIdentity proof。Hub 以同步得到的 daemon public key、DeviceID、AccountID 验证，daemon 自报 metadata 不是授权真值。
- HubDirectory 是 Control Plane 签名的有版本、签发时间、过期时间和 key id 的目录；客户端可缓存，但不得接受回滚版本。

## 3. Hub 投影与同步

Hub 的请求热路径只读内存投影，不直接查询 Control Plane 或数据库。投影包含 device ownership/public key、pairing、account auth epoch、订阅能力、带宽参数、kick/revoke 和区域 Relay budget。

Control Plane 向 Hub 提供按 revision 严格有序的 snapshot/delta 流。Hub 原子应用完整 revision；发现 gap、rollback、签名失败或未知 schema 时拒绝 delta，并在后台请求完整 snapshot。cache miss 必须 fail closed，禁止隐藏的同步 Control Plane fallback；后台可使用 batch、singleflight 和指数退避刷新。

生产 Hub 使用“内存热路径 + 原子持久化的已验证快照/WAL”。presence、signaling、replay 和 EdgeManagedSession 仍是易失短期状态。Hub 重启先恢复已验证快照，再由 client/daemon 重连；不能把易失 signaling 持久化成第二份 session truth。

## 4. 连接消息链路

```text
client startup -> Control Plane: login/refresh + signed edge token + signed HubDirectory
daemon startup -> Control Plane/Hub: refresh identity metadata when needed, then fresh proof presence
Control Plane -> Hub: ordered policy/device snapshot and deltas (background)

client -> Hub: edge token + target DeviceID + offer
Hub: offline token verify + local projection decision + active target presence lookup
Hub: create short-lived EdgeManagedSession and route offer
daemon -> Hub: answer on the authenticated presence stream
Hub -> client: answer
client <-> daemon: ICE/DTLS/DataChannel + daemon-owned CapabilityGrant validation
Hub -> Control Plane: asynchronous audit/usage summaries
```

`EdgeManagedSession` 由 Hub 拥有，绑定 authenticated client connection、client DeviceID、target DeviceID、active target presence、route intent 和短 TTL。Control Plane 不再逐连接创建 ManagedSession，也不为 daemon answer 签发票据。answer 只有在接收 offer 的已认证 presence stream 上才能完成对应 signaling session。

## 5. 故障语义

| 条件 | 新连接 | 已建立 DataChannel |
| --- | --- | --- |
| Control Plane 不可用，Hub 快照仍有效 | 允许本地授权的连接 | 不受影响 |
| 快照超过 `max_staleness` | managed 新连接 fail closed | 不主动中断；安全踢线策略另行显式定义 |
| cache miss、token 过期、auth epoch 不匹配 | fail closed，不同步回源 | 不扩大现有 capability |
| revision gap/rollback/签名失败 | 拒绝 delta，全量重同步前 fail closed 受影响主体 | 不改变 DataChannel 真值 |
| Hub 重启 | 恢复已验证快照后等待重连 | transport 断开并按客户端恢复策略重连 |

升级延迟是保守降级；订阅降级、security revoke 和 kick 必须在定义的同步 SLO 内生效。refresh token 只留在 Control Plane/Companion 安全存储，Hub 不得无限续签账号会话。

## 6. Relay 与用量边界

CLOUD009 只迁移 direct。CLOUD010 为每个区域提供受限委派 issuer 与签名 RelayBudget/PolicySnapshot；不得把 Control Plane root signing key 复制到 Hub。Hub 在预算内签发 session-specific lease/credential，Relay 验证区域 issuer。用量采用带 `event_id` 和单调 sequence 的 at-least-once durable outbox，Control Plane 幂等结算。

## 7. 迁移切片

1. CLOUD009：建立版本化 Hub 授权投影、edge token 离线验证、Hub-owned EdgeManagedSession 和 presence-bound answer；证明 Control Plane listener 关闭后仍可新建 direct。
2. CLOUD010：迁移 single Relay lease/预算和 durable usage outbox，并验证中断与补报。
3. CLOUD011：desktop/Official Android 启动与刷新拿 edge token/HubDirectory，完成真实公网和 ADB 中断验收。

多区域一致性、Relay Mesh、Kubernetes、生产数据库选型和复杂计费不属于本计划当前切片。
