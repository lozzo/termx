# TermX Remote Platform 文档基线

状态：活动基线

生效日期：2026-07-11

基线切片：RP001、RP001A、RP001B、RP001C（完成）

## 1. 文档目的

本目录定义 TermX 远程平台重构后的产品、架构、安全和源码分发边界。任何新的 Hub、Relay、Web Controller、TUI 远程连接或 App 远程连接实现，都必须先满足本目录约束。

当前仓库中的 `termx-hub/`、`web-control/`、`termx-remote/`、`termx-remote-v2/`、`termx-app/` 和 `remote-ui/` 含有可复用资产，但它们是迁移输入，不是新设计的真值来源。旧代码不得以兼容、fallback 或“先继续沿用”的方式反向约束新模型。

## 2. 文档顺序

1. `product-prd.md`：回答为谁解决什么问题、哪些能力免费、哪些持续服务收费。
2. `architecture-spec.md`：回答公开客户端、daemon、私有 Control Plane、Hub 和 Relay 各自拥有什么状态。
3. `network-topology.md`：用网络拓扑和时序图解释 local、SSH、direct WebRTC、Relay fallback 与端到端授权链路。
4. `global-acceleration-spec.md`：回答何时需要 single-relay 智能选区或双 Edge Relay Mesh，以及如何测量、计量和分阶段建设。
5. `distribution-and-cloud-companion-spec.md`：回答公开主程序与闭源 cloud 能力如何拆包、安装、通信、升级和跨平台发布。
6. `security-protocol-spec.md`：回答设备身份、terminal capability、云服务票据和 Relay 租约如何隔离。
7. `source-boundary-and-migration-plan.md`：回答哪些代码公开、哪些代码私有、旧资产如何保留并按什么顺序迁移。

若这些文档发生冲突，按以下顺序处理：

1. 安全不变量优先于实现便利。
2. domain owner 与 truth source 优先于历史代码行为。
3. 免费/付费产品边界优先于旧订阅字段和旧套餐逻辑。
4. `workflow.md` 决定当前允许执行的切片和目录范围。

## 3. 已冻结决策

- `termx-core-v2`、`termx-tui-v3`、CLI、terminal protocol、本地连接、SSH transport 和客户端多 endpoint 管理保持开源、免费。
- App 客户端和公开 remote client/daemon contract 默认开源；平台特定 WebRTC primitive 可以分别实现，但业务协议必须共用。
- Web Controller、托管 Hub、托管 Relay、计费、entitlement、风控和云运维服务不进入公开源码发布。
- WebRTC 是一种 endpoint transport；direct P2P 与 Relay 是同一次连接的候选路径结果，不是两套 terminal protocol。
- 全球加速属于 WebRTC Relay path 的内部实现；`single_relay` 与 `relay_mesh` 不创建新的 endpoint、terminal protocol 或 capability 类型。
- 桌面官方 cloud 能力通过可选闭源 `termx-cloud` companion 提供，普通开源 `termx` 不静态或动态链接私有代码；移动端官方构建使用同一 contract 的私有模块。
- Control Plane、Hub、Relay 和 Web Controller 服务端不进入普通用户安装包；企业私有部署使用单独商业交付物。
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

- `remote-ui/docs/webrtc-rewrite-architecture.md`
- `remote-ui/docs/app-core-v2-contract.md`
- `remote-ui/docs/relay-plan-product-policy.md`
- ME010-ME012 的实现说明和现有 Hub/Web Controller schema

可复用思想必须先映射到本目录定义的 domain owner、安全边界和源码边界，再进入新实现。

## 6. 实现门禁

RP001 完成前不修改 remote、Hub、Web Controller 或 App runtime。进入后续实现时，至少满足：

- 每个切片先有 contract/harness，再接真实服务。
- 公开 client contract 可以用 fake Hub/Control Plane 独立测试。
- 私有服务不可成为 core-v2、TUI、CLI 或 protocol package 的构建依赖。
- 任何请求、日志、指标或持久化中出现原始 `CapabilityGrant` 都视为安全回归。
- 任何订阅判断直接改变 terminal scope 都视为领域边界回归。
