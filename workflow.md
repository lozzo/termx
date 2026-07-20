# 工作流：多 Hub 控制面与 Cloud Development 产品闭环

## 当前结论

- `RTC001-RTC010` 已完成统一 WebRTC Route、Android JNI、Direct/SSH/Cloud、Endpoint、文件、生命周期、弱网和最终 APK E2E；证据见 `docs/remote-platform/rtc010-android-final-e2e.md`。
- Cloud 产品真值见 `docs/remote-platform/cloud-product-spec.md`；多 Hub assignment、纯内存 Hub、daemon topology、CommandOutbox 和 Web 管理真值见 `docs/remote-platform/multi-hub-control-topology-spec.md`。
- 当前最早未完成切片是 `HUB001`：Hub/control/topology/management Proto 与 daemon session registry contract。
- 多 Hub 基础和产品能力存在交叉依赖，必须按本文件交错推进，不能先写完所有 Hub 再补套餐，也不能继续在单进程 devcloud 上堆硬编码。
- development 必须走完整账号、交易、Subscription、Entitlement、managed P2P/Relay、周期 quota、usage、topology 和管理链路；外部 provider 可以使用显式测试实现。
- Web/WASM terminal 产品、iOS/Desktop GUI、多区域数据库、Relay Mesh、真实支付 provider 和复杂计费平台继续延后。

## 架构链路

```text
PlanCatalog -> Subscription -> Entitlement
                         |
                         v
Control Plane -> per-Hub projection/control stream -> memory-only Hub
                         |                              |
                         |                         daemon Presence
                         |                              |
Web CommandOutbox -------+----------------------> daemon PeerSession owner
                                                        |
                              P2P DataChannel or Relay <-+
                                                        |
Relay usage -> durable UsageLedger ---------------------+
```

- P2P 数据不经过 Hub；Web 分开显示 control owner Hub 和 observed data path。
- Hub 不落盘 policy、Presence、signaling、topology 或 command dedupe。
- daemon Go runtime 是 authenticated managed PeerSession 的 owner。
- Cloud command 只能减少服务或权限，不能扩大 daemon CapabilityGrant。
- Local、Direct、SSH、terminal 和 file 不依赖 Cloud 套餐或 Hub。

## 当前允许范围

- 主动范围：`AGENTS.md`、`workflow.md`、`docs/remote-platform/`、`proto/cloudpb/`、`private/cloud/`、`remote/daemon/`、`remote/webrtc/` 和当前切片测试。
- Client/Companion 联动：`private/cloud/companion/`、`client/adapter/managed/`、`client/runtime/`、`client/binding/`、`cmd/termx/`，只在当前消息链路需要时修改。
- Android/Web 管理联动：`clients/mobile/android/`、`clients/ui/`、`private/cloud/web-controller/`，只在对应登录、topology、management 或 E2E 切片修改。
- 受限联动：`core/` 只允许为 daemon-owned PeerSession lifecycle 和 deny-only AccessStore command 增加最小 port；`scripts/`、`Makefile`、`go.work*` 只用于测试装配。
- 冻结：Web/WASM terminal consumer、iOS/Desktop GUI、插件、KCP/QUIC、Relay Mesh、多区域数据库、开源发布工程和 archive。

## 任务队列

| ID | 状态 | 内容 | 完成条件 |
| --- | --- | --- | --- |
| CBASE001 | 已完成 | Cloud 产品文档基线 | `cloud-product-spec.md` 与 AGENTS/PRD/README 一致 |
| HBASE001 | 已完成 | 多 Hub 控制面设计基线 | `multi-hub-control-topology-spec.md` 经分布式、安全、运行时、产品/API 四角度审核收口；旧 Hub WAL 目标降为历史 |
| HUB001 | 待开始 | Proto 与 daemon registry contract | Hub identity/control stream、HubAssignment、per-Hub revision、Presence availability/freshness、PeerSession/access inventory、parent/child CommandOutbox/result、Web/Operator API 全部 proto-first；定义 daemon registry port/harness，不接真实网络 |
| CLOUDP001 | 待开始 | PlanCapability 与 Entitlement | 删除 plan/有效期硬编码；catalog、Subscription projection、Entitlement 和 Hub policy 使用同一能力模型；P2P-only、P2P+Relay、suspended fixture |
| HUB002 | 待开始 | Hub registry、assignment 与纯内存同步 | Hub deployment identity、唯一 control stream generation、strict assignment lease fencing、target-owner routing、per-Hub full/delta/reconciliation；Hub 重启不读磁盘 snapshot |
| HUB003 | 待开始 | daemon ManagedPeerSession 与 topology | Hello 后 READY、完整关闭后 CLOSED；统一 registry revision、上行 runtime report、inventory replacement、Hub topology snapshot、CP 账号/epoch校验和 unknown/stale projection |
| CLOUDP002 | 待开始 | 账号、Subscription 与交易 | 注册/登录/session/refresh、订单、测试 payment event、状态转换、续费/取消/升级降级和持久审计；测试 provider 不直接写 Entitlement |
| CLOUDP003 | 待开始 | managed P2P 准入与并发 | Entitlement -> signed per-Hub policy；P2P enabled、ownership、revoke、auth epoch、assignment 和 concurrency reservation 由 Hub 内存执行 |
| HUB004 | 待开始 | CommandOutbox 与运行时控制 | authority/delivery/execution/effect 分离；KickPresence、daemon revoke 单 Hub、client revoke 跨 Hub child fan-out、CloseManagedPeerSession；精确 fencing、parent 聚合和独立 daemon ack |
| HUB005 | 待开始 | daemon deny-only grant revoke | enrollment control key、CP-signed deterministic command、opaque revoke reference、daemon AccessStore 原子撤销和 session close；Cloud 不能 grant/expand |
| CLOUDP004 | 待开始 | Relay 周期 quota 与 reservation | period used/reserved/remaining、账号/设备并发、region、per-lease bytes/bitrate、refresh 复核、expiry/cancel release |
| CLOUDP005 | 待开始 | durable UsageLedger 与 settlement | signed usage/outbox、event journal、幂等/sequence、period aggregation、reservation settlement 和重启恢复 |
| HUB006 | 待开始 | Relay allocation remote revoke | Relay control identity/stream、lease/session allocation registry、close ack、final usage drain、reservation settlement 和 PARTIAL 结果 |
| CLOUDP006 | 待开始 | 用户账号中心与运营管理面 | Proto JSON API；套餐/usage/device/topology/command 页面；权限矩阵、账号隔离、CSRF、近期重认证和审计 |
| HUB007 | 待开始 | 双 Hub 控制面 E2E | 两个独立纯内存 Hub、assignment migration、CP outage、Hub restart、inventory recovery、四类 command、P2P/Relay close 和隐私扫描；双 Agent 审查 |
| CLOUDP007 | 待开始 | Development 全产品 E2E | Web UI 注册/交易/管理 + Android ARM64 真实 APK P2P/Relay terminal/file、quota、suspend、topology、命令、重启恢复、Direct/SSH 回归；双 Agent 审查 |
| CLOUDP008 | 延后 | Production Cloud 装配与发布 | 仅 HUB007/CLOUDP007 完成后启动；HTTPS、正式存储、Companion 签名、Android production origin、真实 provider |
| WEB001 | 延后 | Web/WASM terminal 产品 | 仅用户明确恢复后启动 |

## 关键准入

### HUB001

- 新 contract 位于 `proto/cloudpb/`，generated/descriptor/round-trip/unknown-field 通过。
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

- 两个 Hub identity 不能冒充；同 Hub ID 旧 control generation 被 fencing。
- 新 assignment 只有旧 Hub fence ack 或旧 lease expiry 后生效，禁止跨 Hub 双活。
- Hub 必须使用 timer 或等价生命周期机制在 assignment expiry 主动关闭旧 Presence/signaling，不能只等下一次请求触发检查。
- 每 Hub projection revision 连续；同 revision/digest reconciliation 幂等，gap/rollback/digest conflict fail closed。
- Hub 无 snapshot store；CP 不可用重启时 readiness=false。
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
- 错 Hub/epoch/auth/presence/session/replay/expiry 全部 fail closed。

### CLOUDP006

- 用户与运营权限矩阵、CSRF、近期重认证、账号隔离和稳定错误通过。
- Web 分开显示 signaling control relation 与 P2P/Relay data path。
- 超时只显示 UNKNOWN/STALE，不伪造 offline。
- UI 不接触 terminal/grant/payload。

### HUB007/CLOUDP007

- 使用独立进程的一个 Control Plane、至少两个纯内存 Hub、Relay、多个 daemon/client。
- Web 行为从真实 Web UI 发起；Android 行为从同一个最终 ARM64 APK UI 发起。
- 记录 APK SHA-256、AVD/ABI/API、Hub identity/assignment revision、command receipt 和 topology evidence。
- 覆盖 CP outage、Hub restart、assignment migration、stale event、command replay、P2P/Relay close、grant revoke、quota、usage、Direct/SSH 回归、crash/secret scan。
- 架构 reviewer 与代码 reviewer 均 PASS。

## 执行规则

1. 每轮先读取 `AGENTS.md`、`cloud-product-spec.md`、`multi-hub-control-topology-spec.md` 和本文件，再检查 `git status --short --branch`。
2. 只执行最早的 `进行中` 或 `待开始` 切片；`延后` 不属于活动队列。
3. 待开始切片先标记 `进行中`，不得跨切片实现后续能力。
4. 新跨边界字段固定执行 `proto -> generated -> compatibility harness -> domain/runtime -> adapter -> UI/client`。
5. 先写最小真实 harness；不能用固定账号、直接写 store、手工改 projection 或 fake ack 冒充产品链路。
6. 不建设 Hub-to-Hub forwarding、多区域、真实支付、复杂优惠、Web terminal 或通用分布式平台。
7. 每个切片完成准入、更新状态并使用中文提交信息提交。
8. `HUB007` 和 `CLOUDP007` 默认双 Agent 审查；其它切片仅用户明确要求时审查。
