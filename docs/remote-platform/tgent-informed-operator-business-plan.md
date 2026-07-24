# Muxvia Cloud 运营后台业务升级规划

状态：待按 `workflow.md` 切片实施

本文参考同级仓库 `../tgent` 已验证的运营业务场景，但不复制其 Next.js 实现、数据库表或状态语义。Muxvia 使用 generated Proto 作为跨边界契约、Go Control Plane 作为领域 owner、PostgreSQL 作为持久真值，Web Controller 只消费确定性投影。

## 1. 目标与原则

目标是补齐九类日常运营能力：用户、订单、订阅、套餐、Hub、Agent、版本、优惠码和用户特权。现有账号、Commerce、Subscription、Entitlement、UsageLedger、HubAssignment、Presence topology、CommandOutbox 和 Audit 必须继续作为唯一真值。

- 所有新增管理 API 先改 `proto/cloudpb/`，再生成 Go/TypeScript，最后接领域、存储与 Web。
- 所有 mutation 都记录 actor、reason、before/after revision、request id 和时间；危险操作要求 admin、CSRF 与近期认证。
- 列表页只保存筛选、分页和展开状态，不保存业务真值。
- 用户、设备、Hub 和订单默认保留审计记录；“移除”使用 revoke、disable、cancel 或 archive，不做无审计硬删除。
- 套餐、订阅和特权最终都归一化为同一 `Entitlement`；Hub 只消费签名后的最小 policy projection。
- 当前只完成单区域 development 运营闭环，不扩成 CRM、财务、发布 CDN 或通用促销平台。

## 2. 九类业务映射

| 模块 | `tgent` 可复用场景 | Muxvia 当前基础 | Muxvia 目标语义 | 明确不照抄 |
| --- | --- | --- | --- | --- |
| 用户管理 | 列表、搜索、套餐状态、角色调整 | Account、Session、Device、Subscription、Usage、Audit | 账号列表/详情、角色变更、暂停/恢复、session/device 撤销；禁止修改自己角色并保护最后一个 admin | Web 自有用户 DTO、无审计直接改角色 |
| 订单管理 | 列表、统计、pending/paid/cancelled | Order、PaymentAttempt、normalized PaymentEvent journal | 按状态/provider/用户筛选，查看价格快照和事件时间线；退款、撤销或人工收款都走明确的审计 transition | 直接把 pending 改成 paid |
| 订阅管理 | 列表、创建、延期、取消 | Versioned Subscription、Entitlement | operator grant/extend/suspend/restore/cancel 使用独立原因和 revision；策略提交后发布新 Hub policy | 为赠送订阅伪造 paid order |
| 套餐管理 | 编辑价格、能力和启停 | Versioned PlanCatalog、PlanCapability | 数据库发布新 catalog version，旧版本只读；订单保存价格/套餐快照；启停只影响新交易 | 原地修改已售套餐、按 plan name 写能力分支 |
| Hub 管理 | 创建、编辑、状态、资源、删除保护 | Hub registry、assignment、Fleet、control attachment | 数据库拥有公共 URL、health URL、region、capacity、enabled/draining；Edge 身份批准后启用；停用先 drain/migrate/fence | 配置文件长期拥有 Hub 目录、硬删有 assignment 的 Hub |
| Agent 概览 | 在线统计、列表、踢下线 | Device、HubAssignment、Presence projection、CommandOutbox | 按机器聚合 daemon；在线由 Presence freshness 计算；Kick/Revoke/Migrate 使用既有命令链路 | 数据库 `online` 布尔值、`pendingKick` 字段 |
| 版本管理 | Agent/App 版本、平台、下载、兼容与强更 | 当前无稳定发布目录 | 管理 CLI/daemon 与 Android 发布 channel、签名制品、SHA-256、兼容范围、灰度比例、启停和回滚 | 只有裸下载 URL、未签名强制更新 |
| 优惠码管理 | 固定/比例优惠、时效、次数、兑换记录 | Commerce 有订单价格快照，暂无 promo owner | checkout 内事务校验、预留、兑换或释放；限制时间、范围、总次数和每账号次数；订单保存折扣快照 | 兑换后修改 code/type/value；复杂营销规则引擎 |
| 用户特权管理 | 账号级额度覆盖、备注、过期 | Entitlement normalization、Audit | 类型化 `EntitlementOverride`，仅覆盖已声明 capability，带生效期、原因、actor 和 revision；过期自动重算 policy | 自由 JSON 覆盖、绕过 Subscription 直接写 Hub policy |

## 3. 关键用户流程

### 3.1 Hub 上线、变更和下线

1. operator 创建 disabled Hub 记录，填写 label、region、public URL、health URL、capacity 和预期身份 fingerprint。
2. 新 Edge 使用自身 Hub/Relay 公钥连接 Controller；operator 核对 fingerprint 后批准并启用。
3. Controller 从 PostgreSQL 目录生成 enrollment candidate、target resolve、policy 和 fleet 投影；普通客户端只读取 Controller 返回的动态目录。
4. 地址或容量变更提交新 revision，新的 resolve/enrollment 立即使用；已有 assignment 保持原 Hub，并在 Presence 重连时读取新地址。
5. 下线先切换为 `draining`，停止新 assignment，执行可审计 migration；assignment 清零且旧 epoch 已 fence 后才允许 disable。记录不硬删除。

配置文件只保留 Controller listener、数据库/签名 secret 和显式 development bootstrap。正式运行时不得在每次启动用静态 deployment 覆盖数据库。

### 3.2 人工处理订阅

operator 赠送或补偿服务时创建 `OperatorSubscriptionAdjustment`，而不是创建虚假订单。事务同时校验当前 Subscription revision、写 adjustment、推进 Subscription、重算 Entitlement 并写 Audit；提交后异步发布新 Hub policy。订单列表可以关联 adjustment，但必须明确显示“人工调整”，不能显示为 provider 已支付。

人工收款则使用独立的 manual payment source 生成 normalized payment event，仍经过订单/attempt/subscription CAS 与幂等 journal，不能直接改 order status。

### 3.3 优惠码结算

checkout 在同一数据库事务中读取 active catalog version、校验优惠范围与用量、创建短期 redemption reservation，并把原价、折扣和应付金额固化到订单快照。Creem provider adapter 必须把套餐版本映射到预先核对的 Creem product ID，把促销映射到 Creem discount code，并在 checkout/transaction 回读时核对 product、currency、原价、discount 和实付金额；Muxvia 内部计算不能单独改变外部应付金额。支付成功转为 redeemed；失败、取消或超时释放。已兑换的经济字段和 Creem mapping 不可变，后续停用只影响新 checkout。

### 3.4 用户特权

特权只允许覆盖 schema 中已有的 `PlanCapability` 字段。Control Plane 按 `PlanCatalog + Subscription + active typed overrides + account risk state` 生成 Entitlement；覆盖项不能携带 terminal scope、CapabilityGrant、设备私钥或直接 Hub policy。创建、修改、撤销和自然过期都推进 Entitlement revision 并发布 policy。

### 3.5 版本发布

发布记录区分产品、channel、platform、OS/arch、版本、version code、最小兼容版本、强制截止时间、rollout 百分比和 changelog。制品必须包含可信 origin、SHA-256 和签名 metadata。激活前验证版本单调性、制品完整性与兼容范围；回滚通过切换 active release revision 完成，不修改历史记录。

### 3.6 Creem 正式支付 provider

Muxvia 的首个正式支付 provider 使用 Creem。生产 API 为 `https://api.creem.io`，sandbox API 为 `https://test-api.creem.io`，服务端使用 `x-api-key`；API key 只能来自 Controller 的部署 secret，不能进入 Web、manifest、日志、仓库或 App。

- checkout 由 Controller 服务端调用 `POST /v1/checkouts`，使用 Muxvia `order_id` 作为稳定 `request_id`，并保存 Creem checkout/customer/order/subscription/transaction ID 映射。
- 每个可售 Plan version 必须绑定已核对的 Creem product ID；Muxvia promo 若影响 Creem 实收金额，必须绑定 Creem discount code。回传资源与订单的 product/currency/amount/discount 不一致时拒绝提交 Subscription。
- 浏览器 success redirect 只负责用户体验和触发立即查询，不能据此开通 entitlement；最终状态必须来自已验签 Webhook 或服务端轮询取得的 Creem 资源。
- Webhook 固定为 `POST https://muxvia.com/pay/creem`。Controller 必须读取未经改写的 raw body，使用独立 webhook secret 对 `creem-signature` 做 HMAC-SHA256 常量时间校验，验签失败不写业务 journal。
- Webhook 与轮询共用一个 Creem adapter 和同一个 normalized `PaymentEvent` 入口。provider event ID 是首选幂等键；轮询事件使用 `creem:resource_kind:resource_id:provider_revision/status` 的确定性键。相同 provider 状态重放返回原结果，不重复延长 Subscription。
- 开通服务以 `subscription.paid` 或已核验的 paid checkout/transaction 为依据；`subscription.active` 只做同步，不能单独扩大 entitlement。取消、scheduled cancel、past due、expired、paused、refund 和 dispute 映射为显式 Subscription transition。
- Webhook 是低延迟主路径；轮询是 reconciliation，不是并行真值。pending order 在用户返回时立即查一次，后台按有界退避查询 checkout/subscription/transaction；进入终态、超过最大期限或账号/订单已撤销后停止。定期 reconciliation 只扫描未终结 attempt 和近期 active Creem subscription，不做全量无限轮询。
- Creem API timeout、429 和 5xx 只更新 PaymentAttempt 的可重试状态，不改变 Entitlement。provider 响应和 webhook payload 只保存完成审计/重放所需的脱敏字段与 digest，不持久化 API key、webhook secret 或支付敏感信息。

## 4. 领域 owner 与存储边界

| 领域 | Go owner | PostgreSQL 真值 | 运行时投影 |
| --- | --- | --- | --- |
| 用户与角色 | account/operator authorization | account、operator role、session、audit | Web account projection |
| 订单与支付 | commerce | order、attempt、event journal、price/discount snapshot | account/operator commerce projection |
| 订阅 | commerce | subscription、adjustment、transition | Entitlement input |
| 套餐 | catalog | immutable catalog/version/plan version | current published catalog |
| Hub | hubregistry | deployment directory、identity、lifecycle、capacity、revision | control attachment/fleet freshness |
| Agent | topology + commandoutbox | device、assignment、last trusted projection、command journal | Hub memory Presence/runtime |
| 发布 | releasecatalog | immutable release、channel activation revision | client update projection |
| 优惠码 | promotion | promotion、scope、reservation、redemption | checkout price projection |
| 用户特权 | entitlement | typed override、effective window、revision、audit | signed per-Hub policy |

Presence 是否在线、Hub 当前 attachment、Relay allocation 和 DataChannel 都不是数据库状态。PostgreSQL 只保存最后可信观察和 freshness 时间，不保存可被误用的实时 `online=true`。

## 5. 实施切片

### OPSHUB001：Hub 管理与动态目录

- Proto-first 补齐 deployment directory、lifecycle mutation、identity approval 和 fleet projection。
- 扩展 `hub_deployments` 保存 public/health URL、region、capacity、enabled/draining 和 revision。
- Operator UI 完成创建、编辑、批准、drain、迁移进度和 disable。
- 删除 Controller runtime 对 `Config.Deployments` 的正式目录 ownership、启动覆盖和客户端 manifest Hub 地址。
- E2E：运行中新增第三个 Edge，不重建 Controller/CLI/APK；新 daemon 可选中它，已有 daemon assignment 不漂移；drain 后无新 assignment，完成 fence 后停用。

### OPSUSER001：用户管理与 Agent 概览

- 账号/机器列表、筛选、详情、角色、session/device revoke、订阅与用量摘要。
- daemon 按稳定机器身份聚合；Presence freshness 展示在线/陈旧/未知。
- Kick、Revoke、Migrate 复用 CommandOutbox，并展示 authority/delivery/execution/effect。
- E2E：self-role 和 last-admin 保护、跨账号隔离、旧 Presence 不复活、命令结果可审计。

### OPSCAT001：套餐发布与用户特权

- 数据库版本化 PlanCatalog 发布、启停、预览和历史只读。
- 类型化 EntitlementOverride 的创建、修改、撤销、过期与原因审计。
- Commerce 保存套餐快照；policy 只从归一化 Entitlement 生成。
- E2E：新旧订阅保留对应版本，override 生效/过期后 Hub policy revision 与准入结果一致。

### OPSCOM001：订单、订阅与优惠码

- 订单/attempt/event 列表和事件时间线；人工收款、退款、撤销走 normalized event。
- operator subscription adjustment 取代 fake order；支持 grant/extend/suspend/restore/cancel。
- 优惠码支持 fixed/percent、scope、时间窗、总量和每账号限制，checkout 原子 reservation/redemption。
- E2E：并发兑换、支付重放、超时释放、迟到事件、调整 revision 冲突和 Audit 全覆盖。

### CREEM001：Creem checkout、Webhook 与轮询对账

- 实现 Go Creem provider adapter、sandbox/production base URL allowlist、secret 配置和有界 HTTP client；不引入 Next.js SDK 或第二套 Commerce 状态机。
- Controller 创建 checkout 并返回托管 checkout URL；订单与 provider ID 映射同事务提交。
- `POST /pay/creem` 验证 raw-body HMAC-SHA256 后写入现有 payment event journal；当前已在 Creem 后台登记该公网 URL，但实现部署前该地址仍只是 SPA fallback，不能视为 Webhook 可用。
- reconciliation worker 查询未终结 checkout、transaction 和 subscription，生成同一 normalized event；Webhook/轮询乱序、重复和并发由 provider event journal 与 Subscription revision CAS 收口。
- sandbox E2E：真实 checkout、支付成功、Webhook、故意阻断 Webhook 后轮询补偿、取消、past due、退款、重复投递、Controller/PostgreSQL 重启；再由 operator 页面核对 order/attempt/event/subscription/audit。
- 部署完成后由用户在 Creem dashboard 发送 test event；证据必须包含 HTTP 2xx、验签结果、event ID/digest、journal 状态和对应 Subscription revision。Webhook secret 由用户通过部署 secret 注入，不进入仓库。

### OPSREL001：CLI/daemon 与 Android 版本管理

- Proto、release catalog、签名制品 metadata、channel activation、兼容/强制策略和 rollout。
- Operator UI 提供创建、校验、激活、暂停和回滚；客户端下载只消费签名发布投影。
- E2E：hash/signature 不符拒绝、版本回退拒绝、灰度稳定分桶、兼容范围和回滚。

### OPSE2E001：九模块运营闭环

- 使用真实 PostgreSQL、一个 Controller、至少两个 Edge 和真实 Web UI。
- 覆盖 readonly/admin、CSRF、近期认证、账号隔离、审计、Controller/Edge/数据库重启。
- 将用户、订单、订阅、套餐、Hub、Agent、版本、优惠码、特权逐项证据纳入 `CLOUDP007`，不以直接写数据库或 fake ack 代替产品流程。

## 6. 删除项

- 删除正式 Controller 配置中的 deployment URL/capacity 列表及启动时 upsert 覆盖；development bootstrap 只能显式、仅首次写入。
- 删除客户端/Companion manifest 中作为运行真值的静态 Hub URL。
- 不新增或删除任何 Web 自有 order/subscription/plan/Hub DTO；统一使用 generated Proto JSON。
- 不新增数据库 online、pending kick、mutable plan、fake paid order、free-form entitlement JSON 或 Hub hard delete 路径。
- 新管理面稳定后，删除只能查询不能管理的旧 fleet 拼装和重复页面状态。

## 7. 准入矩阵

每个切片至少通过 generated/descriptor、Go unit/integration、PostgreSQL transaction/restart、Web typecheck/build 和 API authorization。涉及 Web 用户动作时必须用 Playwright 从真实页面发起；涉及 Hub/Agent 时必须使用独立 Controller/Edge 进程和真实 control transport。最终 `OPSE2E001` 记录 actor、request、revision、数据库结果、Hub 投影 revision、命令回执和页面证据。
