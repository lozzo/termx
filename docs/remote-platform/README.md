# TermX Remote Platform 文档基线

状态：统一 WebRTC DataChannel 产品/架构背景；活动实现真值只看仓库根 `workflow.md`

生效日期：2026-07-19

## 1. 文档目的

本目录保存 TermX 远程平台的产品、架构、安全和历史迁移背景。统一 Route 的当前结论以 `product-prd.md`、`cloud-product-spec.md`、`unified-endpoint-route-refactor-plan.md`、`architecture-spec.md`、`security-protocol-spec.md` 和根 `workflow.md` 为准。

仓库目录 ownership 与依赖方向统一见 [`../development/repository-layout.md`](../development/repository-layout.md)。

`cloud-staging-roadmap.md`、旧 Android 手测、public staging、Hub edge、distribution、global acceleration、source boundary 和 snapshot 文档只记录历史阶段或延后能力，不再驱动当前任务。文档中的旧切片“完成”、旧 App flavor 或旧 transport 行为不能替代 `workflow.md` 的当前完成度判断。

`remote/` 是公开 managed WebRTC/E2E auth runtime，`clients/ui/` 与 `clients/mobile/` 消费同一公开 endpoint contract。旧 `termx-hub/`、`termx-remote/`、`web-control/` 及 remote-ui 的历史 localweb/docs 已收口到 `private/archive/termx-platform-legacy/`；archive 不得以兼容、fallback 或“先继续沿用”的方式反向约束新模型。

## 2. 文档顺序

1. `product-prd.md`：单一 App、免费 Direct/SSH、可选 Cloud 和 Android 用户验收。
2. `cloud-product-spec.md`：账号、套餐、交易、Subscription、Entitlement、managed P2P/Relay、quota、usage 和管理面。
3. `unified-endpoint-route-refactor-plan.md`：versioned Proto Endpoint/Route contract、Go owner 和失败语义。
4. `architecture-spec.md`：Go Client Engine、daemon、Control Plane、Hub 和 Relay 的状态边界。
5. `security-protocol-spec.md`：设备身份、channel binding、terminal capability 和云服务准入隔离。
6. `network-topology.md`：Local、Direct WebRTC TCP、SSH WebRTC TCP 和 managed WebRTC 拓扑。
7. `file-transfer-spec.md`：同一 Proto session 上的文件 owner、授权、流控和失败语义。
8. 其余文件：历史验收、旧阶段决策或延后能力，仅在 `workflow.md` 明确引用时读取。

若这些文档发生冲突，按以下顺序处理：

1. 安全不变量优先于实现便利。
2. domain owner 与 truth source 优先于历史代码行为。
3. 免费/付费产品边界优先于旧订阅字段和旧套餐逻辑。
4. private monorepo 内由 `workflow.md` 决定当前允许执行的切片和目录范围；复制出的 public snapshot 以公开文档和贡献规则为准。

## 3. 已冻结决策

- TermX 只有一个面向用户的 App；Direct 与 SSH 免费且不依赖登录，Cloud 是同一 App 内的可选托管 Route。
- 所有官方客户端连接对象由 Go Client Engine 持有；Android 通过 C ABI + JNI，未来 native wrapper 通过 C ABI，未来浏览器通过 Go/WASM。
- Web Controller、托管 Hub、托管 Relay、计费、entitlement、风控和云运维服务不进入公开源码发布。
- 所有远程 Route 最终使用同一种 WebRTC DataChannel；Direct、SSH 和 Cloud 不是三套 terminal protocol。
- 全球加速属于 WebRTC Relay path 的内部实现；`single_relay` 与 `relay_mesh` 不创建新的 endpoint、terminal protocol 或 capability 类型。
- Cloud companion/module 只提供账号、托管 signaling 和 Relay primitive，不拥有 Endpoint、PeerSession、CapabilityGrant 或 terminal protocol。
- Control Plane、Hub、Relay 和 Web Controller 服务端不进入普通用户安装包；企业私有部署使用单独商业交付物。
- 当前 private monorepo 根许可证保留全部权利；未来公开快照使用 Apache-2.0 + DCO，并从全新空 Git 仓库建立历史。
- public CLI、private Companion 与 App 分别携带可重复生成的第三方 notice；sidecar IPC 不替代 Official/Enterprise 的书面商业条款和专业审查。
- terminal capability 由 owning daemon 签发和验证。Hub、Relay、Web Controller 永远看不到 capability grant，也不拥有 terminal authorization。
- Control Plane 可以签发短期 Hub 服务准入票据；Relay entitlement 通过独立短期 `RelayLease` 表达。
- development Cloud 必须走完整账号、交易、Subscription、Entitlement、准入、限额和 usage 链路；固定测试账号或测试支付 provider 不能绕过这些领域状态。
- 旧代码保留 git 历史和归档引用，但迁移完成后不得继续存在公开/私有双实现或旧 session token fallback。

## 4. 统一术语

| 术语 | 含义 | 非含义 |
| --- | --- | --- |
| Endpoint | 客户端想连接的一个 daemon 目标 | 不是网络地址，也不是 Relay 节点 |
| Route | 到达 Endpoint 的持久配置：local Unix、Direct WebRTC TCP、SSH WebRTC TCP 或 managed WebRTC | 不是 endpoint 身份或 runtime session |
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

当前实现顺序和完成条件只以仓库根 `workflow.md` 为准。`cloud-staging-roadmap.md` 只记录已完成历史阶段，不再提供活动任务队列。Cloud 产品切片至少满足：

- 每个切片先有 contract/harness，再接真实服务。
- 公开 client contract 可以用 fake Hub/Control Plane 独立测试。
- 私有服务不可成为 core-v2、TUI、CLI 或 protocol package 的构建依赖。
- 任何请求、日志、指标或持久化中出现原始 `CapabilityGrant` 都视为安全回归。
- 任何订阅判断直接改变 terminal scope 都视为领域边界回归。
- development profile 必须走完整账号、交易、Subscription、Entitlement、准入、quota 和 usage settlement；测试 provider 不得直接写最终 entitlement。
- 所有跨进程、跨语言和官方客户端 Cloud API 必须先定义在 `proto/cloudpb/`，不得在 Go、Kotlin 或 TypeScript 中复制平行业务 DTO。
