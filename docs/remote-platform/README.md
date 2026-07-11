# TermX Remote Platform 文档基线

状态：设计基线；活动实现真值见 `cloud-staging-roadmap.md`

生效日期：2026-07-11

基线切片：RP001-RP007（含 RP001A-C、RP004A，完成）

## 1. 文档目的

本目录定义 TermX 远程平台重构后的产品、架构、安全和源码分发边界。任何新的 Hub、Relay、Web Controller、TUI 远程连接或 App 远程连接实现，都必须先满足本目录约束。

`cloud-staging-roadmap.md` 是 CLOUD001-CLOUD005 的唯一活动实现真值。其他文档中的 RP/GA “完成”只表示设计、contract、领域组件或 harness 资产已经形成，不代表 managed cloud 已经可以由用户运行。

`remote/` 是公开 managed WebRTC/E2E auth runtime，`clients/ui/` 与 `clients/mobile/` 消费同一公开 endpoint contract。旧 `termx-hub/`、`termx-remote/`、`web-control/` 及 remote-ui 的历史 localweb/docs 已收口到 `private/archive/termx-platform-legacy/`；archive 不得以兼容、fallback 或“先继续沿用”的方式反向约束新模型。

## 2. 文档顺序

1. `cloud-staging-roadmap.md`：回答当前真实完成度、单区域纵向消息链路和 CLOUD002-CLOUD005 的用户 DoD。
2. `product-prd.md`：回答为谁解决什么问题、哪些能力免费、哪些持续服务收费。
3. `architecture-spec.md`：回答公开客户端、daemon、私有 Control Plane、Hub 和 Relay 各自拥有什么状态。
4. `network-topology.md`：用目标网络拓扑解释 local、SSH、direct WebRTC、Relay 与端到端授权链路。
5. `security-protocol-spec.md`：回答设备身份、terminal capability、云服务票据和 Relay 租约如何隔离。
6. `distribution-and-cloud-companion-spec.md`：回答公开主程序与闭源 cloud 能力如何拆包、安装、通信、升级和跨平台发布。
7. `global-acceleration-spec.md`：保留 single-relay 算法背景和延后的 Relay Mesh 输入；不是当前实施队列。
8. `source-boundary-and-migration-plan.md`、`public-snapshot-manifest.md` 与 `../legal/`：正式开源/发布阶段资产；当前 private monorepo 开发不主动扩展或执行。

若这些文档发生冲突，按以下顺序处理：

1. 安全不变量优先于实现便利。
2. domain owner 与 truth source 优先于历史代码行为。
3. 免费/付费产品边界优先于旧订阅字段和旧套餐逻辑。
4. private monorepo 内由 `workflow.md` 决定当前允许执行的切片和目录范围；复制出的 public snapshot 以公开文档和贡献规则为准。

## 3. 已冻结决策

- `core`、`tui`、CLI、terminal protocol、本地连接、SSH transport 和客户端多 endpoint 管理保持开源、免费。
- App 客户端和公开 remote client/daemon contract 默认开源；平台特定 WebRTC primitive 可以分别实现，但业务协议必须共用。
- Web Controller、托管 Hub、托管 Relay、计费、entitlement、风控和云运维服务不进入公开源码发布。
- WebRTC 是一种 endpoint transport；direct P2P 与 Relay 是同一次连接的候选路径结果，不是两套 terminal protocol。
- 全球加速属于 WebRTC Relay path 的内部实现；`single_relay` 与 `relay_mesh` 不创建新的 endpoint、terminal protocol 或 capability 类型。
- 桌面官方 cloud 能力通过可选闭源 `termx-cloud` companion 提供，普通开源 `termx` 不静态或动态链接私有代码；当前 Android 官方构建通过固定私有 source set 使用同一 contract，Community 构建不引用私有路径。
- Control Plane、Hub、Relay 和 Web Controller 服务端不进入普通用户安装包；企业私有部署使用单独商业交付物。
- 当前 private monorepo 根许可证保留全部权利；未来公开快照使用 Apache-2.0 + DCO，并从全新空 Git 仓库建立历史。
- public CLI、private Companion 与 App 分别携带可重复生成的第三方 notice；sidecar IPC 不替代 Official/Enterprise 的书面商业条款和专业审查。
- terminal capability 由 owning daemon 签发和验证。Hub、Relay、Web Controller 永远看不到 capability grant，也不拥有 terminal authorization。
- Control Plane 可以签发短期 Hub 服务准入票据；Relay entitlement 通过独立短期 `RelayLease` 表达。
- 旧代码保留 git 历史和归档引用，但迁移完成后不得继续存在公开/私有双实现或旧 session token fallback。

## 4. 统一术语

| 术语 | 含义 | 非含义 |
| --- | --- | --- |
| Endpoint | 客户端想连接的一个 daemon 目标 | 不是网络地址，也不是 Relay 节点 |
| Transport | 到达 endpoint 的方式，例如 local、SSH、WebRTC | 不是 endpoint 身份 |
| Path | 一次 WebRTC transport 最终采用 direct、single relay 或 relay mesh 的运行结果 | 不是可持久化的 terminal identity |
| Device | 运行 daemon 的长期安全主体 | 不是用户展示 label |
| CapabilityGrant | daemon 签发给客户端的 terminal 权限凭据 | 不是账号 token、Hub token 或订阅凭据 |
| Control Plane | 账号、设备目录、entitlement、票据和计量控制面 | 不转发 terminal protocol，不拥有 terminal history |
| Hub | presence、rendezvous 和 WebRTC signaling 服务 | 不拥有 terminal inventory，不做 terminal authorization |
| Relay | WebRTC 无法直连时转发加密流量的托管数据面 | 不解密 DataChannel，不决定 terminal scope |
| HubAdmissionTicket | 允许客户端或 daemon 使用托管 Hub 的短期票据 | 不授予 terminal 权限 |
| RelayLease | 允许特定会话使用托管 Relay 的短期租约 | 不是永久套餐 token |

## 5. 历史资料状态

下列资料只作为历史背景，不再是活动 spec：

- `webrtc-rewrite-architecture.md`（私有 archive）
- `app-core-v2-contract.md`（私有 archive）
- `relay-plan-product-policy.md`（私有 archive）
- ME010-ME012 的实现说明和现有 Hub/Web Controller schema

可复用思想必须先映射到本目录定义的 domain owner、安全边界和源码边界，再进入新实现。

## 6. 实现门禁

当前实现顺序和完成条件以 `cloud-staging-roadmap.md` 为准。进入 CLOUD002-CLOUD005 时，至少满足：

- 每个切片先有 contract/harness，再接真实服务。
- 公开 client contract 可以用 fake Hub/Control Plane 独立测试。
- 私有服务不可成为 core-v2、TUI、CLI 或 protocol package 的构建依赖。
- 任何请求、日志、指标或持久化中出现原始 `CapabilityGrant` 都视为安全回归。
- 任何订阅判断直接改变 terminal scope 都视为领域边界回归。
