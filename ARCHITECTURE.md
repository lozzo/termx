# Muxvia Cloud 重构设计与执行基线

## 1. 文档地位

本文档保存 Muxvia Cloud 已稳定的产品与技术架构。当前分支的活动范围、切片顺序、测试准入和提交规则由仓库根目录 `workflow.md` 驱动；两者冲突时以 `workflow.md` 为准。

历史方案、旧代码行为和聊天记录不能覆盖当前工作流。需要改变稳定架构时同步修改本文；活动切片的细节先进入 `workflow.md`，验收后再沉淀到本文。

当前工作顺序固定为：

1. 删除旧 Markdown，建立本文档。
2. 按本文定义的边界删除旧 Cloud 代码，但保留可独立工作的本地、Direct 和 SSH 能力。
3. 先定义新 Proto，再按纵向链路重建 Controller、Edge、daemon、客户端和运营后台。
4. 完成真实部署和全产品端到端验收后，才允许宣布上线。

本文首先定义目标架构，不承诺当前代码已经满足这些要求。文中标记为“目标”的目录、接口和行为必须通过后续切片实现和验证。

## 2. 已确定的产品决策

### 2.1 产品与开源边界

- 产品名称是 Muxvia，托管服务名称是 Muxvia Cloud。
- 全部产品代码最终开源，不再维护 public/private 两套源码，也不为“以后可能开源”保留 `private/` 分层、导出器或双仓同步机制。
- 账号、订单、订阅、权限、证书秘密、生产凭据和运营数据属于部署数据，不因源码开源而公开。
- 开源许可证必须在对外发布前单独确定并替换当前过渡性根许可证。没有确定许可证前，不得把“代码可见”误称为已完成开源发布。
- 历史代码不做兼容保留。当前产品尚未正式发布，开发数据、旧 Proto 和旧 Cloud 配置可以重置。

### 2.2 Cloud 的产品位置

- Muxvia 的 terminal、文件、输入、历史和授权真值仍由 daemon 持有。
- Direct 和 SSH 是不依赖 Muxvia Cloud 的基础连接方式；用户未登录、未订阅或 Cloud 故障时，它们仍应工作。
- Muxvia Cloud 只提供托管的设备发现、信令、打洞协助、Relay、账号、订阅、配额、用量和运营管理能力。
- 浏览器管理后台用于账号和云服务管理，不拥有 terminal 会话、CapabilityGrant 或 daemon 内部资源。
- Cloud 不能解密 WebRTC DataChannel 内的 terminal 和文件业务数据。

### 2.3 Edge 是唯一边缘服务

旧设计中的 Hub 和 Relay 合并为一个产品领域和一个部署单元，统一称为 **Edge**。

Edge 对外表现为一个节点：只有一个 `edge_id`、一个节点身份、一个域名入口、一条 Controller 控制流、一个版本和一个生命周期。它既负责信令与打洞，也负责在无法直连时进行 TURN/Relay 转发。

代码内部可以把 gateway、signaling、relay、policy 和 usage 分成模块，但这些只是一个 Edge 内部的职责，不得重新形成两套节点身份、配置、在线状态、管理页面、持久化拓扑或控制连接。

### 2.4 平台支持边界

- Windows 一等支持范围是 Windows 10 1809+/Windows 11 的 amd64 本地产品面：`muxvia` CLI、TUI、ConPTY terminal、当前用户 daemon、Direct/SSH/Cloud 客户端链路、文件传输、历史、配对、安装、升级和卸载。
- Windows terminal 的唯一 PTY owner 是 ConPTY；进程树由 Job Object 管理，退出、强制停止、resize 和输出 drain 不得退回到 pipe 模拟、Unix shell 或 WSL。
- Windows 私有状态使用当前用户、SYSTEM、Administrators 的受保护 DACL，不能用 `os.FileMode` 的 `0600/0700` 映射假装完成权限隔离。Unix 继续使用 owner UID 与 mode 真值。
- Windows 当前用户 daemon 使用同一二进制的 `daemon start/status/stop/restart` 生命周期；安装包写入 `%LOCALAPPDATA%\\Programs\\Muxvia`，用户 PATH 与登录自启动都属于 HKCU，不要求管理员权限。升级必须先按 runtime record 精确停止旧进程，再原子替换二进制并重新启动。
- TUI 图标字形由外部 terminal host 渲染，应用不能通过 ANSI 强制切换字体。Windows 安装包必须携带固定版本且校验摘要的 `JetBrainsMono Nerd Font Mono`，按当前用户注册字体，并通过 `%LOCALAPPDATA%\\Microsoft\\Windows Terminal\\Fragments\\Muxvia` 提供独立的 Muxvia profile；不得重写用户现有 `settings.json` 或全局默认 profile。卸载只删除 Muxvia 自有字体注册、字体副本和 fragment。
- Windows 开发门禁使用仓库 PowerShell 脚本，覆盖 Go 全量测试、Proto 生成、UI/mobile 测试、typecheck、生产构建、三个 Go 二进制构建和安装包 smoke。Android 的 Go/JNI 产物允许在安装了固定 NDK 的 Windows 主机生成。
- `muxvia-cloud-controller` 与 `muxvia-cloud-edge` 必须在 Windows 编译并通过平台无关单测，但 Cloud 生产服务器、一键 Edge 安装、systemd unit、人工程序部署与运维证据仍固定为 Linux/amd64。该服务器部署约束不允许污染 Windows 客户端或本地 daemon 的运行路径，也不得把 Windows 二进制展示成可部署的生产 Edge artifact。

## 3. 系统角色与职责

### 3.1 Controller

目标二进制：`muxvia-cloud-controller`。

Controller 是云端持久业务真值和实时目录的组合入口，负责：

- 账号、登录会话、角色和运营权限。
- 套餐、订单、支付结果、订阅、Entitlement、周期配额和结算。
- daemon 归属、注册身份、公钥和撤销状态。
- Edge 基础配置、域名、端口、区域、容量和当前证书档案。
- Edge 候选列表、短期票据和已签名策略的签发。
- 通过每个 Edge 的单条长连接维护纯内存在线目录。
- 将 Web 管理操作路由到当前持有目标连接的 Edge。
- 接收用量批次并幂等结算。
- 提供用户 API、运营 API、Web 静态资源和实时事件流。

Controller 不直接持有每个 daemon 的网络长连接，不保存实时拓扑快照到数据库，也不代理终端业务流量。

### 3.2 Edge

目标二进制：`muxvia-cloud-edge`。

Edge 负责：

- 向 Controller 建立一条主动发起的双向 gRPC 控制流。
- 接受 daemon 长连接，验证短期 AgentTicket，并维护 daemon Presence。
- 接受客户端 Cloud Route 信令连接，验证短期 ClientTicket。
- 在客户端和 daemon 之间转发 WebRTC offer、answer 和 ICE candidate。
- 提供 STUN/TURN 能力；直连失败时承载 Relay 数据面。
- 执行并发连接数、速率、Relay 字节和租约有效期限制。
- 向 Controller 汇报在线摘要、拓扑变化、控制结果和累计用量。
- 在 Controller 暂时不可用时，让已建立且仍在租约内的连接继续工作。

Edge 的在线拓扑是纯内存状态。Edge 磁盘只允许保存节点私钥/证书和“尚未被 Controller 确认”的 usage outbox，不允许保存 daemon 分配、连接快照或可恢复的在线状态。

### 3.3 daemon

daemon 是 terminal 生命周期和端到端授权的所有者，负责：

- 持有不可导出的 DeviceIdentity 私钥。
- 持有 terminal、文件、CapabilityGrant、客户端授权和已认证 PeerSession。
- 从 Controller 获取 Edge 候选和短期 AgentTicket。
- 选择一个 Edge 并建立长连接；连接断开后自行重新选择和重连。
- 完成 WebRTC DTLS channel binding 后验证 CapabilityGrant。
- 接收 Edge 转发的连接信令和实时控制命令。
- 对已建立的端到端会话执行关闭或拒绝，不把 terminal 权限判断交给 Cloud。

### 3.4 客户端

TUI、CLI、Android、未来 iOS 和桌面 GUI 都通过同一套 Go Client Engine 使用 Direct、SSH 或 Cloud Route。平台 UI 不复制连接、重试、认证或 Proto 状态机。

客户端可以没有 Muxvia 账号。Cloud 目录中的“连接者”必须记录客户端身份和产品类型，不能在没有账号认证的情况下把它显示成某个“用户”。产品类型至少包括：`TUI`、`CLI`、`Android`、`iOS`、`DesktopGUI` 和 `Unknown`。

### 3.5 Web 管理后台

Web 管理后台由 Controller 提供，但只是持久业务和实时目录的视图及操作入口，不建立第二份状态真值。

运营后台默认语言为中文。登录后必须进入稳定的管理壳：左侧始终显示模块导航，右侧显示当前模块的表格、详情或表单。所有正式模块都必须能从左侧菜单进入，不能要求运营人员手工输入 `/operators`、`/users` 等 URL。

切换模块时保留侧边栏、顶部身份和页面框架，只刷新目标数据。已经加载且仍然有效的数据可以使用查询缓存，后台静默刷新；不得每次点击左侧菜单都显示全屏 loading，也不得把所有模块内容堆在同一个页面。

## 4. 总体拓扑

```text
运营人员浏览器
      |
      | HTTPS + JSON/SSE
      v
+----------------------------+
| Controller                 |
| 持久业务 + 纯内存在线目录   |
+----------------------------+
      ^
      | 每个 Edge 一条双向 gRPC/TLS 控制流
      |
+----------------------------+       +----------------------------+
| Edge A                     |       | Edge B                     |
| signaling + STUN/TURN      |       | signaling + STUN/TURN      |
| policy + usage             |       | policy + usage             |
+----------------------------+       +----------------------------+
   ^          ^                         ^          ^
   |          |                         |          |
daemon 长连接  客户端信令连接             daemon 长连接  客户端信令连接
   |          |                         |          |
   +--- WebRTC DataChannel: 优先 P2P，失败时经所在 Edge TURN Relay ---+
```

Controller 的长期连接数与 Edge 数量同阶，而不是与 daemon 或客户端数量同阶。daemon 和客户端连接由各个 Edge 分摊。Controller 只参与控制、目录、票据、策略和结算，不进入业务 DataChannel。

## 5. 通信协议

### 5.1 固定协议选择

- Controller 与 Edge：gRPC over TLS/HTTP2，Edge 主动连接 Controller，生产入口默认使用 TCP 443。
- daemon 与 Edge：双向 gRPC over TLS/HTTP2，daemon 主动连接 Edge。
- 客户端与 Edge：Cloud Route 信令使用 gRPC 或同一 Proto 契约上的受控传输；原生客户端优先复用 Go gRPC 实现。
- 浏览器与 Controller：HTTPS JSON API；实时列表和命令结果使用 SSE。当前不为运营后台建立另一条业务 WebSocket 状态机。
- P2P/Relay 数据面：可靠有序 WebRTC DataChannel，Relay 使用标准 TURN UDP/TCP/TLS 能力。

### 5.2 Proto-first

所有跨进程和跨语言契约必须先定义 versioned Proto，再生成代码。禁止先写 Go/TypeScript DTO，之后再补 Proto。

目标服务形态：

```proto
service EdgeControl {
  rpc Connect(stream EdgeEvent) returns (stream ControllerCommand);
}

service AgentGateway {
  rpc Connect(stream AgentEvent) returns (stream EdgeCommand);
}

service ClientGateway {
  rpc Connect(stream ClientSignal) returns (stream EdgeSignal);
}
```

所有流式 envelope 必须具有协议版本、消息 ID、发送方 ID、连接 generation 和 `oneof` payload。请求/响应型控制还必须具有 correlation ID、deadline、结构化结果和明确错误码。

Edge 只有一套节点密钥和身份。如果不同用途需要隔离签名，使用 domain-separated signing context，不创建第二个 HubIdentity 或 RelayIdentity。

## 6. 真值与持久化边界

### 6.1 Controller 数据库保存什么

Controller 的 PostgreSQL 只保存重启后仍然成立、需要审计或需要结算的持久业务事实：

| 领域 | 持久内容 |
| --- | --- |
| 账号 | 用户、登录会话、角色、运营权限、账号状态 |
| 商业 | 套餐、价格、订单、支付结果、订阅、Entitlement、促销规则 |
| daemon | 所属账号、注册身份、公钥、显示名称、撤销/禁用状态 |
| Edge 配置 | `edge_id`、名称、区域、容量、域名或域名加端口、监听配置、启停状态 |
| 证书 | 证书配置、域名集合、版本、有效期、发布状态、审计记录；私钥按第 11 节处理 |
| 用量 | 已确认的 Relay 用量、周期聚合、配额消耗、结算幂等键 |
| 发布 | Edge 安装令牌摘要、允许版本、升级策略和必要审计记录 |

### 6.2 Controller 内存保存什么

- 当前在线 Edge 连接和 connection generation。
- `daemon_id -> edge_id` 的实时映射。
- 每个 Edge 当前 daemon、客户端信令会话、P2P/Relay 会话摘要。
- 按账号、Edge 和 daemon 建立的查询索引。
- 待返回给浏览器的短期命令 correlation。
- 心跳、版本、负载、实时流量速率和最后上报时间。

这些数据在 Controller 重启后通过 Edge 全量上报重建，不从数据库恢复。

### 6.3 Edge 内存保存什么

- daemon stream、客户端信令 stream 和 generation。
- WebRTC 信令事务、P2P 会话摘要和 Relay allocation。
- 每账号/daemon/Edge 的并发数、速率和租约内字节计数。
- Controller 命令的短期执行状态。
- 向 Controller 汇报的当前拓扑版本和累计用量计数器。

建议使用以 `edge_id`、`daemon_id`、`session_id` 和 `account_id` 为键的 map 及反向索引，保证常用增删改查平均为 O(1)。一个 Edge generation 断开或过期时，Controller 应能整体丢弃该 generation 的命名空间，避免逐条残留。

### 6.4 明确禁止持久化的内容

- “某 daemon 当前连到哪个 Edge”。
- daemon、客户端、P2P 或 Relay 的在线布尔值。
- Presence、当前连接、实时心跳和瞬时负载。
- 等待执行的踢下线、迁移、断开或刷新命令。
- 为恢复在线状态而写入的 Edge snapshot、WAL 或 Hub assignment。
- 已经可以由 Edge 重连后全量上报重建的拓扑 revision。

实时操作失败就向操作者返回失败，不为它建立数据库 CommandOutbox。需要长期生效的“禁用账号”“撤销 daemon”“修改配额”等操作先提交持久业务状态，再做一次 best-effort 在线推送；后续票据签发和短租约到期负责最终收敛。

## 7. Edge 部署与证书控制流

### 7.1 运营人员填写的内容

创建 Edge 时只允许填写部署意图：

- 节点名称。
- 区域和可用区。
- 标称容量和运营备注。
- 对外域名，允许填写域名或域名加端口。
- 证书配置档案。
- 可选的启用时间和版本通道。

国内备案域名和海外域名可以不同，每个 Edge 使用自己的域名和证书配置。健康检查路径、节点 ID、密钥、Controller 地址、注册协议版本和内部服务端口均由系统生成或从部署环境推导，不让运营人员手工填写。

### 7.2 一次性安装流程

1. Controller 持久化 Edge 配置并生成短期、单次使用、只保存摘要的 claim token。
2. 管理页面生成形如 `curl -fsSL https://.../install | sudo sh -s -- --token ...` 的安装命令。
3. 脚本校验操作系统和架构，安装签名后的固定版本二进制与 systemd 服务。
4. Edge 首次启动时在本机生成私钥和 CSR，私钥权限必须为 `0600`，不得显示给运营人员。
5. Edge 使用 claim token、CSR 和机器信息完成注册，Controller 返回 `edge_id`、证书链、域名/端口、限制策略和 Controller endpoint。
6. Edge 启动健康检查与网络监听，随后主动建立 `EdgeControl.Connect`。
7. Controller 只有收到有效的 Hello 和第一份全量快照后，才把节点显示为在线。

健康检查 URL 固定由协议、域名、端口和标准路径 `/healthz` 推导；页面只展示，不提供自由编辑框。

### 7.3 证书统一管理

证书以“证书配置档案”统一管理，可绑定一个或多个 Edge：

- 首选模式是 Edge 本地生成私钥，Controller 或 ACME 流程只签发和保存证书链、版本、有效期与发布状态。私钥不离开 Edge。
- 如果必须导入已有证书和私钥，私钥只能进入独立 secret manager/KMS 加密存储，不能以明文进入普通业务表、日志或前端响应。
- 技术人员上传新版本或自动续期成功后，Controller 创建 staged release，通过现有 Edge 控制流通知目标节点。
- Edge 拉取、校验域名和证书链，写入临时文件并原子切换，成功热加载后上报 applied 版本。
- 更新失败时继续使用仍然有效的旧证书并上报错误，不允许因半写入导致监听不可用。

## 8. daemon 注册、选路和 Presence

### 8.1 首次注册

1. 用户在 Web 账号中创建一次性 daemon enrollment code。
2. daemon 使用该 code 和 DeviceIdentity 公钥向 Controller 注册。
3. Controller 只持久化账号归属、daemon 身份、公钥和撤销状态。
4. Controller 根据区域、容量、实时负载和健康状态返回候选 Edge 列表。
5. daemon 对候选地址做最小网络探测并选择一个 Edge。
6. Controller 签发短期、绑定 `daemon_id`、`edge_id`、能力和有效期的 AgentTicket。
7. daemon 连接 Edge；Edge 离线验证票据并建立 generation。
8. Edge 通过自己的 Controller 长连接上报 `AgentJoined`，Controller 在内存建立 `daemon_id -> edge_id` 映射。

数据库不保存这次 Edge 选择。Edge 断开、daemon 离线或 Presence 超时后，对应内存映射自然消失。

### 8.2 重连和冲突

- 当前 Edge 可用且票据仍有效时，daemon 优先原地重连。
- 当前 Edge 不可用时，daemon 重新向 Controller 获取候选并选择。
- 同一 daemon 出现多个连接时，以经过身份验证的较新 generation 为准；Controller 和 Edge 必须通过 generation fence 拒绝旧连接迟到消息。
- Controller 不为 daemon 维护持久 epoch，也不把旧映射作为重启 fallback。

## 9. 客户端 Cloud Route 控制流

### 9.1 配对与端到端授权

客户端通过 daemon 创建的配对 claim 或已持有的 CapabilityGrant 获得访问能力。Controller 和 Edge 不读取、不签发 terminal capability，也不知道具体 terminal 权限。

客户端身份可以是匿名的 ClientAccessIdentity。只有客户端额外完成 Muxvia 账号认证时，Controller 才能把该连接关联到账号；否则管理页面显示客户端 ID、产品类型和连接元数据，不显示虚构的用户名。

### 9.2 建立连接

1. 客户端向 Controller 查询目标 daemon。
2. Controller 从内存目录定位当前 Edge；若不存在，立即返回离线，不查询数据库中的旧映射。
3. Controller 校验账号状态、订阅、Entitlement 和云服务配额，签发短期 ClientTicket。
4. 客户端连接目标 Edge，Edge 离线验证票据。
5. Edge 通过已有 daemon stream 转发 offer、answer 和 ICE candidate。
6. 客户端与 daemon 优先建立 P2P WebRTC DataChannel。
7. 直连失败且 Entitlement 允许时，双方使用同一个 Edge 的 TURN Relay。
8. DTLS channel binding 完成后，daemon 在 DataChannel 内验证 CapabilityGrant，然后才开放 terminal/file API。
9. Edge 向 Controller 上报连接者 ID、客户端类型、目标 daemon、连接路径、会话状态和用量摘要；这些只进入内存目录。

## 10. 管理操作控制流

### 10.1 踢掉一个客户端

1. 用户或运营人员在 Web 页面选择实时连接并点击断开。
2. Controller 从内存索引找到 owning Edge，发送带 deadline 的 `CloseClientSession`。
3. 如果会话正在 Relay，Edge 立即关闭 allocation，并阻止该 session 继续信令。
4. 如果会话已经 P2P，Edge 通过 daemon stream 请求 daemon 关闭对应 PeerSession；Edge 本身不能伪装成已经切断端到端连接。
5. daemon 返回结果，Edge 再向 Controller 返回结构化 ACK，页面展示实际结果。
6. Edge、daemon 或 session 已离线时立即返回“目标已离线/无法执行”，不写数据库等待以后执行。

### 10.2 撤销与禁用

撤销 daemon、封禁账号、取消订阅和修改 Entitlement 是持久业务操作：

1. Controller 在数据库事务中提交新状态。
2. 新票据立即拒绝签发。
3. Controller 对在线 Edge 做 best-effort 推送。
4. 现有短期票据或租约最晚在过期后失效。
5. 已经建立的 P2P DataChannel 需要 daemon 配合关闭；管理页面必须区分“撤销已生效”和“远端会话已确认关闭”。

### 10.3 配额与 Relay 租约

- Controller 根据订阅和周期用量签发短期已签名策略。
- Edge 本地执行最大 daemon 数、会话数、Relay 并发、速率和字节上限。
- 新建 Relay allocation 前，Edge 通过控制流申请或消费仍有效的 reservation/lease。
- Controller 故障时，不再批准超出现有租约的新 Relay；已建立会话在当前租约和限制内继续。
- 不为理论上的全局实时精确计数引入分布式状态系统；单区域真实闭环完成前不做跨 Edge 强一致并发预留。

### 10.4 用量上报与结算

- Edge 对每个租约/会话维护单调递增累计字节，不使用容易重复计算的瞬时 delta 作为结算真值。
- Edge 定期批量上报，包含稳定幂等键、租约 ID、时间窗口和累计值。
- Controller 在数据库事务中幂等合并到账号周期用量。
- Controller 确认后，Edge 才从本地 usage outbox 删除对应记录。
- Controller 不可用时只积压未确认 usage；不积压 Presence、心跳、信令或控制命令。

## 11. 上报协议与故障语义

### 11.1 上报模型

每条 Edge 控制流使用以下组合：

- 建连后发送一次当前 generation 的全量 runtime snapshot。
- 连接期间发送 `AgentJoined/Left`、`ClientJoined/Left`、路径变化和控制结果等增量事件。
- 定期发送心跳、计数摘要、版本和拓扑 digest。
- Controller 发现序号缺口或 Edge 重连时，要求重新发送全量 snapshot，不尝试从数据库补齐。

### 11.2 Edge 上报失败

- Edge 保持已有 daemon、P2P 和租约内 Relay 会话，不因一次 Controller 上传失败主动清空。
- 使用指数退避加 jitter 重连，成功后发送最新全量 snapshot。
- 不把每个 Presence 事件无限写盘；断线期间只保留当前内存状态，重连后以新快照覆盖。
- 未确认 usage 按第 10.4 节落盘并重试。

### 11.3 Controller 重启或升级

- Edge、daemon 和已建立 DataChannel 继续运行。
- Controller 的实时目录清空，Edge 重连后用全量 snapshot 重建。
- 重建窗口内，新目录查询、Web 实时操作、票据签发或新 quota reservation 可以暂时失败。
- Web 页面显示“控制面正在重建”，不能读取数据库旧拓扑伪装在线。

### 11.4 Edge 重启或故障

- Edge 的所有内存拓扑立即失效，Controller 丢弃对应 generation。
- 经该 Edge 的 Relay 会话中断；P2P DataChannel 是否继续由端到端连接决定，但其云端状态不再显示为可控在线。
- daemon 和客户端按票据及重连策略重新选择 Edge。
- Edge 不从磁盘恢复旧 daemon/客户端在线表。

### 11.5 数据库故障

- 账号、订单、订阅、配置修改、票据签发和结算等需要持久事务的操作失败并明确返回。
- Edge 已建立的运行时在现有签名策略和租约有效期内继续。
- 不提供绕过数据库鉴权或硬编码 Entitlement 的生产 fallback。

## 12. 运营后台功能设计

### 12.1 固定页面框架

登录后的所有运营页面共用一个 App Shell：

- 左侧一级导航固定存在，可折叠但不能因进入某个模块消失。
- 顶部只放当前模块标题、环境、Controller 状态、全局搜索和账号菜单。
- 右侧只显示当前路由模块的内容。
- URL 与菜单选中项双向一致，浏览器刷新可恢复当前模块。
- 页面切换不卸载整个 App，不重复拉取稳定字典数据，不出现全屏启动页。
- 初次加载使用局部 skeleton；刷新使用行内状态或右上角同步状态，不遮挡已有数据。

### 12.2 模块与职责

| 左侧模块 | 能看到什么 | 能做什么 | 真值来源 |
| --- | --- | --- | --- |
| 总览 | 在线 Edge、daemon、客户端会话、P2P/Relay 比例、告警、当期用量 | 跳转到异常节点或会话 | DB 聚合 + 内存实时目录 |
| Edge 管理 | ID、名称、域名/端口、区域、容量、状态、版本、证书、在线数、负载、流量 | 新建安装、编辑配置、启停、绑定证书、查看详情 | 配置来自 DB，在线数据来自内存 |
| 在线 daemon | daemon ID、所属账号、当前 Edge、连接时间、版本、地址摘要、活动会话数 | 查看详情、断开当前连接、进入账号 | 仅内存实时目录 |
| 实时连接 | session ID、客户端 ID、客户端类型、目标 daemon、P2P/Relay、速率、开始时间 | 断开会话、查看关联对象 | 仅内存实时目录 |
| 用户与权限 | 账号资料、角色、状态、daemon 数、订阅和用量摘要 | 禁用/恢复、调整运营角色、查看资产 | DB |
| 套餐与订阅 | 套餐版本、Entitlement、订阅状态、周期、配额 | 创建/发布套餐、变更订阅、查看生效记录 | DB |
| 订单与交易 | 订单、支付状态、退款、幂等键、审计信息 | 查询、人工处理允许的异常流程 | DB |
| 证书 | 档案名称、DNS SAN、指纹、有效期、当前 revision、绑定 Edge、desired/applied 状态 | 上传双文件、替换当前内容、绑定 Edge | DB 元数据 + Controller secret 文件 + Edge applied 状态 |
| 用量与结算 | 账号/周期/Edge 的累计 Relay 用量、配额、结算批次 | 查询、导出、处理结算异常 | DB |
| 审计与系统 | 持久管理操作、版本、Controller 状态、Edge 重连事件 | 查询审计、查看系统健康 | DB 审计 + 内存状态 |

“在线 daemon”和“实时连接”页面不得提供历史数据库 fallback。对象离线后从实时列表消失；需要追责的管理动作进入审计日志，但审计日志不反向构造在线状态。

### 12.3 Edge 列表与详情

Edge 列表至少显示：

- `edge_id`、名称、区域。
- 域名或域名加端口。
- 标称容量和实时负载。
- 在线/离线、最后上报时间、运行版本。
- 当前 daemon 数、客户端会话数、P2P 数、Relay 数和实时吞吐。
- 证书版本、到期时间和 applied 状态。

Edge 详情分为独立页签，不把所有信息堆成一页：

- 概览：基础配置、监听入口、版本、健康和容量。
- 已注册 daemon：当前注册到该 Edge 的 daemon，以及所属账号和活动会话。
- 实时连接：客户端身份、产品类型、目标 daemon、连接路径和流量。
- 证书：绑定域名、当前/待发布版本、有效期和节点应用结果。
- 配置与审计：可编辑字段、安装记录、升级记录和管理操作。

## 13. 目标代码布局

重构后的 Cloud 代码使用公开、单一所有权布局：

```text
cloud/
  controller/
    account/
    commerce/
    enrollment/
    edgeconfig/
    directory/
    control/
    certificate/
    usage/
    apihttp/
    apigrpc/
    postgres/
    runtime/
  edge/
    runtime/
    controllerlink/
    agentgateway/
    clientgateway/
    signaling/
    relay/
    policy/
    usage/
    certificate/
    runtimeconfig/
  daemon/
  client/
  web/
  integration/
  testkit/

proto/cloud/v1/
cmd/muxvia-cloud-controller/
cmd/muxvia-cloud-edge/
```

约束如下：

- `cloud/controller/directory` 是在线目录唯一 owner，纯内存，不依赖 PostgreSQL topology 表。
- `cloud/edge` 是一个服务领域；内部 relay package 不得暴露第二个节点身份或独立控制面。
- `cloud/web` 只消费 Controller API，不定义业务真值。
- `proto/cloud/v1` 是 Cloud 跨边界 schema 唯一真值。
- daemon、client runtime、Direct、SSH 和通用 WebRTC primitive 保留在各自公开领域，不搬入 Cloud。
- 删除 `private/` 后，不允许通过新名字恢复 private import 或闭源 companion。
- 密钥、数据库口令、支付密钥和生产配置只能来自环境、secret manager 或权限受控文件，永不进入仓库。

## 14. 旧代码删除计划

代码删除是下一阶段，必须先形成一个可审查的删除切片。删除目标包括：

- 整个 `private/archive/`。
- 整个旧 `private/cloud/`，包括旧 Controller、Hub、Relay、Companion、Web Controller 和部署脚本。
- 旧 `proto/cloudpb` 及对应生成代码。
- 旧 Hub/Relay 双身份、双配置、双管理页面和持久 topology 模型。
- `shared/cloudcompanion` 及旧 managed-cloud adapter/wiring。
- daemon 和客户端中只为旧 Cloud 协议存在的 reporting、assignment、control 和 fallback。
- 与旧 Cloud 架构绑定的测试、开发环境、部署配置、文档 guard 和 workspace module。
- 旧数据库中的 assignment、connection、presence、snapshot、command outbox 等拓扑迁移。

必须保留并在删除后验证：

- `core/`、`tui/`、terminal protocol 和 API Layer。
- daemon 本地 terminal/file/CapabilityGrant 真值。
- Direct 和 SSH Route。
- 通用 WebRTC、DTLS channel binding 和 DataChannel primitive。
- Go Client Engine、binding、Android 壳和共享 UI 中与旧 Cloud 无关的能力。
- 账号/订单/订阅设计中经重新审查后仍符合本文的持久领域语义；旧实现不能原样默认保留。

删除完成后的第一个门禁是：仓库在“Cloud 暂不可用”的显式状态下可以构建和运行 Direct/SSH，不再 import 旧 Cloud。这个删除检查点不能部署到生产。

## 15. 重建切片顺序

每个切片都必须包含真实消息链路、测试和中文提交，不能用只有 DTO、fake store 或静态页面的结果宣称完成。这里是总顺序，逐项提交边界以第 32 节为准。

1. **R1 契约与进程骨架**：建立 `proto/cloud/v1`、两个二进制、TLS/gRPC Hello、版本协商和生成代码门禁。
2. **R2 Edge 控制流**：实现两个内存 actor、`EdgeControl.Connect`、generation、全量快照、增量事件和 Controller Directory。
3. **R3 Edge 配置与安装**：实现 Edge desired config、claim、EdgeIdentity、mTLS、curl installer 和真实 Edge 管理最小页面。
4. **R4 daemon 接入**：实现 enrollment、候选 Edge、AgentTicket、探测选择、`AgentGateway.Connect`、Presence 和重连。
5. **R5 客户端直连**：实现 CloudRouteGrant、ClientTicket、信令转发和真实 P2P DataChannel。
6. **R6 Relay 与用量**：在同一 Edge 内实现 TURN、租约、配额、速率、usage outbox 和幂等结算。
7. **R7 账号、交易与完整后台**：实现套餐/订单/订阅/Entitlement，并完成中文 App Shell、模块页面和实时控制。
8. **R8 Edge 证书自动更新**：实现双文件证书档案、Edge 绑定、原子热加载、离线重连收敛和 secret 安全门禁；程序升级另行排期。
9. **R9 全产品 E2E 与发布**：完成故障、负载、移动端、部署、安全和开源许可证验收后上线。

不得跨切片恢复旧协议作为 fallback。某一切片需要变更本文边界时，先更新本文并说明现实链路和验收依据。

## 16. 测试与验收基线

### 16.1 控制面

- 证明每个 Edge 只有一条 Controller 控制流，Controller 不直接持有 daemon 长连接。
- 10 万条 daemon/会话内存目录的并发增删查、generation fence 和竞态测试通过。
- Controller 重启后数据库没有实时 topology，Edge 全量上报可以完整重建目录。
- Edge 断线后对应 generation 的对象按明确 grace/TTL 消失，不出现陈旧在线数据。
- 上报序号缺口会触发全量重同步。

### 16.2 连接与数据面

- daemon 首次注册、候选探测、选择、重连和跨 Edge 重新选择可复现。
- TUI、CLI 和 Android 至少各有一条真实 Cloud Route 用户流程。
- P2P 成功时业务数据不经过 Edge；P2P 失败时 TURN UDP/TCP 路径可用。
- CapabilityGrant 只在 daemon 端经 DTLS channel binding 验证。
- 踢掉 Relay 会话与请求 daemon 关闭 P2P 会话分别返回真实结果。

### 16.3 商业和故障

- 未订阅、超并发、超速率、超字节和租约过期均在 Edge 正确拒绝。
- usage 重传不会重复计费，Controller ACK 后本地 outbox 才清理。
- Controller、数据库和 Edge 分别重启时符合第 11 节语义。
- Controller 故障不会中断租约内已有数据面，也不会放行无限新 Relay。

### 16.4 运营后台

- 中文登录后始终可见左侧菜单，点击每个模块显示不同的正确内容。
- 任何正式模块无需手输 URL。
- 模块切换没有全屏 loading，缓存和后台刷新不会显示旧对象为在线。
- Edge 基础信息、当前 daemon、当前客户端连接、证书和容量均可查看。
- 域名/端口、区域、容量和证书档案可编辑；ID、密钥和健康路径不可手填。
- 所有实时控制展示 ACK、超时或目标离线的真实状态。

### 16.5 仓库与发布

- 不存在 `private/`、旧 Cloud Proto、旧品牌、旧 Hub/Relay 双身份或旧 fallback import。
- 源码和生成代码使用同一 Proto；不存在平行业务 DTO。
- secret audit、依赖许可证和选定的开源许可证门禁通过。
- 本地、Direct、SSH、Cloud、P2P、Relay、文件传输、取消、恢复和移动端真实 UI E2E 全部通过。
- 部署产物、源码提交、SHA-256、配置、迁移和线上 smoke test 均有可复现证据。

## 17. 当前不做

在单区域纵向闭环完成前，不做：

- 多 Controller 高可用和分布式在线目录。
- Edge Mesh、多 transit 或全球动态迁移。
- Kubernetes operator、通用插件系统或第二套 Cloud provider。
- 为旧 Hub/Relay 协议保留兼容适配层。
- 浏览器 terminal 产品或新的 WASM 产品入口。
- 无中断跨 Edge 动态换路。
- 为理论扩展提前建立通用 registry、事件总线或复杂计费平台。

## 18. 尚待明确但不阻塞当前删除阶段的事项

- 对外发布采用哪一种开源许可证。
- 运营后台第一版是否把“实时连接”开放给普通用户账号，还是只开放给运营角色。

这些参数选择不得改变本文已经确定的领域 owner、持久化边界、控制链路和失败语义。

## 19. 当前代码的技术处置结论

实现不能从旧 Cloud 上继续叠加。当前代码已经把 Hub、Relay、Controller、Companion、Web Controller 拆成九个独立 Go module，并且旧 `cloudpb` 同时固化了 HubAssignment、CommandOutbox、Hub/Relay 双身份和数据库 topology。它与本文的目标模型相反。

删除阶段按下表处理，不做“目录整体复制后改名”：

| 当前路径 | 处理 | 原因 |
| --- | --- | --- |
| `private/cloud/` | 整体删除，业务规则只允许经测试重新实现 | 多 module、多身份和持久 topology 是旧模型根源 |
| `private/archive/` | 整体删除 | 退出历史，不允许成为 fallback |
| `proto/cloudpb/` | 整体删除，建立全新 `proto/cloud/v1` | 旧 schema 的领域对象和消息方向不可复用 |
| `shared/cloudcompanion/` | 整体删除 | 新架构没有 Companion 进程和 IPC |
| `client/adapter/managed/` | 删除旧 Cloud client 接口，按 `client/adapter/cloud` 重建 | 当前 adapter 依赖 Companion 和旧 cloudpb |
| `remote/daemon/` 中 managed Cloud 文件 | 删除 assignment、Hub Presence、旧 command receipt 和 runtime reporter，再接新 AgentGateway | daemon 不再绑定持久 HubAssignment |
| `remote/webrtc/` | 保留 Pion、DataChannel 和 DTLS primitive，移除对 cloudpb 的依赖 | 这是 Direct/SSH/Cloud 共用的网络 primitive |
| `client/runtime/`、`client/endpoint/` | 保留 | generation、Route 选择和 `ReadyPeerSession` 是正确客户端真值 |
| `client/adapter/direct/`、`client/adapter/ssh/` | 保留并做回归测试 | Cloud 删除不能破坏免费连接路径 |
| `core/`、`api_layer/`、`api_mapping/`、`internal/protocol/`、`tui/` | 保留 | terminal 和应用协议不属于 Cloud 重写范围 |
| `clients/ui/`、`clients/mobile/` | 保留产品壳和无关功能，删除旧 Cloud 页面/API 后重接 | 不能丢失正在进行的用户界面改动 |
| `go.work` | 删除旧九个 module，只保留根 module | 新 Cloud 与客户端在同一个开源 module 中构建 |

当前工作树里已经存在非本次架构工作的 UI 改动。进入代码删除前必须先逐项确认来源，并把需要保留的改动形成独立提交；不得用递归删除把未提交用户工作一起清掉。

`../tgent` 只借鉴以下经过验证的形态：daemon 主动连接 Edge、单条双向流、Edge 内存 agent registry、curl 安装。以下做法明确不复用：脚本硬编码生产密钥、Hub 给 daemon 临时分配身份、使用 HTTP 批量重试 Presence、把终端 REST 请求封装成通用代理、无边界地积压失败事件。

## 20. 目标工程结构与依赖方向

### 20.1 单 module 目录

```text
cloud/
  controller/
    account/             # 账号、session、RBAC
    commerce/            # 套餐、订单、支付、订阅、Entitlement
    enrollment/          # daemon 注册、DeviceIdentity 和 Edge 候选
    edgeconfig/          # Edge desired config、claim 和版本
    certificate/         # 证书档案、绑定与当前 secret
    directory/           # Controller 纯内存在线目录 actor
    control/             # EdgeControl server、命令 correlation
    usage/               # Relay lease、用量幂等结算
    apihttp/              # 浏览器 JSON/SSE adapter
    apigrpc/              # 公共 unary gRPC 和 EdgeControl adapter
    postgres/             # 上述持久 store 的 PostgreSQL adapter
    runtime/              # Controller composition，不拥有领域状态
  edge/
    runtime/              # Edge 纯内存 runtime actor
    controllerlink/       # 主动连接 Controller 和全量同步
    agentgateway/         # daemon 双向流
    clientgateway/        # 客户端信令双向流
    signaling/            # offer/answer/candidate correlation
    relay/                # 同进程 STUN/TURN 数据面
    policy/               # 票据、租约、并发和速率执行
    usage/                # 累计计数和唯一磁盘 outbox
    certificate/          # 本机 applied 状态与证书原子切换
    runtimeconfig/        # 受签名 desired config
  daemon/                 # daemon enrollment、AgentGateway client
  client/                 # Controller directory/ticket 和 Edge signaling client
  testkit/                # 仅测试使用的真实进程/网络装配

proto/cloud/v1/
  common.proto
  ticket.proto
  runtime.proto
  edge_control.proto
  agent_gateway.proto
  client_gateway.proto
  public_api.proto
  operator_api.proto
  commerce.proto
  certificate.proto
  usage.proto

cmd/muxvia-cloud-controller/
cmd/muxvia-cloud-edge/
```

### 20.2 依赖规则

- 两个 `cmd` 目录只解析配置、装配依赖、启动 listener 和执行优雅关闭。
- Controller 各领域 package 定义自己的 Store port；`controller/postgres` 实现这些 port。领域 package 不 import PostgreSQL、HTTP、gRPC 或 React 概念。
- `controller/apihttp` 和 `controller/apigrpc` 只做认证上下文、Proto decode/encode、错误映射和调用编排，不持有业务 map。
- `controller/directory` 是在线状态唯一 owner；其它 Controller package 只能通过其命令/查询 API 使用实时状态。
- `edge/runtime` 是 Edge 在线状态唯一 owner；gateway、relay、usage 和 controllerlink 只能向它提交事件或读取不可变投影。
- `cloud/daemon` 把新 AgentGateway 接到 `remote/webrtc` 和 daemon session owner，不进入 core terminal 领域。
- `cloud/client` 负责 Controller/Edge Cloud 网络协议；`client/adapter/cloud` 负责把它组装成现有 `client/runtime.PeerConnector`。
- `remote/webrtc` 和 `client/port` 改用 Cloud 无关的 `ICEConfig`、`RelayPolicy` 和 SDP primitive，不能 import `proto/cloud/v1`。
- Proto 类型只出现在 transport/application 边界。内部领域类型只在确实承载不变量时存在，不允许逐字段复制一套 API DTO。
- 浏览器 TypeScript 使用同一 Proto 生成类型；UI-only view model 可以存在，但不能成为账号、Edge、连接或用量真值。
- `core/`、`remote/webrtc`、`client/runtime` 不得反向 import `cloud/controller` 或 `cloud/edge`。

### 20.3 技术选型

- RPC：`google.golang.org/grpc` 与 `protoc-gen-go-grpc`，在 R1 固定版本并进入根 `go.sum`。
- Schema：Protocol Buffers v3，Go 与 TypeScript 都从 `proto/cloud/v1` 生成。
- Web：标准 HTTPS JSON + SSE，JSON 使用 `protojson`，不再手写镜像 TypeScript DTO。
- 数据库：PostgreSQL，Go adapter 使用 `pgx/v5`；不引入 ORM。
- WebRTC/STUN/TURN：复用当前 Pion v4 primitive，Edge Relay 使用 Pion TURN v4。
- Edge usage outbox：`go.etcd.io/bbolt` 单文件事务 KV，只保存未确认 `UsageEvent`。
- 运营后台路由与查询缓存：React Router + TanStack Query，在 R7 固定版本。
- 日志：Go `log/slog` 结构化 JSON；请求、连接和会话统一携带 correlation ID。
- 时间：跨边界使用 `google.protobuf.Timestamp`/`Duration`，内部一律 UTC；新 Proto 不再使用散落的 Unix millis 字段。
- ID：持久实体使用 Controller 生成的 UUID；实时 `boot_id`、`connection_id` 和 `session_id` 使用加密安全随机值，不能由数据库自增号推断。

## 21. 进程装配与监听边界

### 21.1 Controller 进程

Controller 进程内只装配一份每类服务：

```text
PostgreSQL stores
      |
account / commerce / enrollment / edgeconfig / certificate / usage
      |                                      |
HTTP JSON/SSE API                       EdgeControl gRPC
      |                                      |
Web 静态资源                         in-memory Directory
```

生产部署使用同一个 Controller 公网域名和 TCP 443。进程内部可以把 HTTP 与 gRPC 监听分开，由同一受控入口按 ALPN/content-type 转发，但对 Edge、daemon 和客户端只公布一个受 TLS 保护的 origin。另开 loopback-only 健康与指标 listener，不对公网提供管理能力。

启动顺序固定为：读取并验证配置 -> 打开 PostgreSQL -> 执行显式 migration gate -> 加载签名 key set -> 创建领域服务 -> 创建空 Directory -> 启动 gRPC/HTTP listener -> 标记 ready。数据库不可用时 Controller 不进入 ready。

关闭顺序固定为：停止接收新 HTTP/RPC -> 通知 Edge drain 控制流 -> 等待有界中的请求 -> 关闭 Directory watcher -> flush 已提交审计 -> 关闭数据库。Controller 关闭不等待 Edge 的业务 DataChannel。

### 21.2 Edge 进程

Edge 一个进程装配：

```text
ControllerLink ----+
AgentGateway ------+--> Runtime actor --> Signaling coordinator
ClientGateway -----+         |                  |
                            Policy            Pion peer signaling
                              |
                         Pion STUN/TURN --> Usage counters --> durable usage outbox
```

Edge 对外主 TLS 地址由运营人员填写的域名/端口推导，承载 AgentGateway、ClientGateway、gRPC health 和固定 `/healthz`。STUN/TURN 监听端口由版本化 Edge 配置生成，首发默认 `3478/udp`、`3478/tcp` 和需要时的 `5349/tcp`，管理页面只展示。

启动顺序固定为：读取 bootstrap config -> 加载/生成 Edge 私钥 -> 加载最后一个有效证书 -> 打开 usage outbox -> 创建空 Runtime -> 启动公网 listener -> 主动连接 Controller -> 验证 desired config -> 完成 snapshot -> 标记 cloud-ready。Controller 暂时不可达时可以启动进程和健康检查，但不能接受需要新票据或新租约的会话。

关闭顺序固定为：进入 drain -> 拒绝新 agent/client/relay -> 向 Controller 汇报 draining -> 给现有信令和控制请求有界完成时间 -> flush usage outbox -> 关闭 TURN allocation 与 gateway stream -> 关闭 ControllerLink。

### 21.3 不存在的第三个进程

新架构没有 Cloud Companion、独立 Hub、独立 Relay、独立 Web Controller 或 daemon-side Cloud helper。Android 通过 Go binding 使用 `cloud/client`，TUI/CLI 直接使用同一 Go package；Web 管理后台由 Controller 提供。

## 22. Proto 与服务契约

### 22.1 Schema 规则

Proto package 固定为 `muxvia.cloud.v1`，Go package 固定为 `github.com/muxvia/muxvia/proto/cloud/v1;cloudv1`。旧 `proto/cloudpb` 尚未公开发布，直接删除，不保留兼容字段或 adapter；新 v1 从本文重新定义。

每个双向流 envelope 至少包含：

```proto
uint32 protocol_version = 1;
string message_id = 2;
string sender_id = 3;
string boot_id = 4;
string connection_id = 5;
uint64 stream_seq = 6;
google.protobuf.Timestamp sent_at = 7;
oneof payload { ... }
```

所有命令必须另带 `command_id`、`correlation_id`、`deadline` 和精确 target generation。所有结果必须带稳定 result code、是否重试、错误 detail 和实际完成时间。

### 22.2 EdgeControl

```proto
service EdgeControl {
  rpc Connect(stream EdgeEvent) returns (stream ControllerCommand);
}
```

`EdgeEvent` payload：

- `EdgeHello`：Edge 身份、boot ID、版本、能力、desired config version、证书 applied version。
- `SnapshotBegin/Chunk/End`：当前 Runtime 的分块全量快照及 digest。
- `RuntimeDelta`：agent/session/allocation 的单调 revision 增量。
- `EdgeHeartbeat`：负载、队列水位、流量速率和最后 runtime revision。
- `CommandResult`：实时命令的精确执行结果。
- `RelayLeaseRequest`：为已接受会话申请短期 Relay 配额。
- `UsageBatch`：从 durable outbox 读取的幂等用量批次。
- `ConfigApplied`、`CertificateApplied`：节点应用结果，不作为 desired truth。

`ControllerCommand` payload：

- `EdgeWelcome`：Controller connection ID、接受的协议版本、签名公钥集和 heartbeat 参数。
- `DesiredConfig`：完整、版本化、带签名的 Edge 配置。
- `ResyncRequired`：序号缺口或 digest 不一致，要求新快照。
- `CloseClientSession`、`CloseDaemonConnection`：有 deadline 的实时命令。
- `RelayLeaseDecision`：grant/deny 和短期限制。
- `UsageAck`：只确认已提交数据库的 event ID。
- `CertificateRelease`、`ReleaseDrain`：证书或二进制发布控制。

### 22.3 AgentGateway

```proto
service AgentGateway {
  rpc Connect(stream AgentEvent) returns (stream EdgeCommand);
}
```

daemon 第一条消息必须是 `AgentHello`，携带 AgentTicket、DeviceIdentity proof、daemon boot ID、版本和非秘密元数据。Edge 验证完成后返回 `AgentReady`，其中包含 connection ID、heartbeat 参数和基础 ICE endpoint。

后续 `AgentEvent` 只允许 heartbeat、signal answer/candidate、session lifecycle 摘要和 daemon command result。`EdgeCommand` 只允许 signal offer/candidate、关闭精确 PeerSession、刷新短期策略和结束当前 connection。禁止恢复通用 HTTP method/path/body 代理。

### 22.4 ClientGateway

```proto
service ClientGateway {
  rpc Connect(stream ClientSignal) returns (stream EdgeSignal);
}
```

客户端第一条消息必须是 `ClientHello`，携带 ClientTicket、ClientAccessIdentity proof、客户端产品类型、版本和当前 attempt generation。Edge 返回 `ClientReady` 后，客户端才用返回的 session-specific ICE/TURN material 创建 offer。

后续 payload 只允许 offer、answer、ICE candidate、信令接受/拒绝、路径建立摘要和关闭。信令流不能承载 terminal command、文件内容或 CapabilityGrant。

### 22.5 Controller 公共与运营 API

Controller unary gRPC 至少分为：

- `AccountService`：注册、登录、刷新、登出、当前账号。
- `EnrollmentService`：daemon enrollment、DeviceIdentity challenge、Edge 候选和 AgentTicket。
- `DirectoryService`：验证 CloudRouteGrant、解析 daemon、签发 ClientTicket。
- `CommerceService`：套餐、订单、订阅、Entitlement 和账号用量。
- `OperatorService`：Edge 配置、证书、账号、交易、实时目录和控制命令。
- `InstallService`：claim token 交换、artifact manifest 和 Edge CSR 注册。

浏览器 JSON 路由使用同一组 generated request/response，通过 `protojson` 映射；SSE data 使用 generated event 的 Proto JSON。HTTP adapter 不定义第二套 JSON struct。

## 23. 身份、票据和授权

### 23.1 密钥层次

- Controller TicketSigner：Ed25519，私钥来自 KMS 或权限受控文件；Edge 只持有带 `key_id` 的公开验证 key set。
- EdgeIdentity：Edge 首次安装本地生成，私钥不离开机器；注册后由 Controller CA 签发 mTLS 证书。
- DeviceIdentity：daemon 本地生成并持久化，Controller 只保存公钥和归属。
- ClientAccessIdentity：客户端 secure store 持有，Controller/Edge 只看到公钥指纹和请求 proof。
- Web 账号 session：独立于上述设备身份，不允许用浏览器 cookie 代替 daemon/client 签名。

票据统一使用 `SignedEnvelope { key_id, payload_bytes, signature }`。签名输入是 domain separation 常量加确定性序列化的 payload，不对 JSON、拼接字符串或包含 signature 自身的 message 签名。

### 23.2 AgentTicket

daemon 完成 enrollment 后，不保存长期 bearer token。它每次向 Controller 请求 AgentTicket 时对 Controller nonce、daemon ID、目标 Edge、请求时间和随机 request ID 签名。Controller 用数据库中的 DeviceIdentity 公钥验证，再签发只绑定该 daemon、Edge、账号状态、能力和期限的 AgentTicket。

首发默认 AgentTicket 有效期 10 分钟，Edge 允许 30 秒时钟偏差。票据过期只阻止新建/重建 AgentGateway，不主动切断已经通过 heartbeat 保持的当前连接；持续禁用通过 desired policy push 和短 lease 收敛。

### 23.3 CloudRouteGrant 与 ClientTicket

账号外客户端不能把 terminal CapabilityGrant 发给 Controller。为解决 Cloud 发现和信令准入，daemon 在配对时额外签发一个 **CloudRouteGrant**：

- 由 DeviceIdentity 签名。
- 只包含 daemon ID、ClientAccessIdentity 公钥、产品类型、随机 grant ID、签发时间和过期时间。
- 只授权“查询该 daemon 并尝试建立信令”，不包含 terminal ID、scope、命令或文件权限。
- 由 daemon 和客户端保存，Controller 不持久化。

客户端请求解析时同时提交 CloudRouteGrant 和 ClientAccessIdentity 对本次 nonce 的签名。Controller 用持久化的 daemon 公钥验证 grant，用 grant 内客户端公钥验证请求，再检查 daemon owner 的订阅和实时 Presence，签发有效期 2 分钟、绑定目标 Edge/daemon/client/产品类型/route policy 的 ClientTicket。

CloudRouteGrant 被本地撤销后，旧 grant 在过期前理论上仍能发起少量信令，所以 Edge 必须先让 daemon 根据本地 AccessStore 接受客户端身份，之后才申请 Relay lease。被撤销客户端拿不到 terminal DataChannel 授权，也不能在 daemon 拒绝后消耗 Relay 配额。CloudRouteGrant 首发默认最长 7 天，客户端在已授权 DataChannel 内滚动更新。

### 23.4 RelayLease

RelayLease 由 Controller 签名，绑定 account、Edge、daemon、client、managed session、最大字节、最大速率、有效期和唯一 lease ID。首发默认 5 分钟，可在原会话内续租。Edge 离线验证并执行，不能扩大限制。

### 23.5 初始运行参数

以下是单区域首发默认值，均通过版本化配置调整，不写死在业务分支中：

| 参数 | 默认值 | 失败语义 |
| --- | --- | --- |
| Edge heartbeat | 10 秒 | 连续 30 秒无有效消息进入断线判断 |
| daemon heartbeat | 15 秒 | 连续 45 秒无有效消息关闭 Agent connection |
| Controller Edge grace | 10 秒 | grace 后整体删除该 connection generation |
| AgentTicket | 10 分钟 | 过期不能新建连接 |
| ClientTicket | 2 分钟 | 过期不能新建信令 session |
| CloudRouteGrant | 最长 7 天 | 过期必须经已授权 daemon 更新 |
| RelayLease | 5 分钟 | 未续租时停止继续转发并结算累计值 |
| 实时控制命令 | 10 秒 | 超时返回 unknown/timeout，不持久重试 |
| SSE runtime event | 30 秒 keepalive | 客户端断线后重新拉当前 projection |

## 24. 实时状态、并发与背压

### 24.1 Controller Directory actor

Controller 使用一个单写者 `Directory` actor 持有：

```text
edges[edge_id] -> EdgeAttachment
daemons[daemon_id] -> DaemonLocation
sessions[session_id] -> SessionLocation
accounts[account_id] -> RuntimeIndex
pending[correlation_id] -> bounded waiter
```

所有 Edge 事件、控制请求和 TTL 都进入一个有界 mailbox，由 actor 顺序修改上述 map，保证“替换 Edge generation、更新正向索引、更新反向索引、发布 SSE”是同一个内存事务。网络发送和数据库调用不得在 actor 内执行；actor 只产生 outbound intent，由独立 writer/worker 执行并回报结果。

查询通过 actor 的 bounded request/reply 获取分页不可变 projection。首发单 Controller、10 万条目录不引入分片和分布式 cache；只有基准测试证明 actor 达不到准入吞吐时，才能在不改变真值的前提下分片。

### 24.2 Edge Runtime actor

每个 Edge 只有一个 `Runtime` actor 持有：

```text
agents[daemon_id] -> AgentConnection
clients[session_id] -> ClientSignalingSession
peers[session_id] -> PeerSummary
allocations[allocation_id] -> RelayAllocation
account_counters[account_id] -> PolicyCounters
pending[correlation_id] -> bounded waiter
runtime_revision -> uint64
```

AgentGateway、ClientGateway、TURN callback 和 ControllerLink 都只提交 typed event。每次被管理对象变化都在 actor 内递增 `runtime_revision`，生成一个可重放到当前控制流的增量，但不落盘。

### 24.3 连接写入规则

- 每条 gRPC stream 只能有一个 reader goroutine 和一个 writer goroutine。
- 多个业务组件不能并发调用同一个 stream 的 `Send`；必须进入该连接的有界 writer queue。
- 控制消息队列满时不静默丢弃：关闭该连接并让 generation 重建。
- heartbeat 和瞬时指标允许 coalesce 为最新值，不能无限排队。
- usage 先进入 durable outbox，再由 pump 发送；它不与 Presence 共用内存重试队列。
- actor、registry mutex 或数据库事务期间禁止执行网络 I/O。

### 24.4 全量快照与增量无缝衔接

Edge 建立新 Controller stream 后，Runtime actor 在 revision `R` 生成一致性快照句柄。ControllerLink 分块发送 `SnapshotBegin(R)`、若干 `SnapshotChunk` 和 `SnapshotEnd(R,digest)`；`R` 之后的增量暂存在该控制流的有界内存队列，快照结束后按 revision 发送。

Controller 在临时 namespace 中组装并校验快照，只有收到合法 `SnapshotEnd` 才原子替换该 Edge connection generation。快照中断、digest 不符或增量不连续时丢弃临时 namespace 并请求新快照，不展示半份数据。

若快照期间增量队列溢出，Edge 主动中止当前快照并从更新的 revision 重来；不把事件写盘，也不继续发送已知有缺口的流。

### 24.5 generation 和冲突规则

- Edge 每次进程启动生成新 `boot_id`，每次 Controller 重连生成新 `connection_id`。
- daemon 每次进程启动生成 daemon boot ID，每次 AgentGateway 重连生成 agent connection ID。
- 同一 daemon 同时出现在多个 Edge 时，Controller 比较自己签发的 AgentTicket `issued_at` 和 ticket ID，较新的 attachment 获胜；相同值按 Edge ID 稳定选择，并 best-effort 关闭 loser。
- 所有迟到事件必须同时匹配 owner ID、boot ID、connection ID 和对象 generation；只匹配 daemon/session ID 不足以修改当前状态。
- generation 只存在内存和短期票据，不建立数据库 epoch 表。

### 24.6 状态机

```text
EdgeControl: Disconnected -> Dialing -> Authenticating -> Synchronizing -> Ready -> Backoff
Agent:       Connecting -> Authenticating -> Ready -> Draining -> Closed
Signaling:   Created -> AgentAccepted -> Negotiating -> Established -> Closed
Relay:       Requested -> Leased -> Active -> Settling -> Closed
Command:     Created -> Sent -> Acknowledged | Rejected | TimedOut
```

非法跳转返回协议错误并关闭最小范围的连接，不通过修改 map 或补一条 fallback 强行恢复。

## 25. PostgreSQL 模型与事务边界

### 25.1 表分组

新 migration 从空 schema 开始，不迁移旧 topology。目标表至少包括：

| 分组 | 表 |
| --- | --- |
| 账号 | `accounts`、`account_sessions`、`account_roles` |
| daemon | `daemons`、`daemon_enrollment_tokens`、`daemon_revocations` |
| Edge | `edge_deployments`、`edge_claim_tokens`、`edge_config_versions` |
| 证书 | `certificate_profiles`、`edge_certificate_bindings` |
| 套餐 | `plan_catalog_versions`、`plans`、`plan_prices`、`entitlement_overrides` |
| 交易 | `orders`、`payment_attempts`、`payment_events`、`subscriptions`、`subscription_adjustments` |
| 用量 | `usage_periods`、`relay_usage_events`、`relay_usage_aggregates` |
| 审计 | `operator_audit_events` |

禁止出现 `hub_assignments`、`presence_topology`、`managed_peer_topology`、`connection_snapshots`、`runtime_heads`、`management_commands` 或 `command_outbox`。

### 25.2 关键约束

- 所有 tenant-owned 表必须显式包含 `account_id` 外键，查询不能依赖前端传入的账号范围。
- `daemons.device_public_key` 和规范化 fingerprint 唯一；撤销不删除历史审计。
- claim/enrollment token 由至少 192 bit 随机值产生，数据库只保存 SHA-256 摘要、过期时间、消费时间和目标 ID。
- `payment_events(provider, provider_event_id)` 唯一，支付状态机在单事务中幂等推进。
- `relay_usage_events(edge_id, event_id)` 唯一；重复 batch 返回相同 ACK，不重复增加 aggregate。
- certificate private key 和证书 PEM 不进入普通表。表中只保存不可猜测的 secret reference、指纹、DNS SAN、有效期和当前 revision；本机 secret 目录与文件权限分别为 `0700`、`0600`。
- 所有 mutable aggregate 使用显式 revision 做 optimistic concurrency；API 冲突返回 `ABORTED`/HTTP 409，不做 last-write-wins。

### 25.3 事务规则

- 创建订单、接收支付事件、推进订阅和重算 Entitlement 必须在同一个数据库事务内，外部支付 API 调用不能放在事务中。
- daemon enrollment 的 token 消费与 daemon identity 创建在同一事务中。
- Edge claim token 消费与 EdgeIdentity/CSR 绑定在同一事务中。
- usage event 插入与周期 aggregate 增量在同一事务中，提交后才发 ACK。
- 持久状态提交后再做 Edge 在线推送；推送失败不回滚已经成立的账号/撤销/配置事实。
- 审计记录与对应运营 mutation 在同一事务中写入，读取实时目录本身不写审计。

### 25.4 Migration 纪律

Migration 位于 `cloud/controller/postgres/migrations`，使用单调版本和 checksum。生产启动只验证 schema 已到目标版本，不默认自动执行破坏性 migration；部署流水线先执行独立 migrate 命令，再滚动启动 Controller。

开发阶段允许重置空数据库。旧 Cloud schema 不写兼容 migration，直接删除并重新初始化。

## 26. daemon 与客户端接线

### 26.1 daemon Cloud runtime

`cloud/daemon` 是 daemon 进程内唯一 Cloud owner，但它不拥有 terminal：

```text
DeviceIdentity + local enrollment record
              |
       Controller EnrollmentService
              |
     candidates + AgentTicket
              |
        AgentGateway stream
              |
 offer/candidate/close command
              |
       remote/webrtc.Answerer
              |
 DTLS DataChannel -> remote auth -> internal/protocol -> api_layer -> core
```

daemon 本地只持久化 `daemon_id`、Controller origin、DeviceIdentity private key reference 和 enrollment 状态。它不持久化 `edge_id`、AgentTicket、Presence、当前 session 或 Edge 候选。每次启动和 Edge 故障时重新解析。

AgentGateway 收到 offer 后，先用本地 AccessStore 检查 CloudRouteGrant 中的 ClientAccessIdentity 是否仍允许尝试，再创建 Pion answer。真正的 CapabilityGrant、scope 和 terminal authorization 仍在 DataChannel 内验证。预检查失败只返回稳定拒绝码，不透露本地 terminal inventory。

新 daemon runtime 替换当前 `remote/daemon/agent.go`、`managed_runtime.go` 和 assignment-based registry。可复用的 DataChannel session close primitive 下沉为 Cloud 无关接口：

```go
type PeerSessionOwner interface {
    CloseExact(ctx context.Context, sessionID string, generation uint64) CloseResult
}
```

Cloud command 只能调用该接口，不能直接操作 core terminal map。

### 26.2 客户端 Cloud adapter

现有 `client/runtime.SessionOwner` 继续拥有 Endpoint generation 和唯一 ReadyPeerSession。Cloud 只实现一个 route adapter：

```text
client/runtime AttemptRequest
        |
client/adapter/cloud Connector
        |
cloud/client ControllerClient + EdgeClient
        |
resolve -> ClientTicket -> ClientGateway -> WebRTC
        |
DTLS-bound remote auth -> protocol Hello
        |
client/runtime ReadyPeerSession
```

Endpoint registry 中 Cloud Route 只保存：

- 目标 daemon 的稳定身份 pin。
- CloudRouteGrant 的 secure credential reference。
- route preference，例如 auto、direct-only、relay-only。
- 可选的 Controller profile reference。

不能保存当前 Edge 地址、ClientTicket、TURN credential、session ID 或上次 connection generation。Edge 地址每次从 Controller 内存目录解析，票据和 TURN credential 只存在于当前 attempt。

Cloud adapter 不能自行与 Direct/SSH 竞速，也不能失败后偷偷切换 route；仍由现有 planner 和 `SessionOwner` 决定 race/fallback。Cloud attempt 成功的条件保持不变：daemon identity、CapabilityGrant authorization 和 protocol Hello 全部完成后才能返回 `ReadyPeerSession`。

### 26.3 平台 binding

- CLI/TUI 直接装配 Go `cloud/client`。
- Android 仍通过窄 C ABI/JNI 提交 Proto command 和接收 Proto event；Kotlin/TypeScript 不建立 Cloud socket。
- ClientAccessIdentity 和 CloudRouteGrant 存在平台 secure store，JavaScript 只持 opaque credential reference。
- Android 进程/Activity generation 变化时，旧 ClientGateway、PeerConnection、DataChannel 和 resource handle 全部失效。
- 当前不实现浏览器 terminal，因此不为 Cloud Route 增加 WASM 或浏览器 RTCPeerConnection 分支。

## 27. Edge 内部技术设计

### 27.1 Agent registry

`agentgateway` 完成 TLS、AgentTicket、DeviceIdentity proof 和版本校验后，向 Runtime actor 提交 `AttachAgent`。Runtime 原子替换同 daemon 的旧 connection，返回新的 agent connection generation；旧 writer 收到关闭信号后退出，迟到消息因 generation 不匹配被拒绝。

AgentConnection 只持有：daemon/account ID、boot/connection ID、客户端产品无关元数据、连接时间、最后心跳、writer handle 和当前 peer session ID 集合。它不持有 terminal 列表、CapabilityGrant 或账号商业 aggregate。

### 27.2 信令事务

一个 managed session 固定绑定同一个 Edge、daemon connection generation 和 client connection generation：

1. ClientHello 验证成功，Runtime 创建 `SignalingSession(Created)`。
2. Edge 把客户端身份摘要和 session metadata 发给 daemon。
3. daemon 本地预检查后返回 accept/deny。
4. accept 后 Edge 生成 session-specific ICE material；需要 Relay 时先申请 RelayLease。
5. 客户端生成 offer，经 Edge 转发 daemon；daemon 返回 answer/candidate。
6. 双方报告 selected path 后，Runtime 转为 `Established`，Controller 只看到摘要。
7. 信令完成后可以关闭 gRPC signaling stream；PeerSession 生命周期由 daemon/client WebRTC owner 和 Edge runtime 摘要共同观察。

任何步骤的超时、generation 替换或 actor 拒绝都关闭当前 signaling session。SDP、ICE candidate 和 TURN credential 不进入日志、数据库、Controller snapshot 或运营 API。

### 27.3 STUN/TURN

- STUN 和 TURN 与 gateway 共享同一 EdgeIdentity 和 runtime，不再有 Relay 节点注册。
- TURN authentication callback 只接受当前 Runtime 已创建、未过期且绑定 RelayLease 的临时 credential。
- credential 由 Edge 使用进程内随机 secret 对 session/lease/expiration 做 HMAC 派生，Edge 重启后自然失效。
- allocation 创建和关闭必须回到 Runtime actor 更新并发计数和 session path。
- rate limit 在 TURN 数据写入边界执行，按 lease/account/session 三层取最严格限制。
- Relay 数据只按字节转发，不进入 terminal protocol parser，也不写 payload 日志。

### 27.4 Usage outbox

Edge 唯一持久 runtime 组件是 `usage.Outbox`。首发使用 `go.etcd.io/bbolt` 单文件事务 KV，key 为随机 `usage_event_id`，value 为 versioned `UsageEvent` protobuf bytes；版本在 R6 固定到根依赖。

写入规则：

1. Relay allocation 关闭或租约窗口结束时，Runtime 冻结单调累计计数。
2. Outbox 事务提交 usage event。
3. pump 批量读取并经 EdgeControl 发送。
4. Controller 幂等提交 PostgreSQL 后返回精确 event ID ACK。
5. Outbox 在单事务中删除已 ACK key。

磁盘损坏时 Edge 进入 `usage_degraded`，停止创建新 Relay allocation，但仍允许 P2P 和有界关闭现有 allocation；不能删除 outbox 后继续收费服务。Presence、信令和命令禁止写入该 KV。

### 27.5 配置与证书热更新

`DesiredConfig` 包含单调 config version、适用 `edge_id`、监听入口、容量、policy 上限、Controller ticket key set 和生效时间，并由 Controller ConfigSigner 签名。证书使用独立的 `EdgeCertificateBundle`，不塞入配置签名或通用发布协议。

Edge 先验证签名和 target，再在内存构造 candidate config。可以热更新的限额和 key set 原子替换；需要重启的监听变化返回 `restart_required`，由人工部署流程处理。不得把半应用配置标记为 applied。

证书 loader 通过原子指针向 TLS handshake 提供当前 certificate。运营人员只上传匹配的 `fullchain.pem` 与 `privkey.pem`；Controller 校验证书链、私钥匹配、DNS SAN 和当前有效期，将 PEM 放入仅 Controller 服务用户可访问的 `0700` secret 目录，并仅通过目标 Edge 的 mTLS `EdgeControl` 下发。Edge 再次校验后把单一 protobuf 状态文件以 `0600` 原子写入、fsync、rename，再切换指针并上报 applied revision。失败保留旧文件、旧指针和旧 applied revision；这是失败保护，不形成历史版本或回滚产品。

## 28. Controller 应用层技术设计

### 28.1 Edge 选择

`enrollment` 从 Directory 读取在线 Edge projection，再与 `edgeconfig` 的 desired state 合并。首发选择算法只考虑：

1. Edge 已 ready、未 drain、版本兼容、证书有效。
2. 区域与客户端/daemon 明确区域偏好匹配。
3. `current_agents / agent_capacity` 和 Relay 当前负载。
4. 稳定 hash 作为同分 tie-break，避免每次请求随机抖动。

Controller 返回最多三个候选，daemon 自己做网络探测并选择。选择结果不写数据库。当前阶段不做跨区域动态迁移或复杂质量学习。

### 28.2 Entitlement 计算

`commerce` 是套餐、订单、订阅和 Entitlement owner。运行时准入只消费一个不可变 `EffectiveEntitlement`：

```text
active plan version
  + subscription state
  + bounded operator override
  + current usage period
  = EffectiveEntitlement
```

Controller 在签发 AgentTicket、ClientTicket 和 RelayLease 时计算并冻结必要限制。Edge 不读取订单表，也不理解支付 provider 状态；它只验证签名并执行票据里的限制。

Entitlement 不是永久缓存。Controller 可以在进程内做短 TTL read-through cache，但数据库 mutation 提交后必须按 account ID 失效。缓存缺失或数据库故障时拒绝新收费能力，不能使用硬编码免费额度 fallback。

### 28.3 实时控制

Operator API 创建实时命令时不写数据库 command 表：

1. API 完成账号/RBAC/recent-auth 校验。
2. Directory 在当前 generation 创建有界 correlation waiter。
3. outbound writer 向 owning Edge 发送 command。
4. Edge/daemon 返回结果，Directory 完成 waiter。
5. API 返回 applied/rejected/stale/timeout/unavailable。
6. 对这次操作另写持久审计事实，审计只记录目标摘要和结果，不用于重放。

HTTP 请求取消时撤销 waiter，但已经发送的命令可以完成并进入审计。页面重试必须使用新的 command ID，不假设前一次没有执行。

### 28.4 SSE 投影

SSE 是提示 UI 更新的实时投影，不是可靠事件日志。每个事件带 `controller_instance_id`、`event_seq`、resource kind、resource ID 和 operation。

- watcher mailbox 有界；慢消费者断开并重新拉 snapshot。
- Controller 重启后 instance ID 改变，浏览器丢弃旧 cursor 并重新查询当前列表。
- SSE 事件不能携带 SDP、ICE、票据、密钥或大对象。
- UI 收到事件后更新对应 query cache 或执行局部 refetch，不重新挂载 App Shell。

## 29. 运营后台前端架构

### 29.1 路由

后台使用成熟的客户端路由和查询缓存库，在 R7 固定版本。目标路由：

```text
/login
/overview
/edges
/edges/:edgeId/overview
/edges/:edgeId/daemons
/edges/:edgeId/connections
/edges/:edgeId/certificates
/edges/:edgeId/settings
/daemons
/connections
/accounts
/accounts/:accountId
/plans
/subscriptions
/orders
/certificates
/usage
/audit
/system
```

登录成功固定 replace 到 `/overview`。受保护路由全部渲染在同一个 `OperatorShell` 中；路由 outlet 只替换右侧内容。侧边栏项从一个静态、带 RBAC filter 的 route manifest 生成，不从后端返回任意导航结构。

### 29.2 数据层

- generated Proto client 是唯一 API client。
- query key 必须包含资源种类、稳定 ID、filter、sort 和 cursor。
- 字典、套餐版本和 Edge desired config 使用较长 stale time；实时列表使用短 stale time 加 SSE 局部更新。
- 路由切换优先展示缓存数据并在后台刷新；只有首次无数据时显示内容区 skeleton。
- mutation 成功后只失效受影响 query；不能清空整个应用 cache。
- 后端 offline 与浏览器网络失败必须区分，不能用空表冒充“没有数据”。

### 29.3 页面边界

- 列表页负责筛选、排序、分页和选择，不嵌套完整编辑表单。
- 创建/编辑使用独立路由或 modal；表单字段直接对应可编辑 desired state。
- Edge runtime 与 Edge config 在视觉和数据源上明确分组，实时字段不可编辑。
- destructive mutation 显示精确对象、影响和二次确认；实时 disconnect 与持久 disable 使用不同文案。
- 所有默认界面和错误文本为中文，英文 locale 作为可选资源，不允许 key fallback 直接出现在页面。

## 30. 安装、升级与部署

### 30.1 Edge 安装命令

运营人员创建 Edge 后，Controller 生成一条只显示一次的命令：

```bash
curl -fsSL https://controller.example.com/install/edge/<one-time-claim> | sudo sh
```

claim 至少 192 bit、10 分钟过期、单次消费。入口日志必须在落盘前把 URL token 段重写为 `[REDACTED]`。下载请求原子消费 claim 并返回只对该 Edge 有效的 bootstrap script；失败后由运营人员显式重新生成，不复活旧 token。

安装脚本只做：

1. 检测 Linux、CPU 架构、systemd 和必要端口。
2. 下载 Controller 返回的固定 artifact manifest。
3. 验证 artifact SHA-256 和 Controller release signature。
4. 安装到 `/opt/muxvia-cloud-edge/releases/<version>/`，原子切换 `current` symlink。
5. 创建 `/var/lib/muxvia-cloud-edge/` 与 `/etc/muxvia-cloud-edge/`，权限为专用系统用户可读。
6. 写入只含 Controller origin 和一次性 bootstrap credential 的初始配置。
7. 安装并启动 `muxvia-cloud-edge.service`。
8. Edge 注册成功后删除 bootstrap credential。

脚本不包含长期 Controller secret、Edge private key、证书 private key、数据库 DSN、手填健康地址或 Hub/Relay 两套参数。

### 30.2 Edge 配置文件

`/etc/muxvia-cloud-edge/config.yaml` 只保存启动所需本机配置：Controller origin、Edge state directory、listener bind override 和日志级别。域名、容量、区域、证书版本、ticket key set 和 policy 来自签名 DesiredConfig 缓存；缓存只能让已注册 Edge启动并连接 Controller，不能自行扩大能力。

EdgeIdentity、证书和 usage outbox 位于 `/var/lib/muxvia-cloud-edge/`，文件 owner 为专用用户，私钥 `0600`。日志进入 journald/stdout，不把 secret 写入环境 dump。

### 30.3 Edge 程序部署边界

- 当前不建设 artifact registry、release channel、rollout、在线更新器、drain 编排或自动回滚平台。
- Edge 二进制继续通过已签名安装产物或人工 SSH/systemd 部署；运营页面只显示 Edge 实际上报的软件版本，不提供升级按钮或目标版本。
- 程序部署与证书自动更新是两个独立边界。证书只经 `EdgeCertificateBundle` 热加载，不能复用成通用文件发布或进程更新协议。
- 后续出现真实批量升级需求时单独建立切片，不在证书领域提前扩展。

### 30.4 Controller 部署

Controller artifact 与 Web 静态资源同版本构建。部署顺序：备份/验证 PostgreSQL -> 执行 migration job -> 启动新 Controller -> 验证 HTTP/gRPC health -> 等待 Edge 全量重建 Directory -> 运行用户/运营 smoke test -> 切流。

在单 Controller 阶段，升级窗口允许短暂无法签发新票据，但必须验证已有 P2P/租约内 Relay 不被 Controller 重启打断。

## 31. 错误、安全与可观测性

### 31.1 错误模型

统一 `CloudErrorDetail` 至少包含：`reason`、`resource_type`、`resource_id`、`retryable`、`retry_after` 和 `correlation_id`。gRPC 使用标准 status code，HTTP 做固定映射：

| 场景 | gRPC | HTTP |
| --- | --- | --- |
| 参数/Proto 非法 | `INVALID_ARGUMENT` | 400 |
| 未认证/票据非法 | `UNAUTHENTICATED` | 401 |
| 无权限/无 Entitlement | `PERMISSION_DENIED` | 403 |
| 实时对象已离线 | `NOT_FOUND` | 404 |
| revision/generation 冲突 | `ABORTED` | 409 |
| 超配额 | `RESOURCE_EXHAUSTED` | 429 |
| deadline | `DEADLINE_EXCEEDED` | 504 |
| Controller/Edge 暂不可用 | `UNAVAILABLE` | 503 |

禁止通过匹配错误字符串决定重试。只有服务端明确标记 retryable 且 operation 尚未创建资源时，客户端才能自动重试。

### 31.2 日志与隐私

结构化日志允许记录：correlation ID、Edge/daemon/session 的脱敏 ID、generation、状态转换、耗时、字节计数和稳定错误码。

绝不记录：密码、session cookie、claim/enrollment token、SignedTicket、CloudRouteGrant、CapabilityGrant、private key、TURN credential、完整 SDP/ICE candidate、terminal/file payload 或支付 provider secret。

客户端 IP 只在安全事件中按部署隐私策略处理，不进入普通 Presence 和运营列表；页面默认展示地区/网络类别等摘要。

### 31.3 指标

Controller 最少暴露：

- Edge connections、snapshot duration/failure、Directory entities。
- ticket issued/denied、operator command result/latency。
- PostgreSQL transaction latency/error、usage duplicate/commit。
- HTTP/gRPC request rate、latency 和 status。

Edge 最少暴露：

- agent/client connections、signaling state、P2P/Relay path。
- actor mailbox/writer queue 水位、generation replacement。
- TURN allocations、bytes、rate-limit rejection。
- usage outbox depth/oldest age、Controller reconnect 和 snapshot retry。
- certificate expiry/applied version、config applied version。

指标 label 禁止使用 account ID、daemon ID、session ID 或任意高基数字段。精确对象排障使用带 correlation ID 的日志。

### 31.4 基础安全门禁

- Controller public API、Edge gateway 和 installer 全部要求 TLS；生产禁止明文 fallback。
- EdgeControl 注册后使用 mTLS，证书 subject/SAN 必须绑定 edge ID。
- Web session cookie 使用 `HttpOnly`、`Secure`、`SameSite`，mutation 具备 CSRF 防护和 recent-auth 门禁。
- 所有公开入口按 IP、账号、设备身份和 claim 类型实施有界 rate limit。
- Proto decode 设置消息大小上限；snapshot 使用分块，不允许单个 100k 对象 message。
- SDP、ICE、压缩数据和上传内容都必须有明确大小、数量和 deadline 上限。
- release、DesiredConfig、ticket 和证书变更都有独立签名域和 key ID，不能复用一个无上下文签名。

## 32. 实现与提交顺序

### 32.1 D0：文档基线（已完成）

当前切片只包含旧 Markdown 删除、notice 改名和本文。准入是仓库自有 Markdown 只有本文、文档差异无格式错误。本切片不部署。

### 32.2 D1：工作树保护（已完成）

1. 列出所有未提交文件及来源。
2. 把需要保留的现有 UI 工作形成独立提交，或者由用户明确决定丢弃。
3. 为删除前最后一个可运行旧 Cloud 提交打本地安全 tag。
4. 记录现有 Direct/SSH 最小回归结果。

未知改动未处理前不得执行 Cloud 递归删除。

### 32.3 D2：旧 Cloud 删除（已完成）

1. 删除第 19 节列出的旧目录、Proto、generated code 和 module。
2. 清理 `go.work`、根依赖、npm scripts、旧数据库 migration、deploy 和测试引用。
3. 删除已经失效的 README/AGENTS/workflow/public-snapshot 文档 guard。
4. 让 UI 明确显示 Cloud 暂不可用，不保留旧 API fallback。
5. 运行根 Go test、UI typecheck/test、Direct/SSH integration。

这个提交的完成条件是“没有旧 Cloud，但非 Cloud 产品仍可运行”，不能部署。

### 32.4 R1：新契约和双进程骨架（已完成）

- 新建 `proto/cloud/v1`、生成门禁和 compatibility fixture。
- 新建两个 `cmd`、配置校验、TLS、health、graceful shutdown。
- 建立真实 `EdgeControl.Connect` Hello/Welcome harness。
- 固定 grpc-go、pgx、Proto generation toolchain。

### 32.4.1 R1D：在线开发环境装配（已完成）

- 当前产品尚未发布，允许把 R1 双进程部署到公网开发服务器进行真实跨机验证；该环境不具备生产或可用 Cloud 产品语义。
- 旧 Controller、Hub、Relay、Edge、Web 和运行状态先进入带时间戳的可恢复归档，再从活动 systemd、Nginx 和 `/opt/muxvia` 路径退出，禁止新旧进程并存或复用旧协议。
- Controller 固定部署到 `155.94.155.192`；首个 Edge 固定部署到 `114.66.58.243`，公网健康入口使用已确认的 `muxvia-cn1.omscd.com:41102`。
- R1D 使用独立 staging Edge CA 和显式 mTLS 文件权限；手工签发只用于当前开发装配，不替代 R3 的 claim、CSR、证书轮换和 curl installer。
- 完成条件是 Linux artifact checksum、systemd active、Controller `/healthz` 与 `/readyz`、Edge 公网 `/healthz` 与 `/readyz`、跨服务器 mTLS `Hello/Welcome`、重启恢复和旧监听端口消失均有真实证据。

2026-07-26 的 R1D 实际部署使用源码提交 `3f7288a5`：Controller 位于 `155.94.155.192:18443`，SHA-256 为 `1cb45c1b788e38690f2253d4cefef457f3ae583ec0d925ddaaac2539bf0ccb50`；Edge 位于 `https://muxvia-cn1.omscd.com:41102`，SHA-256 为 `0dca4b2226f959db46111789fc27f886016a07b5f416a3252d69878790461055`。Controller 故障时 Edge 保持 alive、撤销 ready，Controller 恢复及两个 unit 分别重启后均自动重建 `Hello/Welcome`；活跃控制流存在时 Controller 在 211ms 内正常退出，systemd 结果为 success、退出码为 0。旧活动资产分别归档在两台主机的 `/var/backups/muxvia/r1d-20260726T013831Z`；开发公网证书在 2026-08-07 到期，必须在到期前更换，不得把它视为 R8 证书管理完成。

### 32.5 R2：Edge runtime 与 Controller Directory（已完成）

- 实现两个 actor、generation、writer queue、snapshot/chunk/delta。
- 实现 10 万对象基准、race test、断线整体删除和 Controller restart 重建。
- 此时只使用测试 agent/session event，不提前实现 daemon 连接。

R2 建立了 `cloud/edge/runtime.State` 与 `cloud/controller/directory.Directory` 两个有界单写者 actor；EdgeControl 现在通过唯一 writer queue 发送确定性分块快照、严格连续增量和心跳，Controller 校验完整摘要后才原子发布并返回 `SnapshotAccepted`。本地准入覆盖半快照隔离、revision gap 重同步、generation fence、断线整体清理、Controller 空目录重启重建、并发 race 和 10 万 daemon 快照/查询基准；R2 不包含真实 AgentGateway 或客户端连接。

R2 实现提交为 `0a391012`。2026-07-26 的 R3 在线装配同时复验了 R2 重建语义：Controller 重启后 Directory 不读取 PostgreSQL 拓扑，Edge 自动重连并获得新的 `connection_id`，页面随后恢复为在线。

### 32.6 R3：Edge 配置与安装（已完成）

- 实现 Edge desired config、claim、EdgeIdentity CSR/mTLS 和 curl installer。
- 实现运营 Edge 列表最小页面，能看到真实在线 Edge 和基础信息。
- 在一台干净 Linux VM 上完成真实安装、重启和重连证据。

R3 建立了 PostgreSQL Edge deployment/config version 与两阶段一次性 claim 事务、Ed25519 desired config 签名、Edge 本机双私钥/CSR、Controller CA 签发、固定 artifact SHA-256 + 发布签名校验、原生 HTTPS Proto JSON API 和简体中文 Edge 管理页。真实 PostgreSQL 纵向 harness 已证明创建、列表、编辑、install claim 单次消费、bootstrap claim 单次消费、CSR 绑定、私钥 `0600`、注册后凭据删除和 signed config 在 `SnapshotAccepted` 前应用。

2026-07-26 的最终在线验收使用源码提交 `ad1b2d4dcefea5349b9bc508acf1416b9e1314f2`。Controller 部署在 `155.94.155.192:18443`，原生 HTTPS 管理/安装入口为 `muxvia-controller.omscd.com:18444`，Controller SHA-256 为 `c2c6a8b5ed24aca2e810c364c608031175c4bcdfcd5445aadccac46f3c46a716`；Edge `65f6aade-5560-416e-9765-0d6fb0bacc00` 部署在 `muxvia-cn1.omscd.com:41102`，SHA-256 为 `68baf9b2e801aa02553a2390616dc8480b5692cd2ef4c26e577f44551b55e0ce`。最终安装从空的 `/opt/muxvia-cloud-edge`、`/etc/muxvia-cloud-edge` 和 `/var/lib/muxvia-cloud-edge` 运行态执行页面生成的 curl 命令，artifact 摘要和 Ed25519 发布签名校验成功，systemd `NRestarts=0`，配置目录为 `root:muxvia-edge 0770`，配置和两把私钥均为 `0600`，bootstrap token 自动删除，install claim 重放返回 HTTP `410`。Edge 与 Controller 分别重启后均自动重连，Controller 重启后的 `connection_id` 从 `ce2e6bc3-4562-4260-8b5f-799c70d7856d` 变为 `6539640c-5be0-4e31-87cd-a14ee1bcca1f`。

Playwright 1.61.0 对线上页面完成了 `1440x900` 和 `390x844` 两种视口验收：简体中文页面、固定左侧导航、真实在线 Edge 表格、编辑表单、容量 `1000 -> 1001 -> 1000` 的在线提交、静态资源、API 和浏览器错误门禁均通过。Controller 没有引入 Nginx；当前 DNS 尚未发布 `muxvia-controller.omscd.com` 记录，测试 Edge 使用明确的 hosts 映射，Playwright 使用等价 host resolver。该 DNS 记录和 2026-08-07 到期的开发证书必须在 R8 生产化前处理，不能把当前环境视为生产上线。

### 32.7 R4：daemon enrollment 与 AgentGateway（已完成）

- 实现 DeviceIdentity challenge、候选选择、AgentTicket 和 daemon runtime。
- 接真实 `remote/webrtc.Answerer`，先完成 daemon Presence，不接客户端。
- Controller 页面能实时看到 daemon 注册到哪个 Edge，Controller 重启后从 Edge 重建。

R4 通过 `e5cfc600` 建立了 Proto-first EnrollmentService、一次性 DeviceIdentity challenge、PostgreSQL token 消费与 daemon identity 同事务、候选 Edge 选择、独立 Controller TicketSigner、十分钟 AgentTicket、Edge 离线验票、AgentGateway 单 writer/generation fence、daemon 唯一 Cloud runtime 和 Presence delta；`1f3431ce` 补齐了管理投影中的当前 Edge 域名/端口。真实 PostgreSQL harness 证明 code 重放失败且持久 daemon 不会被误报在线；真实 TLS integration 证明同 daemon replacement 和 Controller 在同一地址重启后由 Edge 快照重建 Presence。

2026-07-26 的在线开发验收中，Controller `155.94.155.192:18443/18444` SHA-256 为 `3b6368b3be4eddb2b1caaec12ffd8ad8edcb67b6b2e9815bb8f658a96c05d6a3`，Edge `muxvia-cn1.omscd.com:41102` SHA-256 为 `82e828d8951bec9f0b0d343b713f46b188622bce5982dfd9e12f742ef8cfe3c6`，Linux daemon CLI SHA-256 为 `7c73e6a5dccec93ccad2c2512641f8c10e0ce0a48c5116373005f79135acdc32`。真实 daemon `7332516b-063a-41f6-a010-c3579eb92e29` 通过一次性命令注册到 Edge `65f6aade-5560-416e-9765-0d6fb0bacc00`；Controller 重启后数据库只恢复 identity，Directory 从 Edge 内存快照恢复为在线，三个进程均为 `NRestarts=0`。Playwright 对 `1440x900` 与 `390x844` 验证了中文固定侧栏、Edge/Daemon 独立模块、实时在线行、当前 Edge 域名/端口、注册表单、页面/console error 门禁，共 2 项通过。

### 32.8 R5：客户端 P2P 纵向（已完成）

- 实现 CloudRouteGrant、ClientTicket、ClientGateway 和 `client/adapter/cloud`。
- TUI/CLI 从左到右完成 resolve、offer/answer、P2P DataChannel、remote auth、protocol Hello、terminal 输入输出。
- Android 通过同一 Go engine 完成真实 UI 连接，不允许平台层替代信令。

R5 通过 `0316f725` 建立了 Proto-first Directory/ClientGateway、Controller 签发与 Edge 离线校验 ClientTicket、daemon 真实 Pion answer/revocation、客户端 resolve -> ticket -> P2P -> 端到端认证 -> protocol Hello 链路，以及 Android Cloud Route 的同一 Go engine 装配；`4417cc51` 修正了可移植配对路由。最终收口补齐了 Go Endpoint registry 对 managed Cloud preference 的双向持久化映射，以及 ConnectionSnapshot 的 Cloud route kind 投影，防止已建立 Cloud 连接在 UI 中显示为未知路由。

2026-07-26 的在线开发验收中，Controller `155.94.155.192:18443/18444` SHA-256 为 `5e9e6ec85200d08e1bce5154d59f94537e09e9c80cfb60c8eae6e774ca37e3c1`，Edge `muxvia-cn1.omscd.com:41102` SHA-256 为 `bf093903650ce35643f3e1aa7f5c2ecfc5a5c58776ed08a1a511de12aca4d90a`，Linux daemon CLI SHA-256 为 `ad5da5ba18bf57902a71adcd2c3fb571aed14ff33435893ce186e01f62726492`，三个 systemd 服务均为 active 且 `NRestarts=0`。CLI 通过公网 Controller resolve 和 Edge ClientGateway 建立 host/host UDP P2P DataChannel，在远端 `/bin/cat` 终端完成 `muxvia-r5-online-cli` 输入输出。

最终 Android ARM64 debug APK SHA-256 为 `7aefcb71d21779ca9174e80e9c8e711d8061a1db10752f15e6b9eff469174fd7`。Playwright 1.62.0 通过 API 35 ARM64 模拟器中的真实 App UI 选择 Muxvia Cloud、打开远端终端并输入 `muxvia-r5-android-final`；daemon terminal capture 得到两次 cat 回显。连接投影显示 route ID `cloud`、实际路径 P2P direct、host/host candidate、UDP/UDP transport 和 51ms RTT；Android crash 扫描无异常。Cloud 相关 Go 全域测试通过，integration 连续运行 10 次通过。R5 只完成 P2P Cloud Route，P2P 失败后的 TURN Relay、配额和 usage 属于 R6。

### 32.9 R6：Relay、配额与 usage（已完成）

- 在同一 Edge 进程接 TURN、RelayLease、rate/concurrency/byte limit。
- 实现 durable usage outbox、Controller 幂等结算和故障恢复。
- 验证 P2P 失败后 Relay、Controller 故障租约语义和重复用量不计费。

R6 通过 `a150e684` 建立同一 Edge 进程内的 Pion TURN UDP/TCP、短期 RelayLease、共享 allocation 配额、速率/字节限制、bbolt usage outbox 和 Controller PostgreSQL 幂等结算；`61a35830` 删除迁移期旧 Relay 用量表，`01d3e379`、`90a761f6` 与 `acbc1a6f` 收口 systemd 网络权限、多网卡双端 allocation 和租约并发上限。Android/Go Client Engine 增加由 Go registry 持久化的 Cloud P2P/Relay 与 UDP/TCP 策略；Android Pion 只使用默认路由网络投影，不依赖 SELinux 禁止的 netlink 接口枚举。共享 UI 的 terminal Proto adapter 按同一 terminal 的用户输入顺序串行等待 ACK，避免并发 command completion 重排 PTY 字符。

2026-07-26 的在线开发验收中，Controller `155.94.155.192` 的 `18443` 明确只承载 EdgeControl mTLS，公开 DirectoryService/HTTPS 使用 `18444`；Controller 二进制 SHA-256 为 `ebaeab1d64206dd77fb521598dde1416f01a1338b4a0b3e58ac1149ee72f446e`。Edge `muxvia-cn1.omscd.com:41102` 与内置 TURN `3478/udp,tcp` 的二进制 SHA-256 为 `c6ca49c78d6cd80627a54c143f5dff79150c2517d0993796b494307a19c55848`。两个 systemd unit 均为 active、`NRestarts=0`；旧 Controller、Hub、Relay、Edge unit 和运行文件没有恢复。

最终 Android ARM64 debug APK SHA-256 为 `f9d203861a7e0b31412826a8cdb3cb0563309739c96c7739d9a76b5dbabe79ab`。Playwright 1.62.0 在 API 35 ARM64 模拟器的真实 App UI 中选择 Muxvia Cloud、强制 Relay only + UDP，连接投影显示 `Actual path=Relay`、`Relay transport=UDP`、RTT `102-105 ms`；随后打开远端 `/bin/cat` 并零延迟输入 `muxvia-r6-android-relay-ordered-20260726`，daemon authoritative live screen 得到两次完整回显，Android crash 扫描为空。关闭 App 后，线上 Controller 收到并结算 UDP usage，Edge outbox 副本为 `pending_usage_events=0`；当前 Edge 聚合为 `121` 个事件、ingress `767705`、egress `785859` 字节。UDP/TCP integration 证明 Controller 中断时租约内数据面继续、恢复后 durable usage 清空；隔离 PostgreSQL schema 证明重复 event ACK 不会重复增加 aggregate，测试 schema 已删除。

### 32.10 R7：账号、交易与完整后台（已完成）

- 完成套餐、订单、支付、订阅、Entitlement 和运营 mutation。
- 完成第 12、29 节全部菜单、路由、表格、表单和实时操作。
- 中文默认、稳定侧栏、无全屏重复 loading 进入 Playwright 门禁。

R7 通过 `f0c87300` 建立 Proto-first 账号、角色、会话、交易、Subscription、Entitlement、运营 mutation、审计和 PostgreSQL `0004`，通过 `95ba21b5` 收口管理员引导参数，通过 `2eaef74d` 增加可重复执行的线上 Playwright 门禁并修正手机抽屉关闭后仍拦截点击、窄屏查询按钮换行的问题。运营后台默认简体中文，登录后始终通过左侧导航或手机抽屉进入独立的总览、Edge、daemon、实时连接、用户与权限、套餐、订阅、订单与交易、证书、用量与结算、审计和系统模块；各模块拥有独立路由和内容，不再使用全屏单页堆叠或重复 boot loading。

2026-07-26 按开发阶段策略完整重建了 `muxvia_staging` schema，并重新执行 `0001` 到 `0004`；最终数据库只包含重建后的系统管理员、当前 Cloud 配置和唯一 CN1 Edge，不保留旧 daemon、Hub/Relay 拓扑、连接、交易或迁移期表。重建前的 Controller 数据库 dump、二进制和 unit/env 备份位于 `/var/backups/muxvia/r7-20260726T141921Z`，Edge 旧运行态备份位于 `/var/backups/muxvia/r7-20260726T142120Z`；它们不进入活动服务或新数据库。

最终在线开发环境的 Controller 位于 `155.94.155.192`，公开入口为 `https://muxvia-controller.omscd.com:18444`，二进制 SHA-256 为 `328d4b30353ae67e7560936dfd5f28f2e7d82107029d95e82c4779da6287ca3a`。唯一 Edge `acc6075f-a28e-4b08-a67c-a419b5709199` 的区域为 `CN1`，入口为 `muxvia-cn1.omscd.com:41102`，容量为 `1000`，二进制 SHA-256 为 `a7da7541af5a8f06ab283f39ecce1e5148f3a0c396fe72a6745a38a895779c5d`。Controller 与 Edge 均为 `active/ready`、`NRestarts=0`，活动 unit 中没有旧 Controller、Hub、Relay 或 Edge 服务。国内 Edge 不再把跨境下载速度当作安装门禁：最终安装先由本地转传同一 artifact，只替换生成脚本中的 artifact 下载步骤，固定 SHA-256、Ed25519 发布签名、CSR、一次性 bootstrap 和 systemd 装配仍由原脚本验证。后续程序批量升级属于独立切片，不再由 R8 承担。

Playwright 1.62.0 对真实线上登录和 API 完成 `1440x900` 桌面与 Pixel 7 `390x844` 手机验收：桌面固定侧栏逐项进入 11 个管理模块并查看真实在线 CN1 Edge，手机连续打开抽屉进入 4 个模块，关闭后不再遮挡或拦截内容，用户表加载真实系统管理员且查询按钮保持单行。最终线上结果为 2 项通过、2 项按设备项目正常跳过；页面 error、console error、横向页面溢出和服务 crash 门禁均通过。

### 32.11 R8：Edge 证书上传与自动更新（进行中）

- 运营人员上传匹配的证书链和私钥，Controller 解析非敏感元数据并保存一个当前 revision；不提供 CSR/ACME、历史版本、灰度发布或回滚。
- 一个档案可以绑定多个 Edge，同一 Edge 只选择一个当前档案。档案替换后在线 Edge 自动更新，离线 Edge 重连后按 desired/applied revision 收敛。
- Controller secret 文件、PostgreSQL 元数据、mTLS 传输、Edge 二次校验、`0600` 原子状态文件和 TLS loader 分别保持自己的安全边界。
- Edge 程序升级维持人工 SSH/systemd 部署，运营面只展示实际软件版本；artifact、rollout、在线更新和自动回滚不属于本切片。

2026-07-26 已先完成开发环境的正式域名入口迁移，但 R8 仍为未完成。Muxvia Cloud 主站、Web Controller、JSON API 和公开客户端 gRPC 统一使用 `https://cloud.muxvia.com`，Cloudflare DNS-only A 记录指向海外 Controller `155.94.155.192`；该机保留现有共享 Nginx，在 `443` 终止公开 TLS，并分别反向代理 Web/API 和 `/muxvia.cloud.v1.*` gRPC 到 Controller 原生 `18444` listener。EdgeControl mTLS 继续使用 `155.94.155.192:18443`，不经过公开 Nginx，也不因此重新注册 Edge。国内唯一 Edge 部署在 `114.66.58.243`，继续使用阿里云解析的 `muxvia-cn1.omscd.com:41102`，TURN 使用同机 `3478/udp,tcp`。

`cloud.muxvia.com` 的 Let's Encrypt 证书有效期至 2026-10-24；证书私钥只保存在海外服务器，仓库只保留 Nginx 路由和续期装配。每日 systemd timer 在宿主机运行容器化 Certbot，续期后复制证书到 Nginx 只读挂载目录并执行配置检查和热加载。迁移后 Controller 与 Edge 均完成真实重启并返回 ready、`NRestarts=0`；标准 `443` gRPC 请求实际到达 `DirectoryService`，Playwright 1.62.0 从新域名完成桌面和手机视口真实登录、全部侧栏路由及在线 CN1 Edge 验收，结果为 2 项通过、2 项按项目正常跳过。公开主站 Nginx 证书续期与 Edge 证书档案是两个独立 owner；前者不通过 EdgeControl 分发，后者的最终线上证据由本节后续记录补充。

### 32.12 R9：上线门禁

- 完成第 16 节全部 E2E、故障注入、负载、Android 和部署证据。
- 选择并落地开源许可证，删除过渡性 private license 文案。
- 使用候选 artifact 在 staging 完整部署，再按明确变更单部署生产。
- 生产 smoke test、监控和回滚验证完成后才宣布上线。

R1D 的公网开发装配不改变 R9：它只验证当前切片的真实网络链路，不得宣称为生产部署、公开发布或可供用户使用的 Cloud。

每个切片只提交自己的纵向能力，提交前必须测试通过。不得把 D2 到 R9 合成一次大删除/大重写，也不得在某个切片未完成时用静态页面或 fake 数据提前标记后续能力完成。

## 33. 开始编码前的最终检查

真正开始 Cloud 实现时，第一条代码工作不是创建新 package，而是 D1 工作树保护和 D2 旧 Cloud 删除。原因是旧 package 名称、旧 generated Proto 和旧 module 会持续诱导新代码 import 错误边界。

D2 完成后才建立 R1。R1 的第一条可观察链路必须是：

```text
muxvia-cloud-edge
  -> 使用 mTLS 建立 EdgeControl.Connect
  -> Controller 校验证书 Edge URI SAN、envelope 和协议版本
  -> Controller 返回 EdgeWelcome 并回显当前 connection generation
  -> Edge 进入 ready；Controller 不可达或控制流断开时撤销 ready
```

这条链路不依赖 daemon、客户端、订单或 Relay，先证明双进程、TLS、Proto、版本协商、健康状态和连接 generation 的基本方向。R2 再在同一控制流上增加 Directory actor、全量 snapshot、增量事件、grace 删除和重建 harness；不得把这些状态提前塞回 R1 的 transport service。
