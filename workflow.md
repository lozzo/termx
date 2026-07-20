# 工作流：Cloud Development 产品能力闭环

## 当前结论

- `RTC001-RTC010` 已完成统一 WebRTC Route、Android JNI、Direct/SSH/Cloud、Endpoint registry/share、文件传输、生命周期、弱网和最终 APK E2E。历史完成证据见 `docs/remote-platform/rtc010-android-final-e2e.md`，不再在本文件重复展开。
- 当前最早未完成切片是 `CLOUDP001`：统一套餐能力与 Entitlement contract。
- 当前目标不是先做生产部署，而是在显式 development Cloud 中完成真实账号、交易、Subscription、Entitlement、managed P2P/Relay 准入、周期 quota、usage 和管理闭环。
- development 只允许支付、邮件、短信、DNS/TLS 等外部 provider 使用测试实现；不得使用固定 entitlement、硬编码套餐能力或绕过交易/结算状态机。
- Cloud 产品稳定语义以 `docs/remote-platform/cloud-product-spec.md` 为准；连接和安全边界继续以 `product-prd.md`、`architecture-spec.md` 和 `security-protocol-spec.md` 为准。
- Web/WASM terminal 产品、iOS/Desktop GUI、多区域、Relay Mesh、真实支付 provider 和复杂计费平台继续延后。

## 产品链路

```text
PlanCatalog
  -> Subscription
  -> Entitlement
  -> signed Hub EdgePolicy / RelayBudget
  -> managed P2P admission / RelayLease
  -> signed Relay UsageEvent
  -> durable UsageLedger + billing-period quota
  -> Account Center / Operator Console
```

- Local、Direct、SSH、Endpoint、pairing、terminal 和 file 不依赖 Cloud 套餐。
- managed P2P 与 Relay 是两个独立套餐能力；禁止 Relay 不等于禁止 managed P2P。
- Cloud 套餐不能扩大或撤销 daemon `CapabilityGrant`。
- managed P2P 不经过云数据面，只统计 signaling/session metadata，不伪造精确 bytes。
- Relay 可以按加密 packet bytes 统计流量，但不能读取 DataChannel payload。

## 当前允许范围

- 主动范围：`AGENTS.md`、`workflow.md`、`docs/remote-platform/`、`private/cloud/` 和当前切片对应测试。
- Proto 联动：`proto/cloudpb/` schema 及生成代码，只在当前切片存在跨进程或跨语言产品 contract 时按 proto-first 顺序修改。
- Android/UI 联动：`clients/mobile/android/`、`clients/ui/` 只在账号中心、Cloud 登录、套餐/usage 展示或 Android E2E 切片最小修改。
- Client 联动：`client/adapter/managed/`、`client/runtime/`、`client/binding/` 和 `cmd/termx/` 只在服务能力错误、managed Route eligibility 或真实纵向消息链路需要时修改。
- 受限联动：`scripts/`、`Makefile`、`go.work*`，只用于当前切片测试和装配。
- 冻结：Web/WASM terminal consumer、iOS/Desktop GUI、插件、KCP/QUIC、Relay Mesh、多区域、开源发布工程和 archive。

## 任务队列

| ID | 状态 | 内容 | 完成条件 |
| --- | --- | --- | --- |
| CBASE001 | 已完成 | Cloud 产品文档基线 | `cloud-product-spec.md` 定义账号、套餐、交易、Subscription、Entitlement、P2P/Relay、quota、usage、用户/运营管理和 development E2E；AGENTS、PRD、README 与本工作流引用一致 |
| CLOUDP001 | 待开始 | 统一 PlanCapability 与 Entitlement contract | 删除 devcloud 按 plan/有效期硬编码能力；catalog、Subscription 和 Entitlement 使用同一能力模型；development fixture 至少证明 P2P-only、P2P+Relay、suspended 三种结果；跨边界字段 proto-first |
| CLOUDP002 | 待开始 | 账号、Subscription 与交易状态机 | 注册/登录/session/refresh、订单、测试支付 normalized event、trial/active/grace/past_due/suspended/cancelled/expired、续费/取消/升级降级和幂等审计形成持久闭环；测试 provider 不直接写 Entitlement |
| CLOUDP003 | 待开始 | managed P2P 套餐准入 | Subscription -> Entitlement -> signed EdgePolicy；Hub 离线执行 managed P2P enabled、账号/device ownership、revoke、auth epoch 和 concurrency；失败/取消/过期释放 reservation，不伪造 target offline 或 Relay fallback |
| CLOUDP004 | 待开始 | Relay 周期 quota 与 lease reservation | 在已有 per-lease bytes/bitrate/concurrency enforcement 上增加 billing period used/reserved/remaining、账号/设备并发、region、lease refresh 复核、到期释放和 quota deny；重复申请 lease 不能绕过周期额度 |
| CLOUDP005 | 待开始 | durable UsageLedger 与结算 | signed usage event/outbox/幂等/sequence 规则保留；event journal、reservation、settlement、周期聚合和重启恢复持久化；按 session+route 只计一次，提供账号和管理查询 contract |
| CLOUDP006 | 待开始 | 用户账号中心与运营管理面 | 用户可见套餐、Subscription、周期、used/reserved/remaining、订单、登录设备和 daemon；运营侧可查询、suspend/restore、撤销 device、调整测试账号套餐并查看 quota deny/audit；不暴露 terminal/grant/payload |
| CLOUDP007 | 待开始 | Development 全产品 E2E | Web UI 注册/登录/checkout/test payment -> Entitlement/EdgePolicy；Android ARM64 模拟器真实 APK 登录、enroll/pair、P2P terminal、升级后 Relay terminal/file；验证速率/并发/per-lease/period quota、suspend、Direct/SSH 不回归、重启恢复和 crash/secret scan；双 Agent 审查 |
| CLOUDP008 | 延后 | Production Cloud 装配与发布 | 仅 CLOUDP007 完成后启动；production HTTPS adapter、正式存储、Companion 签名发布、Android production origin、真实 provider 和邀请制发布 |
| WEB001 | 延后 | Web/WASM terminal 产品恢复 | 仅用户明确恢复 Web 后启动；不能抢占 Cloud development 产品闭环 |

## 切片准入

### CLOUDP001

- PlanCapability validation、catalog round-trip、unknown/duplicate plan 和非法 quota 测试。
- P2P-only、P2P+Relay、suspended fixture 必须从同一 PlanCapability 生成 Entitlement 和 EdgePolicy。
- 扫描禁止 `if plan == "pro"`、按 `validUntil` 猜能力以及 devcloud 固定 quota 成为运行真值。
- `go test ./... -count=1` 覆盖受影响 private Cloud modules；`git diff --check`。

### CLOUDP002

- Subscription 状态转换表测试；非法转换 fail closed。
- checkout 不改变 Entitlement；只有 normalized、已验签、幂等 payment event 可以提交商业状态。
- payment replay、失败、取消、续费、升级降级、到期和 suspend 测试。
- 账号/session/Subscription/order/payment event 重启恢复。
- `git diff --check`。

### CLOUDP003

- Hub 不回源的 signed policy harness。
- managed P2P enabled/disabled、账号隔离、device revoke、stale revision、并发耗尽和释放测试。
- entitlement deny 与 target offline 使用不同稳定错误；不得自动 Relay fallback。
- 相关 race；`git diff --check`。

### CLOUDP004

- period reservation 并发一致性和 crash/restart 恢复。
- 多次 lease、refresh、expiry、cancel、超额、region、账号/设备 concurrency 测试。
- Relay 实际执行 bitrate、bytes、allocation concurrency；无共享 credential fallback。
- 相关 race；`git diff --check`。

### CLOUDP005

- signed usage、重复、冲突、sequence rollback、迟到和 lease 越界测试。
- outbox/ledger/reconciliation 重启恢复和 reservation settlement 测试。
- 同 session/route 多事件和未来 multi-hop fixture 只聚合一次。
- `git diff --check`。

### CLOUDP006

- Web/API 账号隔离、CSRF/session、用户 usage 查询、运营审计和敏感字段扫描。
- UI 不推导套餐能力，不读取 terminal inventory 或 CapabilityGrant。
- build/typecheck；`git diff --check`。

### CLOUDP007

- Web 交易动作必须由真实 Web UI 发起；Android 连接、terminal、file 和恢复动作必须由真实 APK UI 发起。
- 使用同一个最终 development APK，记录 APK SHA-256、AVD/ABI/API、服务配置和证据矩阵。
- 覆盖 P2P-only、升级后 Relay、四类 quota、suspend、重启恢复、Direct/SSH 回归、logcat/native crash 和 secret scan。
- 全仓相关 Go/race、Web build/test、Android unit/instrumentation/build 和 `git diff --check`。
- 架构 reviewer 与代码 reviewer 只按本切片规格和证据判断；两者均 PASS 后提交。

## 执行规则

1. 每轮先读取 `AGENTS.md`、`docs/remote-platform/cloud-product-spec.md` 和本文件，再检查 `git status --short --branch`。
2. 只执行任务队列中最早的 `进行中` 或 `待开始` 切片；`延后` 不属于活动队列。
3. 待开始切片先标记 `进行中`，只实现该切片，不跨切片扩张。
4. 新跨边界字段固定执行 `proto -> generated -> compatibility harness -> domain/runtime -> adapter -> UI/client`。
5. 先补当前切片最小真实 harness，再修改实现；不得把固定测试账号或直接写 store 当作产品链路证据。
6. 不为未来多区域、真实支付、复杂优惠、Web terminal 或分布式 quota 提前增加抽象。
7. 每个切片完成准入、更新状态并使用中文提交信息提交。
8. 只有 `CLOUDP007` 默认执行双 Agent 审查；其它切片仅在用户明确要求时执行。
