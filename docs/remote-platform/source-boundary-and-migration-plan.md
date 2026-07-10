# TermX Remote Platform 源码边界与迁移计划

状态：RP001 活动基线

版本：v1 draft

日期：2026-07-11

## 1. 目标

本计划定义三件事：

1. 哪些能力属于公开开源产品，哪些能力属于私有托管服务。
2. 当前仓库中的旧 remote、Hub、Web Controller 和 App 代码如何保留为可追溯资产。
3. 新实现按什么顺序建立 contract、迁移服务并删除旧路径，避免公开/私有双真值。

本切片只写文档，不移动目录、不改 module、不重写 git 历史。

## 2. 源码分发原则

### 2.1 公开仓库必须足以独立使用

公开仓库在没有任何私有源码时必须可以：

- 构建 daemon、TUI、CLI 和 App client。
- 使用 local 与 SSH endpoint。
- 生成、导入、验证和撤销 CapabilityGrant。
- 编译 WebRTC transport adapter 和端到端授权状态机。
- 使用公开 interface 对接官方私有 Control Plane/Hub/Relay。
- 用 fake Control Plane、fake Hub 和 in-process WebRTC harness 完成测试。

公开仓库不能依赖私有仓库才能完成 `go test`、App build 或 local/SSH runtime。

### 2.2 私有仓库只拥有持续服务实现

私有仓库拥有：

- 账号、organization、登录和 subscription/billing。
- device directory、managed presence projection 和云端配置 metadata。
- Hub/region 调度、Hub admission 和 ticket signing keys。
- Hub signaling runtime。
- Relay/TURN runtime、lease enforcement、usage collection 和结算。
- Web Controller 管理 UI/API、风控、审计、运维和基础设施配置。

私有仓库可以依赖发布后的公开 contract module，但公开仓库不得反向依赖私有实现。

### 2.3 安全边界必须与源码边界一致

把服务端代码设为私有不是安全机制。所有公开 wire contract 仍按恶意 Hub/Relay/Control Plane 设计；terminal capability 的保密和 daemon 最终授权不依赖服务端源码不可见。

## 3. 目标仓库布局

仓库名称是工作名，可在迁移切片中调整；边界不可调整。

### 3.1 Public: `termx`

目标公开内容：

```text
termx-core-v2/              terminal lifecycle/history truth
termx-vterm/                terminal semantic interpreter
termx-tui-v3/               TUI and EndpointManager
termx-cli/                  daemon/CLI assembly
termx-app/                  mobile client UI and platform adapters
termx-shared/               endpoint/transport/remote auth contracts
internal/protocol/          termx protocol implementation
termx-proto/                versioned public wire messages
termx-remote-v2/            public WebRTC client/daemon orchestration only
docs/remote-platform/       public product/security/client architecture docs
```

`termx-remote-v2/` 最终只允许包含：

- 公共 endpoint dialer 与 daemon remote acceptor。
- WebRTC primitive interface 和平台 adapter。
- DataChannel authorization state machine。
- ControlPlaneClient/HubClient/RelayLeaseProvider interface。
- 公开 wire DTO、error taxonomy、fixtures 和 fake harness。

它不得包含 Hub server、Relay server、数据库、billing、plan limits、私有签名 key management 或 Web Controller API implementation。

### 3.2 Private: `termx-cloud`

目标私有内容：

```text
control-plane/              account/device/entitlement/admission/lease
web-controller/             private admin and customer control UI/API
hub/                        regional presence and signaling runtime
relay/                      TURN/relay enforcement and usage meter
route-planner/              private quality graph, SmartRoute and Relay Mesh
contracts-adapter/          implementation of public client contracts
infra/                      deployment, secrets, observability, runbooks
```

Control Plane、Hub 和 Relay 即使同仓库也保持独立 package/module 和部署单元。Hub 不允许直接 import billing database model；它只消费签名 admission 或明确的私有 service interface。

### 3.3 Private archive: `termx-platform-legacy`

旧实现资产进入只读私有 archive，保留原始路径、commit metadata 和迁移说明：

```text
termx-hub/
web-control/
termx-remote/
remote-ui/
termx-app/legacy-remote-parts
termx-remote-v2/pre-rp-contract
```

archive 不是 module dependency、git submodule 或 runtime fallback。需要借鉴代码时，开发者必须把概念按新 contract 重新实现，而不是从 archive 建立 import/replace。

## 4. 现有资产处理矩阵

| 当前资产 | 现状问题 | 可保留资产 | 目标去向 |
| --- | --- | --- | --- |
| `termx-core-v2/` | 无远程领域所有权问题 | scoped transport、terminal/history truth | 公开仓库保留 |
| `termx-tui-v3/` | Hub dialer 仍绑定旧信令语义 | EndpointID、TerminalRef、EndpointManager、局部失败状态 | 公开仓库保留并接新 contract |
| `termx-shared/remoteauth/` | grant 概念可用，交付链路需重做 | DeviceIdentity、fingerprint、scope、revoke | 公开仓库演进为 E2E auth owner |
| `termx-shared/transport/datachannel/` | primitive 基本可用 | reliable ordered packet transport | 公开仓库保留 |
| `termx-remote-v2/` | grant 经 Hub signaling；pending answer 不完整 | Pion adapter、dial/answer harness、core scoped session 接线 | 公开仓库重写 orchestration；旧版本进 archive |
| `termx-hub/client/` | client 与 server module/内部 wire 耦合 | stream/signaling API 经验 | contract 抽到公开仓库；实现迁私有 |
| `termx-hub/internal/hub/` | 非空 Bearer 即通过、长期 agent token、terminal inventory | TTL presence、offer/answer correlation、ICE/traffic 思路 | 私有 Hub/Relay 按新模型重建；旧代码进 archive |
| `web-control/` | agent/server 限额、heartbeat kick、订阅直接控制在线状态 | 用户、订单、支付、管理 UI 和运维经验 | 私有 Control Plane 重建；旧 schema 进 archive |
| `termx-app/` | Android `HubConnector` 维护独立 session token/WebRTC 流程 | 原生 WebRTC、Keystore、前后台生命周期 | 公开 App 保留，业务流改接共同 contract |
| `remote-ui/` | machine/session-token contract 已归档 | 产品交互、配对和 Relay 策略历史 | 设计资产进 archive；不作为 runtime fallback |
| `termx-remote/` | 旧 remote/localweb 模型 | 历史行为和 UI 参考 | 私有 archive，不恢复依赖 |

## 5. Git 历史和许可证处理

### 5.1 先确认是否已经公开发布

在执行 RP007 前必须确认：

- 当前仓库或相关 commit 是否已推送到公开远端。
- `termx-hub/`、`web-control/` 是否已按开源许可证发布或分发。
- 第三方贡献是否允许迁入私有服务仓库。

如果代码已经公开或已经按开源许可证分发，删除文件或重写公开 git 历史不能撤回外部已有副本和既有许可证授权。此时只能把后续重写版本设为私有，并保留必要的 attribution/许可证义务。该问题需要在正式发布前做法律和许可证核对。

### 5.2 推荐迁移方式

1. 在受控私有远端创建完整镜像和不可变归档 tag，例如 `remote-platform-legacy-2026-07`。
2. 使用 path-filtered history 分别生成 `termx-cloud` 和 `termx-platform-legacy`，尽量保留相关 commit author、date 和 message。
3. 对私有迁移结果做 hash/commit mapping 清单，记录原 commit 到新 commit 的映射。
4. 在公开仓库完成新 contract 后再删除服务实现路径，避免中途丢失可参考资产。
5. 若仓库尚未公开，首次公开发布应从过滤后的 clean public history 生成，确保私有路径从未进入公开对象库。
6. 若仓库已经公开，不宣称历史实现已被“隐藏”；对外只说明后续托管服务实现私有。

以上操作具有历史重写和发布影响，必须在 RP007 单独执行并备份；RP001 不运行任何 `git filter-repo`、force push 或 destructive command。

## 6. 实施顺序

### RP002：公开 remote contract 抽取

范围：公开仓库。

先建立：

- `ControlPlaneClient`、`HubClient`、`RelayLeaseProvider` interface。
- versioned DTO、credential envelope tag 和稳定 error taxonomy。
- fake Control Plane/Hub/Relay lease provider。
- contract fixture，证明信令 schema 不含 grant、terminal 或 scope。
- dependency guard，禁止 public client import Hub server/private schema。

完成条件：TUI/App/daemon 可以只依赖公开 interface 编译；现有真实 Hub adapter 暂时可以不接通，但不能保留 opaque grant-in-signaling 作为 fallback。

### RP003：端到端设备证明与 capability handshake

范围：公开 `termx-shared/remoteauth`、`termx-remote-v2` 和必要 protocol fixture。

先建立：

- versioned `AuthEnvelope` 和 canonical encoding fixture。
- DeviceHello 与实际 DTLS peer certificate fingerprint binding。
- CapabilityOpen challenge proof。
- strict protocol switch 与 stable rejection errors。
- Go/Kotlin 至少两端的 shared vectors；iOS 接入前补 Swift vector。

完成条件：恶意 fake Hub 无法冒充 device 或读取 grant；daemon 仅在 handshake 后调用 `ServeScopedTransport`。旧 pre-answer grant validation 和 session-token 字段直接删除。

### RP004：私有 Control Plane 领域重建

范围：私有 `termx-cloud/control-plane` 与 Web Controller。

按以下 aggregate 建模：

- Account/User/Organization。
- DeviceRegistration/Ownership。
- Entitlement/QuotaPolicy。
- ManagedSession/HubAssignment。
- HubAdmission/SigningKey。
- RelayLease/UsageLedger。
- PairingApproval/AuditEvent metadata。

完成条件：订阅代码不能 import terminal scope；heartbeat 不再按未订阅踢掉 daemon；Control Plane 能签发短期 admission/lease 并对 usage 幂等结算。

### RP005：私有 Hub/Relay 重建

范围：私有 Hub 和 Relay。

先建立：

- admission offline verification 与 key rotation。
- 无 terminal inventory 的 device presence。
- offer/answer/candidate streaming、TTL、cancel 和 async answer。
- session-specific RelayLease/TURN credential。
- limit enforcement 和 signed usage event。
- direct 与 relayed 的真实 WebRTC integration harness。

完成条件：Hub schema 和日志中没有 capability；Relay 无法解密 DataChannel；无 lease fail closed；旧非空 Bearer、长期 agent token、24h machine lease 和共享 TURN credential 删除。

### RP006：TUI/App 统一接入

范围：公开客户端。

- TUI `hub-p2p` dialer 改为新的 managed WebRTC adapter。
- App 删除独立 `HubConnector` 业务协议，复用同一 endpoint state machine 和 fixtures。
- 两端统一 direct/Relay path、admission/entitlement/auth 错误展示。
- 凭据分别使用 file credential store 与平台 Keychain/Keystore，但共享 `grant_ref` 语义。

完成条件：同一 endpoint 配置和 grant 可以在 TUI/App 中得到一致授权结果；平台差异只停留在 WebRTC primitive 和安全存储。

### RP007：仓库分拆与清场

范围：发布和仓库治理。

- 创建私有完整备份、archive 和 cloud 服务仓库。
- 验证历史映射、第三方许可证和 secret scanning。
- 公开仓库删除 `termx-hub` server、`web-control` server 和旧 remote runtime。
- 删除公开 module 对私有路径的 replace/import/script/Makefile 入口。
- 保留公开 contract、fake harness 和服务 API 文档。
- 对公开仓库做 dependency、license、secret 和 object-history 审计。

完成条件：公开 clone 无法访问私有服务源码且仍能完成所有公开构建/测试；私有 archive 可按 commit mapping 追溯旧资产；运行时不存在旧 fallback。

## 7. 迁移期间的禁止项

- 不在当前仓库新建 `legacy/` 目录复制 Hub/Web Controller 源码；这会继续把闭源目标放在未来公开仓库中。
- 不用 build tag 同时保留 public/private 两个 Hub 实现。
- 不把私有仓库作为公开仓库 submodule。
- 不让 fake Hub 逐渐演变为可部署的第二套服务端。
- 不保留 `session_token` 字段承载“新票据或旧 grant 两种含义”。
- 不迁移旧数据库 schema 后再靠 nullable 新字段叠加；开发周期直接建立新 aggregate 和新库。
- 不以“套餐禁用”为由 kick daemon presence；只让对应 admission/lease 按策略过期或拒绝续签。
- 不在客户端收到新错误时 fallback 到旧 remote、local、SSH 或原始 shell。

## 8. 数据迁移策略

当前仍处开发周期，默认不迁移旧 runtime 数据：

- 旧 agent/server 记录不迁为新 DeviceRegistration。
- 旧 session token 不转换为 CapabilityGrant 或 HubAdmissionTicket。
- 旧 24h TURN credential 不转换为 RelayLease。
- 旧 plan 的 `maxAgents/maxServers/relayBandwidthKbps` 不直接映射新 entitlement。
- 旧配对记录不自动信任，用户按新 DeviceFingerprint 重新配对。

可以迁移且需显式审核的业务数据：

- account identity；
- 已完成订单、支付和发票记录；
- 法律要求保留的审计/财务 metadata。

迁移脚本只存在私有仓库，并经过一次性 dry run、行数核对和回滚备份。

## 9. 测试与准入矩阵

| 门禁 | Public | Private | Cross-repo |
| --- | --- | --- | --- |
| local/SSH 无云可用 | 必须 | 不涉及 | 云服务关闭 integration |
| contract fixtures | owner | consumer | 双方版本兼容 |
| E2E auth malicious Hub | owner | fake/adapter | 真实 Hub 不可绕过 |
| Hub admission | verifier fixture | issuer/verifier | key rotation integration |
| Relay lease/usage | DTO fixture | enforcement/ledger | over-quota integration |
| App/TUI 一致性 | owner | fake service | staging smoke |
| secret/log scan | grant/ticket redaction | service credentials | release gate |
| dependency guard | 不依赖 private | 只依赖 public release | CI graph audit |

公开 contract 的 breaking change 必须显式升 version。私有服务可以先向后兼容一个公开客户端版本窗口，但公开客户端不得保留旧安全协议 fallback；服务端兼容窗口只允许同一安全模型内的字段/version 演进。

## 10. 发布和回滚

### 10.1 发布顺序

1. 发布 public contract library 与 fake fixtures。
2. 部署兼容新 contract 的私有 Control Plane/Hub/Relay staging。
3. 发布 daemon/TUI/App opt-in preview。
4. 完成 direct、Relay、quota、revocation 和 region failure 测试。
5. 切换默认 managed endpoint 到新服务。
6. 删除旧服务和旧客户端路径。
7. 执行仓库分拆与公开发布审计。

### 10.2 回滚边界

允许回滚：

- 新 Hub/Relay deployment 到同一新 contract 的上一个版本。
- client UI 或非安全状态投影。
- Control Plane entitlement 配置。

禁止回滚：

- capability grant-in-signaling。
- 非空 Bearer 即通过。
- 长期共享 TURN credential。
- Web Controller subscription 直接决定 terminal scope。
- 旧 remote runtime fallback。

若新安全协议出现问题，应停止 managed WebRTC preview，保留 local/SSH，而不是恢复旧不安全路径。

## 11. RP001 完成标准

- PRD 明确免费/收费能力和目标用户旅程。
- 架构 spec 明确所有 domain owner、truth source 和消息链路。
- 安全 spec 明确五类凭据、E2E handshake、Hub admission 和 Relay lease。
- 本计划明确公开/私有/归档边界、旧资产映射和 RP002-RP007 顺序。
- `workflow.md` 与根 `AGENTS.md` 使用相同术语，不再把现有 Hub/Web Controller runtime 当目标 contract。
- 文档通过 `git diff --check` 并作为单独中文提交落库。
