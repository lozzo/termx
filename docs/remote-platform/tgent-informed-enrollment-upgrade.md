# 基于 tgent 体验的 Muxvia Cloud 节点注册升级设计

## 文档状态

- 状态：`ENROLLUX001-ENROLLUX004` 已实现并通过准入；`ENROLLUX005` 正在完成最终产品 E2E。
- 目标：参考同级仓库 `../tgent` 的顺滑注册体验，收敛 Muxvia daemon enrollment、Hub 选择、在线和移除交互。
- 约束：本提案服从 `workflow.md`、`cloud-product-spec.md`、`multi-hub-control-topology-spec.md` 和 `multi-hub-technical-plan.md`；它不改变 DeviceIdentity、Hub 纯内存、Control Plane 持久真值、CapabilityGrant 端到端授权或 Direct/SSH 免费可用边界。
- 非目标：不把 `tgent` 作为构建或运行依赖，不复制其私钥共享、同步中心校验、在线状态落库真值或删除即失忆的实现。

## 结论

Muxvia 不需要减少现有安全能力，而是需要把内部复杂度藏回正确的领域边界。目标体验收敛为：

1. 用户在 Web Controller 生成一次性短码。
2. daemon 提交固定 DeviceIdentity 和设备信息，同时在本地探测 Controller 提供的候选 Hub。
3. 终端立即提示用户去网页确认；网页展示账号冲突、设备身份和替换影响。
4. 用户只确认一次。daemon 提交自己选出的首选 Hub，Controller 校验后一次事务完成设备绑定、Hub assignment、device session 和审计。
5. daemon 拿到短期凭据后立即连接指定 Hub；注册已完成和 Hub 暂时离线是两个状态，不再混成 `DEVICE_ENROLLMENT_REQUIRED`。
6. 客户端连接设备时只向 Controller 的逻辑 API 解析当前 owning Hub，然后直连该 Hub 建立 signaling/WebRTC；terminal 权限仍由 daemon 在端到端 DataChannel 内验证。

这保留了 `tgent` 的“一次 setup、客户端测速、自动上线、直接解析 Hub”体验，同时保留 Muxvia 的多账号、订阅、撤销、assignment epoch、离线 Hub 验签和端到端 CapabilityGrant。

## tgent 的实际链路

本节是对同级仓库的代码行为总结，不构成 Muxvia 运行依赖。

### 注册

`tgent` 的 daemon 首次启动生成 agent ID 和 Ed25519 密钥，通过长期 access token 调用 Web setup API。Web 校验账号与订阅后直接创建或更新 agent 记录，然后 daemon 把本地 setup 标记为完成。

相关实现：

- `../tgent/tgent-go/internal/agent/client.go`
- `../tgent/tgent-web/src/app/api/agents/setup/route.ts`

它顺滑的原因不是状态少，而是用户只感知一个 setup 动作，后续步骤都自动进行。

### Hub 选择

Web 返回在线 Hub 列表和容量信息；daemon 并发做 TCP 延迟探测，在本地按延迟、负载和容量打分，选择最优 Hub。连续连接失败后会重新发现和选择。

相关实现：

- `../tgent/tgent-web/src/app/api/hubs/discover/route.ts`
- `../tgent/tgent-go/internal/agent/discover.go`
- `../tgent/tgent-go/cmd/tgent/daemon.go`

这是值得复用的核心：网络质量由实际使用该网络的 daemon 测量，中心只负责资格和容量约束。

### Hub 注册、在线和连接解析

daemon 连接选中的 Hub 并发送 agent ID、公钥和 metadata。Hub 同步调用 Web verify API；成功后把连接保存在内存，并批量向 Web 上报 online/offline。客户端请求 connect API 后，Web 返回 owning Hub 地址及短期 token，客户端直接连接该 Hub。

相关实现：

- `../tgent/tgent-go/internal/hubgrpc/server.go`
- `../tgent/tgent-go/internal/hub/agent_reporter.go`
- `../tgent/tgent-web/src/app/api/internal/hubs/agents/report/route.ts`
- `../tgent/tgent-web/src/app/api/agents/[id]/connect/route.ts`

### 移除

Web 尝试向当前 Hub 发送 kick，然后直接删除 agent 记录。daemon 再连接时若发现公钥不匹配或未 setup，会清除本地身份并重新注册。

相关实现：

- `../tgent/tgent-web/src/app/api/agents/[id]/route.ts`
- `../tgent/tgent-go/internal/agent/client.go`

这个行为操作上简单，但不能作为 Muxvia 的撤销模型。

## 值得复用与必须舍弃的部分

| tgent 行为 | Muxvia 处理 | 原因 |
| --- | --- | --- |
| 一条 setup 路径完成注册 | 复用体验 | 用户不应理解内部多个状态机 |
| daemon 本地并发测 Hub | 复用并加强 | daemon 所在网络的实测最可信 |
| 选中 Hub 后立即建立长连接 | 复用 | 注册后尽快形成可观察在线状态 |
| 连续失败后自动重新发现 | 复用，但走 assignment migration | 不能把迁移伪装成重新注册 |
| connect API 返回 owning Hub | 复用 | 客户端无需知道全局 topology |
| 长期 access token 直接 setup | 舍弃 | 泄露后的设备绑定权限过大 |
| 相同 agent ID 可直接改账号和公钥 | 舍弃 | 可造成账号抢占和身份替换 |
| 加密后上传 daemon 私钥供客户端取回 | 舍弃 | DeviceIdentity 私钥不得离开 daemon |
| Hub 每次注册同步请求 Web 验证 | 舍弃 | Controller 故障会阻断已有合法设备上线 |
| Web 数据库中的 online/hubId 是真值 | 舍弃 | Presence 是短暂运行态，Hub 必须纯内存 |
| 删除数据库行即撤销 | 舍弃 | 无法抵御旧 token、迟到报告和重放 |
| 被移除后 daemon 自动生成新身份 | 舍弃 | 破坏 DeviceIdentity 连续性和审计 |

## 升级后的领域边界

注册流程拆成四个独立但连续的领域动作：

```text
账号授权意图          设备持久绑定             Hub 运行时在线
Web approval    ->    Control Plane commit -> daemon Presence
短期、内存             PostgreSQL 真值          Hub 内存真值
                              |
                              v
                    客户端解析 owning Hub
                              |
                              v
                  WebRTC DataChannel 端到端鉴权
                    CapabilityGrant 由 daemon 验证
```

| 状态 | owner | 持久性 |
| --- | --- | --- |
| enrollment flow、短码、challenge、候选集 | Controller enrollment service | 十分钟内存态 |
| DeviceIdentity 公钥、账号归属、revoked、auth epoch | Control Plane | PostgreSQL 持久真值 |
| HubAssignment、assignment epoch | Control Plane | PostgreSQL 持久真值 |
| device session refresh credential | Control Plane，secret 只交 credential store | PostgreSQL 持久真值 |
| Hub policy projection | Control Plane 签名、Hub 消费 | Hub 内存缓存，有 TTL |
| daemon Presence、PeerSession | owning Hub / daemon runtime | 纯运行时 |
| ClientAccessIdentity、CapabilityGrant、terminal scope | owning daemon | daemon 本地持久真值 |

## 首次注册目标流程

### 1. 创建短期意图

已登录用户在 Web Controller 创建十分钟一次性 enrollment flow，得到 `MXD-...` 短码。Controller 只在内存保存 flow、账号、过期时间和随机 challenge，不提前写 ownership、assignment 或 session。

### 2. daemon 提交身份并开始测速

daemon 用短码提交固定 `device_id`、DeviceIdentity public key、可核对的设备信息和 challenge proof。Controller 返回最多 8 个当前账号、套餐、区域和容量策略允许的 Hub，并携带 `candidate_set_digest` 和过期时间。

daemon 立即并发探测候选 Hub。探测至少验证目标 TLS/协议入口可建立连接，而不只测裸 TCP；本地排序以可达性和实测 RTT 为主。Controller 负责过滤不合格 Hub，不把精确跨租户负载暴露给客户端。Web approval 和 daemon 测速可以并行。

### 3. Web 明确确认

CLI 在身份提交成功后立即输出：

```text
Daemon identity submitted.
Review this device and approve it in Web Controller; waiting for approval...
```

Web 根据归属显示不同动作：

- 未绑定：确认添加设备；
- 已属于当前账号：提示设备已在当前账号中，不创建第二份绑定；
- 活跃属于其他账号：拒绝并提示先在原账号移除；
- 已撤销属于其他账号：显示替换账号确认和旧链接失效影响；
- DeviceIdentity public key 变化：始终拒绝，不静默替换。

### 4. daemon 提交首选 Hub

daemon 提交 `preferred_hub_id`、`candidate_set_digest`、有界 probe observations，以及覆盖 flow、device、候选 digest、首选 Hub 和 observations digest 的 DeviceIdentity proof。

Controller 不再替 daemon 重新计算“最快 Hub”，只校验候选未过期、首选属于候选集、Hub 仍可分配、账号能力有效、proof 正确和设备归属允许提交。

如果首选 Hub 在竞争中失效，Controller 返回 `HUB_CANDIDATE_STALE`，daemon 可在同一个已批准 flow 内提交本地排序的下一个候选；不要求用户重新生成短码或再次批准，也不静默改选。

### 5. 一次事务提交持久真值

所有验证和 credential 预生成完成后，只执行一个最终 PostgreSQL 事务：

1. 新建或更新 DeviceBinding；
2. 写入唯一 HubAssignment 和新的 assignment epoch；
3. 撤销该设备旧 device session；
4. 写入新 device session 的 digest/metadata；
5. 清理允许转移场景下旧账号 topology projection；
6. 写入审计事件。

任何一步失败则全部回滚。Hub Presence、在线报告和 Web 页面投影不进入事务。响应丢失时，delivery grace 只能恢复同一事务结果，不能重复创建 session 或提升 epoch。

### 6. 注册完成后连接 Hub

事务成功即表示“设备已加入账号”。Controller 返回当前 assignment、Hub 目录、短期 EdgeAccess token、device session refresh credential 和 control verification keys。daemon 保存 credential 后立即连接 owning Hub；Hub 使用 Controller 签名 token 和本地有效 policy projection 离线验签，再验证 DeviceIdentity proof。

用户必须看到两个独立结果：

- `Enrollment complete`：持久绑定已提交；
- `Online on <region>`：Presence 已建立。

Hub 暂时不可达时显示“注册完成，正在连接 Hub”并后台有界重试；不得返回 `DEVICE_ENROLLMENT_REQUIRED`，也不得要求重新扫码。

## 重启、掉线和迁移

正常 daemon 启动不执行 enrollment，而是读取固定 DeviceIdentity 和 device session，refresh 当前 assignment 和短期 token，然后连接 owning Hub。refresh 明确返回 revoked 时停止 Cloud Route，但保留 DeviceIdentity、Direct、SSH 和本地 terminal 数据。

同一 Hub 的短暂掉线只重连。连续失败达到阈值后请求 assignment migration，由 Controller 提供新候选，daemon 本地测速并提议新 Hub。以下操作不能再互相替代：

- enrollment：账号首次绑定或明确的撤销后转移；
- reconnect：恢复同一 assignment 的 Presence；
- migration：改变 owning Hub。

## 账号替换

账号替换必须是明确事务，不能通过再次输入短码隐式发生：

1. 活跃归属于其他账号时，新账号 enrollment 一律拒绝。
2. 用户先在原账号移除，Control Plane 写 revoked tombstone 和更高 auth epoch。
3. 新账号用新短码发起 transfer，daemon 使用同一 DeviceIdentity proof。
4. Web 说明旧 Cloud 链接、旧 account session 和旧设备授权投影将失效；Direct/SSH 与本地数据不受影响。
5. transfer 复用同一 Hub 并提升 epoch；跨 Hub 调整留给后续 migration，避免绑定转移同时引入双 Hub 时序。

转移资格由 revoked ownership 和 DeviceIdentity 连续性决定，不依赖旧 assignment 是否恰好过期。旧 assignment 只用于 fencing 和选择是否复用原 Hub，不能把已合法撤销的设备卡在 enrollment 之外。

## 移除与撤销

“Controller 通知 Hub 移除并撤销公钥”在产品层正确，但实现必须先提交持久权限，再执行运行时命令：

1. Control Plane 单事务写 `revoked=true`、`auth_epoch+1`、撤销 device sessions、创建精确 fenced CommandOutbox child 和审计事件。
2. Web 在持久事务完成后显示设备已移除；Hub 是否立即应答作为独立执行状态。
3. 新 policy projection 使旧 token、旧 assignment epoch 和旧 session 不能重新进入 Hub。
4. Controller 向 owning Hub 下发关闭 Presence/PeerSession 命令；Hub 可达时立即断开。
5. daemon 只删除 Cloud device session/control enrollment，不删除 DeviceIdentity、Direct/SSH 配置或本地 terminal truth。
6. Hub 暂时不可达时，撤销仍然成立；恢复后 policy 和 epoch fencing 阻止旧身份上线。

“撤销公钥”应实现为撤销 `(device_id, public_key, auth_epoch)` 的授权关系并保留 tombstone，而不是物理删除公钥记录。这样才能抵御迟到报告、旧 token 重放，并支持审计和同一 DeviceIdentity 的明确转移。

## 客户端连接目标流程

```text
Go Client Engine
  -> Controller/Companion ResolveManagedEndpoint
  <- EndpointRouteLease {
       target_device_id, owning_hub, assignment_epoch,
       short_lived_edge_token, expires_at
     }
  -> owning Hub signaling
  -> daemon Presence
  -> P2P ICE-UDP or TURN UDP/TCP
  -> reliable ordered DataChannel
  -> DeviceHello + channel binding + CapabilityGrant
```

Controller 是逻辑解析入口，Companion 可以承载私有账号/session 适配；Hub 热路径不得同步查询 Controller 或数据库。Hub 只离线校验短期 token、本地有效签名 policy 和 assignment epoch。

Cloud 认证只回答账号/设备是否可使用 managed signaling/P2P/Relay。terminal 列表、输入、文件和 CapabilityGrant 仍在 DataChannel 内由 daemon 验证，Hub/Relay/Controller 看不到或扩大 terminal scope。

## 用户状态、错误和响应延迟

终端只展示少量阶段：Submitting device、Waiting for Web approval、Testing Cloud regions、Completing enrollment、Enrollment complete、Connecting to Cloud、Online via region 和 Action required。

稳定错误至少区分：

- `ENROLLMENT_CODE_EXPIRED`
- `ENROLLMENT_APPROVAL_PENDING`
- `DEVICE_ACTIVE_IN_ANOTHER_ACCOUNT`
- `DEVICE_TRANSFER_CONFIRMATION_REQUIRED`
- `DEVICE_IDENTITY_MISMATCH`
- `NO_REACHABLE_HUB`
- `HUB_CANDIDATE_STALE`
- `ENROLLMENT_COMMIT_CONFLICT`
- `DEVICE_REVOKED`

`Hub connecting`、`Hub temporarily unavailable` 和 `Presence retrying` 是连接状态，不是 enrollment 失败。`DEVICE_ENROLLMENT_REQUIRED` 只能用于本地确实没有有效绑定/session 的场景。

批准后的迟钝来自一秒一次的短轮询和状态混杂。现有 `CompleteDeviceEnrollment` 在等待批准时执行最长 25 秒的有界等待：状态变化后立即继续同一次 complete，等待窗口超时才返回 `ENROLLMENT_APPROVAL_PENDING` 供 CLI 续订。这样不新增 IPC operation、DTO、adapter 或持久消息队列。

## Proto-first 变更

1. `DeviceEnrollmentChallenge` 增加 `candidate_set_digest` 和 `flow_revision`。
2. `CompleteDeviceEnrollmentRequest` 增加 `preferred_hub_id` 和 `candidate_set_digest`。
3. `DeviceEnrollmentProofInput` 绑定首选 Hub、候选 digest 和 observations digest。
4. enrollment projection 增加 revision 和 account conflict/transfer action，但不泄露另一账号 PII。
5. endpoint resolve 返回带 assignment epoch、audience 和 expiry 的短期 `EndpointRouteLease`；现有类型能表达时复用，不另建 DTO。

字段已追加到现有 wire contract 并保持原字段号不变；生成代码和 descriptor baseline 均由仓库生成器更新，未引入第二套 IPC operation 或业务 DTO。

## 实现切片

### ENROLLUX001：契约与状态模型

- Proto 在现有 enrollment message 上增加首选 Hub、候选 digest、proof binding、revision、action 和稳定错误。
- 补 descriptor、round-trip、proof tamper 和兼容测试。
- 固化 enrollment/reconnect/migration/transfer 四种操作边界。

### ENROLLUX002：Controller 最终事务与选择语义

- 从“根据 observations 重新选 Hub”改为“校验 daemon 提议的 Hub”。
- 候选失效可在同一已批准 flow 内重试。
- revoked transfer 不受旧 assignment 过期时间阻塞；同 Hub 提升 epoch，跨 Hub 走 migration。
- 保持现有 ownership、assignment、session、审计单事务和 delivery grace。

### ENROLLUX003：Go Client Engine 与 CLI

- 候选 Hub 有界并发协议探测和本地排序，probe 与 Web approval 并行。
- 复用现有 complete 调用感知批准并输出清晰阶段。
- commit 后独立建立 Presence，失败不回退为未注册。
- 连续失败只触发 migration，不删除 DeviceIdentity。

### ENROLLUX004：Web 替换与移除体验

- 批准前展示设备身份、归属类别和替换影响。
- 活跃跨账号拒绝，已撤销同身份显式 transfer。
- 移除以持久 revoke 成功为主结果，Hub 执行状态单独展示。

### ENROLLUX005：纵向验收

- Go harness 覆盖状态机、proof、事务、重放、响应丢失和迁移。
- 公网 Controller + 双 Edge 覆盖首绑、同账号重试、活跃跨账号拒绝、撤销后转移、过期 assignment、候选失效和 outage。
- Android 模拟器从真实 App UI 覆盖账号登录、设备解析、Cloud AUTO/P2P/Relay、terminal 和移除后不可重连。
- 物理设备纯 5G + Clash 作为补充网络证据，不替代模拟器门禁。

## 实现与验证记录

截至 2026-07-24，当前实现保持在原有 enrollment 链路内：

- `DeviceEnrollmentChallenge`、`DeviceEnrollmentProofInput` 和 `CompleteDeviceEnrollmentRequest` 补充候选摘要、首选 Hub、观测摘要和 flow revision；签名验证覆盖四类篡改。
- daemon 对 Controller 给出的最多八个候选执行有界协议探测并本地排序；Controller 只校验提议 Hub 的候选资格、当前容量、可达观测和既有 assignment，不按客户端上报 RTT 代选。
- 原有 `CompleteDeviceEnrollment` 最长等待网页批准 25 秒，批准事件直接唤醒；超时返回稳定的 `ENROLLMENT_APPROVAL_PENDING`，CLI 在原 flow 内继续等待。首选 Hub 失效时，CLI 重签下一候选并复用同一次批准。
- ownership、assignment、device session、审计和旧账号清理仍由现有 PostgreSQL transaction 一次提交；批准、探测和中间状态不落库。响应丢失通过既有 delivery grace 返回相同结果。
- Web Controller 用稳定 action 区分首次批准、同账号 session 替换、已撤销身份转移、活跃跨账号冲突和身份不匹配；页面不展示另一账号 PII。

已通过的门禁：

- enrollment 目标 Go 包、全量 `make test`、`make test-private`、race、generated check、`make test-clients`。
- Web typecheck/build 与 `WEBUX001` Playwright 9/9。
- `make test-android` 的 standard/devcloud APK 和单 App Cloud 边界检查。
- ARM64/API 35 模拟器安装本次 devcloud APK，真实 `com.muxvia.app/.MainActivity` 冷启动、设备页 UI 树/截图和 Java/native crash scan 通过。APK SHA-256 为 `a042cfa6072b9b439c4f58c67c2ab71549b7063bd38ee3d47415f6b05780046f`；证据位于 `.artifacts/enrollux005/`。

`ENROLLUX005` 尚未完成的唯一门禁是：使用同一 APK 和真实登录后端，从模拟器 App UI 完成 Cloud resolve/connect，并在 Web 移除 daemon 后证明旧连接不可重建。当前模拟器只完成 APK 回归，不能替代这条产品 E2E。

## 验收标准

1. 首次注册只需一个短码和一次 Web 确认；同一 flow 内候选失效不重复确认。
2. CLI 立即提示网页确认；批准后不等待固定轮询周期。
3. daemon 选择实际网络下最快的合格 Hub；Controller 只校验资格，不静默改选。
4. 最终持久写只发生一次事务；批准、测速和中间状态不逐步写数据库。
5. enrollment 成功但 Hub 暂时离线时，设备保持已绑定并自动重连。
6. 重启和掉线不重新 enrollment；Hub 迁移不更换 DeviceIdentity。
7. 活跃设备不能被另一账号抢占；已撤销设备可用同一身份转移，不依赖旧 assignment 到期。
8. 移除在 Control Plane 提交后立即具有权威性；Hub 离线不影响撤销成立。
9. Controller 临时不可用时，Hub 仍可按有效 token、policy 和 epoch 接受已有合法连接。
10. daemon 私钥、CapabilityGrant 和 terminal scope 不进入 Web、Controller、Hub 或 Relay。

## 最终取舍

对 `tgent` 的升级不是增加更多注册步骤，而是保留它的用户路径、替换它的信任模型：

- 用户路径：一次授权、本地测速、自动选 Hub、立即上线、连接时直接解析 owning Hub；
- 持久模型：DeviceBinding + HubAssignment + device session + revoke tombstone；
- 运行模型：Hub policy + Presence + PeerSession 全部可重建；
- 权限模型：Cloud service admission 与 terminal CapabilityGrant 严格分离；
- 失败模型：注册、连接、迁移和撤销各自返回准确状态。

这样可以让 Muxvia 在功能更多的前提下接近 `tgent` 的操作体验，同时避免账号抢占、私钥外流、中心同步依赖和撤销失忆。
