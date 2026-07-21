# Muxvia 多 Hub 控制面与 Cloud 产品技术实施规划

## 1. 文档定位

本文把以下稳定设计下沉为可直接执行的代码规划：

- `docs/remote-platform/multi-hub-control-topology-spec.md`：多 Hub、assignment、topology、CommandOutbox 和远程管理架构真值。
- `docs/remote-platform/cloud-product-spec.md`：账号、套餐、订阅、Entitlement、Relay quota、usage 和管理产品真值。
- `workflow.md`：当前分支活动切片、顺序、范围和测试准入的唯一驱动文件。

本文负责回答“具体改哪些 Proto、Go package、状态机、存储、HTTP 链路和测试”。它不改变上面两份规格的产品或安全决策。若实现发现设计冲突，先修正规格和 `workflow.md`，不得在代码中增加隐式 fallback。

## 2. 当前实现与迁移结论

当前仓库已经完成 Controller/Edge composition 与 Hub control 基础：

- `private/cloud/controller` 组合 Control Plane、Web catalog、PostgreSQL 与 public/internal/operator 三个 listener。
- `private/cloud/edge` 组合纯内存 Hub、Hub public signaling、Relay data listener、health listener 与唯一 Hub control client。
- `private/cloud/devcloud` 只负责启动一个 Controller 与两个独立 Edge 子进程，不再持有 Cloud 业务状态或通过 Go pointer 连接网络边界。
- Hub policy、assignment、Presence 和 signaling 不落盘；Edge 重启必须重新 full sync，Relay 只恢复未确认 usage outbox。
- Hub admission 显式消费当前 assignment epoch，projection remove/replace/expiry 只 fence 精确旧 epoch。
- daemon 已在 auth + protocol Hello 后注册 READY ManagedPeerSession，并在完整 teardown 后注册 CLOSED；单 reporter 上报完整 runtime inventory。
- Control Plane 已持久拥有 Hub registry、assignment、control generation、per-Hub projection head、topology replacement 和 parent/child CommandOutbox。
- Controller dispatcher 从 PostgreSQL 重试未完成 child；Hub/daemon 独立 result 才推进 delivery/execution，topology 观察不能冒充执行 receipt。
- 设备 ownership、auth epoch、revoke 与 Presence public key 已进入 PostgreSQL authority；Controller 重启不会用静态配置覆盖已提交 revoke。
- Controller 已持久拥有账号 verifier、单次 refresh session、versioned Subscription、Entitlement、订单、provider event journal 和交易审计。
- Controller public listener 已暴露 Proto JSON 注册、登录、refresh、logout、改密、checkout、显式测试付款、Subscription transition 和账号交易查询；测试付款默认关闭。
- Controller 已从持久 Account auth revision 与 Entitlement 构建 per-Hub signed policy；静态 `Config.Accounts` 已退出，账号/套餐变化按 assignment 只重发相关 Hub full projection。
- 旧 Web Controller 自有的账号密码、browser session、订单 map/本地数据库表、webhook 和直接 Entitlement 写入口已经删除；`CLOUDP006` 在当前 API 上重建完整用户/运营页面。

迁移原则：不重写已工作的 WebRTC、remote auth、terminal protocol 和 Presence 下行流。新增最小 owner/port，把单进程 direct call 替换为 Proto 网络链路；对应新路径通过后直接删除旧调用和旧 snapshot 路径。

## 3. 目标进程与依赖方向

development 最终只维护两类 Cloud 服务二进制：

```text
muxvia-cloud-controller
├── Control Plane
├── Controller/User/Operator API
├── Web Controller static assets
└── PostgreSQL persistent store
          |
          +---- HubControl ---- muxvia-cloud-edge A/B/...
          +---- RelayControl -- muxvia-cloud-edge A/B/...

muxvia-cloud-edge
├── Hub runtime: memory only
└── Relay runtime: allocation + durable usage outbox

daemon Companion/Agent ---- Presence/signaling ---- owning Hub
client Companion/Engine ---- resolve/signaling ----- owning Hub

client <========== reliable ordered WebRTC DataChannel ==========> daemon
                    direct P2P or Relay data path
```

依赖规则：

1. Controller 内的 Control Plane 持久化账号、设备、Subscription、Entitlement、Hub registry、assignment、topology projection、CommandOutbox、quota 和 usage；Web Controller 只消费这些 service/projection。
2. Edge 内的 Hub 只持有当前 control generation 下的 policy、assignment、Presence、signaling、runtime inventory 和 command delivery 状态；进程退出即丢失。
3. daemon Go runtime 持有 authenticated managed PeerSession 与 terminal access 真值。
4. Edge 内的 Relay 持有 active allocation、短期计量状态和未确认 usage event 的 durable outbox；已接收 usage、周期聚合和 settlement 属于 Control Plane。
5. Web 只读 Control Plane projection，只通过 CommandOutbox 发起控制，不能直接调用 Hub、daemon 或 Relay。
6. P2P payload 不进入 Hub/Control Plane；Relay 只能看到传输统计，不能看到 terminal payload 或 CapabilityGrant。

合并约束：Controller 与 Edge 只是 composition root。`control-plane`、`web-controller`、`hub`、`relay` 继续是独立 package；HubControl 与 RelayControl 继续使用独立 identity/key role、generation、sequence、command/result 和 lifecycle。

## 4. Proto 契约拆分

### 4.1 文件职责

新增三个 schema 文件：

| 文件 | 内容 |
| --- | --- |
| `proto/cloudpb/cloud_hub_control.proto` | Hub identity/control stream、assignment、policy projection、Hub/daemon/Relay command |
| `proto/cloudpb/cloud_topology.proto` | Presence、ManagedPeerSession、daemon runtime report、terminal access、Hub snapshot、availability/freshness |
| `proto/cloudpb/cloud_management.proto` | 用户与 operator 查询、CommandOutbox、分页、过滤、稳定错误 |

`cloud_companion.proto` 继续拥有账号登录、设备 enrollment、客户端 route/signaling、Presence 连接和 Relay lease。新 schema 不复制这些字段；通过 message reference 或稳定 ID 关联。

### 4.2 envelope 与版本

所有长连接消息使用显式 envelope，不使用 method string 分发：

```proto
message HubControlEnvelope {
  string hub_id = 1;
  string deployment_id = 2;
  uint64 control_generation = 3;
  SenderRole sender_role = 4;
  uint64 sender_sequence = 5;
  int64 issued_at_unix_ms = 6;
  int64 expires_at_unix_ms = 7;
  oneof payload {
    ControlStreamReady ready = 10;
    FullProjectionSnapshot full_projection = 11;
    PolicyDelta policy_delta = 12;
    FenceAssignment fence_assignment = 13;
    HubCommand command = 14;
  }
}

message HubRuntimeEnvelope {
  string hub_id = 1;
  string deployment_id = 2;
  uint64 control_generation = 3;
  SenderRole sender_role = 4;
  uint64 sender_sequence = 5;
  int64 issued_at_unix_ms = 6;
  int64 expires_at_unix_ms = 7;
  oneof payload {
    ReconciliationDigest digest = 10;
    HubTopologySnapshot topology = 11;
    HubCommandResult command_result = 12;
    RelayCommandResult relay_result = 13;
  }
}
```

Control Plane 与 Hub 各自维护发送 sequence；不能把双向消息混入同一个 sequence。建立 control generation 前必须完成 `BeginHubControl -> fresh challenge -> HubHello(challenge proof) -> ControlStreamReady`，认证 transport 的 key fingerprint 必须映射到持久 Hub registry，不能信任 `HubHello` 自报 identity。`control_generation` 由 Control Plane 使用事务/CAS 签发；旧 generation、错误 sender role、过期 envelope 或 sequence replay 全部拒绝。

### 4.3 必须显式出现的 fencing 字段

```text
hub_id
deployment_id
control_generation
sender_role
sender_sequence
issued_at
expires_at
account_id
auth_epoch
assignment_epoch
presence_session_id
managed_session_id
session_incarnation
daemon_runtime_generation
registry_revision
projection_revision
command_id
parent_command_id
target_generation/epoch/incarnation
```

任何 command 不得只用 device ID 定位 active runtime。任何 topology event 不得只用时间戳判断新旧。

### 4.4 生成和兼容门禁

`scripts/check-generated-code.sh` 扩展为：

1. 同时生成四个 `cloudpb/*.proto` 的 Go 文件。
2. 生成 Cloud TypeScript 文件到 `clients/ui/src/generated/cloudpb/` 和 `private/cloud/web-controller/web/src/generated/cloudpb/`；两处都是同一 schema 的 generated consumer，不允许手写镜像 DTO。
3. 生成 `proto/cloudpb/testdata/cloud-platform-v1.pb` descriptor fixture。
4. 比较 committed Go、TypeScript 和 descriptor，不允许手改 generated 文件。
5. 增加 round-trip、unknown field preservation、enum unknown 和 oneof exclusivity 测试。

`clients/ui/package.json` 的 `proto:api` 必须包含全部 `cloudpb/*.proto`。`private/cloud/web-controller/web/package.json` 增加 `@bufbuild/protobuf`、`@bufbuild/protoc-gen-es`、`proto` 脚本，并由根 `npm run proto` 统一调用两个 workspace。generated stale check、两个 workspace 的 typecheck/build 必须在同一门禁中通过。

## 5. Control Plane 代码结构

新增领域 package，避免建立通用分布式框架：

```text
private/cloud/control-plane/
  commerce/          账号、session、订单、provider journal 与 Subscription 状态机
  policy/            Account/Entitlement 到 HubAccountPolicy 的确定性映射
  hubregistry/       Hub deployment、control generation、assignment lease
  hubcontrol/        per-Hub projection 发布、stream coordination、reconciliation
  topology/          validated Presence/session/access projection
  command/           durable parent/child CommandOutbox 与结果聚合
  postgres/          上述领域 store port 的 PostgreSQL 持久化 adapter

private/cloud/controller/
  cmd/muxvia-cloud-controller/  Control Plane + Web Controller composition root

private/cloud/edge/
  cmd/muxvia-cloud-edge/        Hub + Relay composition root
```

已有 `directory`、`entitlement`、`usage` 等 package 保持各自领域。`postgres` 只实现存储 port，不拥有业务状态机。`controller` 与 `edge` 只负责配置、listener、依赖装配、进程生命周期和健康检查，不得定义第二份业务模型。

### 5.1 development PostgreSQL schema

当前阶段使用单区域单写 PostgreSQL 和 `pgx`，不提前引入分布式锁、读写分离或多区域复制；Supabase 只是首个生产托管商：

```sql
hub_deployments(
  hub_id PRIMARY KEY, deployment_id, credential_fingerprint,
  region, enabled, last_control_generation, updated_at
)

hub_assignments(
  daemon_device_id PRIMARY KEY, account_id, hub_id,
  assignment_epoch, lease_expires_at, fence_state,
  previous_hub_id, previous_epoch, updated_at
)

hub_projection_heads(
  hub_id PRIMARY KEY, projection_revision, digest,
  published_at, acknowledged_at
)

control_receive_cursors(
  hub_id, control_generation, sender_role,
  accepted_sequence, accepted_digest, updated_at,
  PRIMARY KEY(hub_id, control_generation, sender_role)
)

hub_topology_heads(
  hub_id, control_generation,
  topology_revision, topology_digest, observed_at,
  PRIMARY KEY(hub_id, control_generation)
)

presence_projections(
  daemon_device_id PRIMARY KEY, account_id, hub_id,
  assignment_epoch, presence_session_id,
  availability, freshness, observed_at, expires_at,
  daemon_runtime_generation, registry_revision
)

peer_session_projections(
  daemon_device_id, managed_session_id, session_incarnation,
  account_id, client_device_id, hub_id, assignment_epoch,
  daemon_runtime_generation, registry_revision,
  lifecycle, observed_path, observed_at,
  PRIMARY KEY(daemon_device_id, managed_session_id, session_incarnation)
)

terminal_access_projections(
  daemon_device_id, opaque_access_ref, account_id,
  client_label, subject_fingerprint_summary, state,
  issued_at, expires_at, access_projection_revision,
  daemon_runtime_generation, registry_revision, observed_at,
  PRIMARY KEY(daemon_device_id, opaque_access_ref)
)

commerce_accounts(
  account_id PRIMARY KEY, email UNIQUE, projection,
  password_hash, auth_revision
)

commerce_sessions(
  session_id PRIMARY KEY, account_id,
  access_hash UNIQUE, refresh_hash UNIQUE,
  access_expires_at, refresh_expires_at, revision, revoked
)

commerce_orders(
  order_id PRIMARY KEY, account_id, revision, projection
)

commerce_payment_attempts(
  payment_attempt_id PRIMARY KEY, order_id, account_id,
  revision, projection
)

commerce_payment_events(
  provider_event_id PRIMARY KEY, digest, event, state, result
)

commerce_subscriptions(account_id PRIMARY KEY, revision, projection)
commerce_entitlements(account_id PRIMARY KEY, projection)
commerce_audit(audit_id PRIMARY KEY, account_id, occurred_at, projection)

management_commands(
  command_id PRIMARY KEY, parent_command_id, account_id,
  actor_type, actor_id, command_kind, target_json,
  authority_result, delivery_state, execution_state,
  observed_effect, expires_at, created_at, updated_at
)

management_command_children(
  parent_command_id, child_command_id, target_hub_id,
  target_json, delivery_state, execution_state,
  observed_effect, last_error_code,
  PRIMARY KEY(parent_command_id, child_command_id)
)

assignment_migrations(
  migration_id PRIMARY KEY, daemon_device_id,
  source_hub_id, source_assignment_epoch,
  target_hub_id, fence_command_id,
  fence_control_generation, fence_sender_sequence,
  state, requested_at, fence_acked_at, completed_at
)

audit_events(
  event_id PRIMARY KEY, account_id, actor_type, actor_id,
  action, target_type, target_id, result_code,
  correlation_id, occurred_at, detail_proto BLOB
)
```

`target_json` 只是 PostgreSQL persistence adapter 对已验证 Proto target 的序列化，不是第二套 API DTO。加载后必须反序列化回 generated Proto 并重新 validation。`audit_events` 接管当前内存 audit contract，所有 operator/destructive 请求、授权拒绝、stale/replay command 和 assignment fencing 都必须在同一业务事务或可靠 outbox 中落审计。Hub runtime envelope 产生的 receive cursor/digest、topology head、projection、command result 和 audit 更新必须在同一个 PostgreSQL 事务提交；HTTP ACK 只能返回该事务已提交的连续 sequence。

### 5.2 assignment 事务

```text
AssignDaemon(device, targetHub, now):
  begin immediate transaction
  current = load assignment for update
  if current is active on targetHub:
      renew same epoch; commit
  if current is active on another Hub and no fence ack and lease not expired:
      create/reuse assignment_migration with exact source hub+epoch
      enqueue dedicated FenceAssignment with fence_command_id
      commit; return PENDING
  nextEpoch = current.epoch + 1
  write new assignment(targetHub, nextEpoch, lease)
  mark exact migration completed
  bump source Hub projection revision for assignment removal
  bump target Hub projection revision for assignment addition
  commit
```

旧 Hub fence ack 与绝对 lease expiry 是仅有的迁移放行条件。fence ack 必须匹配 `migration_id + fence_command_id + source_hub_id + source_assignment_epoch + fence_control_generation`。不能因为 control stream 断开、健康检查失败或用户再次点击就跳过 fencing。`HUB002` 先冻结 assignment 事务与精确 epoch fence 状态；`HUB004` 使用统一 CommandOutbox 持久投递 `FenceAssignment` 和接收独立结果，不再建立第二套 migration-only outbox。

### 5.3 topology ingest 事务

daemon runtime 与 Hub reconciliation 是两个不同替换范围。Control Plane 都先从持久 directory/assignment 推导 account 和 owner，不信任 Hub payload 自带的 account：

```text
ApplyDaemonRuntimeInventory(inventory):
  authenticate hub/deployment/control_generation
  require inventory.assignment_epoch == current assignment epoch
  require daemon runtime generation/revision monotonic
  validate daemon/session belongs to assignment account
  transactionally replace this single daemon scope
  empty inventory deletes previous rows in the same scope

ApplyHubTopologySnapshot(snapshot):
  authenticate hub/deployment/control_generation
  require snapshot.revision > stored Hub revision, or exact same revision+digest
  validate every included daemon against current assignment
  transactionally reconcile the whole Hub generation scope
  included rows replace current projection
  previously READY rows missing from the full snapshot become UNKNOWN/SUPERSEDED
  missing rows never become OFFLINE without explicit close evidence
```

revision gap、rollback、同 revision 不同 digest、错误账号或旧 assignment 都 fail closed，并要求 full snapshot。

Control Plane 不持久化 delta log。Control Plane 重启、发送游标丢失、Hub 报告 revision gap 或 reconciliation 冲突时，一律按当前持久真值重新生成并发送 per-Hub full projection；不得猜测或恢复半段 delta。

## 6. Hub 控制链路与纯内存 owner

### 6.1 transport 选择

当前不引入 gRPC/WebSocket 依赖。复用已有 HTTPS + length-prefixed Proto framing，建立两个逻辑方向：

```text
POST /v1/hub/control/challenge
  request: HubControlChallengeRequest authenticated by deployment transport identity
  response: fresh bounded challenge

POST /v1/hub/control/open
  request: HubHello with challenge proof
  response body: continuous HubControlEnvelope stream

POST /v1/hub/control/report
  request body: one bounded HubRuntimeEnvelope batch
  response: ReportHubRuntimeResponse with accepted through sender sequence
```

Hub 循环提交有界 batch；一个 batch 在 Control Plane 事务内按 sequence 顺序处理，response 只确认已持久接受的连续 sequence。断线时 Hub 从最后 accepted sequence 重发未确认 batch，重复同 sequence+digest 幂等，不同 digest 冲突。

两个方向共享 `hub_id + deployment_id + control_generation`，但 sender sequence 独立。control lifecycle 为：

```text
ATTACHED -> DISCONNECTED_FRESH -> EXPIRED
        \-> REPLACED_BY_NEW_GENERATION
```

transport 断开只解除 attachment，不立即删除已验证 projection；Hub 在 `DISCONNECTED_FRESH` 且 projection 未超过 `max_staleness` 时继续本地准入。新 generation 由 Control Plane CAS 签发时原子 fence 旧 generation；超过 staleness 后 Hub fail closed。所有旧 handler 每次处理 envelope 前重新校验 registry 当前 generation。

daemon Presence 继续使用现有 Hub response stream，新增独立上行 API：

```text
POST /v1/hub/daemon/runtime
  authorization: daemon edge credential + active Presence binding
  request: ReportDaemonRuntimeRequest
  response: ReportDaemonRuntimeResponse
```

这样能复用当前 Companion adapter，不把 Presence 重写成全双工 transport。

### 6.2 Hub 内存结构

`private/cloud/hub` 增加：

```text
control_client.go       唯一 control generation、full/delta sync、report pump
projection.go           当前已验证 policy/assignment projection
assignment.go           lease timer、fence 与 target-owner routing
runtime_report.go       daemon inventory/event validation 与 Hub snapshot
command.go              Hub/daemon command delivery、dedupe 与 result
```

Hub 可以接受新 managed 请求的 readiness 条件必须同时成立：

1. deployment identity 已认证。
2. 唯一 control generation 未被 replacement fencing；transport 可以是 attached 或 disconnected-fresh。
3. full projection 已完整应用。
4. projection 未超过 `max_staleness`。
5. assignment expiry timer 已启动。

Hub 不再构造或恢复 `EdgeSnapshotStore`。`edge_snapshot_store.go` 及其 production/dev composition 在 `HUB002` 删除；如果 fixture 仍需序列化证明，只在测试内使用字节快照，不提供 runtime 文件 store。

### 6.3 projection apply

```text
ApplyFull(snapshot):
  verify signature, hub_id, revision, digest, validity window
  build complete candidate maps off-lock
  validate assignments/policies/revocations
  lock
  replace all current projection maps atomically
  set revision/digest/receivedAt
  unlock

ApplyDelta(delta):
  require delta.from_revision == current.revision
  verify signature and resulting digest
  build candidate from current clone
  apply and validate
  atomically replace candidate
```

不能逐条修改 live map 后再发现错误；失败必须保留上一份完整 projection。

## 7. daemon ManagedPeerSession registry

### 7.1 owner 与数据模型

新增 `remote/daemon/session_registry.go`。registry 只跟踪 Cloud managed DataChannel；Direct/SSH 传空 managed context，不进入 Cloud topology。

```go
type ManagedSessionKey struct {
    ManagedSessionID   string
    SessionIncarnation uint64
}

type ManagedSessionRecord struct {
    Key                     ManagedSessionKey
    EstablishedPresenceID   string
    ControlPresenceID       string
    AssignmentEpoch         uint64
    ClientDeviceID          string
    GrantID                 string
    SubjectKeyFingerprint   string
    ObservedPath            cloudpb.ObservedPath
    State                   SessionState
}
```

registry 创建时生成进程内随机唯一且进程内严格不复用的 `daemon_runtime_generation`；每次可观察修改递增 `registry_revision`。inventory snapshot 与 lifecycle event 必须来自同一把锁下的 revision。`session_incarnation` 由 owning Hub 在接受一次新 signaling session 时单调生成，并随 offer 发送；daemon 不能自行选择或复用。

### 7.2 生命周期

```text
WebRTC DataChannel open
  -> remote auth success
  -> registry Begin(AUTHENTICATED)
  -> core protocol Hello accepted and Hello response queued
  -> registry MarkReady(READY)
  -> protocol/resource/DataChannel serving
  -> all serving goroutines return and transport closed
  -> registry MarkClosed(CLOSED, reason)
```

READY 不能在 SDP answer、ICE connected、DataChannel open 或 CapabilityGrant 单独成功时上报。CLOSED 不能由 topology timeout 推断。

### 7.3 core 最小观察 port

`core` 增加内部观察接口，不改变 Proto API：

```go
type TransportLifecycleObserver interface {
    HelloAccepted()
}

func (server *Server) ServeScopedTransportObserved(
    ctx context.Context,
    conn transport.Transport,
    scope TransportScope,
    observer TransportLifecycleObserver,
) error
```

现有 `ServeScopedTransport` 调用 observed 版本并传 `nil`。`protocolSession` 必须维护 `helloAccepted`：Hello 前的业务 request 一律拒绝；第一条合法 Hello 的 response 成功进入发送队列后原子标记并调用一次 `HelloAccepted`；重复 Hello 拒绝或幂等响应，但不得再次回调。observer 不能读取 command payload、修改 core state 或决定授权。

### 7.4 managed metadata 接线

`remote/webrtc.Answerer` 增加内部 `SessionContext`，来源只允许是已由 Hub 路由的 `SignalingOffer`：

```go
type SessionContext struct {
    ManagedSessionID   string
    SessionIncarnation uint64
    ClientDeviceID     string
    PresenceSessionID  string
    AssignmentEpoch    uint64
    ObservedPath       cloudpb.ObservedPath
}
```

`SignalingOffer` 的新 Proto 字段必须携带 `session_incarnation + presence_session_id + assignment_epoch`。Agent 使用当前 Presence 和 assignment 校验三者后构造 `SessionContext`；旧 epoch、旧 Presence 或重复 incarnation 直接拒绝。`DataChannelSessionHandler` 接收该 context。Direct embedded signaling 传零值，保持现有行为；不能在 SDP 自定义字段中解析这些 ID。

Begin/READY 由 `SessionAcceptor` 与 core observer 驱动；最终 CLOSED 由仍持有 PeerConnection 的 `Answerer` 驱动。registry 返回一次性 session handle，Answerer 在 handler 返回后关闭 transport/peer、等待 peer close 完成，再调用 handle `MarkClosed`。exact close 先通过 handle cancel/close，等待 handler 和 peer done，最后提交 CLOSED revision。

### 7.5 runtime report pump

registry 变更只唤醒单个有界 reporter，不在锁内做网络 IO：

```text
on registry revision change:
  nonblocking notify reporter

reporter:
  coalesce notifications
  snapshot current inventory under lock
  POST ReportDaemonRuntime(presence, runtime_generation, revision, inventory)
  retry same revision with bounded backoff while this Presence remains current
  on newer revision, send newest full inventory
```

reporter 生命周期绑定单个 Presence context。Presence replacement 时先停止并等待旧 reporter；新 Presence 的第一条报告必须发送当前 registry 的完整 inventory，包括旧 Presence 建立但仍存活的 PeerSession。session record 分开保存 `established_presence_session_id` 与当前 report envelope 的 `control_presence_session_id`；远程命令使用当前 control Presence fencing。每次都发送完整 inventory，避免 daemon 到 Hub 之间维护无法恢复的 delta。空 inventory 是有效替换，必须清除旧 projection。

## 8. terminal access projection 与 deny-only revoke

daemon 从 `AccessStore.ListClientAccess` 生成最小 projection，不上报 CapabilityGrant 原文。opaque reference 计算为：

```text
base64url(SHA-256("muxvia-access-ref-v1" || daemon_device_id || grant_id))
```

它不可逆且稳定；daemon 执行 revoke 时扫描本地 access records 重算并匹配。projection 只包含 client label、subject fingerprint 摘要、状态和 issued/expires 时间，不包含 grant body、scope、client public key、terminal ID、私钥、grant token 或文件路径。

daemon control command 必须由 enrollment control key 验证：

```text
VerifyDaemonCommand(command):
  verify CP signature and key id
  require deterministic signed input includes command id/kind, account id,
          target device/session/access ref, hub id, assignment epoch,
          auth epoch, presence session id, daemon runtime generation,
          issued at, expires at and control key id
  require account/device/auth epoch/Hub/assignment/Presence/runtime all current
  require command.kind is deny-only
  require now <= expires_at
  require command id+digest not conflicting with persistent receipt
  resolve opaque ref to current local grant
  AccessStore.RevokeGrant atomically
  close every exact registry session using that grant
  persist bounded receipt beside AccessStore before returning result
  return deterministic DaemonCommandResult; exact replay returns same result
```

daemon-local `ControlReceiptStore` 只保存未过期 command ID、digest 和 result，不拥有 Cloud session 或授权真值；进程重启后用于幂等 replay。Cloud 不得通过该通道创建 grant、扩大 scope、读取 terminal 列表或注入 terminal command。

## 9. CommandOutbox

### 9.1 状态分离

每条 command 独立记录：

```text
authority_result: NOT_APPLIED | APPLIED | REJECTED
delivery_state:   PENDING | SENT | ACKED | EXPIRED
execution_state:  NOT_STARTED | RUNNING | SUCCEEDED | FAILED | PARTIAL
observed_effect:  UNKNOWN | OBSERVED | NOT_OBSERVED
```

持久 authority revoke 成功后不能因为 runtime child 超时而回滚。topology CLOSED 只能更新 `observed_effect`，不能冒充 daemon/Relay execution ack。

### 9.2 dispatcher

```text
DispatchPending(now):
  claim unexpired child commands in PostgreSQL transaction
  load current assignment/topology target
  if exact fence changed: mark stale target
  send to target Hub/Relay control generation
  persist delivery receipt
  await independent command result
  persist execution result
  recompute parent aggregate
```

parent command 只聚合 children，不作为额外网络消息。client device revoke 根据当前 topology 为每个 daemon/session 生成 child；一个 child 失败或超时得到 `PARTIAL`，其他已完成 child 不回滚。

## 10. Relay remote revoke

Relay 与 Hub 由同一个 `muxvia-cloud-edge` 进程装配，但 Relay 增加独立 service identity/logical control stream，不能借用 Hub control generation。`private/cloud/relay` 在内存中维护：

```text
lease_id -> allocation IDs
managed_session_id -> allocation IDs
allocation_id -> connection handle + close state
lease_id -> current usage sequence
```

处理 revoke：

```text
CloseRelayTarget(command):
  validate relay identity, generation, expiry and exact lease/session target
  atomically mark matching allocations closing
  close every connection handle
  after all allocation handles settle, drain one final lease-level signed usage event
  report per-allocation close result and final lease usage sequence
```

usage 幂等键继续使用稳定契约 `relay_id + lease_id + sequence`，不能加入 allocation 改写计费粒度。Control Plane 收到 close ack 后仍需等待 durable usage ingest 和 reservation settlement。三者任一未完成时 command 为 `PARTIAL` 或 `RUNNING`，不能提前显示成功。

## 11. Web/API projection

`private/cloud/web-controller` 不再维护 hand-written Cloud 业务 DTO。HTTP body 使用 generated Proto JSON，并通过 Control Plane service 查询：

- 设备页：ownership、revocation、assignment、Presence availability/freshness。
- topology 页：signaling control Hub 与 `P2P_DIRECT`/`SINGLE_RELAY` data path 分开显示。
- access 页：daemon 上报的 opaque terminal access reference。
- command 页：authority、delivery、execution、effect 和 child 明细。
- fleet 页：Hub deployment、control generation、projection revision、readiness、freshness。

用户只能访问自己 account；operator API 使用独立 actor/permission。所有 destructive mutation，包括 KickPresence、device revoke、CloseManagedPeerSession、RevokeTerminalGrant 和 Relay revoke，都要求账号隔离、CSRF、近期重认证和持久审计。Web 不直接接触 Hub URL credential、daemon command signature key、CapabilityGrant 或 terminal payload。

## 12. 切片级实施计划

### HUB001：Proto 与 daemon registry contract

修改范围：

- 新增三个 Cloud Proto 和 generated Go/TypeScript。
- 更新 generated/descriptor 检查脚本与 compatibility tests。
- 新增 daemon registry 纯模型、fake lifecycle observer 和 fake reporter harness；不接 Pion/HTTP。

完成证明：

- registry 并发 Begin/Ready/Close 线性化；重复/旧 incarnation fail closed。
- inventory 与 event 使用同一 revision；空 inventory 可表达完整替换。
- command parent/child、terminal access、management query/error 全部可 round-trip。

### CLOUDP001：PlanCapability 与 Entitlement

- `PlanCatalog` 输出统一 capability model。
- Subscription projection 只引用 plan/version/status，不在 Hub 猜套餐。
- policy projection 从 Entitlement 生成；删除 plan string、有效期和固定 quota 分支。

### HUB002：registry、assignment 与纯内存同步

- 实现 Control Plane `hubregistry/hubcontrol/postgres`。
- 实现 Hub 双向逻辑 control transport、full/delta/reconciliation 和 expiry timer。
- 增加两个 composition root：`private/cloud/controller/cmd/muxvia-cloud-controller` 组合 Control Plane + Web Controller；`private/cloud/edge/cmd/muxvia-cloud-edge` 组合 Hub + Relay。Controller PostgreSQL DSN 只来自 0600 配置或部署 secret；manifest 只暴露数据库引擎。Edge 参数只包含 Controller URL、usage-outbox 路径、Hub/Relay identity credential 和显式 dev fixture 路径。
- Controller 同一进程使用 public、internal-control、operator 三个独立 listener/middleware；Edge 同一进程使用 Hub public/signaling、Relay data、health 三类 listener，不允许 public handler 访问 Controller store。
- `private/cloud/devcloud/cmd/muxvia-cloud-dev` 降为 development supervisor：启动一个 Controller 和至少两个 Edge 子进程，写入包含 PID、监听地址、Hub/Relay identity、数据库、usage outbox、配置和日志路径的 manifest。`HUB007` 在该稳定进程 harness 上增加精确 stop/restart/migration 故障控制，不得回退单进程装配或 fake ack。
- Control Plane 不再通过 `state.hub` pointer 发布 policy，Hub module 不通过 Go pointer 直接控制 Relay allocation。
- 删除 `EdgeSnapshotPath`、runtime snapshot restore 和 active `FileEdgeSnapshotStore`。

### HUB003：daemon registry 与 topology

- 接通 Answerer managed context、SessionAcceptor registry、core Hello observer。
- Companion 增加 `ReportDaemonRuntime`，Agent 启动单 reporter pump。
- Hub 聚合 daemon full inventory，Control Plane 校验并替换 topology projection。
- 删除 Web `SetCloudDaemonOnline` direct write；Web 后端从 topology store 查询 availability/freshness。HUB003 期间旧页面允许把查询结果确定性投影为显示 bool，但不落盘、不成为第二份 truth；CLOUDP006 只迁移页面和 Proto JSON consumer，并删除该临时 UI projection。
- 当前实现把 `ObservedPath` 与 `ReportDaemonRuntimeRequest/Response` 归到 `cloud_topology.proto`；`cloud_companion.proto` 只增加 daemon runtime capability 与 IPC operation，Hub HTTP 与 HubControl 复用同一 generated message。
- daemon reporter 对同一 revision 保留并重试同一份 Proto，只有 registry 新 revision 才重建 full inventory；Presence replacement 会先停止并等待旧 reporter。
- Hub 只在当前 assignment/Presence 下接受 inventory，同 runtime revision 单调、同 revision digest 幂等、被替换 runtime generation 永不复活；每个 control generation 首包为完整 `HubTopologySnapshot`。
- Controller 从持久 assignment 与 device ownership 推导账号，PostgreSQL full replacement 对缺失项降级为 `UNKNOWN/STALE`；control stream 丢失同样只降级，不伪造 offline。

### CLOUDP002：账号、Subscription 与交易

- `cloud_product.proto` 定义账号/session、Price/展示目录、订单价格快照、PaymentAttempt、normalized payment event、Subscription transition、交易审计和稳定错误；Go/Web 只消费 generated 类型。
- `control-plane/commerce` 拥有注册、登录、单次 refresh rotation、logout、改密后全 session revoke、checkout、测试 provider 和 Subscription 状态机。
- PostgreSQL adapter 原子保存账号、session、Subscription、Entitlement、订单、PaymentAttempt 和审计；payment event 必须先写 `RECEIVED` journal，再以 order/attempt/subscription revision CAS 提交，拒绝写 `REJECTED`，成功保存精确响应供重放。
- checkout 记录源 Subscription revision/plan version；迟到 payment event fail closed，不能覆盖后续套餐变化。退款、撤销和 chargeback 只作用于当前 Subscription 的 source order。
- Controller public listener 暴露 `/api/v1/account/*`、`/api/v1/checkout*` 和 `/api/v1/subscription/transition` Proto JSON API，使用 HttpOnly access/refresh Cookie、same-origin 与 CSRF；响应不把 token 暴露给 Web JavaScript。
- development test provider 只有 `enable_test_payment_provider=true` 时注册；它生成 normalized event，禁止直接写 Entitlement。
- 公开用户 transition 只允许取消续订和恢复续订；升级、降级、续费必须经过订单/payment event，运营 suspend/restore/expire 留给后续 operator authorization。
- 删除旧 `web-controller` 自有 `CommerceService`、`UserCenterStore`、手写 Order/PaymentEvent/Session DTO、旧 webhook 和 `SetEntitlement` direct write。
- harness 覆盖注册/登录、refresh replay、改密撤销、Price 快照、PaymentAttempt、Controller/PostgreSQL 重启、支付成功/失败/退款/撤销/chargeback、精确 event replay、stale order reject、升级/降级/续费/取消/恢复/暂停/到期和 Cookie/CSRF。

### CLOUDP003：managed P2P 准入与并发

- `control-plane/policy` 从持久账号 `auth_revision` 与 Entitlement status/effective window/PlanCapability 确定性生成 `HubAccountPolicy`；不读取 plan name、价格或 terminal 数据。
- Controller 初始 full projection 与 commerce 变更重发都读取同一 PostgreSQL 真值；静态 `Config.Accounts` 和 supervisor 手写 HubAccountPolicy 删除。显式 dev supervisor 先走真实 commerce 注册，再创建设备/assignment。
- commerce 在注册、改密 auth revision、payment replay/commit 和 Subscription transition 持久提交后通知 Controller；Controller 只对该账号当前 assignment 所在 Hub 分配新 revision 并发布 signed full。
- Controller 还必须在 projection TTL 内周期性从 PostgreSQL 重建并签发 fresh full，避免无账号变更的稳定运行在旧 full 过期后拒绝全部 managed P2P；刷新仍使用同一 per-Hub persistent revision 与 HubControl，不增加第二套 scheduler 真值。
- Edge 继续通过真实 HubControl 接收并验签 full/delta；Hub authorizer 对 capability 执行与 Control Plane 相同的结构 validation，错误/陈旧/revoke/epoch mismatch fail closed。
- 删除旧 JSON-token `servicecredential.EdgePolicy` 与 `ApplySignedSnapshot`；唯一 policy wire truth 是 Proto `FullProjectionSnapshot/PolicyDelta`，Edge 重启不能从磁盘恢复 Hub policy。
- Hub `ReserveManagedP2P` 在签名 policy、client/daemon ownership、revoke、auth epoch 和账号 concurrency limit 的同一内存锁下占用精确 signaling reservation。
- Hub HTTP 保持失败分类：无有效 Entitlement 返回 `ENTITLEMENT_DENIED`，账号并发占满返回可重试 `QUOTA_EXHAUSTED`，credential/epoch/revoke 仍返回认证失败，目标 ownership 不泄漏跨账号存在性。
- reservation 首先绑定 signaling session；answer 后由 daemon-owned 完整 PeerSession inventory 转交为 runtime reservation，AUTHENTICATED/READY/CLOSING 的 DIRECT session 持续占用，完整 inventory 缺失、未 answer 取消、pending TTL、daemon failure 或精确 assignment fence 才释放。新 owning Hub 可从完整 inventory 重建仍存活的 P2P；Relay-only session 留给 `CLOUDP004` Relay reservation，不占 managed P2P 名额。
- policy suspend/revoke 只拒绝新 reservation，不伪造对已有端到端 DataChannel 的即时 Cloud 控制能力。
- harness 覆盖真实 Controller public 交易到 signed policy revision、真实 Controller/Edge control transport、P2P limit、释放、ownership、target/client/account revoke、auth epoch、suspend、assignment 缺失和 stale policy。

### HUB004：CommandOutbox 与 session control

- 已实现 durable parent/child store、dispatcher 和账号隔离的 Proto JSON command 创建/查询。
- Hub 对精确 Presence 执行 Kick；daemon registry 对精确 session incarnation 调用 `CloseExact`，等待真实 owner `Done` 后独立回报结果。
- daemon/client revoke 先在同一 PostgreSQL 事务提交 authority；daemon 生成单 Hub Presence child，client 按当前 topology 生成跨 Hub session children。
- daemon command 使用独立 Ed25519 key、确定性 Proto bytes、expiry、device auth epoch、Hub/assignment/Presence/runtime/session fencing；Hub 只能转发。
- HUB004 的 daemon replay receipt 只覆盖当前进程；HUB005 已由 enrollment-owned `ControlReceiptStore` 持久化受信控制 key 与精确 command/result 回执，使 daemon 重启后仍可幂等返回原结果。
- 真实 harness 已串联 Controller Web API、HubControl HTTP、Edge Presence、daemon result 回传与 PostgreSQL parent `APPLIED`。

### HUB005：terminal grant revoke

- enrollment 使用生成的 `DeviceEnrollmentServiceSession` 返回账号、Hub credential 和受有效期约束的 daemon control public key binding；Companion 验签后才安装 OS session，daemon 在消费一次性 enrollment 前先打开 owner-only `ControlReceiptStore`。
- daemon AccessStore 持久维护单调 `access_projection_revision`，runtime report 只上报 opaque access reference、状态和必要 fence，不上传 grant、token、terminal capability 或 terminal 内容；Hub 用 registry/access 双 revision 替换内存 topology，允许 access-only 更新并拒绝回退。
- Web 通过账号 session 查询 Controller PostgreSQL 中的 terminal access projection；创建 revoke 时由 Control Plane 补齐 assignment、Presence、runtime generation 和 access revision fence，Controller 对确定性 Proto command 签名，Hub 仅做精确 Presence 转发。
- daemon 先在 AccessStore 原子 revoke，再关闭所有匹配 opaque access reference 的 managed session，并把结果写入持久回执后回传；Cloud 路径没有 grant、expand 或直接修改 terminal capability 的入口。
- 真实 harness 已串联 Controller enrollment、Edge runtime report、Web 查询/创建命令、Hub 转发、daemon 执行、AccessStore/session 可观察结果和 Controller PostgreSQL `APPLIED`；generated、public/private、race、双 Edge、doctor 与受影响 vet 门禁通过。

### CLOUDP004：Relay quota 与 reservation

- `RelayQuotaPeriod`、`RelayLeaseReservation` 和 Edge 到 Controller 的 `ReserveRelayLeaseRequest` 已进入 Proto；PostgreSQL 是 billing period 与 reservation 唯一持久 owner，原子计算 `used/reserved/remaining`、active lease count、revision 和单 lease clamp。
- reservation 输入只来自当前 Entitlement period/capability、已验证 client/target ownership、精确 Hub assignment、region 和 Edge deployment；同 lease 相同输入精确 replay，不同输入冲突，账号/设备并发或周期剩余额度不足返回稳定 quota exhausted。
- Hub 为 endpoint resolution 保存纯内存短 TTL relay intent；client 与 daemon 使用同一 managed session、client/target binding 和 lease ID，因此共享一个 reservation 和 signed lease，但 Edge Relay authority 分别派生 caller-specific TURN username/password。
- 临近 lease expiry 时 Hub 单调轮换 lease ID；Controller 对新 ID 重新读取 Entitlement 和 quota。旧 reservation 在 expiry 加 usage report grace 后释放，显式 cancel 只用于已确认无 allocation 和待 drain usage 的 lease。
- 真实 harness 已走账号注册、订单/payment event 升级、Controller 与 Edge 独立 listener、daemon Presence、endpoint resolve、client/daemon Relay lease、隔离 credential、并发耗尽和 Relay session topology；generated、public/private、race、client、双 Edge 和 doctor 门禁通过。

### CLOUDP005：durable usage 与 settlement

- `RelayUsageEvent`、`RelayUsageRecord`、report/ack 和 `RelayUsageAggregate` 已进入 Proto；Go `usage.Event` 只保留为 Relay 内部 canonical signing model，Edge 到 Controller wire 只传 generated message。
- Edge 从 0600 配置加载独立 Relay control private key，Controller deployment registry 持久化并校验对应公钥/fingerprint；Hub control、Relay usage、Controller projection 和 daemon command key role 不复用。
- Relay authority 以 `FlushUsageOutbox` 作为本地计量提交点：先签名并原子写 0600 durable outbox，成功后才推进 sequence、窗口和 pending counters；同秒 shutdown 使用最小一秒窗口，Controller 暂时拒绝未来 end 时 outbox 保留并在下一秒重试。
- Edge pump 把原始 signed lease 与 signed event 组成有界 at-least-once batch；Controller 重新验证 RelayLease issuer/audience/region/binding，再按 deployment Relay key 验 event。只有 PostgreSQL settlement 完整提交后才返回精确 event/sequence ack，网络失败、Controller outage 或 ack 丢失都保留队列。
- PostgreSQL 在一个事务内维护 `(relay_id, lease_id, sequence)` journal/digest、严格递增 sequence、period used、reservation used/state 和 managed session/route aggregate；相同 body replay 返回 duplicate，不同 body、回退、越界或错误 binding fail closed。termination event 结算后释放未使用 reservation；无 final event 的 reservation 继续由 expiry+report grace 收敛。
- harness 已覆盖 journal/aggregate/settlement 的 PostgreSQL 重启、Edge outbox 重启补报、same-second shutdown、重复/冲突/sequence/bytes 边界，以及真实 Controller-Edge-Relay Pion relay-only DataChannel 产生流量、outbox 上报、Controller 入账和 ack 清空。Relay allocation 的远程关闭与强制 final drain 仍属于 HUB006。

### HUB006：Relay allocation remote revoke

- `RelayControlChallengeRequest/ProofInput`、`ReportRelayRuntimeRequest/Response` 与显式 `usage_settlement_complete/settled_usage` 已进入 Proto；Hub/Relay control generation、sender sequence、cursor 和 replay 空间完全分离。
- Controller 内 `relaycontrol` publisher/server 只拥有 authenticated stream 与 result transport；CommandOutbox planner 从持久 reservation 校验 account/Hub/Relay binding，dispatcher 直接发布 `RelayControlCommand`，Hub module 不接触 Relay allocation map。
- Edge 内独立 Relay control client 按 generation/sequence fail closed，并通过 Relay Server port 按 lease 或 managed session 关闭真实 relay socket；command replay digest 排除传输 generation，ack 丢失重连不会重复执行数据面副作用。
- final close 与周期 usage pump 共享单一提交锁；零字节 lease 也生成一次 termination event，Controller PostgreSQL settlement 释放剩余额度后 Edge 才回传完成结果。allocation close、usage drain 或 settlement 任一步不完整时返回可解释 `PARTIAL`。
- harness 已覆盖独立 generation、reservation target binding、Relay dispatcher、零字节 final event、单 child PARTIAL、race，以及真实 Controller-Edge-Pion relay-only DataChannel remote close、payload 停止转发、usage ack、reservation release 和 CommandOutbox `APPLIED`。

### CLOUDP006：用户与运营管理面

- Controller public/operator listener 从同一个可选 `web_static_dir` 提供生产 Web build；账号 API、operator API、Control Plane service 与静态资源仍在同一 `muxvia-cloud-controller` composition 内，internal control listener 不提供页面。
- `cloud_management.proto` 已定义近期认证、operator session/角色、账号列表/详情和 suspend/restore；`cloud_product.proto` 的账号交易查询同时返回 normalized payment event journal。Go 与 TypeScript consumer 都从同一 schema 生成。
- 用户账号中心直接消费 generated Proto JSON，展示账号、Subscription/Entitlement、Relay quota、设备、Presence、managed PeerSession data path、CommandOutbox、订单、payment event 与审计；signaling control Hub 和 `DIRECT`/`SINGLE_RELAY` data path 分开显示，stale 不投影成 offline。
- 账号 destructive command 必须先使用当前密码换取五分钟 HttpOnly recent proof，并同时通过账号 Cookie、same-origin 和 CSRF；请求中的 `account_id` 被当前账号覆盖，不能跨账号查询或控制。
- 独立 operator listener 使用高熵部署 token、HttpOnly session、独立 CSRF Cookie 与 `readonly/admin` 角色；只读角色只能查询，admin 在登录后五分钟内可以 suspend/restore、撤销设备或创建已有 management command，结果进入 Subscription/Commerce audit 或 CommandOutbox 持久投影。
- fleet 页面只把同时存在的 Hub/Relay attachment 标记为 fresh，`last_control_seen_at` 只来自真实 attachment，不用 deployment 配置时间伪造在线证据。
- development supervisor 构建同一 Web 资产，并把随机账号密码和 operator token 写入独立 `0600` credentials 文件；runtime manifest 只记录文件路径，不复制 secret。
- 已删除旧 `web-controller/controller.go` hand-written facade、`Center/Node/Billing` DTO、旧 `/api/center`、旧 `/api/auth/*`、无 Proto 后端的 `/api/device-login*` 页面、referrals 与 staging 登录 fallback。

### HUB007：双 Hub 控制面 E2E

- 独立进程运行一个 Controller、两个 Edge 和多个 daemon/client；每个 Edge 内 Hub 无盘，Relay 只有 usage outbox 可持久化。
- 验证 assignment migration、旧 epoch fencing、Controller outage、Edge restart、inventory recovery、四类 command、P2P/Relay close 和隐私扫描。

### CLOUDP007：Development 全产品 E2E

- Web UI 完成注册、交易、套餐、设备、topology、command 和 usage。
- Android ARM64 APK UI 完成 P2P/Relay terminal、文件、quota、suspend、重连；Direct/SSH 回归。
- 记录 APK hash、AVD/ABI/API、控制流 revision、command receipt、usage 与 crash/secret scan。

## 13. 旧路径删除清单

| 时点 | 删除或停止使用 |
| --- | --- |
| HUB002 | `EdgeSnapshotPath`、Hub runtime 文件 snapshot restore、Control Plane 到 Hub policy direct pointer call |
| HUB003 | `SetCloudDaemonOnline` direct projection、Presence bool 作为 Web truth、无 registry 的 managed session 推断 |
| CLOUDP002 | Web Controller 自有账号/password/session/order/payment 表、内存 commerce map、签名 staging webhook、测试付款直接发布 Entitlement、`SetEntitlement` direct write |
| CLOUDP003 | Controller `Config.Accounts` 静态 HubAccountPolicy、dev supervisor 手写套餐能力、旧 JSON-token EdgePolicy/持久快照语义、按 session map 数量猜账号套餐并发、Relay-only 复用 P2P reservation |
| HUB004 | staging 直接 `Hub.RevokeDevice` 作为管理操作完成条件、用 topology CLOSED 冒充 ack |
| HUB006 | 只按 lease metadata 撤销但不关闭 allocation 的路径 |
| CLOUDP006 | Web hand-written Cloud management DTO 和 direct store mutation handler |
| HUB002 | devcloud 单进程/单 Edge E2E 装配作为主验收入口 |

`control-plane/directory` 中短期 `ManagedSession` 只保留到真实 daemon registry/topology 和 signaling correlation 接管完成。若届时无调用者，直接删除该 map/API，不把它改造成第二份 active session truth。

## 14. 测试矩阵

### 14.1 每切片基础门禁

```sh
./scripts/check-generated-code.sh
git diff --check
```

按修改模块运行：

```sh
go test ./remote/... ./core/... -count=1
(cd private/cloud/control-plane && go test ./... -count=1)
(cd private/cloud/hub && go test ./... -count=1)
(cd private/cloud/companion && go test ./... -count=1)
(cd private/cloud/relay && go test ./... -count=1)
(cd private/cloud/devcloud && go test ./... -count=1)
(cd private/cloud/web-controller && go test ./... -count=1)
npm run proto
npm run typecheck --workspace @muxvia/web-controller
npm run build --workspace @muxvia/web-controller
```

涉及 registry、stream、assignment、command dispatcher、allocation 的切片必须对受影响 package 运行 `go test -race`。

### 14.2 必备 harness

- Proto descriptor compatibility、unknown field、unknown enum、oneof。
- assignment lease/fence 虚拟时钟和双 Hub generation。
- assignment fence 前后及 ack 前后 Control Plane 重启恢复，旧/目标 Hub revision 同步推进。
- Hub full/delta/reconciliation、gap/rollback/conflict。
- daemon registry auth/Hello/close 并发与 reporter retry/coalesce。
- request-before-Hello、非法/重复 Hello、PeerConnection 完整关闭后 CLOSED、Presence replacement full inventory。
- topology full replacement、空 inventory、旧 generation/revision。
- CommandOutbox replay、expiry、stale target、parent partial。
- terminal access opaque ref、signature、replay、deny-only。
- Relay allocation close、final usage、settlement partial。
- Web account isolation、operator permission、CSRF、recent auth。
- Web generated stale check、typecheck/build 与 UI 发起 destructive mutation。

### 14.3 E2E 证据

HUB007/CLOUDP007 的操作必须从真实入口发起，不得直接调用 store/runtime 伪造结果。证据至少包含：

- 进程拓扑与监听地址。
- Hub deployment/control generation、assignment epoch、projection revision。
- daemon runtime generation、registry revision、Presence/session incarnation。
- command parent/child receipt 和最终状态。
- Relay allocation/final usage/settlement correlation。
- Android APK SHA-256、模拟器 ABI/API、UI 操作步骤、terminal/file 输出与 logcat。

HUB002 提供 `make test-cloud-controller-edge`，启动一个独立 Controller 和 Edge A/Edge B，并输出包含进程、配置、listener、数据库、usage outbox 和日志路径的 manifest。HUB007 必须基于该入口增加精确 kill/restart 单个 Edge、停止 Controller、assignment migration 和网络故障注入；不得回退到单进程 Go pointer harness，也不得用 fake ack 代替真实 command/result。

CLOUDP007 必须新增 `make e2e-cloud-development`，先运行 `make test-android` 生成唯一 `.artifacts/android/app-devcloud-debug.apk`，记录其 SHA-256，再将同一文件安装到仓库指定 ARM64 AVD 并运行 UI instrumentation。Gradle build output 与安装文件必须 hash 一致。

HUB007 证据矩阵至少逐项记录：assignment migration、Controller outage、Edge restart、断线丢失 CLOSED 后 full snapshot、stale event、command replay、KickPresence、device revoke、session close、grant revoke、Relay revoke 及其独立 ack/partial result。

CLOUDP007 证据矩阵至少逐项记录：Web UI 注册/登录/测试交易/套餐管理/命令发起；同一 APK 的 P2P/Relay terminal 与文件上传下载；quota exhausted、suspended 后 managed Route 拒绝；未登录或 suspended 时 Direct/SSH 继续可用；锁屏/后台/网络切换恢复；crash、ANR、native fatal、secret、SDP/credential 与 terminal payload 日志扫描及产物路径。

## 15. 风险与收敛规则

1. 最大风险是多处 active truth：directory managed session、Hub signaling session、daemon registry、Web online bool 必须按切片逐步降为各自明确的 correlation/projection，不得同时声称 active session owner。
2. 第二风险是通过时间戳或断线推断状态。所有状态更新必须使用 generation/epoch/revision/incarnation；超时只能产生 `UNKNOWN/STALE`。
3. 第三风险是 command 成功虚报。authority、delivery、execution、effect 必须分别持久化和展示。
4. 第四风险是 composition 合并掩盖领域边界。HUB002 起 Controller 与 Edge 之间必须通过真实 HTTP Proto adapter；Edge 内 Hub/Relay 只能通过窄 port 或各自 control handler 协作，不能共享业务 map。最终 E2E 必须是一个 Controller 与至少两个独立 Edge 进程。
5. 不做 Hub-to-Hub forwarding、Relay Mesh、动态无中断换路、多区域数据库、真实支付 provider、通用事件总线或 Web terminal。这些发现只能记录 deferred observation。

## 16. 执行约束

- 每轮只执行 `workflow.md` 最早未完成切片。
- 跨边界改动固定顺序：Proto -> generated -> compatibility harness -> domain/runtime -> adapter -> consumer/UI。
- 新增内部 Go interface 必须只解决当前 owner/观察边界；不能先抽象未来 iOS、Web、多区域或第三方 provider。
- 每个切片完成后删除已被替换的旧路径，不保留双写、fallback 或长期 migration adapter。
- HUB007 与 CLOUDP007 必须双 Agent 审查；其他切片仅在用户或 `workflow.md` 明确要求时审查。
