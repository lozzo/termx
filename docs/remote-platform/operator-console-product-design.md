# Muxvia Cloud 运营管理后台产品设计

状态：已按 `OPSCONSOLE001-OPSCONSOLE007` 实施并部署；验收与部署证据见 `docs/remote-platform/opsconsole007-operator-console-e2e.md`

本文定义 Muxvia Cloud 运营管理后台的信息架构、导航、路由、数据加载、交互规则和九个功能模块。实现不改变现有 Control Plane、Proto、PostgreSQL、Hub、Relay、CommandOutbox 或发布系统的领域真值。

业务语义必须服从：

- `workflow.md`：活动范围、实施顺序、准入和提交规则。
- `docs/remote-platform/cloud-product-spec.md`：账号、套餐、订单、订阅、Entitlement、额度和交易真值。
- `docs/remote-platform/multi-hub-control-topology-spec.md`：Hub assignment、Presence、PeerSession 和 CommandOutbox 真值。
- `docs/remote-platform/tgent-informed-operator-business-plan.md`：九模块业务范围和明确禁止项。

## 1. 背景与现有问题

现有管理入口完成了九模块业务纵向验证，但页面结构仍是验收工作台，不是可长期使用的运营后台：

1. 普通账号页左侧的九个管理入口都跳转到同一个 `/operator` 页面，只通过 hash 指向不同 section。
2. `/operator` 是独立页面，但没有独立的管理后台导航壳；进入后普通账号页的左侧栏消失。
3. `/operator` 首次加载会并发请求用户、Agent、订单、订阅、套餐、优惠码、Hub 和版本等全部数据。
4. 九个模块在同一页面纵向全部渲染，因此点击不同菜单看到的仍然是同一批内容。
5. 模块切换、搜索或 mutation 容易触发全量 `load()`，导致无关内容重复 loading。
6. 巨型页面同时持有九个领域的列表、筛选、详情、表单、错误和 busy 状态，无法形成清晰的模块边界。

这些问题属于前端信息架构和状态生命周期问题，不需要修改业务真值来解决。

## 2. 产品目标

运营后台面向中国运营人员，目标是提供安静、紧凑、可扫描、可重复操作的工作界面：

- 保留独立 `/operator` 管理路由，不把运营能力混入普通用户账号页面。
- 进入管理后台后始终保留管理导航，明确展示当前位置和可用模块。
- 九个菜单对应九个独立 URL 和九个独立页面，只渲染当前模块。
- 模块切换不重建应用壳，不重复请求无关数据，不出现全页 loading。
- 访问过的模块可立即恢复列表、筛选、分页、滚动和详情位置，并在后台按需刷新。
- 所有 mutation 使用现有 generated Proto API、后端授权、CSRF、近期认证、revision 和 Audit。
- 简体中文是管理后台首次默认语言；英文是可切换的次要语言。
- 桌面端优先支持高频运营操作，移动端保留完整查询和必要操作能力。

## 3. 明确不做

- 不建设第二套账号、订单、订阅、套餐、Hub、Agent、版本、优惠码或特权 DTO。
- 不把 Web 页面状态写回 PostgreSQL 作为业务真值。
- 不引入 CRM、工单系统、财务总账、发票税务、通用营销平台或发布 CDN。
- 不新增数据库 `online`、`pendingKick`、mutable paid 字段或自由 JSON entitlement。
- 不通过前端隐藏菜单代替后端逐请求授权。
- 不为切换页面新增大而全的全局状态 registry；只建立管理壳和模块级查询缓存。
- 不在本设计阶段修改、提交、推送或部署现有实现。

## 4. 用户、角色与入口

### 4.1 角色

| 角色 | 能力 |
| --- | --- |
| 普通账号 | 只能使用普通账号中心；访问 `/operator/*` 返回 403 或重定向账号中心 |
| `readonly` | 可以进入后端投影允许的管理模块并查询；所有 mutation 由后端拒绝 |
| `admin` | 可以查询，并在通过 CSRF 与近期认证后执行允许的 mutation |

角色只保存在 PostgreSQL 和后端授权上下文中，不把 `isAdmin` 或原始角色字段暴露给前端。前端只消费 `OperatorWorkspaceModule` 投影决定展示哪些导航项；每个 API 仍必须独立鉴权。

### 4.2 入口

- 普通账号中心只保留一个“运营管理”入口，不再在账号页重复展示九个深层菜单。
- 入口跳转到 `/operator`；服务端或前端根据 workspace projection 定位第一个可见模块。
- `/operator` 本身不建设无排期的统计首页，默认重定向到 `/operator/users`。如果当前角色没有用户模块，则进入第一个获准模块。
- 管理后台页头提供“返回账号中心”，不把账号中心菜单复制到管理导航中。

## 5. 信息架构与路由

### 5.1 一级路由

| 模块 | URL | 默认任务 |
| --- | --- | --- |
| 用户管理 | `/operator/users` | 查找账号并处理账号、会话、设备和订阅摘要 |
| Agent 概览 | `/operator/agents` | 观察 daemon 运行状态并执行精确管理命令 |
| 订单管理 | `/operator/orders` | 查看订单、支付尝试、事件和对账状态 |
| 订阅管理 | `/operator/subscriptions` | 查询并执行赠送、延期、暂停、恢复或取消 |
| 套餐管理 | `/operator/plans` | 查看、校验和发布不可变套餐目录版本 |
| 用户特权 | `/operator/privileges` | 管理类型化 EntitlementOverride |
| 优惠码管理 | `/operator/promotions` | 管理优惠定义、Creem mapping 和兑换状态 |
| Hub 管理 | `/operator/hubs` | 管理 Hub/Relay 目录、身份、容量、drain 和停用 |
| 版本管理 | `/operator/releases` | 管理 CLI、daemon、Android 制品与 channel |

### 5.2 详情路由

列表选中对象后使用可复制、可刷新、可前进后退的详情 URL：

- `/operator/users/:accountId`
- `/operator/agents/:deviceId`
- `/operator/orders/:orderId`
- `/operator/subscriptions/:subscriptionId`
- `/operator/plans/:catalogVersion`
- `/operator/privileges/:accountId`
- `/operator/promotions/:promotionId`
- `/operator/hubs/:hubId`
- `/operator/releases/:releaseId`

详情可以使用桌面双栏、移动端独立页面或抽屉表现，但 URL 和领域对象保持一致。不得只把“当前选中项”保存在组件内存中。

### 5.3 导航行为

- 点击左侧菜单只切换右侧 outlet，不卸载管理壳。
- 当前菜单使用 `aria-current="page"`、背景和文字三重状态表达，不只依赖颜色。
- 浏览器刷新直接恢复当前模块和详情。
- 浏览器前进/后退恢复模块、query string、分页和选中对象。
- 搜索、状态、provider、region、channel、分页等可分享筛选写入 query string。
- hash 只允许定位当前详情内部的审计片段，不再承担一级导航。

## 6. 管理应用壳

### 6.1 桌面布局

```text
┌──────────────────┬──────────────────────────────────────────────────────┐
│ Muxvia 运营中心   │ 用户管理                         刷新  中文  账号中心 │
│                  ├──────────────────────────────────────────────────────┤
│ 用户管理          │ 搜索 / 筛选 / 批量或当前对象操作                    │
│ Agent 概览        ├──────────────────────────────────────────────────────┤
│ 订单管理          │                                                      │
│ 订阅管理          │ 当前模块的列表、详情、表单、审计与局部状态          │
│ 套餐管理          │                                                      │
│ 用户特权          │ 不渲染其它八个模块                                  │
│ 优惠码管理        │                                                      │
│ Hub 管理          │                                                      │
│ 版本管理          │                                                      │
└──────────────────┴──────────────────────────────────────────────────────┘
```

- 左侧栏桌面宽度约 232px，使用现有紧凑边框、语义色和 Lucide 图标。
- 左侧栏在长列表滚动时保持可见；不得覆盖正文或建立与正文竞争的嵌套滚动。
- 页头展示模块标题、必要摘要、当前模块刷新、语言、近期认证状态和账号中心入口。
- 正文最大化用于表格和详情，不使用营销型大标题、hero 或装饰卡片。

### 6.2 移动端布局

- 小于 768px 时左侧栏变成页头菜单按钮打开的全高抽屉。
- 抽屉包含九个模块、当前选中状态、语言和账号中心入口。
- 关闭抽屉后正文使用单列；列表项打开独立详情页，不在 390px 强行并排列表与详情。
- 所有触控目标至少 44px；菜单和危险操作必须有文字标签。
- 页面不得横向滚动；技术 ID、URL 和 fingerprint 允许内部换行或复制。

### 6.3 近期认证

- 近期认证是后端 Session 上的安全状态，不是页面自行拥有的授权真值。
- 管理壳可以显示“只读操作 / 变更已解锁至 HH:mm”，但不能用它替代每个 mutation 的后端校验。
- 普通浏览不主动要求密码。用户发起危险操作时，在具体对象和动作上下文中弹出密码确认。
- 一次确认成功后，管理壳保留后端返回的 expiry；有效期内跨模块执行允许操作不重复输入密码。
- expiry 到期、后端 401/403 或 recent-auth 错误后立即恢复未解锁表现。
- 对象、影响、原因、expected revision 和不可逆后果必须在确认面板中可见。

## 7. 数据加载、缓存与反馈

### 7.1 查询边界

管理壳只加载一次：

- 当前账号 Session 是否有效。
- `OperatorWorkspaceModule` 可见模块投影。
- 最近认证 expiry 的当前投影或最近一次确认结果。

每个模块只加载自己的列表和当前详情。不得进入 `/operator/users` 时同时请求订单、Hub、版本和优惠码。

### 7.2 缓存键

查询缓存至少由以下信息组成：

```text
module + filters + sort + page cursor + selected resource revision
```

- 列表缓存只保存服务端投影，不成为业务真值。
- 返回已访问模块时立即显示缓存内容，并在数据过期时后台刷新。
- 账号、Hub、Agent 等实时性较高的投影使用短 freshness；套餐历史和签名制品可使用更长 freshness。
- 退出登录、角色降级、401/403 或账号切换时清空全部管理缓存。
- 不把包含敏感投影的缓存写入长期 localStorage。

### 7.3 Loading 规则

- 第一次进入管理后台：显示稳定应用壳，右侧当前模块使用骨架屏。
- 第一次进入某模块：只在该模块内容区显示骨架，不隐藏左侧栏和页头。
- 返回已加载模块：立即显示缓存，不出现阻塞 spinner；后台刷新只显示非阻塞状态。
- 搜索和翻页：保留旧结果直到新结果返回，并标记结果区正在更新。
- mutation：只禁用提交按钮和受影响行，显示进行中状态；不得让整个后台进入 loading。
- 超过 300ms 的等待提供反馈；快速缓存命中不闪烁 loading。

### 7.4 Mutation 后刷新

| 操作 | 必须失效或更新的投影 |
| --- | --- |
| 撤销账号 Session | 当前用户详情、账号 Session 列表 |
| 暂停/恢复订阅 | 当前订阅、用户摘要、Entitlement/policy 状态 |
| Kick Agent | 当前 Agent、对应 Presence、CommandOutbox 结果 |
| 迁移 Agent | 当前 Agent、源/目标 Hub 摘要、CommandOutbox 结果 |
| 支付/退款/撤销/对账 | 当前订单、相关订阅、事件时间线 |
| 发布套餐 | 套餐历史、active catalog；不刷新 Hub 和版本目录 |
| 创建/撤销特权 | 当前账号特权、Entitlement/policy revision |
| 创建优惠码 | 优惠码列表；不刷新无关订单历史 |
| Hub approve/drain/disable | 当前 Hub、fleet 摘要、相关 assignment 进度 |
| 发布/激活/暂停/回滚版本 | 当前产品/channel 的版本列表 |

### 7.5 错误与空态

- 查询失败保留最后成功内容，在内容区顶部显示稳定错误分类、重试和时间。
- mutation 失败保留用户填写内容，并把错误放在对应表单或操作行附近。
- revision conflict 明确提示“数据已被其他操作更新”，刷新当前对象后允许重新提交。
- 401 跳转登录；403 返回账号中心或显示无权限，不保留旧敏感内容。
- 空态说明为什么为空以及下一步，不显示面向开发者的 Proto message 或底层英文错误。

## 8. 通用列表与详情规则

- 列表必须有明确列名、稳定排序、分页和结果数量；不一次加载无界记录。
- 筛选变更后重置页游标，浏览器后退可恢复旧游标与筛选。
- 技术 ID 使用等宽字体并提供复制，不作为主标题。
- 主标题优先使用邮箱、显示名、公开 Hub label、版本号或优惠码等运营可识别信息。
- 状态必须显示中文文字，颜色只作辅助；枚举名和稳定 code 可以进入高级详情。
- 时间按管理后台语言和时区格式化，并在高级详情提供精确时间戳。
- 所有 mutation 必填操作原因；原因、actor、request ID、before/after revision 和时间进入 Audit。
- 详情页把“当前状态”“可执行操作”“历史审计”分开，不能把历史记录误当成当前真值。

## 9. 用户管理

### 9.1 目标

围绕账号查找、状态判断、Session/设备处理和关联业务跳转工作。用户管理不直接修改订单、套餐或 Hub policy。

### 9.2 列表

筛选：

- 邮箱、显示名、account ID。
- 账号状态、订阅状态、套餐。
- 是否存在有效 Session、daemon、活动特权或风险状态。

建议列：

- 用户：显示名、邮箱、account ID。
- 账号状态与授权 revision。
- 当前套餐、订阅状态和周期。
- daemon 数、有效账号 Session 数。
- Relay 周期用量摘要。
- 最近活动时间。

### 9.3 详情

详情分区：

1. 账号摘要：身份、状态、创建时间、授权 revision。
2. 订阅摘要：套餐、状态、周期、Entitlement revision、用量。
3. daemon 与设备：名称、DeviceIdentity、assignment、revoke 状态。
4. 账号 Session：客户端设备 ID、有效期、revision、revoke 状态，不展示 token/hash。
5. 用户特权摘要：活动和历史 override。
6. 最近管理命令和运营审计。

### 9.4 操作

- 撤销精确账号 Session，或使用后端已有的显式批量撤销能力。
- 撤销 daemon Cloud ownership/access；不删除 daemon 本地 DeviceIdentity、Endpoint 或 Direct/SSH 能力。
- 跳转到该用户的订阅、订单、Agent 和特权过滤视图。
- 如果后续恢复角色管理，必须保护当前操作者和最后一个 admin；当前页面不自行扩大角色 mutation。

### 9.5 禁止

- 不返回 access token、refresh token、密码、hash 或 secret。
- 不在用户详情中直接把订单改为 paid。
- 不直接写 Entitlement 或 Hub policy。
- 不硬删除账号、设备、Session 审计记录。

## 10. Agent 概览

### 10.1 目标

按稳定 daemon DeviceIdentity 聚合 assignment、Presence、PeerSession 和命令结果，帮助运营人员判断机器是否可管理以及命令执行到哪一步。

### 10.2 列表

筛选：

- Agent 名称、device ID、账号邮箱/account ID。
- Presence：在线且新鲜、陈旧、离线、未知。
- owning Hub、region、assignment epoch。
- 是否存在活动 PeerSession、是否 revoked。

建议列：

- Agent 名称与 device ID。
- 所属账号。
- Presence availability 与 freshness，分别展示。
- 当前 Hub、assignment epoch、presence session。
- 活动 PeerSession 数。
- 最近上报时间和运行版本。

### 10.3 详情与操作

- 展示 DeviceIdentity、assignment、Presence、runtime generation 和 active session 摘要。
- `Kick` 只针对精确 fresh Presence；stale/unknown 不展示可执行 Kick。
- `Migrate` 选择已就绪且非当前 Hub 的目标，通过 CommandOutbox、source fence 和 assignment transaction 执行。
- `Revoke` 提交持久 authority revoke，并独立展示 runtime enforcement 进度。
- 命令结果必须分开显示 authority、delivery、execution 和 observed effect，不用“已提交”冒充“已生效”。

### 10.4 禁止

- PostgreSQL 不保存可作为真值的 `online=true`。
- 不根据最后一条数据库记录把 stale Agent 显示为离线。
- 不增加 `pendingKick` 或 Web 自有命令状态。
- 不让旧 Presence、旧 assignment epoch 或旧 generation 覆盖当前投影。

## 11. 订单管理

### 11.1 目标

查看不可变经济快照、支付尝试、normalized provider event journal、优惠应用和最终订单状态，并对异常支付执行受控处理。

### 11.2 列表

筛选：

- order ID、用户邮箱/account ID。
- 订单状态、provider、PaymentAttempt 状态。
- 套餐、币种、创建时间、是否使用优惠码。
- 是否需要 reconciliation、是否存在 rejected/conflict event。

建议列：

- 订单、用户、套餐版本。
- 原价、折扣、实付金额、币种。
- 订单状态、provider 和最新 attempt 状态。
- 创建时间、最近事件时间。
- 对账告警或退款状态。

### 11.3 详情

1. 订单经济快照：套餐版本、价格、折扣、应付金额。
2. PaymentAttempt：provider resource mapping、状态、revision、下次对账时间。
3. PaymentEvent 时间线：来源、provider event ID、验签/核对结果、journal 状态。
4. 优惠 reservation/redemption 状态。
5. 关联 Subscription transition 和 Audit。

### 11.4 操作

- 对可重试 Creem attempt 执行立即 reconciliation。
- 人工线下收款使用明确 `operator-manual` source 生成 normalized payment event。
- 退款和撤销也进入同一个 event journal，并要求原因、request ID 和 revision。
- 可跳转到关联用户、订阅和优惠码详情。

### 11.5 禁止

- 不提供“直接改为已支付”按钮。
- success redirect 不开通 Entitlement。
- 不跳过 provider product、currency、amount 和 discount 核对。
- 不在 Web 保存 provider raw secret 或敏感 payload。

## 12. 订阅管理

### 12.1 目标

管理 versioned Subscription 的生命周期和人工服务调整，并观察 Entitlement 与 Hub policy 的后续投影。

### 12.2 列表与详情

筛选：订阅 ID、账号、套餐、状态、周期、是否人工调整、是否即将到期或 past due。

详情包含：

- 当前 plan ID/version、Subscription revision 和状态。
- 周期开始/结束、cancel-at-period-end、grace/past-due 信息。
- 创建时订单和套餐快照引用。
- OperatorSubscriptionAdjustment 历史。
- Entitlement revision、policy 发布和 Hub ack 摘要。
- 相关 payment/subscription event 和 Audit。

### 12.3 操作

- `grant`：无订单赠送服务。
- `extend`：延长当前服务周期。
- `change plan`：使用明确目标 plan version 变更。
- `suspend`、`restore`、`cancel`：通过现有 Subscription transition 执行。
- 所有操作提交 expected revision、原因和 request ID。

### 12.4 禁止

- 赠送或补偿不能伪造 paid order。
- 不能原地修改历史 Subscription version。
- 不能直接修改 Entitlement、Relay quota 或 Hub policy。
- 迟到 payment event 不能覆盖后续人工或用户变更。

## 13. 套餐管理

### 13.1 目标

发布不可变 PlanCatalog 版本，管理套餐展示、价格、能力和 provider mapping，同时保证旧订单和旧订阅继续引用购买时版本。

### 13.2 列表与详情

- active catalog 与历史 catalog 分开展示。
- 每个版本展示发布时间、actor、reason、digest 和 plan 数量。
- plan 详情展示名称、说明、计费周期、价格、币种、PlanCapability 和 Creem product mapping。
- 提供结构化差异：价格、能力、可售状态和 provider mapping 的 before/after，而不是只展示整段 JSON。
- Proto JSON 保留在高级编辑/检查区，不作为普通运营人员的唯一编辑界面。

### 13.3 操作

- 从 active catalog 复制为草稿。
- 在草稿中新增或调整未来版本的套餐展示、价格、能力和 mapping。
- 发布前校验字段完整性、能力范围、currency、计费周期、Creem mapping 和 canonical digest。
- 发布生成新 catalog version；历史版本只读。
- 套餐停用只影响新 checkout，不改变历史订单和已有订阅。

### 13.4 禁止

- 不原地修改已发布 catalog 或已售 plan version。
- 不按 plan name 在 Go、Hub 或 Web 中写能力分支。
- 页面不自行计算最终 Entitlement。
- 不用 staging 硬编码套餐覆盖数据库目录。

## 14. 用户特权

### 14.1 目标

为客服补偿或事故处置提供类型化、限时、可审计的 PlanCapability 覆盖。

### 14.2 列表与详情

筛选：账号、capability、active/expired/revoked、有效时间和 actor。

详情包含：

- account ID 与当前套餐能力。
- capability field mask、覆盖值和数据类型。
- 生效时间、到期时间、revision、actor 和原因。
- 创建、修改、撤销和自然过期历史。
- Entitlement 重算结果和 per-Hub policy revision/ack。

### 14.3 操作

- 只从 schema 允许的 capability 列表选择字段。
- 创建带明确时间窗和原因的 override。
- 使用 expected revision 更新允许变更的时间窗或值。
- 显式撤销；自然过期由领域状态自动重算，不靠前端定时器修改真值。

### 14.4 禁止

- 不接受自由 JSON、未知字段或跨领域值。
- 不携带 terminal scope、CapabilityGrant、私钥或 DeviceIdentity。
- 不直接写 signed Hub policy。
- 不把永久自定义套餐伪装成临时特权。

## 15. 优惠码管理

### 15.1 目标

管理有界 fixed/percent 优惠、适用范围、Creem discount mapping、reservation、redemption 和审计。

### 15.2 列表与详情

筛选：优惠码、状态、类型、套餐、有效时间、是否达到总量、Creem mapping。

建议列：

- code、类型和值。
- 适用 plan/version 和计费周期。
- 生效/到期时间。
- 总限额、已预留、已兑换、已释放。
- 每账号限制、状态和 revision。

详情展示不可变经济字段、Creem mapping、reservation/redemption 记录、关联订单和 Audit。

### 15.3 操作

- 创建 fixed 或 percent 优惠，设置范围、时间窗、总量和每账号限制。
- 登记经过核对的 Creem discount code mapping。
- 启用或停用未来兑换；停用不能修改已兑换订单快照。
- 查看 reservation 超时释放、支付成功 redeemed 和失败/cancelled 释放结果。

### 15.4 禁止

- 不在兑换后修改 code/type/value、币种或适用范围。
- Muxvia 内部折扣不能与 Creem 实收不一致。
- 不建设叠加、等级、裂变、渠道等通用营销规则引擎。
- 不允许仅靠前端计数判断剩余额度。

## 16. Hub 管理

### 16.1 目标

管理 PostgreSQL Hub directory、Edge 身份批准、公开地址、容量和下线生命周期，同时正确区分持久目录与进程内 readiness。

### 16.2 列表

筛选：hub ID、region、lifecycle、identity approval、draining、Hub/Relay readiness。

建议列：

- public label、hub ID、region。
- lifecycle：pending、active、draining、disabled/archived。
- identity approval 状态。
- assignment 数/容量。
- Hub control、Relay control readiness 与 generation。
- public URL、health URL 和 directory revision。

### 16.3 详情与操作

1. 创建 pending deployment：hub/edge/relay ID、region、label、URL、capacity 和预期公钥。
2. 身份审核：并排显示提交和观察到的 Hub/Relay fingerprint，确认后 approve。
3. 编辑目录：地址、label、region、capacity，提交 expected revision 和原因。
4. 开始 drain：停止新 assignment，不自动移动已有 assignment。
5. 迁移进度：展示每个 assignment 的 source fence、目标 epoch 和 CommandOutbox 状态。
6. 取消 drain：仅在后端状态允许时恢复接收新 assignment。
7. disable/archive：只在 assignment 清零且 fence 完成后允许，保留目录和审计记录。

### 16.4 禁止

- 不硬删除 Hub。
- 不把 readiness、control generation 或 `online` 写成 PostgreSQL 生命周期真值。
- 不让 Controller 配置或客户端 manifest 覆盖数据库动态目录。
- 不因 URL/label 编辑改变 Hub、Relay 独立 identity 和 control owner。

## 17. 版本管理

### 17.1 目标

管理 CLI、daemon、Android 的不可变签名发布 metadata 和可变 channel head，支持兼容、强制、稳定灰度、暂停和显式回滚。

### 17.2 列表与详情

筛选：产品、channel、platform、OS/arch、active/paused/history、版本和发布时间。

建议列：

- 产品、channel、版本、version code、目标平台。
- active/paused/history 状态。
- rollout 百分比、最小兼容版本、强制截止时间。
- SHA-256 摘要、可信下载 origin、签名状态。
- 发布时间、actor、reason 和 channel revision。

详情展示完整签名 payload、changelog、兼容规则、rollout、channel 历史和 Audit；private signing key 永远不进入页面或 Controller 投影。

### 17.3 操作

- 校验并发布已签名 metadata；校验 target、hash、签名、HTTPS origin 和版本单调性。
- 激活更高 version code 的 release。
- 暂停/恢复 channel 分发。
- 对低 version code 使用显式 rollback，并要求原因和 expected channel revision。
- 调整允许的 rollout/强制策略时发布新的不可变 release metadata 或按既有契约更新 channel，不修改历史 payload。

### 17.4 禁止

- 不接受裸下载 URL 或未签名 metadata。
- 不把 release private key 放入 Controller、Web、日志或仓库。
- 不覆盖历史制品记录。
- 不建设 CDN、自动应用商店发布或通用制品平台。

## 18. 审计、权限与安全

- 所有 Operator 查询和 mutation 继续通过普通账号 Session 进入后端逐请求授权。
- mutation 必须携带同源 CSRF、近期认证、actor、reason、request ID 和 expected revision。
- readonly/admin 权限变化必须立即影响后续请求；前端缓存不能延迟权限收缩。
- Audit 以领域事务结果为准，页面不得先显示成功再等待数据库提交。
- 敏感字段禁止进入 DOM、URL、localStorage、日志、截图或错误文案。
- Hub/Relay 无权查看 terminal capability；运营后台也不能展示 terminal payload、history、文件内容或 CapabilityGrant。
- 账号、订单、订阅、优惠、特权、Hub、Agent 命令和发布各自保留领域审计，不合并成不可追踪的自由文本日志。

## 19. 视觉与交互规范

- 风格：数据密集、克制、工作导向；使用现有浅色/深色语义 token，不重新建设设计系统。
- 卡片只用于独立重复对象、工具或弹窗；页面 section 使用边框分区，不嵌套装饰卡片。
- 主体字号以 12/14/16px 为主，页面标题保持克制，不使用营销型大字。
- 状态色固定语义：正常/完成、警告/等待、危险/失败，同时必须配文字或图标。
- 图标统一使用 Lucide；陌生图标按钮提供 tooltip/aria-label。
- 表格数字使用 tabular figures；ID/hash 使用等宽字体。
- 表单有可见 label、必要 helper、就近错误和提交反馈；不可只用 placeholder。
- 动效限制为 150-300ms 的抽屉、hover 和局部状态过渡，并遵守 reduced motion。
- 简体中文首次默认；中文术语固定为用户、Agent、订单、订阅、套餐、用户特权、优惠码、Hub、版本。

## 20. 可访问性与响应式准入

- 键盘可遍历侧栏、筛选、列表、详情和确认弹窗；焦点顺序与视觉顺序一致。
- 路由切换后焦点进入模块 `h1`，返回列表时恢复触发元素或合理位置。
- drawer 打开时使用 focus trap，Escape 可关闭，关闭后焦点回到菜单按钮。
- 表格在小屏转换为有 label 的行，不依赖横向滚动完成主要操作。
- 390、768、1280、1440px 和 150% 缩放下无页面级横向溢出。
- 文字、图标、边框、focus 和 disabled 状态满足 WCAG AA 对比度。
- loading、成功、失败和权限变化通过 `aria-live` 适度播报，不重复干扰。

## 21. 测试与验收矩阵

### 21.1 导航

- 九个可见菜单分别进入九个 URL，只渲染对应模块。
- 左侧栏在模块切换后保持；移动端 drawer 正确开关和标记当前模块。
- 直接访问详情、刷新、前进和后退均恢复正确页面。
- 普通账号看不到管理入口且后端返回 403；readonly 不能 mutation。

### 21.2 加载与请求隔离

- 首次进入只请求 workspace 和当前模块 API。
- 点击其它模块不请求已离开模块的数据。
- 返回缓存模块不出现全页 loading，并能后台刷新。
- mutation 只失效明确关联的查询键。
- 401/403 会清空敏感缓存，revision conflict 不丢失表单输入。

### 21.3 九模块行为

- 用户：查询、详情、精确 Session revoke、设备 revoke、跨模块跳转。
- Agent：fresh/stale/unknown、Kick、Migrate、Revoke 和四阶段命令结果。
- 订单：经济快照、attempt/event、reconciliation、人工支付、退款和重放。
- 订阅：grant/extend/change/suspend/restore/cancel、revision conflict 和 policy ack。
- 套餐：草稿校验、新版本发布、历史只读和旧订阅版本保持。
- 特权：类型化字段、创建/撤销/过期、Entitlement/policy revision。
- 优惠码：fixed/percent、reservation/redemption/释放、总量竞争和不可变快照。
- Hub：create/edit/approve/drain/migrate/disable、readiness 与持久状态分离。
- 版本：签名/hash/origin、activate/pause/resume/rollback、灰度和兼容策略。

### 21.4 真实纵向门禁

- Web typecheck/build 和 locale key 对称。
- Playwright 覆盖中文默认、390/768/1440、键盘和 150% 缩放。
- 真实 PostgreSQL、Controller、至少两个独立 Edge；Hub/Agent 操作走真实 control transport。
- 管理 UI 发起 mutation，数据库、领域 revision、Hub policy/CommandOutbox 和页面结果使用同一 request/resource 关联。
- Controller、PostgreSQL 和 Edge 重启后，路由、权限、持久状态和运行时 freshness 正确恢复。

## 22. 后续实施建议

以下只作为未来切片候选，不能绕过 `workflow.md` 当前最早切片直接启动：

1. `OPSCONSOLE001`：稳定管理壳、真实子路由、workspace 权限导航、中文默认和移动 drawer。
2. `OPSCONSOLE002`：模块级 query cache、loading/error/mutation invalidation 和深链接恢复。
3. `OPSCONSOLE003`：用户与 Agent 页面拆分，详情路由和 CommandOutbox 展示。
4. `OPSCONSOLE004`：订单与订阅页面拆分，支付时间线和调整流程。
5. `OPSCONSOLE005`：套餐、特权和优惠码页面拆分，结构化表单和不可变历史。
6. `OPSCONSOLE006`：Hub 与版本页面拆分，生命周期和签名发布流程。
7. `OPSCONSOLE007`：九模块真实 E2E、响应式、请求隔离、重启恢复和旧单页删除。

每个候选切片都必须保持现有 Proto/API/Store 真值；只有发现当前 API 无法表达已存在业务能力时，才按 proto-first 顺序补契约，不得先写 Web 私有 DTO。

## 23. 迁移与删除条件

- 新管理壳和九个模块 E2E 全部通过前，旧 `/operator` 单页不能直接删除。
- 迁移期间禁止同时维护两份 mutation 状态机；新页面必须调用同一 generated Proto API。
- 新模块稳定后，删除账号页九个 hash 链接，替换为单一运营管理入口。
- 删除巨型 `OperatorPage` 的全量 `load()`、九模块同页渲染和重复局部状态。
- 删除以 hash 作为一级模块导航的测试断言，替换为真实 route、请求隔离和缓存行为断言。
- 最终提交、推送和部署必须由未来活动切片执行，并记录构建产物、Controller/Web 版本、线上 URL 和回滚点。
