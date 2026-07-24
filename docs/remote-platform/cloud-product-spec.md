# Muxvia Cloud 产品与服务能力规格

状态：Cloud 产品能力主线唯一稳定产品基准

活动切片、实现顺序、允许目录和验收证据只记录在仓库根目录 `workflow.md`。本文定义账号、套餐、交易、服务能力、限额、用量和管理面的稳定产品语义，不维护研发状态。

多 Hub assignment、纯内存同步、daemon Presence、PeerSession topology、CommandOutbox 和 Web 远程管理以 `multi-hub-control-topology-spec.md` 为唯一稳定架构基准。

## 1. 产品目标

Muxvia Cloud 是同一个 Muxvia App 内的可选托管连接能力，负责账号目录、managed WebRTC signaling、ICE-UDP、TURN Relay、套餐准入、用量统计和账号管理。

开发模式必须提供完整产品链路：

```text
注册/登录
  -> 选择套餐或取得默认套餐
  -> 订单与测试支付
  -> Subscription
  -> Entitlement
  -> signed Hub policy
  -> managed P2P / Relay admission
  -> Relay usage
  -> 周期额度结算
  -> 用户与运营管理视图
```

开发模式只允许把真实支付、邮件、短信、DNS、TLS 证书签发等外部 provider 替换为显式测试 provider。不得用固定账号、固定 entitlement、硬编码套餐能力或绕过交易状态机来冒充产品完成。

## 2. 免费能力与收费边界

- Local Unix、Direct WebRTC TCP、SSH WebRTC TCP、Endpoint、pairing、terminal protocol、history 和 file API 不依赖 Cloud 账号或订阅。
- Muxvia Cloud managed P2P 和 Muxvia Cloud Relay 是可由套餐控制的托管服务能力。
- Cloud 订阅失效只拒绝新的 managed service admission 或 lease，不删除 Endpoint、不撤销 daemon `CapabilityGrant`，也不阻断 Direct/SSH。
- 已建立的端到端 DataChannel 不经过 Control Plane。套餐变化默认在下一次连接、重连或 Relay lease refresh 时生效；不得伪造服务端可立即切断任意端到端会话的能力。
- Relay 只统计转发的加密 packet bytes，不能读取 terminal payload。managed P2P 的 DataChannel bytes 不经过云数据面，因此不能被伪装成精确流量统计。

## 3. 领域真值与依赖方向

```text
PlanCatalog
  -> Subscription
  -> Entitlement
  -> signed Hub policy / RelayBudget
  -> Hub admission / RelayLease
  -> Relay UsageEvent
  -> UsageLedger / BillingPeriodUsage
  -> AccountCenter / OperatorConsole
```

### 3.1 PlanCatalog

`PlanCatalog` 是套餐名称、价格展示和能力模板的发布真值。页面不能自行推导价格，Control Plane 也不能根据 plan ID 写硬编码分支。

套餐能力至少包含：

```text
managed_p2p_enabled
managed_p2p_max_concurrency
relay_enabled
relay_allowed_regions[]
relay_max_concurrency
relay_max_bitrate_kbps
relay_max_bytes_per_lease
relay_max_bytes_per_period
relay_max_lease_duration
cloud_device_limit
```

- 金额、币种、周期和 CTA 属于 catalog 展示与交易输入。
- 实际准入只读取归一化 `Entitlement`，不能在 Hub 或 Relay 中解释商品价格和订单。
- plan ID 不得成为能力判断。增加套餐应优先增加配置记录，不增加 `if plan == ...` 分支。
- 第一阶段 development fixture 至少覆盖三种能力结果：允许 managed P2P 但不允许 Relay、同时允许 managed P2P 与受限 Relay、暂停后两者均拒绝。名称和价格可以由测试 catalog 配置，不写死到领域层。

### 3.2 Subscription

`Subscription` 是账号当前商业关系的持久真值，至少包含：

```text
account_id
plan_id
status
current_period_start
current_period_end
cancel_at_period_end
provider_ref?
revision
```

状态至少支持：

```text
trialing
active
grace
past_due
suspended
cancelled
expired
```

- `trialing`、`active` 和产品显式允许的 `grace` 可以生成有效 entitlement。
- `past_due`、`suspended`、`cancelled` 和 `expired` 的新服务准入规则必须由产品 policy 显式决定，不能由 UI 猜测。
- 升级、降级、续费、取消续订、支付失败、恢复和人工暂停都必须生成单调 revision 和审计事件。

### 3.3 Entitlement

`Entitlement` 是 Subscription、套餐能力、风控和周期额度归一化后的服务准入真值。

- Hub 只消费签名后的最小 edge policy projection。
- Relay 只消费短期、session-bound、quota-bound 的签名 lease。
- 客户端提交的套餐名、额度、速率或地区偏好不能扩大 entitlement。
- entitlement 不能包含 terminal scope、terminal ID、CapabilityGrant 或 daemon 私钥。

### 3.4 UsageLedger

`UsageLedger` 是 Relay 已验签用量事件和周期聚合的持久真值。

- Relay 先把 signed usage event 写入 durable outbox，再异步至少一次上报。
- Control Plane 按 `relay_id + lease_id + sequence` 幂等接收，并拒绝签名错误、sequence 回退、lease 越界和重复冲突。
- 用户账单按 `managed_session_id + route_id` 只计算一次；未来多 hop Relay 不能机械重复计费。
- 周期统计至少提供 `used_bytes`、`reserved_bytes`、`remaining_bytes`、`period_start` 和 `period_end`。
- usage event、reservation 和 settlement 必须在进程重启后恢复。

### 3.5 Proto API contract

- PlanCatalog、Subscription、Entitlement projection、订单/支付结果、周期 Usage、账号中心和运营管理的所有跨进程、跨语言或官方客户端 API 必须先定义在 `proto/cloudpb/`。
- Go service、Companion、Android/Kotlin、TypeScript Web Controller 和测试 provider 只能消费生成类型或由生成类型确定性映射出的 UI view model，不得手写第二套业务 DTO。
- 浏览器 HTTP 可以选择 protobuf media type，或把 proto message 以确定性的 Proto JSON projection 暴露；HTTP framing 不改变 proto schema truth。
- 数据库 row 和 provider SDK object 可以作为内部模型，但进入领域状态机或对外 API 前必须显式映射到 proto/domain contract，不能直接泄漏给客户端。
- 新字段固定执行 `proto schema -> generated code -> compatibility harness -> domain -> service adapter -> client/UI`。

## 4. 账号与设备

### 4.1 账号能力

开发模式和正式模式都必须支持：

- 邮箱密码注册、登录和退出。
- 浏览器账号 session 与 Android/Companion device activation。
- access credential refresh/rotation。
- 登录设备列表和设备撤销。
- 修改密码及撤销旧账号 session。
- 当前套餐、订阅状态、周期和用量查询。
- 订单历史和套餐变更入口。

测试 provider 可以跳过真实邮件投递，但不能跳过注册、密码校验、session、refresh、撤销和审计链路。

### 4.2 daemon enrollment

- daemon 使用自身 `DeviceIdentity` 对一次性 enrollment challenge 签名。
- Control Plane 保存账号 ownership、daemon public key、revoke 和 auth epoch。
- enrollment 只让账号目录和 Hub 识别 daemon，不授予 terminal capability。
- 移除或撤销 daemon 后拒绝新的 managed connection；Direct/SSH 和 daemon 本地 identity 不被删除。

### 4.2.1 Web 账号与设备入口

- 普通用户导航固定围绕概览、设备、套餐和账号；topology、command outbox 等诊断能力只能作为设备页的高级详情，不得与主要任务争夺一级入口。
- Web 只提供一个“添加设备”入口，再由用户选择手机/平板 activation 或 daemon enrollment；两条流程都必须展示创建、等待设备提交、核对公开 metadata、批准和完成状态。
- 设备列表优先展示用户可识别的名称和在线/撤销状态；device ID、Hub、assignment、generation 等技术身份只进入可展开详情。
- 撤销设备、断开连接和其它危险管理动作必须按具体动作展示近期认证确认，不得依赖一个全局解锁面板掩盖动作对象。
- 该信息架构只消费既有 Proto API，不改变 activation、enrollment、topology、command 或账号安全真值。

### 4.3 terminal pairing

- Cloud 登录和 daemon enrollment 不能替代 `CapabilityGrant`。
- 用户仍需通过 daemon 签发的一次性 PairingTicket 或显式授权流程取得 client-bound grant。
- App 必须区分“账号已登录”“daemon 已发现”“terminal 尚未授权”和“可以连接”。

## 5. Managed P2P 准入

managed P2P 指 Muxvia Cloud 提供目录和 signaling，最终 selected path 为 peer-to-peer ICE-UDP 的连接。

- Hub 使用本地已验签 edge policy 判断账号、client device、target daemon、revoke、auth epoch 和 `managed_p2p_enabled`。
- Hub 请求热路径不得同步查询 Control Plane 或数据库。
- `managed_p2p_max_concurrency` 限制账号当前正在创建或存活的 managed P2P session；取消、失败和过期必须释放 reservation。
- managed P2P 禁用时返回稳定 entitlement 错误，不得伪造目标离线，也不得自动改走 Relay 掩盖拒绝原因。
- Cloud 只能统计 signaling、session outcome、时长近似值和 path metadata，不能统计 P2P DataChannel 精确 bytes。

## 6. Relay 准入与执行

### 6.1 周期额度 reservation

Control Plane 或受限区域 issuer 在签发 Relay lease 前必须：

1. 验证 Subscription 和 Entitlement。
2. 验证 region、session、client、target daemon 和 route intent。
3. 检查账号/设备并发。
4. 从当前 billing period 可用额度创建有过期时间的 reservation。
5. 把本次硬上限写入短期签名 RelayLease。

不得只使用 `max_bytes_per_lease` 代替周期额度；否则重复申请 lease 可以绕过套餐流量限制。

### 6.2 Relay 数据面 enforcement

Relay 必须离线执行：

- lease 签名、issuer、audience、session 和 credential binding。
- lease expiry。
- allocation concurrency。
- 每 lease byte 上限。
- 每秒 bitrate 上限。
- principal 与 client/daemon credential 隔离。

Relay 拒绝不得回退到共享 TURN credential、长期 secret 或未计量连接。

### 6.3 settlement

- Relay usage 上报后，Control Plane 将 reservation 转为 settled usage。
- lease 到期或明确关闭后释放未使用 reservation。
- usage 迟到时仍须在允许窗口内幂等结算。
- 超出周期额度后拒绝新的 Relay lease；是否仍允许 managed P2P 由套餐能力独立决定。

## 7. 交易

交易领域至少包含：

```text
Product/Price
Checkout
Order
PaymentAttempt
PaymentEvent
SubscriptionTransition
Refund/Revocation
```

- checkout 只创建 pending order，不直接扩大 entitlement。
- 只有已验签、幂等的 provider event 可以把支付结果提交为 subscription transition。
- 相同 provider event 重放必须返回原结果，不得重复延长周期或重复奖励。
- 支付成功、失败、退款、撤销和 chargeback 都必须有明确状态转换和审计记录。
- development `TestPaymentProvider` 必须走与正式 provider 相同的 normalized event 入口，不能直接调用 `SetEntitlement`。
- development test provider 必须由 Controller 显式配置启用；默认配置不得暴露测试付款入口。
- checkout 必须记录创建时的 Subscription revision 与源套餐版本。迟到 payment event 不能覆盖后续套餐变化；拒绝结果必须进入 durable journal 和审计。
- development 第一阶段接入 Creem sandbox；切换 production API key 和真实收费仍属于发布装配。sandbox 交易必须从 Web 用户操作开始，经过 checkout、provider event、subscription、entitlement 和 Hub policy 全链路。

### 7.1 Creem provider 与双通道收口

Muxvia 首个正式支付 provider 是 Creem。Creem 只拥有外部 checkout、customer、order、transaction 和 subscription 资源；Muxvia `Order`、`PaymentAttempt`、`PaymentEvent`、`Subscription` 与 `Entitlement` 仍是产品真值。

- Controller 服务端使用 deployment secret 调用 Creem sandbox/production API，Web 与客户端不能接触 API key。
- checkout 使用 Muxvia order ID 作为稳定 request ID，并持久化 provider resource 映射和创建时的价格/套餐 revision。
- 每个可售 Plan version 绑定已核对的 Creem product ID；影响实收金额的 promo 绑定 Creem discount code。Webhook/轮询回读的 product、currency、amount 和 discount 必须与订单快照一致，否则事件进入 rejected journal。
- `POST /pay/creem` 使用独立 webhook secret 对原始 body 的 `creem-signature` 做 HMAC-SHA256 验证。未验签 payload、success redirect 和浏览器自报状态都不能改变 Subscription。
- Webhook 与轮询 reconciliation 必须映射到同一个 normalized payment event journal。Webhook 提供低延迟；轮询只补偿未终结 attempt、丢失/延迟事件和近期 subscription 漂移，不能建立第二份订阅真值。
- `subscription.paid` 或服务端核验的 paid transaction 才能开通新周期；`subscription.active` 只同步 provider 状态。cancel、scheduled cancel、past due、expired、paused、refund 和 dispute 映射为显式 transition。
- provider event ID 或确定性 resource/status revision 作为幂等键；乱序、重复、Controller 重启和 Webhook/轮询并发都由 event journal 与 Subscription revision CAS 收口。
- API timeout、429 和 5xx 只改变 PaymentAttempt/reconciliation 的可重试状态，不扩大或撤销 entitlement。

人工赠送、补偿或延长订阅必须使用带 actor、reason 和 revision 的 operator adjustment，不能伪造 Creem 已支付订单。人工线下收款必须使用明确的 manual provider source，经同一个 normalized event 入口提交。

## 8. 用户账号中心

用户必须可以查看和操作：

- 账号身份和登录设备。
- daemon 节点、在线状态、撤销和 enrollment。
- 当前套餐、订阅状态、周期开始/结束时间。
- Relay 已用、预留和剩余流量。
- Relay 速率、并发和 region 能力。
- 订单和支付结果。
- 升级、降级、取消续订和恢复入口。

账号中心不能展示 terminal inventory、terminal 内容或 CapabilityGrant body。

## 9. 运营管理面

第一阶段运营管理面提供完成产品闭环所需的能力：

- 查询账号、Subscription、Entitlement 和 revision。
- suspend/restore 账号服务能力。
- 查询和撤销 client/daemon device。
- 查询订单、provider event 和审计事件。
- 查询周期 usage、reservation 和 quota deny 原因。
- 对开发测试账号执行可审计的套餐调整。
- 发布不可变的套餐版本，并查看历史订单使用的价格/能力快照。
- 创建带有效期和原因的类型化账号 Entitlement override；自由 JSON 不能进入 policy。
- 管理 Hub directory、identity approval、capacity、drain 和 disable；Presence 在线状态仍来自 freshness。
- 管理 Creem checkout/payment/subscription 对账状态，并可重试失败的 reconciliation，不能直接把订单改成已支付。
- 管理有界 fixed/percent 优惠码及兑换记录；已兑换经济字段不可变。
- 管理带签名/hash/兼容范围/channel 的 CLI、daemon 和 Android 发布记录。

不提前建设通用 CRM、财务平台、工单系统、复杂营销规则引擎、发票、税务、多组织 RBAC 或数据仓库。

## 10. 错误语义

客户端只消费稳定分类，不接触套餐数据库字段：

```text
login_required
subscription_required
subscription_suspended
managed_p2p_disabled
relay_disabled
quota_exhausted
rate_limited
device_limit_reached
region_unavailable
payment_required
temporary
```

- 错误必须带可恢复性，但不能泄漏账号是否存在、内部价格、provider secret 或其它账号数据。
- entitlement 拒绝、目标离线、terminal authorization 失败必须保持不同错误类别。

## 11. 开发模式完成条件

Cloud 产品能力不能只由 unit test 或后台 API 证明。最终 development E2E 至少覆盖：

1. 用户注册并登录 Web 账号中心。
2. 默认套餐允许 managed P2P、拒绝 Relay。
3. 用户从 Web 发起测试 checkout，测试 provider 产生规范化 payment event。
4. Subscription 和 Entitlement 更新，signed Hub policy revision 生效。
5. Android App 通过真实设备授权登录同一账号。
6. daemon enrollment 和 terminal pairing 完成。
7. App 通过 managed P2P 打开 terminal 并持续交互。
8. 升级后 Relay 可用，App 经 Relay 完成 terminal 和文件传输。
9. Relay 速率、并发、每 lease bytes 和周期 bytes 至少各触发一次可证明拒绝。
10. 账号中心展示与 usage ledger 一致的 used/reserved/remaining。
11. 取消、到期或 suspend 后拒绝新的对应服务，但 Direct/SSH 和已有 grant 保持可用。
12. 服务重启后账号、Subscription、Entitlement、usage 和设备状态恢复。

Android 用户动作必须从 ARM64 模拟器中的真实 APK UI 发起；Web 交易必须从真实 Web UI 发起。测试夹具只作为支付 provider、流量制造和结果 oracle。

## 12. 明确延后

- Creem production key 与真实收费、正式 OAuth 和真实邮件/短信 provider；Creem sandbox adapter、Webhook 和轮询 reconciliation 不延后。
- 多区域 Hub、Relay Mesh 和全球动态路由。
- Web/WASM terminal 客户端。
- iOS/Desktop GUI。
- 复杂优惠、推荐奖励扩张、税务、发票和通用财务平台。
- Kubernetes、数据库集群、数据仓库和实时风控平台。
- 用户自建 Muxvia managed Cloud provider。

这些事项不得阻塞单区域 development 产品闭环，也不得作为 reviewer `FAIL` 的理由。
