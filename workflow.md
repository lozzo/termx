# 工作流：多 Hub 控制面与 Cloud Development 产品闭环

## 当前结论

- `RTC001-RTC010` 已完成统一 WebRTC Route、Android JNI、Direct/SSH/Cloud、Endpoint、文件、生命周期、弱网和最终 APK E2E；证据见 `docs/remote-platform/rtc010-android-final-e2e.md`。
- Cloud 产品真值见 `docs/remote-platform/cloud-product-spec.md`；多 Hub assignment、纯内存 Hub、daemon topology、CommandOutbox 和 Web 管理真值见 `docs/remote-platform/multi-hub-control-topology-spec.md`；具体 Proto、package、存储、伪代码、迁移删除项和测试矩阵见 `docs/remote-platform/multi-hub-technical-plan.md`。
- 多 Hub 的 assignment、topology、安全和 runtime 核心规划此前已经过四维度 reviewer 复审；最新部署决策进一步收敛为两个二进制：`termx-cloud-controller` 组合 Control Plane + Web Controller，`termx-cloud-edge` 组合 Hub + Relay，但四个领域 owner、身份、generation、状态机和存储边界不合并。
- `HUB001` 已完成 Edge/Hub/Relay control、topology、management Proto，双 TypeScript consumer、descriptor/compatibility 门禁和 daemon ManagedPeerSession registry 纯模型。
- `CLOUDP001` 已完成 PlanCapability、versioned Subscription、Entitlement 与 Hub policy 的统一能力模型；catalog 不再按套餐名分支，devcloud 不再按有效期猜 Relay 配额。
- `HUB002` 已完成 Controller/Edge 双 composition、一个 Controller + 两个 Edge 独立进程、真实 Proto Hub control、strict assignment epoch fencing、纯内存 full/delta/reconciliation、Hub public signaling、无 snapshot 重启和 Relay usage outbox 恢复。
- `HUB003` 已完成 daemon auth + protocol Hello 后 READY、完整 peer teardown 后 CLOSED、单 reporter full inventory、Hub 内存 topology、Controller assignment/ownership 校验与 SQLite replacement，并删除 Web 在线状态直写。
- `CLOUDP003` 已完成持久 Entitlement 到 signed per-Hub policy、周期 fresh full、Hub 内存 managed P2P reservation、稳定拒绝分类及 signaling 到 daemon runtime inventory 的生命周期转交。
- `HUB005` 已完成 enrollment control key、daemon 持久控制回执、opaque terminal access inventory、Web 查询、Controller 签名 revoke、AccessStore 原子撤销和关联 session close 的真实闭环。
- `CLOUDP004` 已完成 Proto-first Relay 周期额度、SQLite 原子 reservation、账号/设备并发、region 与 per-lease clamp、refresh 复核、取消和延迟过期释放，并接通 Controller-Edge-Companion caller-specific TURN credential 纵向链路。
- `CLOUDP005` 已完成 Proto-first signed usage record、独立 Relay control key、Edge durable outbox/pump、Controller 双签名验证、SQLite event journal/sequence 幂等、period/session 聚合、reservation settlement 与重启补报。
- 当前最早未完成切片是 `HUB006`：Edge 内 Relay allocation remote revoke。
- 多 Hub 基础和产品能力存在交叉依赖，必须按本文件交错推进，不能先写完所有 Hub 再补套餐，也不能继续在单进程 devcloud 上堆硬编码。
- development 必须走完整账号、交易、Subscription、Entitlement、managed P2P/Relay、周期 quota、usage、topology 和管理链路；外部 provider 可以使用显式测试实现。
- Web/WASM terminal 产品、iOS/Desktop GUI、多区域数据库、Relay Mesh、真实支付 provider 和复杂计费平台继续延后。

## 架构链路

```text
PlanCatalog -> Subscription -> Entitlement
                         |
                         v
termx-cloud-controller
  Control Plane + Web/API + persistent store
                         |
              HubControl | RelayControl
                         v
termx-cloud-edge × N
  memory-only Hub + Relay runtime/usage outbox
                         |
                    daemon Presence
                         |
                 daemon PeerSession owner
                         |
              P2P DataChannel or Edge Relay
```

- P2P 数据不经过 Hub；Web 分开显示 control owner Hub 和 observed data path。
- Hub 不落盘 policy、Presence、signaling、topology 或 command dedupe。
- Relay 与 Hub 同进程部署，但只允许 Relay usage outbox 落盘；Hub/Relay 不共享业务 map、identity、generation 或 command state。
- daemon Go runtime 是 authenticated managed PeerSession 的 owner。
- Cloud command 只能减少服务或权限，不能扩大 daemon CapabilityGrant。
- Local、Direct、SSH、terminal 和 file 不依赖 Cloud 套餐或 Hub。

## 当前允许范围

- 主动范围：`AGENTS.md`、`workflow.md`、`docs/remote-platform/`、`proto/cloudpb/`、`private/cloud/`（包括后续 `controller/`、`edge/` composition root）、`remote/daemon/`、`remote/webrtc/` 和当前切片测试。
- Client/Companion 联动：`private/cloud/companion/`、`client/adapter/managed/`、`client/runtime/`、`client/binding/`、`cmd/termx/`，只在当前消息链路需要时修改。
- Android/Web 管理联动：`clients/mobile/android/`、`clients/ui/`、`private/cloud/web-controller/`，只在对应登录、topology、management 或 E2E 切片修改。
- 受限联动：`core/` 只允许为 daemon-owned PeerSession lifecycle 和 deny-only AccessStore command 增加最小 port；`scripts/`、`Makefile`、`go.work*` 只用于测试装配。
- 冻结：Web/WASM terminal consumer、iOS/Desktop GUI、插件、KCP/QUIC、Relay Mesh、多区域数据库、开源发布工程和 archive。

## 任务队列

| ID | 状态 | 内容 | 完成条件 |
| --- | --- | --- | --- |
| CBASE001 | 已完成 | Cloud 产品文档基线 | `cloud-product-spec.md` 与 AGENTS/PRD/README 一致 |
| HBASE001 | 已完成 | 多 Hub 控制面设计基线 | `multi-hub-control-topology-spec.md` 经分布式、安全、运行时、产品/API 四角度审核收口；旧 Hub WAL 目标降为历史 |
| TBASE001 | 已完成 | 多 Hub 技术实施规划 | `multi-hub-technical-plan.md` 明确 Proto、代码 owner、控制 transport、SQLite 事务、daemon lifecycle、切片修改范围、旧路径删除和 E2E 证据；四个独立 reviewer 均 PASS |
| DBASE001 | 已完成 | Controller/Edge 双二进制部署基线 | 稳定架构与技术规划冻结 `termx-cloud-controller = Control Plane + Web Controller`、`termx-cloud-edge = Hub + Relay`；只合并 composition/deployment，不合并领域真值或安全身份 |
| HUB001 | 已完成 | Proto 与 daemon registry contract | Edge deployment metadata、独立 Hub/Relay identity/control generation、HubAssignment、per-Hub revision、Presence availability/freshness、PeerSession/access inventory、parent/child CommandOutbox/result、Web/Operator API 全部 proto-first；daemon registry port/harness 证明 revision、READY/CLOSED、Presence replacement 和精确 close |
| CLOUDP001 | 已完成 | PlanCapability 与 Entitlement | `cloud_product.proto` 定义 versioned catalog/Subscription/Entitlement 与统一 PlanCapability；catalog、commerce、Control Plane normalization、signed Hub policy 共用 generated capability；删除 plan/有效期 quota 推断；P2P-only、P2P+Relay、suspended fixture 与 generated/private/race/Web 门禁通过 |
| HUB002 | 已完成 | Controller/Edge composition、assignment 与纯内存 Hub 同步 | 两个 composition root、一个 Controller + 两个 Edge 独立进程、Hub identity/generation、assignment epoch admission/fencing、full/delta/reconciliation、Hub public signaling、无 snapshot 重启与 Relay-only usage outbox 恢复均有真实 socket/process harness；generated、public/private、race、Web 与 layout 门禁通过 |
| HUB003 | 已完成 | daemon ManagedPeerSession 与 topology | Hello 后 READY、完整关闭后 CLOSED；统一 registry revision、上行 runtime report、inventory replacement、Hub topology snapshot、CP 账号/epoch校验和 unknown/stale projection；generated/public/private/race/Web/layout 与双 Edge process harness 通过 |
| CLOUDP002 | 已完成 | 账号、Subscription 与交易 | Proto Price/PaymentAttempt/账号/session/订单/event/transition；SQLite 原子 journal、revision fencing、精确 replay 与重启；Controller Cookie/CSRF Proto JSON；旧 Web 账号交易真值和 direct Entitlement 写入口已删除；generated/public/private/race/Web/双 Edge/doctor 门禁通过 |
| CLOUDP003 | 已完成 | managed P2P 准入与并发 | 持久 Entitlement/auth revision 生成 signed per-Hub policy并在 TTL 内刷新；Hub 原子校验 P2P enabled、ownership、revoke、auth epoch、assignment 与账号并发；reservation 从 signaling 转交 daemon 完整 PeerSession inventory，空 replacement/pending TTL/精确 fence 释放且新 Hub 可重建，Relay-only 保持认证但不占 P2P 名额；public/private/race/客户端生成/双 Edge/doctor 门禁通过 |
| HUB004 | 已完成 | CommandOutbox 与运行时控制 | SQLite parent/child/result journal 与 device revoke 原子事务；Web Proto JSON 创建/查询；KickPresence、daemon revoke 单 Hub、client revoke 跨 Hub fan-out、签名 CloseManagedPeerSession；generation/epoch/Presence/runtime/session/replay/expiry fencing、parent 聚合和独立 daemon ack；真实 Controller-Edge-Presence HTTP harness、public/private/race/client/双 Edge/doctor 门禁通过 |
| HUB005 | 已完成 | daemon deny-only grant revoke | enrollment 返回受窗口约束的 daemon control key；daemon 持久化 enrollment 与精确 replay receipt；双 revision runtime report 上报无 secret 的 opaque access inventory；Web 账号隔离查询并创建 fenced terminal revoke；Controller 签名、Hub 精确转发、daemon AccessStore 原子撤销并关闭关联 session，结果持久回传；真实 Controller-Edge-daemon-Web harness 与 public/private/race/generated/doctor 门禁通过 |
| CLOUDP004 | 已完成 | Relay 周期 quota 与 reservation | generated RelayQuotaPeriod/RelayLeaseReservation/Edge reservation contract；SQLite 原子维护 used/reserved/remaining、精确 replay、账号/设备并发、region、单 lease clamp、取消和 expiry+report-grace 释放；Hub 短期 relay intent 让 client/daemon 共用同一 reservation 并近到期轮换；Controller 重新验证 edge principal、ownership、assignment 与 Entitlement，Edge 离线验签并派生隔离 TURN credential；真实 Controller-Edge-Presence harness、public/private/race/client/双 Edge/doctor 门禁通过 |
| CLOUDP005 | 已完成 | durable UsageLedger 与 settlement | generated RelayUsageEvent/Record/Report/Ack 与 RelayUsageAggregate；Edge 使用稳定独立 Relay control key，authority 先写 durable outbox 再清 pending bytes，at-least-once pump 仅在 Controller 事务提交后 ack；Controller 重验 signed lease 和 Relay event，SQLite 原子提交 event journal、严格 sequence、精确 replay、period/session aggregate、reservation used/release；same-second shutdown、Controller outage、Edge/Controller/SQLite 重启和真实 Pion relay-only DataChannel usage E2E 通过；public/private/race/client/双 Edge/doctor 门禁通过 |
| HUB006 | 待开始 | Edge 内 Relay allocation remote revoke | 在 Edge composition 内接通独立 Relay identity/control generation、lease/session allocation registry、close ack、final usage drain、reservation settlement 和 PARTIAL 结果；不新增第三类服务二进制 |
| CLOUDP006 | 待开始 | Controller 用户账号中心与运营管理面 | Web/API 与 Control Plane 同一 Controller composition；Proto JSON API、套餐/usage/device/topology/command 页面、权限矩阵、账号隔离、CSRF、近期重认证和审计 |
| HUB007 | 待开始 | 双 Edge 控制面 E2E | 一个 Controller + 两个 Edge 独立进程、assignment migration、Controller outage、Edge restart、inventory recovery、四类 command、P2P/Relay close 和隐私扫描；双 Agent 审查 |
| CLOUDP007 | 待开始 | Development 全产品 E2E | Web UI 注册/交易/管理 + Android ARM64 真实 APK P2P/Relay terminal/file、quota、suspend、topology、命令、重启恢复、Direct/SSH 回归；双 Agent 审查 |
| CLOUDP008 | 延后 | Production Cloud 装配与发布 | 仅 HUB007/CLOUDP007 完成后启动；HTTPS、正式存储、Companion 签名、Android production origin、真实 provider |
| WEB001 | 延后 | Web/WASM terminal 产品 | 仅用户明确恢复后启动 |

## 关键准入

### HUB001

- 新 contract 位于 `proto/cloudpb/`，generated/descriptor/round-trip/unknown-field 通过。
- Edge deployment metadata 不得替代 Hub/Relay 独立 identity、sender role、control generation 和 sequence。
- 明确 `assignment_epoch`、`presence_session_id`、`session_incarnation`、`daemon_runtime_generation`、`registry_revision` 和 per-Hub `projection_revision`。
- Presence availability 与 freshness 分离；command authority/delivery/execution/effect 分离。
- Browser management query/command、pagination/filter/error 也必须 proto-first。
- parent/child command、`TerminalAccessInventorySnapshot` 和 `ListDaemonTerminalAccess` 必须进入 Proto；Web 不从 active session 猜 revoke reference。
- daemon registry fake harness 证明 READY/CLOSED/inventory 线性化和精确 close result。
- `git diff --check`。

### CLOUDP001

- 同一 PlanCapability 生成 Entitlement 和 Hub policy；禁止 `if plan == "pro"`、按 `validUntil` 猜能力和固定 quota。
- P2P-only、P2P+Relay、suspended fixture 通过。
- 受影响 private Cloud module tests；`git diff --check`。

### HUB002

- `termx-cloud-controller` 只组合 Control Plane、Web/API、store 和独立 listener；`termx-cloud-edge` 只组合 Hub、Relay 和 health/listener。
- 至少启动一个 Controller 与两个 Edge 独立进程；禁止继续把 Controller、Hub、Relay 全放在同一进程或用 direct pointer 连接网络边界。
- 两个 Hub identity 不能冒充；同 Hub ID 旧 control generation 被 fencing。
- 新 assignment 只有旧 Hub fence ack 或旧 lease expiry 后生效，禁止跨 Hub 双活。
- Hub 必须使用 timer 或等价生命周期机制在 assignment expiry 主动关闭旧 Presence/signaling，不能只等下一次请求触发检查。
- 每 Hub projection revision 连续；同 revision/digest reconciliation 幂等，gap/rollback/digest conflict fail closed。
- Hub 无 snapshot store；CP 不可用重启时 readiness=false。
- Edge 重启时 Hub 必须 full sync；Relay 只能恢复未确认 usage event，不能恢复 allocation/connection。
- 相关 race；`git diff --check`。

### HUB003

- daemon registry 在 auth + Hello 后 READY，资源全部结束后 CLOSED。
- inventory/event 共用 registry revision；旧 revision/epoch/presence/runtime generation 不覆盖当前 projection。
- CP 从持久 ownership/assignment 推导 account，不信任 Hub account 字段。
- online/offline/unknown 与 fresh/stale 场景测试。
- 相关 race；`git diff --check`。

### CLOUDP002-CLOUDP005

- 每个切片按 `cloud-product-spec.md` 的状态机、quota 和 usage 规则补 contract/harness。
- payment replay、Subscription transition、P2P concurrency、period reservation、signed usage、迟到/冲突/重启恢复必须分别证明。
- 不为真实支付、多区域或分布式 quota 扩大范围。

### HUB004-HUB006

- Kick 绑定精确 Presence；旧 command 不影响新 incarnation。
- persistent revoke commit 与 runtime enforcement 独立显示。
- client device revoke 根据当前 topology 对多个 Hub/session 生成 child command；一个 child 超时只使 parent PARTIAL，不回滚 authority revoke。
- P2P close 使用独立 daemon CommandResult，不用 topology CLOSED 冒充 ack。
- grant revoke 端到端验签、deny-only、daemon owner 执行。
- Relay close 必须精确关闭全部 lease allocation、drain usage 并 settlement；部分完成返回 PARTIAL。
- Hub 与 Relay 即使同 Edge 进程也必须通过独立 port/control owner 协作，禁止 Hub 直接修改 Relay allocation map。
- 错 Hub/epoch/auth/presence/session/replay/expiry 全部 fail closed。

### CLOUDP006

- 用户与运营权限矩阵、CSRF、近期重认证、账号隔离和稳定错误通过。
- Web 分开显示 signaling control relation 与 P2P/Relay data path。
- 超时只显示 UNKNOWN/STALE，不伪造 offline。
- UI 不接触 terminal/grant/payload。

### HUB007/CLOUDP007

- 使用独立进程的一个 Controller、至少两个 Edge、多个 daemon/client；每个 Edge 内 Hub 纯内存，Relay 只有 usage outbox 可持久化。
- Web 行为从真实 Web UI 发起；Android 行为从同一个最终 ARM64 APK UI 发起。
- 记录 APK SHA-256、AVD/ABI/API、Hub identity/assignment revision、command receipt 和 topology evidence。
- 覆盖 Controller outage、Edge restart、assignment migration、stale event、command replay、P2P/Relay close、grant revoke、quota、usage、Direct/SSH 回归、crash/secret scan。
- 架构 reviewer 与代码 reviewer 均 PASS。

## 执行规则

1. 每轮先读取 `AGENTS.md`、`cloud-product-spec.md`、`multi-hub-control-topology-spec.md`、`multi-hub-technical-plan.md` 和本文件，再检查 `git status --short --branch`。
2. 只执行最早的 `进行中` 或 `待开始` 切片；`延后` 不属于活动队列。
3. 待开始切片先标记 `进行中`，不得跨切片实现后续能力。
4. 新跨边界字段固定执行 `proto -> generated -> compatibility harness -> domain/runtime -> adapter -> UI/client`。
5. 先写最小真实 harness；不能用固定账号、直接写 store、手工改 projection 或 fake ack 冒充产品链路。
6. 不建设 Hub-to-Hub forwarding、多区域、真实支付、复杂优惠、Web terminal 或通用分布式平台。
7. 每个切片完成准入、更新状态并使用中文提交信息提交。
8. `HUB007` 和 `CLOUDP007` 默认双 Agent 审查；其它切片仅用户明确要求时审查。
