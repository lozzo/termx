# AnyTTY Cloud 商业化与权益策略

本文记录 AnyTTY Cloud 首发阶段的商业基线、套餐权益语义、跨端产品反馈契约，以及对应的实施计划和当前交付状态。

## 1. 产品与开源边界

AnyTTY 的付费价值不是单独出售 TURN 流量，而是让用户从任意设备可靠地回到自己的终端现场。

- daemon、CLI/TUI、客户端、Android、Local、SSH、Direct、WebRTC、公开协议和 Cloud Agent 计划使用 Apache-2.0 发布。
- AnyTTY Cloud Controller、账号与计费、风控、Edge 调度、运营后台和生产部署系统保留为托管服务实现。
- Local、SSH 和 Direct 不依赖 Cloud 订阅；开源版本离开官方 Cloud 仍能工作。
- Cloud 免费层承担试用和获客，付费订阅购买托管连接、可靠的 Relay fallback、容量和服务保障。
- AnyTTY App 不登录 Cloud、不持有订阅，也不从 Cloud 同步 daemon 或 endpoint；每个 endpoint 仍由用户扫码加入并保存在 App 本机。

## 2. 首发套餐基线

价格和容量在公开测试取得真实 P2P 成功率、Relay 使用量和付费转化数据后可以通过不可变套餐新版本调整。

| 权益 | Cloud Free | Personal Pro | Power User |
| --- | ---: | ---: | ---: |
| 月付 | 免费 | USD 9.99 / CNY 68 | USD 19.99 / CNY 138 |
| 年付 | 免费 | USD 99 / CNY 648 | USD 199 / CNY 1,288 |
| 已注册 daemon | 2 | 10 | 30 |
| Managed P2P | 不限并发，合理使用 | 不限并发 | 不限并发 |
| Relay 周期配额 | 200 MB | 20 GB | 100 GB |
| Relay 并发 | 1 | 3 | 8 |
| Relay 速率上限 | 2 Mbps | 10 Mbps | 30 Mbps |
| 规划中的额外 50 GB | 不提供 | USD 6.99 / CNY 48 | USD 6.99 / CNY 48 |

当前数据库价格模型的一个套餐版本只支持一种货币；本轮上线的 catalog v2 使用 CNY 价格。表中的 USD 是后续区域化 catalog 的建议锚点，在多币种选区和支付税务交付前不得对外显示。额外流量包同样只作为定价方向，尚未进入可售权益。

免费 Relay 是保证首次体验和偶发受限网络可用的应急容量，不应长期覆盖把 Relay 当作主要路径的用户。免费额度到达上限后硬停止，不要求绑卡、不产生欠费或自动超额账单。

套餐页首先说明完整的 AnyTTY Cloud 连接价值，再列 Relay 配额。不得把套餐描述成单纯购买流量，也不得暗示 Cloud、Controller 或 Edge 能读取端到端终端内容。

## 3. 权益计数语义

### 3.1 daemon 数量

`cloud_daemon_limit` 限制账号下状态不是 `DELETED` 的已注册 daemon 数量，不是同时在线数量。

- `ACTIVE` 和 `BLOCKED` 都占用名额；临时停用不能规避套餐限制。
- `DELETED` 不再占用名额，并且删除不可恢复。
- 创建 enrollment code 可以做前置提示，但最终限制必须在消费 code、创建 daemon identity 的数据库事务中原子执行，避免并发注册超额。
- 已满时不得生成一个看似可用的命令；若前置检查后发生并发竞争，daemon 端仍必须收到稳定的 `cloud_daemon_limit_exhausted` 失败。
- 降档后已有 daemon 数超过新上限时不自动删除或停用；保留现状，但在数量降回限制以内前禁止注册新的 daemon。

### 3.2 Relay 流量

Controller 继续分别保存 ingress 和 egress，商业可计费量以 Relay 发出的 egress 为准，避免同一份用户数据进入和离开 Relay 后被用户感知为计算两次。迁移完成前，控制台必须明确标注当前配额究竟按 egress 还是 ingress + egress 扣减，不能静默改变口径。

- 周期用量达到 50% 时只做信息提示。
- 达到 80% 时显示持续 warning，并在 Cloud Web 提供升级入口。
- 达到 100% 时拒绝新的 Relay reservation；P2P、Local、SSH 和 Direct 不受影响。
- 阈值按已结算用量加当前 reservation 预留量计算；Cloud Web 分开展示已结算流量和 Relay 预留，避免出现“页面未满但新连接被拒绝”。
- 已建立 Relay 的额度由 reservation/lease 预留和续租控制，不允许实际转发无限超过已授权字节。
- 免费套餐不产生超额费用；付费套餐只有用户明确购买流量包后才能增加当期额度。

### 3.3 Relay 并发

一个正在协商 Relay 或实际使用 Relay 的 reservation 占用一个账号级并发名额。并发上限按账号计算，不按 daemon、App 或 Edge 分开计算。

`AUTO` 为了给 ICE 提供 TURN candidate，可以在选路前取得短期 provisional reservation。客户端确认最终路径是 P2P 后，Edge 必须立即按零用量结算并释放该 reservation；不得让一个长期 P2P 会话持续占用 Relay 并发或预留流量。若最终路径是 Relay，reservation 才转为 active 并跟随会话续租。provisional reservation 必须有很短的服务端 TTL，即使客户端在选路中崩溃也能自动回收。

**并发满时保留旧连接，拒绝新的 Relay reservation；不得自动顶掉旧连接。** 自动顶掉会中断用户正在操作的终端或文件传输，并且无法从服务端判断哪一个会话更重要。

- `AUTO`：provisional Relay reservation 被拒绝后仍继续尝试 P2P；P2P 成功时不向用户报错。
- `AUTO` 且 P2P 也失败：最终错误必须保留 `relay_concurrency_exhausted` 原因，提示关闭另一条 Relay 连接、改用 P2P/SSH/Direct，或升级套餐。
- `RELAY_ONLY`：立即返回 `relay_concurrency_exhausted`，不伪装成服务不可用。
- 客户端不能无界自动重试并发耗尽；只允许用户显式重试，或在观察到网络/路由策略变化后重新连接。
- 正常断开必须及时结算并释放名额。客户端崩溃或断网时，由短期 lease 到期回收，不永久占槽。
- 套餐降档导致当前并发超限时不踢现有连接，只拒绝新 reservation；订阅失效、账号/daemon 被阻断等安全和生命周期事件仍可关闭现有 Cloud 会话。

## 4. 稳定失败分类

服务端不能依赖英文错误消息让客户端猜测商业状态。下列稳定错误码必须从 Controller 经 Edge、Go client runtime 一直传到 TUI 和 App：

| 错误码 | 含义 | 是否自动重试 |
| --- | --- | --- |
| `cloud_daemon_limit_exhausted` | 已注册 daemon 达到套餐上限 | 否 |
| `relay_not_in_plan` | 当前套餐不包含 Relay | 否 |
| `relay_quota_exhausted` | 本周期 Relay 流量已用完 | 否 |
| `relay_concurrency_exhausted` | 当前 Relay 活跃连接数已满 | 否 |
| `subscription_inactive` | 订阅未生效、暂停或到期 | 否 |
| `relay_region_unavailable` | 套餐不允许当前 Edge 区域 | 可在重新选区后重试 |
| `cloud_service_unavailable` | Controller/Edge 暂时不可用 | 是，带退避 |

响应可以附带不含账号敏感信息的结构化参数，例如 `limit`、`used`、`remaining_bytes`、`period_end`。客户端展示不得解析服务端英文 message，也不得把套餐拒绝归类为认证失败或要求重新扫码。

## 5. 各端提示职责

### 5.1 Cloud Web

Cloud Web 是订阅和用量的唯一完整管理界面，负责展示：

- 当前套餐、订阅状态、当前周期和续费状态。
- daemon 已用数量与总量，例如 `2 / 2`；达到上限时禁用“注册 daemon”并提供升级或删除现有 daemon 的入口。
- Relay 已用、剩余、周期总量、并发上限和当前活跃并发。
- 50%、80%、100% 阈值提醒；100% 时明确说明 P2P 和非 Cloud route 仍可使用。
- 套餐升级、降档和取消；流量包上线后也在此购买。App 和 TUI 不直接实现支付。

不能把 `cloud_daemon_limit` 写成“同时在线 daemon”。用户管理的是注册槽位，在线状态只是设备列表中的运行时信息。

### 5.2 CLI/TUI

CLI 在 `cloud enroll` 成功后显示当前 daemon 使用量；失败时显示稳定原因及 Cloud 控制台地址。TUI 对 Cloud endpoint 的连接失败进行本地化分类：

- Relay 并发满：`Relay 连接数已满（1 / 1）。关闭另一条 Relay 连接后重试，或改用 P2P、SSH、Direct。`
- Relay 流量用完：`本周期 Relay 流量已用完。P2P、SSH 和 Direct 仍可使用；请前往 Cloud 控制台查看套餐。`
- 订阅不可用：说明由 daemon 所属 Cloud 账号处理，不要求 endpoint 重新配对。

TUI 不需要常驻展示订阅，也不应为了套餐状态主动建立额外 Cloud 账号会话；只在注册、用户打开连接信息或发生权益错误时展示相关信息。

### 5.3 AnyTTY App

App 保持无账号设计，因此不展示“我的订阅”，也不尝试判断当前手机用户是否有权购买 daemon 所属账号的套餐。

- 连接成功时只展示实际路径是 P2P 还是 Relay，不打扰用户展示额度。
- `AUTO` 因并发/额度拒绝 Relay 但 P2P 成功时不显示 warning。
- 连接失败时展示具体原因，并说明“由此设备所属的 AnyTTY Cloud 账号处理”。
- 提供“重试”和“连接设置”操作；可以提供打开公开 Cloud 控制台的链接，但不能携带账号 ID、订阅 ID 或自动登录信息。
- 权益失败不触发重新扫码。只有身份不匹配、授权撤销或 enrollment 删除才引导重新配对。

建议文案：

| 场景 | App 文案 |
| --- | --- |
| Relay 并发满 | `此设备所属账号的 Relay 连接数已满。关闭另一条 Relay 连接后重试，或切换到 P2P。` |
| Relay 流量用完 | `此设备所属账号本周期的 Relay 流量已用完。P2P 可用时仍会自动连接。` |
| 套餐不含 Relay | `当前网络无法建立 P2P，而此设备所属套餐不包含 Relay。` |
| 订阅失效 | `此设备的 AnyTTY Cloud 订阅当前不可用，请由设备所有者在 Cloud 控制台处理。` |

## 6. 收入模型基线

以下数字只用于首年预算，不是收入承诺。模型假设 85% 付费用户选择 Personal Pro、15% 选择 Power User、70% 年付，综合每个平均付费账号年收入约 USD 121，支付手续费、退款和坏账合计按收入 5% 计。

| 情景 | 年末注册账号 | 付费转化 | 年收入 | 人力前利润 | 计入建议人力后的税前经营利润 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 保守 | 15,000 | 2.5% | USD 23K | 约 USD 0 | -USD 60K |
| 基准 | 60,000 | 4% | USD 145K | USD 76K | USD 16K |
| 乐观 | 200,000 | 6% | USD 727K | USD 446K | USD 266K |

基准情景中的成本预算为支付 USD 7K、服务器与 Relay USD 15K、营销 USD 35K、客服/监控/法务 USD 12K。中国大陆 Relay 必须独立核算带宽、节点和合规成本，不能直接套用国际节点模型。

## 7. 实施计划与状态

本轮更新按以下顺序实施，避免前端先依赖不稳定的英文错误文本：

1. **Wire contract（已完成）**：增加稳定的 Cloud entitlement 枚举、结构化额度参数、Relay 拒绝投影和客户端选路确认消息。
2. **daemon 原子准入（已完成）**：生成 enrollment code 前检查注册槽位；最终消费 code 时锁定订阅并在同一数据库事务中检查非 `DELETED` daemon 数量。并发竞争不会注册超额。
3. **Relay reservation 生命周期（已完成）**：并发满时保留旧连接并拒绝新 reservation；Relay-only 返回具体商业原因；实际选中 Direct/P2P 后停止续租并按已有零用量结算路径立即释放 provisional reservation。
4. **Go client、CLI 与 TUI（已完成）**：商业拒绝映射为 entitlement/resource-exhausted，而不是普通认证或网络失败；TUI 保留可执行提示；`cloud enroll` 成功后显示 daemon 使用量，满额失败时指向 Cloud 设备管理页。
5. **AnyTTY App（已完成）**：并发满、流量用完和套餐不含 Relay 分开提示；不要求登录 Cloud，不把权益错误误导为重新配对，文案明确 P2P、Direct 和 SSH 的可用性。
6. **Cloud Web（已完成）**：设备页显示 `已注册 / 总量` 并在满额时禁用注册；套餐页统一使用“已注册 daemon”；订阅页展示 Relay 剩余与活跃并发；用量页提供 50%、80%、100% 提醒并明确当前按入口加出口统计。
7. **验证（已完成）**：Proto/descriptor 重新生成并校验；Controller、PostgreSQL、Edge、Cloud client、daemon、binding、TUI、CLI 和集成测试通过；Cloud Web 与 App UI 完成类型检查和相关测试。

### 7.1 后续独立工作

以下项目不应混入本轮权益准入更新，需要单独的 schema、迁移、支付或运营设计：

- **egress 计费迁移**：当前仍按 `ingress + egress + recovery` 扣减，Cloud Web 已明确标注。改成仅 egress 前必须完成历史用量迁移、双口径核对和账单测试。
- **流量包**：commerce schema 尚无流量包订单与当期余额，首发页面不得承诺“额外 50 GB”，直到购买、退款、到期和并发结算全部交付。
- **真实支付**：当前 Cloud Web 仍是 Development 支付适配器，公开售卖前必须接入支付提供方、税务、退款、账单和 webhook 对账。
- **升级入口**：设备满额和 80% 用量提醒已经可见；正式套餐路由与支付上线后，再把提示按钮接到可购买的目标套餐，而不是预置无效入口。
