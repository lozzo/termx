# TermX Remote Platform 产品需求文档

状态：RP001 活动基线

版本：v1 draft

日期：2026-07-11

## 1. 产品定义

TermX 是一个以开源 terminal daemon、TUI 和跨端客户端为基础的多设备终端工作台。用户可以在本机、SSH 可达服务器和 WebRTC 可达设备之间管理 terminal，并在 TUI 或移动 App 中使用统一的 endpoint 体验。

TermX 的商业产品不是“解锁 terminal 功能”，而是降低跨网络连接和多人治理的持续成本：托管设备发现、NAT 穿透、全球 Relay、移动端云同步、团队权限、审计和服务保障。

## 2. 产品原则

### 2.1 开源形成采用，服务形成收入

- core-v2、TUI、CLI、terminal protocol、本地连接、SSH、多 endpoint 管理和公开 remote client/daemon contract 永久免费并开源。
- 用户可以完全不注册 TermX 云账号，仅使用 local 和 SSH。
- 用户购买的是托管基础设施、跨设备便利和组织治理，不是被人为锁住的基础 terminal 能力。

### 2.2 一种 endpoint，多种到达方式

- 用户管理的是 daemon endpoint，不是“SSH 机器”“P2P 机器”或“Relay 机器”三套资源。
- local、SSH、WebRTC 是 transport。
- direct P2P 或 Relay 是 WebRTC session 的 path；连接策略可以偏好 direct，但不改变 terminal 身份和权限。

### 2.3 云端不拥有 terminal

- terminal lifecycle、history、输入、resize 和 capability scope 始终属于 daemon。
- 云端只保存完成托管服务所需的最小设备目录、session metadata、entitlement 和计量数据。
- 云端不保存 terminal 列表、terminal 内容、history、命令输出或 capability grant。

## 3. 目标用户

| 用户 | 核心场景 | 主要价值 |
| --- | --- | --- |
| 独立开发者 | 在笔记本、家中主机、云服务器之间切换 | 免费 local/SSH；复杂网络下可购买稳定 Relay |
| Homelab 用户 | 手机访问家中设备，网络经常变化 | 托管发现、打洞、移动配对和有限 Relay |
| 多机重度用户 | TUI 同时观察多个 endpoint | 开源多 endpoint 工作台；按需购买云连接便利 |
| 小团队 | 共享受控 endpoint，统一权限和审计 | 团队成员、审批、审计、配额和集中管理 |
| 企业 | 私有部署、合规、SLA 和区域控制 | 专属 Relay、SSO、策略、审计导出和支持服务 |

## 4. 版本与收费边界

### 4.1 Community

免费、开源、无需账号：

- core-v2 daemon、TUI、CLI 和公开 protocol。
- local unix socket transport。
- SSH 连接远端 TermX daemon。
- 多 endpoint 展示、切换、同时观察和本地 workbench/layout。
- terminal create/list/attach/input/resize/history/copy 等 daemon 能力。
- capability grant 的离线生成、扫码或文件导入、撤销和本地安全存储。
- 公开的 WebRTC client/daemon transport contract 和端到端授权握手。

Community 不依赖 Web Controller、Hub、Relay 或订阅状态。多机 TUI 观察和 SSH 不作为收费项，因为它们是产品建立信任、传播和开发者采用的关键入口，而且不会持续消耗官方基础设施。

桌面 Community 只安装公开 `termx`；不安装 Cloud Companion 也必须完整使用 local、SSH 和公开 daemon 能力。

### 4.2 Managed Free

免费账号，使用官方托管服务的基础能力：

- 有限数量的个人设备目录和在线状态。
- 托管 Hub rendezvous/signaling 与 STUN，用于 direct P2P 建连。
- 移动 App 与 TUI 使用同一账号查看自己的 endpoint metadata。
- 明确的 direct/Relay path 结果和连接诊断。
- 可以提供小额试用 Relay 配额，但不承诺可用性、区域或持续带宽。

Managed Free 的价值是降低首次成功连接的门槛。具体设备数、试用流量和速率属于运营配置，不写死在公开协议中。

桌面 managed cloud 用户显式安装闭源 `termx-cloud` companion；移动端 official build 使用同一 contract 的私有 cloud module。Companion 缺失或未登录只影响 managed endpoint。

### 4.3 Pro

个人订阅，收费核心是稳定的托管连接服务：

- Managed Relay 正式配额、更多区域和更高并发。
- SmartRoute：根据实时质量在 direct 与 single-relay 候选中选择更稳定路径，而不是把“打洞成功”直接等同于最佳路径。
- 网络变化后的快速重连和 Relay failover 策略。
- 更多托管设备和跨端 endpoint 配置同步。
- 加密的客户端配置备份、配对记录管理和设备撤销入口。
- 计量面板、配额预警和按需流量包。
- 面向个人用户的服务可用性目标和优先支持。

计费单位优先采用“订阅包含 Relay 流量/时长/并发，超额购买流量包”，而不是按 terminal 数或 SSH endpoint 数收费。最终价格、流量额度、区域和超额价格必须经过真实 Relay 成本与用户测试后确定。

### 4.4 Team

团队订阅，收费核心是治理能力：

- Organization、成员、角色和 endpoint 归属。
- 共享 endpoint 的显式邀请、审批和撤销。
- capability policy 模板，但最终 capability 仍由 daemon 签发。
- 登录、设备、连接、授权变更和 Relay 使用审计。
- 团队 Relay 配额、并发控制、成本中心和管理员报表。
- SSO/SCIM、审计导出和策略保留可按版本逐步提供。

Team 不把 Web Controller 变成 terminal 权限真值。它可以管理“谁有资格请求/接收配对”和组织策略，但 daemon 对每次 terminal session 仍做最终授权。

### 4.5 Enterprise

合同制服务：

- 专属或区域固定 Relay。
- 双 Edge Relay Mesh、专属优化骨干或第三方优质传输线路。
- 私有 Control Plane/Hub 部署或混合部署。
- 合规、数据驻留、SLA、支持和定制策略。
- 企业身份系统、审计存档和运维集成。

Enterprise 私有部署是商业授权和交付能力，不要求公开托管 Hub/Web Controller 的服务端源码。

## 5. 不收费与收费能力矩阵

| 能力 | Community | Managed Free | Pro | Team/Enterprise |
| --- | --- | --- | --- | --- |
| 本地 daemon/TUI/CLI | 免费开源 | 同左 | 同左 | 同左 |
| SSH 到远端 daemon | 免费开源 | 同左 | 同左 | 同左 |
| TUI 多 endpoint 同时观察 | 免费开源 | 同左 | 同左 | 同左 |
| 端到端 capability grant | 免费开源 | 同左 | 同左 | 同左 |
| 官方设备目录和 Hub signaling | 不提供 | 基础额度 | 扩展额度 | 组织额度/SLA |
| Direct P2P | 可由公开客户端实现 | 官方托管协助 | 官方托管协助 | 策略化管理 |
| 官方 Managed Relay | 不提供 | 试用或极小额度 | 包含配额/可加购 | 团队配额/专属区域 |
| SmartRoute / 全球加速 | 不提供 | 不提供 | single-relay 智能选区；Mesh 可作为附加项 | Relay Mesh、专属线路和策略 |
| 跨端配置同步 | 本地文件 | 基础 metadata | 加密同步 | 组织策略 |
| 团队 RBAC/审计 | 不提供 | 不提供 | 个人记录 | 收费能力 |

## 6. 核心用户旅程

### 6.1 本地与 SSH 免费旅程

1. 用户安装 daemon 和 TUI。
2. `local` endpoint 默认可用。
3. 用户在 `connections.yaml` 中添加 SSH endpoint。
4. TUI 使用本机 SSH config 和 host key 校验连接远端 daemon。
5. 全程不要求登录 TermX 云端。

验收：云服务完全不可用时，local、SSH 和多 endpoint 工作台行为不退化。

### 6.2 手机通过 direct P2P 连接

1. 用户在 daemon 侧生成带 scope 和 expiry 的 capability grant。
2. App 扫码或导入，保存 endpoint metadata、device fingerprint 和本地 `grant_ref`。
3. App 与 daemon 使用短期 Hub admission ticket 完成 signaling。
4. WebRTC 优先尝试 direct candidate。
5. DTLS DataChannel 建立后，daemon 做设备证明，App 在加密通道内提交 grant challenge proof。
6. daemon 接受 scope 后才开放 termx protocol。

验收：Hub、Control Plane、日志和 SDP 中都不存在 capability grant。

### 6.3 网络无法直连时使用 Relay

1. direct candidate 在策略窗口内失败。
2. 客户端根据套餐请求短期 Relay lease。
3. ICE 使用租约派生的短期 TURN credentials 建立 relayed candidate。
4. Relay 只转发 DTLS 加密流量，并生成幂等 usage events。
5. 客户端展示实际 path、区域、用量和失败原因。

验收：无有效 Relay lease 时只能拒绝 Relay path，不能扩大或缩小 daemon capability，也不能影响 local/SSH endpoint。

### 6.4 团队共享 endpoint

1. 管理员将 device 归入 organization，并配置可邀请成员范围。
2. 成员通过云端工作流请求配对。
3. daemon owner 明确批准并签发 capability grant。
4. Team Control Plane 保存审批和 grant reference metadata，但不保存原始 grant。
5. daemon 撤销 grant 后，下一次连接由 daemon 拒绝；云端同步展示撤销状态。

验收：团队管理员不能仅通过修改数据库直接获得 terminal session。

## 7. 功能需求

### 7.1 统一 endpoint 管理

- TUI 和 App 共享 endpoint identity、transport 配置、device fingerprint、grant reference、relay policy 和连接错误分类。
- 桌面 cloud adapter 通过专用 Cloud Companion IPC 接入；它不是通用插件，也不能接收 grant、DataChannel 或 terminal payload。
- 同一个 endpoint 可以存在 local、SSH 或 WebRTC 配置，但同时只由选定 transport 建立一个 protocol session。
- endpoint label 仅用于展示，不参与认证或路由真值。

### 7.2 连接体验

- 基础策略优先 direct，超过明确超时后才尝试允许的 Relay candidate；SmartRoute 可以在 direct 可达但质量较差时主动选择 Relay。
- UI 必须区分“正在 signaling”“正在 direct 建连”“正在测量路径”“正在申请 Relay”“通过 single Relay/Relay Mesh 已连接”等状态。
- 失败只影响 owning endpoint，保留其他 endpoint 和最后可用画面。
- 不允许隐式 fallback 到原始 SSH shell、旧 remote runtime 或未授权 Relay。

### 7.3 配对与撤销

- daemon owner 可以生成 daemon-wide、single-terminal 或 machine-events scope grant。
- grant 有明确 expiry、device fingerprint 和 revoke identity。
- 客户端只在安全凭据存储中保存原始 grant，普通配置仅保存 `grant_ref`。
- 云端最多保存 grant 状态 reference、签发设备和审计 metadata，不保存原始 grant。

### 7.4 Relay 计量

- Relay lease 明确限定 account、device pair、session、region、expiry、速率和并发。
- usage 以会话级字节数和时长增量上报，事件必须可去重、可补报、可审计。
- 套餐额度和实时限流属于私有服务策略，不进入 terminal protocol。
- 计量失败采用保守的短租约和重试策略，不能无限期放行未计费 Relay。

### 7.5 隐私和可解释性

- 产品页面明确区分 direct 与 Relay；不把“P2P”宣传成必然不经过服务器。
- 用户可以看到当前 endpoint 的 transport、path、Hub/Relay region、grant scope 和 expiry。
- 云端数据清单和保留期必须文档化，terminal 内容默认零采集。

## 8. 非目标

- v1 不做浏览器直接渲染完整 terminal 的 Web IDE。
- v1 不让 Web Controller 代理 terminal protocol 或保存 terminal history。
- v1 不按 terminal 数、SSH endpoint 数或 TUI pane 数收费。
- v1 不承诺自建 Hub/Relay 服务端开源；公开的是客户端 contract 和安全协议。
- v1 不实现任意 transport plugin 系统。
- v1 基础 Relay 不实现固定地区链或任意 N 跳 Relay Mesh；全球加速按独立阶段和成本门禁建设。
- v1 不保留旧 session token、旧 machine-scoped RTC API 或双协议兼容。

## 9. 产品指标

### 9.1 采用指标

- Community 安装后 local 首次成功时间。
- SSH endpoint 首次连接成功率。
- TUI 多 endpoint 活跃用户数。
- App 配对完成率。

### 9.2 连接质量指标

- direct P2P 成功率和 P50/P95 建连时间。
- direct、single-relay、relay-mesh 各路径的 RTT、丢包、抖动、有效吞吐与稳定会话比例。
- Relay fallback 比例、各区域失败率和重连成功率。
- Relay 每会话字节数、时长、峰值并发和单位成本。
- capability handshake 拒绝原因分布，不采集 grant 内容。

### 9.3 商业指标

- Managed Free 到 Pro 转化率。
- 因 Relay 配额、区域或并发升级的转化比例。
- Pro Relay 毛利和每活跃订阅用户基础设施成本。
- Team seat、活跃 organization、受管 device 和审计使用率。

## 10. 发布门禁

远程平台 v1 进入公开测试前必须满足：

- local、SSH、多 endpoint 在无云环境完整可用。
- capability grant 不经过 Hub、Relay 或 Control Plane。
- direct 与 Relay 共用同一 protocol 和 capability handshake。
- 订阅失效只影响付费云服务，不改变 daemon terminal authorization。
- App 与 TUI 通过相同 contract harness。
- 公开 `termx` 在不安装 private companion 时可构建、测试并完整使用 local/SSH；signed companion 安装失败不影响这些能力。
- 旧 session token 和旧 Hub terminal inventory 路径已删除，不存在 fallback。
- Relay 成本、试用额度和 Pro 定价已通过真实流量数据校准；PRD 不提前承诺未经验证的固定数值。

## 11. 待产品验证项

以下参数由运营配置和用户研究决定，不阻塞架构实现：

- Managed Free 的设备数、signaling 频率和试用 Relay 配额。
- Pro 的月度包含流量、峰值速率、并发数和超额流量包。
- Relay 区域优先级和故障转移范围。
- Team 的最低 seat、审计保留期和 SSO 所属版本。
- 是否提供官方 self-host Hub/Relay 商业镜像；该选择不改变公开 client contract。
