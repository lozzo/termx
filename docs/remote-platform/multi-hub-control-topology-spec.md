# TermX 多 Hub 控制面、实时拓扑与远程管理设计

状态：多 Hub 控制面稳定设计基准

活动切片、实现顺序和完成证据只记录在仓库根目录 `workflow.md`。本文定义一个逻辑 Control Plane/Controller Panel 管理多个纯内存 Hub、daemon 长连接、实时连接拓扑和远程控制的稳定架构。

## 1. 目标与边界

TermX Cloud 需要支持：

- 一个逻辑 Control Plane authority 和一个面向用户的 Controller Panel。
- 多个独立部署、可横向扩容的 Hub server。
- 每个 daemon Companion 与唯一 owning Hub 建立经过 DeviceIdentity 证明的长 Presence 连接。
- Hub 的授权投影、Presence、signaling、managed session、topology 和 command execution state 只存在内存，不落盘。
- Control Plane 持久拥有账号、设备、Subscription、Entitlement、Hub registry、HubAssignment、CommandOutbox、审计和 Web topology projection。
- Hub 主动连接 Control Plane，完成首次 full snapshot、实时 delta、周期 reconciliation、command delivery 和 runtime event 回报。
- 用户从 Web 查看自己账号下的 daemon、client、控制关系和 P2P/Relay 数据路径。
- 用户可以临时 Kick Presence、撤销 Cloud device、关闭指定 managed PeerSession，并通过 daemon-owned deny-only command 撤销 terminal grant。

当前不做：

- 多 Control Plane authority、多区域数据库和跨区域一致性。
- Hub-to-Hub signaling forwarding。
- 一个 daemon 同时在多个 Hub 建立 active Presence。
- Hub 无中断迁移、Relay Mesh、全球动态路由或通用消息总线。
- 把 terminal inventory、terminal ID、命令、屏幕、文件名或文件内容同步到 Cloud。
- Browser 直接调用 Hub，或 Hub 直接访问 Control Plane 数据库。

## 2. 总体架构

```text
                           Browser
                              |
                              v
                    Controller Panel/API
                              |
                              v
                    Control Plane authority
 Account / Device / Subscription / Entitlement / HubDirectory
 HubAssignment / CommandOutbox / Audit / TopologyProjection
                              |
             authenticated bidirectional control streams
             +----------------+----------------+
             |                |                |
           Hub A            Hub B            Hub C
        memory only      memory only      memory only
        policy cache     policy cache     policy cache
        presence map     presence map     presence map
        session map      session map      session map
        topology map     topology map     topology map
             |                |                |
        daemon Presence  daemon Presence  daemon Presence
```

控制路径和数据路径必须分开表达：

```text
控制关系：Client -- signaling --> owning Hub -- Presence --> Daemon

P2P 数据：Client <========== DTLS DataChannel ==========> Daemon

Relay 数据：Client <=> Relay <=> Daemon
```

P2P DataChannel 不经过 Hub。Web topology 和 Proto 需要分别使用 `control_owner_hub_id` 与 `observed_data_path`，不得用一条边暗示 P2P 数据经过 Hub。

“一个 Controller Panel”表示只有一个逻辑管理入口和 Control Plane authority。后续可以部署多个无状态 HTTP replica，但它们必须共享同一持久真值和 revision。

## 3. 真值归属

| 状态 | Owner | 持久化 | Web 语义 |
| --- | --- | --- | --- |
| Account、Subscription、Entitlement | Control Plane | 是 | 当前商业状态 |
| Device ownership/public key/revoke/auth epoch | Control Plane | 是 | 脱敏设备状态 |
| Hub registry/control identity/config | Control Plane | 是 | fleet 状态 |
| daemon -> HubAssignment | Control Plane | 是，lease + epoch | 当前 assignment |
| Hub policy projection | Hub memory | 否 | revision/freshness |
| daemon Presence | owning Hub memory | 否 | availability projection |
| signaling session | owning Hub memory | 否 | connecting projection |
| authenticated managed PeerSession | daemon Go runtime | 否 | Hub/CP 只保存投影 |
| Relay allocation | Relay runtime | 否 | lease/allocation projection |
| Command | Control Plane CommandOutbox | 是 | authority/delivery/execution |
| terminal CapabilityGrant | owning daemon AccessStore | 是 | 仅 opaque revoke reference |
| Web topology | Control Plane projection | 是，带 freshness | 不是活跃连接真值 |

Control Plane topology projection 只能表示“最后一次经过校验的观察”。任何 Web 状态必须携带 observation 与 freshness，不能把旧记录伪装为实时状态。

## 4. Hub registry、实例身份与控制流

### 4.1 Hub registry

Control Plane 保存：

```text
hub_id
region
public_url
control_identity_fingerprint
enabled
capacity_class
last_control_seen_at
```

- Hub 使用部署证书或等价非对称 identity 主动连接 Control Plane。
- Control Plane 根据认证公钥/证书 fingerprint 查询唯一 `hub_id`；`HubHello` 自报的 Hub ID、region 和 URL不能覆盖 registry。
- 同一个 `hub_id` 同时只允许一个 active `control_stream_generation`。新流建立后旧流被 fencing，旧流的 command result/runtime event 不再更新当前投影。

### 4.2 control stream handshake

```text
Hub -> CP: authenticated transport
CP -> Hub: fresh control challenge
Hub -> CP: HubHello(challenge proof, software version, last projection revision)
CP -> Hub: ControlStreamReady(stream_generation, full snapshot or resume decision)
```

后续 envelope 必须绑定：

```text
hub_id
control_stream_generation
sender_role
sender_sequence
issued_at
expires_at
payload
```

Hub -> Control Plane 与 Control Plane -> Hub 分别维护独立严格递增 sequence，不能共用一个计数器。双向认证保护当前 stream；stream generation、sender role 和 sequence 防止跨重启重放。需要离线转发给 daemon 的 command 仍必须由 Control Plane 独立签名，不能只依赖 Hub transport。

## 5. HubAssignment 与全局单活 fencing

### 5.1 Assignment lease

每个 daemon 同时只能拥有一个有效 assignment lease：

```text
device_id
account_id
hub_id
assignment_epoch
not_before
expires_at
```

- daemon edge credential、Presence request、runtime report 和 topology event 都必须携带 `assignment_epoch`。
- Hub 在 Presence、signaling 和 command 热路径校验 assignment Hub audience、epoch 和绝对过期时间。
- 同 Hub、同 epoch 下只允许一个 active Presence。新的 Presence 必须显式 replacement 旧 Presence，并生成新的 `presence_session_id`；不保留“替换或拒绝”二义性。

### 5.2 严格单活迁移

Control Plane 只有满足以下任一条件才能签发更高 epoch 的新 assignment：

1. 旧 Hub 对 `FenceAssignment` 返回已关闭旧 Presence 的确认；或
2. 旧 assignment lease 已达到绝对 `expires_at`。

旧 Hub 不可达时必须等待 lease 到期，接受有界 failover 延迟，不能在旧 lease 仍有效时承诺严格单活并同时分配新 Hub。

旧 epoch 的 Presence、inventory、topology event、command result 只能进入审计，不能修改当前 projection。

### 5.3 client target-owner routing

第一阶段不做 Hub-to-Hub forwarding：

1. 客户端通过签名 HubDirectory/resolve 得到 target daemon 当前 assignment。
2. 客户端直接连接 owning Hub。
3. edge token audience、target assignment 和 Hub ID 必须一致。
4. assignment 变化后旧 Hub 拒绝新 signaling；客户端刷新目录后连接新 Hub。

## 6. Hub policy 同步

### 6.1 每 Hub 独立 projection revision

不同 Hub 只接收与自身 assignment、region 和 service budget 有关的最小投影，因此使用每 Hub 独立 revision：

```text
hub_id
projection_revision
previous_projection_revision
snapshot_digest
generated_at
expires_at
```

- Control Plane 为每个 Hub 单独生成连续 `projection_revision`。
- 与 Hub 无关的全局变化不会制造该 Hub 的 revision gap。
- 相同 revision + 相同 digest 的 reconciliation snapshot 是幂等成功。
- 相同 revision + 不同 digest、revision rollback、previous mismatch 或未知 operation 全部拒绝。

### 6.2 同步方式

- 每次 Hub 进程启动必须接收 `FullProjectionSnapshot`。
- 账号、设备、Entitlement、revoke、auth epoch 和 assignment 变化后发送 ordered delta。
- 定期发送 digest reconciliation；发现差异后发送 full snapshot。
- 请求热路径只读内存投影，不同步查询 Control Plane。
- quota contract 尚未完成前，snapshot 不提前定义第二套 `regional_budgets`；相关字段随 `CLOUDP004` 按 Proto contract 增加。

## 7. Hub 纯内存启动与故障代价

正式 Hub 不配置 `EdgeSnapshotStore`，也不持久化 policy、Presence、signaling、topology、command dedupe 或 runtime event。

```text
process start
  -> authenticate control stream
  -> receive/verify FullProjectionSnapshot
  -> atomically publish memory projection
  -> readiness=true
  -> accept daemon Presence and client signaling
```

首次 full snapshot 完成前：

- liveness 可以为 true。
- readiness 必须为 false。
- daemon Presence 和 managed connection fail closed 或只等待有界时间。
- 不使用旧磁盘 snapshot、固定账号或 allow fallback。

明确代价：

- Hub 进程仍运行且内存 projection 未超过 `max_staleness` 时，Control Plane 暂时不可用不影响本地准入。
- Hub 重启且 Control Plane 不可用时无法恢复服务。
- Control Plane 恢复后 Hub 重新 full sync，daemon 重连并上报完整 runtime inventory。

## 8. daemon Presence transport

当前 HTTP Presence 是 Hub -> daemon 的 server stream。第一阶段不重写为全双工 transport，而是明确拆分：

```text
下行长流：OpenPresence response stream
  ready / offer / candidate / daemon_control_command / closed

上行有序 API：ReportDaemonRuntime
  inventory_snapshot / lifecycle_event / command_result / heartbeat
```

`ReportDaemonRuntime` 必须绑定当前 edge credential、Hub ID、assignment epoch、presence session ID、daemon runtime generation 和 registry revision。每个 daemon Presence 对上行报告串行发送；Hub 按 revision 接收，网络重试使用相同 report ID 幂等处理。

未来可以把二者换成真正的双向 stream，但不得改变 Proto 消息、owner、revision 和 ack 语义。

## 9. daemon-owned ManagedPeerSession registry

signaling answer 不等于连接 ready。daemon 侧必须新增独立 registry，不能复用客户端 `client/runtime.SessionOwner`。

最小记录：

```text
managed_session_id
session_incarnation
source_cloud_device_id
authenticated_client_fingerprint
opaque_grant_reference
observed_data_path
state
close_handle
opened_at
```

### 9.1 生命周期边界

```text
signaling accepted
-> PeerConnection/DataChannel ready
-> DeviceIdentity/channel binding complete
-> CapabilityGrant accepted
-> protocol Hello complete
-> registry READY

close requested or transport ended
-> stop protocol/resource owners
-> close DataChannel/PeerConnection
-> wait Done
-> registry CLOSED
```

只有完成 protocol Hello 后才上报 READY。daemon 必须在 protocol session、DataChannel 和关联 resource 均结束后才确认 CLOSED。

### 9.2 runtime generation 与线性化

daemon process 启动时创建新的 `daemon_runtime_generation`。registry 对 inventory 和增量共用一个严格递增 `registry_revision`：

```text
InventorySnapshot(runtime_generation, registry_revision, sessions[])
LifecycleEvent(runtime_generation, registry_revision, session event)
```

- Hub 在同一 assignment epoch + Presence session + runtime generation 内只接受递增 revision。
- 先收到 revision 11 后再收到 inventory revision 10，旧 inventory 被拒绝。
- 新 Presence 建立时 daemon 首先发送完整 inventory；空 inventory 明确关闭/替换上一 incarnation 的全部活跃投影。
- daemon process generation 变化表示旧进程 session 全部不再可确认，除非新 generation inventory 显式报告。

## 10. 拓扑模型

### 10.1 Presence 状态

Presence projection 必须分开 availability 和 freshness：

```text
availability: ONLINE | OFFLINE | UNKNOWN
freshness: FRESH | STALE
observed_at
fresh_until
source: HUB_OPEN | HUB_CLOSE | HUB_TOPOLOGY_SNAPSHOT | CONTROL_STREAM_LOST
assignment_epoch
presence_session_id
```

- `OFFLINE` 只来自当前 assignment/presence 的明确 close、revoke 或 replacement 证据。
- Hub control stream 中断、Hub 重启或观察超时只能变为 `UNKNOWN/STALE`，不能未经关闭证据标记 offline。
- daemon Presence 断开不代表已建立 P2P DataChannel 一定关闭。

### 10.2 PeerSession projection

```text
managed_session_id
session_incarnation
client_device_id
daemon_device_id
control_owner_hub_id
assignment_epoch
presence_session_id
daemon_runtime_generation
observed_data_path: P2P | SINGLE_RELAY
state: CONNECTING | READY | CLOSING | CLOSED | UNKNOWN
freshness
connected_at
observed_at
fresh_until
close_reason?
relay_lease_id?
```

禁止包含 terminal ID、terminal inventory、grant body/scope、SDP、ICE candidate、IP、TURN credential、命令或文件 metadata。

### 10.3 事件校验与 projection 替换

Control Plane 不信任 Hub 提交的 `account_id`。它根据持久真值验证：

- client 和 daemon 当前属于同一账号。
- daemon 当前 assignment 指向该 Hub 和 assignment epoch。
- event 来自当前 control stream generation。
- managed session tuple 与 Hub 已报告的 signaling admission 一致。
- Relay event 与已验签 RelayLease claims 一致。

活动 projection 的稳定主键是：

```text
(daemon_device_id, managed_session_id, session_incarnation)
```

新 assignment epoch、Presence session 或 daemon runtime generation 的完整 inventory 具有该 daemon 当前 projection 的替换权：

- snapshot 中存在的 session 变为当前状态。
- snapshot 中缺失的旧 active session 标记 `superseded/unknown`，不能继续显示 ready。
- 旧 epoch、旧 Presence 或旧 runtime generation 的迟到事件只写审计，不覆盖当前 projection。

Hub control stream 重连后必须先发送 `HubTopologySnapshot`，再发送增量，修复断线期间丢失的 CLOSED 事件。

## 11. CommandOutbox 与状态语义

### 11.1 持久命令

```text
command_id
parent_operation_id?
account_id
actor_user_id
kind
target_hub_id
target_device_id?
target_managed_session_id?
expected_assignment_epoch?
expected_presence_session_id?
expected_session_incarnation?
expected_auth_epoch?
created_at
expires_at
```

命令结果拆成三个维度：

```text
authority_result:
  NOT_APPLICABLE | COMMITTED | REJECTED

delivery_state:
  PENDING | HUB_RECEIVED | RUNTIME_RECEIVED | EXPIRED

execution_state:
  PENDING | APPLIED | ALREADY_SATISFIED | PARTIAL | REJECTED | UNKNOWN

observed_effect:
  PRESENCE_OFFLINE | SESSION_CLOSED | RELAY_CLOSED | GRANT_REVOKED | UNKNOWN
```

持久 revoke 已提交但运行时断开未确认时，必须显示 `authority=COMMITTED, execution=PENDING/UNKNOWN`，不能把整个操作误报失败，也不能声称 daemon 已经下线。

一个 authority operation 可以拥有多个 child enforcement command。parent 保存持久商业/安全变更结果，child 分别绑定精确 Hub/Presence/session；parent 根据 child 结果聚合 `APPLIED/PARTIAL/EXPIRED`，不能用单个 `target_hub_id` 代表跨 Hub fan-out。

### 11.2 重放 fencing

- `KickPresence` 绑定精确 `assignment_epoch + presence_session_id`。
- `CloseManagedPeerSession` 绑定 `managed_session_id + session_incarnation`。
- daemon command/result 同时绑定 command ID、assignment epoch、Presence session 和 runtime generation。
- 目标已经 replacement 或自然关闭时返回 `stale_target` 或 `already_satisfied`，不能操作新对象。
- Hub 内存 dedupe 丢失后，Control Plane 可以重发；目标 fencing 保证旧命令不影响新 Presence/session。

## 12. 远程操作

### 12.1 KickPresence

- 只关闭精确 Presence incarnation 和它尚未完成的 signaling。
- 不修改 enrollment、auth epoch、Entitlement 或 CapabilityGrant。
- daemon 可以重新认证上线。
- Hub 返回匹配 presence ID 的结果；旧命令不能踢掉后来重连的新 Presence。

### 12.2 RevokeCloudDevice

1. Control Plane 持久写 revoked/auth epoch，`authority_result=COMMITTED`。
2. 生成新 per-Hub projection revision。
3. 撤销该设备 refresh session。
4. 如果目标是 daemon，向它的 owning Hub 创建一个精确 Presence/session enforcement child command。
5. 如果目标是 client，按当前 topology 枚举它在不同 Hub/daemon 上的全部 active managed session，并为每个精确 session 创建 child close command。
6. Hub/runtime ack 后分别更新 child delivery/execution/effect，再聚合 parent operation。

即使某个 Hub 不可用，持久 revoke 仍然成功；已知 child 可以显示 `PARTIAL/EXPIRED`。之后发现的旧 client credential 或新 signaling 继续由 auth epoch/revoke policy 拒绝。Cloud revoke 不等于 terminal grant revoke。

### 12.3 CloseManagedPeerSession

```text
Web -> CP CommandOutbox
-> owning Hub
-> CP-signed DaemonControlCommand via Presence
-> daemon registry 精确查找 incarnation
-> close protocol/resources/DataChannel
-> wait Done
-> independent DaemonCommandResult
-> Hub/CP update command
```

拓扑 `PeerSessionClosed` 事件不能代替 command ack。CommandResult 至少包含：

```text
command_id
managed_session_id
session_incarnation
assignment_epoch
presence_session_id
daemon_runtime_generation
result
closed_registry_revision
completed_at
```

### 12.4 RevokeTerminalGrant

daemon enrollment 明确表示 daemon owner 信任 TermX Cloud account owner 发起 deny-only administration；unenroll 后不再接受新 Cloud command。

- 只有账号 owner 可以从 Web 发起自己 daemon 的 terminal grant revoke。
- 平台运营角色默认不能创建、扩大或撤销 terminal grant。
- Web 只能选择 daemon 先前投影的 opaque revoke reference，不能上传 grant body。
- Cloud client DeviceID 与 `ClientAccessIdentity` 不等价；opaque reference 由 daemon 绑定到本地 grant/subject fingerprint。
- daemon 原子写 AccessStore revoke 并关闭关联 session 后返回 receipt。
- already revoked 返回幂等 `ALREADY_SATISFIED`。

Control Plane 不能直接写 AccessStore，也不能签发 grant、增加 scope 或延长 expiry。

### 12.5 Terminal access reference projection

Web 不能从 active PeerSession 猜测 grant，也不能要求用户手工输入 reference。daemon AccessStore 必须生成最小脱敏管理投影：

```text
daemon_device_id
opaque_revoke_reference
client_label
subject_fingerprint_summary
state: ACTIVE | REVOKED | EXPIRED
issued_at
expires_at
access_projection_revision
```

- 不包含 grant body、scope、client public key、terminal ID 或私钥。
- daemon 通过当前 authenticated Presence 的上行 runtime API 发送完整 `TerminalAccessInventorySnapshot`；revision 与 daemon AccessStore mutation 单调对应。
- Hub 只转发并绑定 assignment epoch/Presence/runtime generation；Control Plane 重新校验 daemon ownership 和当前 assignment。
- Control Plane 保存账号隔离的最新投影和 freshness；旧 epoch/Presence/revision 不能覆盖当前记录。
- 没有 active session 的 grant 仍然可以出现在管理投影中。
- 只有账号 owner 可以查询自己 daemon 的 projection 并创建 `RevokeTerminalGrant` command。

## 13. daemon command 端到端认证

Hub 只能转发 daemon-authoritative command，不能伪造。Control Plane 使用 daemon control 专用 signing key 签发 deterministic Proto command，daemon 通过 enrollment 保存的 verification key ring 验证。

签名输入至少包含：

```text
domain_separator = TERMX_DAEMON_CONTROL_V1
command_id
command_kind
account_id
target_device_id
target_session_or_grant_ref
hub_id
assignment_epoch
auth_epoch
presence_session_id
daemon_runtime_generation?
issued_at
expires_at
control_key_id
```

- 错 daemon、错 account、错 Hub、错 epoch、错 Presence、过期或 replay 全部拒绝。
- `KickPresence` 是 Hub 本地命令，不需要 daemon command 签名；Close session 和 grant revoke 必须端到端验签。
- daemon command key 与 edge token、Relay lease、usage event key 使用不同 domain/key role。

## 14. Relay allocation revoke

Relay 需要独立 authenticated control stream 或等价固定管理通道；Hub 不直接操作 Relay 内存。

```text
CP CommandOutbox
-> Relay control stream
-> Relay allocation registry
-> close every allocation bound to lease/session
-> final usage drain to durable outbox
-> RelayCommandResult
-> Control Plane usage settlement
-> release remaining reservation
```

Relay registry 必须支持按 `lease_id` 和 `managed_session_id` 定位全部 allocation/connection handle。结果拆分为：

```text
peer_session_close
relay_allocations_close
usage_drain
reservation_settlement
```

只有所有强制步骤完成才是 `APPLIED`；部分完成显示 `PARTIAL`。没有 Relay ack 时不能声称流量已立即切断。

## 15. Web API、页面与权限

### 15.1 页面

用户账号中心：

- 自己的 client/daemon device。
- daemon assignment region/Hub label。
- Presence availability + freshness。
- client 与 daemon 的 signaling 控制关系。
- P2P 或 Relay 数据路径。
- session state、开始时间、last observed、Relay bytes。
- Kick Presence、Revoke Cloud Device、Close Session、Revoke Terminal Access。

运营页面：

- Hub fleet、control stream、readiness、projection revision/freshness。
- Hub 的 daemon/session/allocation 数量和少量固定容量指标。
- 账号、设备、命令、quota deny 和审计查询。

普通用户只看到 region/Hub label 和自己的 topology，不看到其它租户统计、Hub 内部地址或完整 fleet。

### 15.2 权限矩阵

| 操作 | Account Owner | Operator Readonly | Operator Admin |
| --- | --- | --- | --- |
| 查看自己设备/topology | 允许 | 按工单授权只读 | 允许只读 |
| 查看完整 Hub fleet | 禁止 | 允许 | 允许 |
| Kick 自己 daemon Presence | 允许，近期重认证 | 禁止 | 允许，强审计 |
| Revoke 自己 Cloud device | 允许，近期重认证 | 禁止 | 允许，强审计 |
| Close 自己 managed session | 允许 | 禁止 | 允许，强审计 |
| Revoke terminal grant | 仅账号 owner且 daemon 已 opt-in | 禁止 | 禁止默认操作 |

所有 destructive Web 请求要求账号隔离、CSRF、近期重新认证和稳定拒绝码。`actor_user_id` 既用于审计，也必须经过授权判断。

## 16. Proto-first contract

建议新增：

```text
proto/cloudpb/cloud_hub_control.proto
proto/cloudpb/cloud_topology.proto
proto/cloudpb/cloud_management.proto
```

Hub/control：

```text
HubHello / ControlStreamReady
FullProjectionSnapshot / PolicyDelta / ReconciliationDigest
HubAssignment / FenceAssignment
HubControlEnvelope / HubRuntimeEnvelope
HubCommand / HubCommandResult / RelayCommandResult
DaemonControlCommand / DaemonCommandResult
ReportDaemonRuntimeRequest/Response
TerminalAccessInventorySnapshot
```

Topology：

```text
PresenceProjection
ManagedPeerSessionProjection
PeerSessionInventorySnapshot
PeerSessionLifecycleEvent
HubTopologySnapshot
Availability / Freshness / ObservationSource
```

Web/Operator API：

```text
ListAccountDevicesRequest/Response
ListAccountTopologyRequest/Response
GetManagedSessionRequest/Response
ListDaemonTerminalAccessRequest/Response
CreateManagementCommandRequest/Response
GetManagementCommandRequest/Response
ListManagementCommandsRequest/Response
ListHubFleetRequest/Response
GetHubStatusRequest/Response
ManagementActorProjection
ManagementErrorDetail
PageCursor / filters / freshness fields
```

Browser HTTP 使用 Proto JSON projection。Go、Kotlin 和 TypeScript 不得维护平行业务 DTO；数据库 row 和内部 runtime struct 通过显式 mapping 转换。

## 17. 稳定错误

```text
unauthenticated
forbidden
recent_auth_required
not_found
stale_target
assignment_changed
presence_changed
session_incarnation_changed
policy_unavailable
command_expired
command_partial
runtime_unavailable
relay_unavailable
temporary
```

不同错误不能互相伪装：entitlement deny、target offline、projection stale、terminal authorization deny 和 command 未执行必须保持独立分类。

## 18. 故障行为

| 故障 | 行为 |
| --- | --- |
| CP 短暂不可用，Hub 内存 policy 有效 | Presence 和允许的新 managed connection 继续；新 Web command 无法提交或保持持久 pending |
| Hub 重启，CP 可用 | full sync 后 ready；daemon Presence 重连并发送 inventory |
| Hub 重启，CP 不可用 | 不 ready，不恢复磁盘 snapshot |
| Hub control stream 断开 | `max_staleness` 内继续本地准入；恢复后先发 full topology snapshot |
| delta gap/rollback/signature failure | 保留当前完整 projection；请求 full snapshot；过期后 fail closed |
| owning Hub 不可用 | CP 等待旧 assignment 被 fenced 或 lease expiry，再签发更高 epoch |
| daemon Presence 断开 | Presence 明确 close 可标 offline；P2P session 单独按 freshness/inventory 判断 |
| observation 超时 | `UNKNOWN/STALE`，不能直接变 offline |
| 旧 command 重放 | 精确 target fencing 返回 stale/already-satisfied，不影响新 Presence/session |
| Relay revoke 部分完成 | command=PARTIAL，逐项展示 allocation/usage/settlement 状态 |

## 19. 当前实现对照

已有：

- `private/cloud/hub.Service` 使用内存 map 持有 Presence、signaling、challenge 和有界队列。
- daemon 使用 Hub fresh challenge/DeviceProof 打开下行 Presence stream。
- Hub 离线验证 edge token、ownership、revoke、auth epoch 和 managed direct/Relay policy。
- Web 可以显示 daemon online/offline 和 revoked。
- 单进程 staging `RevokeCloudDevice` 会更新目录、refresh session、policy revision 并调用 `Hub.RevokeDevice`。
- Relay 已有 lease enforcement、usage event 和 durable outbox。

缺失：

- 独立多 Hub registry、control identity 和 per-Hub control stream。
- HubAssignment lease/epoch/fencing。
- 每 Hub projection revision、delta 和 reconciliation。
- 正式 Hub 纯内存装配。
- daemon-owned ManagedPeerSession registry 和 lifecycle hooks。
- 上行 `ReportDaemonRuntime`、inventory/event 线性化。
- CP topology validation、full replacement 和 Web topology API。
- daemon AccessStore 的 opaque terminal access inventory 与 Web 查询 API。
- durable CommandOutbox 与精确 target fencing。
- CP-signed daemon control command。
- Relay control stream、allocation registry 和精确 revoke ack。
- 当前 Web online bool 不能表达 unknown/stale。

旧 `hub-edge-control-plan.md` 中“生产 Hub 持久 snapshot/WAL”的目标由本文纯内存设计取代；该文档只保留历史背景。

## 20. 实现顺序

多 Hub 基础与 Cloud 产品能力存在交叉依赖，必须交错推进，不能先完成全部 HUB 切片再做全部产品切片：

1. `HUB001`：Hub/control/topology/management Proto、parent/child command、terminal access projection 与 daemon registry contract。
2. `CLOUDP001`：PlanCapability 与 Entitlement，提供 policy projection 输入。
3. `HUB002`：Hub registry、identity、assignment fencing、纯内存 full/delta sync。
4. `HUB003`：daemon ManagedPeerSession registry、runtime report 和 topology projection。
5. `CLOUDP002`：账号、Subscription 与交易状态机。
6. `CLOUDP003`：managed P2P Entitlement 与 concurrency admission。
7. `HUB004`：CommandOutbox、KickPresence、daemon/client RevokeCloudDevice fan-out、CloseManagedPeerSession。
8. `HUB005`：CP-signed deny-only RevokeTerminalGrant。
9. `CLOUDP004`：Relay billing-period quota 与 reservation。
10. `CLOUDP005`：durable UsageLedger 与 settlement。
11. `HUB006`：Relay control stream、allocation revoke 和复合 command result。
12. `CLOUDP006`：用户账号中心与运营管理面。
13. `HUB007`：双 Hub 控制面、拓扑和命令 E2E。
14. `CLOUDP007`：Android/Web development 全产品 E2E。

## 21. 最终验收

必须证明：

1. 两个独立纯内存 Hub 从同一 Control Plane 建立唯一 control stream identity。
2. daemon 只在有效 assignment lease 的 owning Hub 建立 active Presence。
3. 旧 Hub 未 fence 时 Control Plane 不签发重叠 assignment；旧 epoch event 不能覆盖新 projection。
4. Control Plane 停止后，运行中 Hub 在有效 projection/assignment 窗口内仍可完成新 managed P2P。
5. Hub 重启且 Control Plane 不可用时不 ready，不读取旧磁盘 snapshot。
6. daemon registry 在 Hello 后上报 READY，完整关闭后上报 CLOSED。
7. inventory/event 使用统一 registry revision，空 inventory 能清除旧 projection。
8. Web 分开显示 signaling 控制关系和 P2P/Relay 数据路径。
9. Web 对 online/offline/unknown 与 fresh/stale 的显示符合证据，不把超时伪装 offline。
10. KickPresence 不影响新 Presence；RevokeCloudDevice 持久生效并阻止重连。
11. client device revoke 对跨两个 Hub 的 active session 生成独立 child command，parent 正确聚合 partial/complete。
12. CloseManagedPeerSession 只有精确 daemon CommandResult 后才成功。
13. Web 可以列出没有 active session 的 opaque terminal access reference；RevokeTerminalGrant 由 daemon AccessStore 执行，Cloud 只能 deny，不能 grant。
14. Relay revoke 关闭所有 lease-bound allocation、drain usage 并 settlement reservation；部分完成显示 PARTIAL。
15. 所有 Web/Hub/topology/command API proto-first，账号隔离、CSRF、近期重认证和审计通过。
16. Local、Direct 和 SSH 在 Control Plane/Hub 故障或 Cloud revoke 后继续可用。

## 22. 审核记录

本文初稿分别经过四个相互独立的只读审核：

- 分布式架构与无盘故障恢复。
- 安全、账号隔离和 deny-only authorization。
- Go runtime、Presence、PeerSession 与 Relay 生命周期。
- 产品管理面、状态可解释性和 Proto-first API。

初审指出的 assignment 双活、per-Hub revision、command replay、inventory 线性化、控制/数据路径混淆、freshness、daemon command 签名、跨账号 topology、P2P/Relay 虚报、跨 Hub client revoke 和 opaque access reference 等问题均已写入本文和 `workflow.md`。四个 reviewer 复审结论均为 `PASS`，没有当前设计阻塞 finding。

不阻塞的 deferred observation 只包括：实现时使用 timer 主动收敛 assignment expiry、后续评估 Presence 真双向 transport、生产数据保留期和复杂 fleet scheduling；这些不得扩大 `HUB001-HUB007` 当前范围。
