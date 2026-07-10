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

### 2.1 Public namespace 必须可以独立复制

当前 private monorepo 中计划开源的目录，在复制到全新仓库且没有任何 `private/` 源码时必须可以：

- 构建 daemon、TUI、CLI 和 App client。
- 使用 local 与 SSH endpoint。
- 生成、导入、验证和撤销 CapabilityGrant。
- 编译 WebRTC transport adapter 和端到端授权状态机。
- 使用公开 interface 对接官方私有 Control Plane/Hub/Relay。
- 用 fake Control Plane、fake Hub 和 in-process WebRTC harness 完成测试。

public namespace 不能依赖 `private/` 才能完成 `go test`、App build 或 local/SSH runtime。

### 2.2 `private/` 只拥有闭源能力

当前 monorepo 的 `private/` 命名空间拥有：

- 账号、organization、登录和 subscription/billing。
- device directory、managed presence projection 和云端配置 metadata。
- Hub/region 调度、Hub admission 和 ticket signing keys。
- Hub signaling runtime。
- Relay/TURN runtime、lease enforcement、usage collection 和结算。
- Web Controller 管理 UI/API、风控、审计、运维和基础设施配置。

`private/` 可以依赖 public contract，但 public namespace 不得反向依赖私有实现。

### 2.3 安全边界必须与源码边界一致

把服务端代码设为私有不是安全机制。所有公开 wire contract 仍按恶意 Hub/Relay/Control Plane 设计；terminal capability 的保密和 daemon 最终授权不依赖服务端源码不可见。

## 3. 目标 Monorepo 布局

当前开发阶段只维护本 private monorepo。开源时复制 public namespace 到全新空 Git 仓库，不复制当前历史；目录边界不可调整。

### 3.1 Public namespace：未来复制到 `termx`

目标公开内容：

```text
termx-core-v2/              terminal lifecycle/history truth
termx-vterm/                terminal semantic interpreter
termx-tui-v3/               TUI and EndpointManager
termx-cli/                  daemon/CLI assembly
termx-app/                  mobile client UI and platform adapters
termx-shared/               endpoint/transport/remote auth/cloud companion contracts
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

### 3.2 Private namespace：仅留在当前 monorepo

目标私有内容：

```text
private/termx-cloud/control-plane/      account/device/entitlement/admission/lease
private/termx-cloud/companion/          desktop/headless official cloud sidecar
private/termx-cloud/web-controller/     private admin and customer control UI/API
private/termx-cloud/hub/                regional presence and signaling runtime
private/termx-cloud/relay/              TURN/relay enforcement and usage meter
private/termx-cloud/route-planner/      quality graph, SmartRoute and Relay Mesh
private/termx-cloud/contracts-adapter/  implementation of public client contracts
private/termx-cloud/infra/              deployment, secrets, observability, runbooks
```

Control Plane、Hub 和 Relay 即使同仓库也保持独立 package/module 和部署单元。Hub 不允许直接 import billing database model；它只消费签名 admission 或明确的私有 service interface。

### 3.3 Private archive

旧实现资产进入只读私有 archive，保留原始路径、commit metadata 和迁移说明：

```text
private/archive/termx-platform-legacy/termx-hub/
private/archive/termx-platform-legacy/web-control/
private/archive/termx-platform-legacy/termx-remote/
private/archive/termx-platform-legacy/remote-ui/
private/archive/termx-platform-legacy/app-legacy-remote/
private/archive/termx-platform-legacy/remote-v2-pre-rp-contract/
```

archive 不是 module dependency、git submodule 或 runtime fallback。需要借鉴代码时，开发者必须把概念按新 contract 重新实现，而不是从 archive 建立 import/replace。

## 4. 现有资产处理矩阵

| 当前资产 | 现状问题 | 可保留资产 | 目标去向 |
| --- | --- | --- | --- |
| `termx-core-v2/` | 无远程领域所有权问题 | scoped transport、terminal/history truth | public namespace 保留 |
| `termx-tui-v3/` | Hub dialer 仍绑定旧信令语义 | EndpointID、TerminalRef、EndpointManager、局部失败状态 | public namespace 保留并接新 contract |
| `termx-shared/remoteauth/` | grant 概念可用，交付链路需重做 | DeviceIdentity、fingerprint、scope、revoke | public namespace 演进为 E2E auth owner |
| `termx-shared/transport/datachannel/` | primitive 基本可用 | reliable ordered packet transport | public namespace 保留 |
| `termx-remote-v2/` | grant 经 Hub signaling；pending answer 不完整 | Pion adapter、dial/answer harness、core scoped session 接线 | public namespace 重写 orchestration；旧版本进 private archive |
| `termx-hub/client/` | client 与 server module/内部 wire 耦合 | stream/signaling API 经验 | contract 抽到 public namespace；实现迁 `private/` |
| `termx-hub/internal/hub/` | 非空 Bearer 即通过、长期 agent token、terminal inventory | TTL presence、offer/answer correlation、ICE/traffic 思路 | `private/` Hub/Relay 按新模型重建；旧代码进 archive |
| `web-control/` | agent/server 限额、heartbeat kick、订阅直接控制在线状态 | 用户、订单、支付、管理 UI 和运维经验 | `private/` Control Plane 重建；旧 schema 进 archive |
| `termx-app/` | Android `HubConnector` 维护独立 session token/WebRTC 流程 | 原生 WebRTC、Keystore、前后台生命周期 | public App 保留，业务流改接共同 contract |
| `remote-ui/` | machine/session-token contract 已归档 | 产品交互、配对和 Relay 策略历史 | 设计资产进 private archive；不作为 runtime fallback |
| `termx-remote/` | 旧 remote/localweb 模型 | 历史行为和 UI 参考 | private archive，不恢复依赖 |

## 5. Git 历史和许可证处理

### 5.1 当前开发方式

当前仓库保持私有，公开和闭源代码都在本地 Git 正常提交：

- 不维护第二个开发仓库。
- 不做 public mirror 同步、exporter 或日常历史过滤。
- public namespace 保持不依赖 `private/`，为未来复制做准备。
- 闭源实现逐步收口到 `private/`，但迁移只按对应实现切片执行。

### 5.2 未来开源方式

正式开源时：

1. 选择一个通过测试的 private monorepo commit 作为快照来源。
2. 创建全新的空 Git 仓库，不复制 `.git/`。
3. 按最终公开目录清单复制 public namespace 文件。
4. 删除内部配置、secret、private docs、私有 module reference 和不可分发资产。
5. 添加公开许可证、notice、贡献说明和公开 README。
6. 在新目录独立运行构建、测试、license 和 secret scan。
7. 通过后创建 public repo 的第一个 commit 并推送公开远端。

当前不实现复制脚本；RP007 只要求人工可重复的目录清单和发布检查。若未来重复发布频率使手工复制容易出错，再单独评估自动化。

## 6. 实施顺序

### RP002：public remote contract 抽取

范围：当前 private monorepo 的 public namespace。

先建立：

- `ControlPlaneClient`、`HubClient`、`RelayLeaseProvider` interface。
- `CloudCompanionClient`、versioned local IPC DTO、caller role/capability negotiation 和稳定 lifecycle error。
- versioned DTO、credential envelope tag 和稳定 error taxonomy。
- fake Companion/Control Plane/Hub/Relay lease provider。
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

范围：`private/termx-cloud/control-plane` 与 `private/termx-cloud/web-controller`。

按以下 aggregate 建模：

- Account/User/Organization。
- DeviceRegistration/Ownership。
- Entitlement/QuotaPolicy。
- ManagedSession/HubAssignment。
- HubAdmission/SigningKey。
- RelayLease/UsageLedger。
- PairingApproval/AuditEvent metadata。

完成条件：订阅代码不能 import terminal scope；heartbeat 不再按未订阅踢掉 daemon；Control Plane 能签发短期 admission/lease 并对 usage 幂等结算。

### RP004A：私有 Cloud Companion

范围：`private/termx-cloud/companion`。

- 实现 account/device cloud session、Control Plane/Hub adapter、presence/signaling、RelayLease、quality summary 和 route plan。
- 使用 OS credential store 和 public companion contract fixtures。
- 不接收 grant、DeviceIdentity private key、DataChannel 或 terminal payload。

完成条件：private companion 可以驱动 public fake/real adapter contract；恶意或崩溃 companion 只影响 managed endpoint，不能绕过 E2E DeviceIdentity/capability 验证。

### RP005：私有 Hub/Relay 重建

范围：`private/termx-cloud/hub` 与 `private/termx-cloud/relay`。

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

### RP006A：Companion 安装与官方构建

范围：公开 CLI installer/lifecycle、私有 desktop artifact 和官方移动构建。

- CLI 实现 install/login/enroll/status/doctor/update/logout/uninstall。
- manifest、artifact、hash、签名、平台和 protocol version 全部验证后原子安装。
- Android/iOS official build 接私有 cloud module；Community build 使用 disabled/fake adapter。
- companion 缺失、崩溃和不兼容只影响 managed endpoint。

完成条件：桌面 signed install/update/uninstall harness 与移动端 contract fixture 通过；普通公开构建不需要私有源码。

### RP007：私有命名空间与开源快照准备

范围：发布和仓库治理。

- 闭源实现和 legacy assets 收口到 `private/`。
- 建立未来公开目录清单，不实现日常 exporter/sync。
- 删除 public namespace 对私有路径的 import/replace/script/Makefile 依赖。
- 在临时空目录按清单复制公开文件，验证独立构建、测试、license 和 secret scan。
- 记录创建全新 public Git 仓库的发布步骤，不复制 private `.git/` 历史。

完成条件：从选定 private commit 手工复制出的 public snapshot 可独立构建测试且不含 `private/`；当前 private monorepo 继续作为完整开发真值；运行时不存在旧 fallback。

## 7. 迁移期间的禁止项

- 不把闭源实现散落到计划公开目录；legacy 只能进入 `private/archive/`。
- 不用 build tag 同时保留 public/private 两个 Hub 实现。
- 不让 public package import `private/`，也不靠复制时临时删除 import 修补构建。
- 不把当前 private `.git/` 目录复制或直接改成 public remote。
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

迁移脚本只存在 `private/`，并经过一次性 dry run、行数核对和回滚备份。

## 9. 测试与准入矩阵

| 门禁 | Public namespace | `private/` | Future public snapshot |
| --- | --- | --- | --- |
| local/SSH 无云可用 | 必须 | 不涉及 | 云服务关闭 integration |
| contract fixtures | owner | consumer | 双方版本兼容 |
| E2E auth malicious Hub | owner | fake/adapter | 真实 Hub 不可绕过 |
| Hub admission | verifier fixture | issuer/verifier | key rotation integration |
| Relay lease/usage | DTO fixture | enforcement/ledger | over-quota integration |
| App/TUI 一致性 | owner | fake service | staging smoke |
| secret/log scan | grant/ticket redaction | service credentials | release gate |
| dependency guard | 不依赖 `private/` | 可以依赖 public contract | copy 后独立构建 |

公开 contract 的 breaking change 必须显式升 version。私有服务可以先向后兼容一个公开客户端版本窗口，但公开客户端不得保留旧安全协议 fallback；服务端兼容窗口只允许同一安全模型内的字段/version 演进。

## 10. 发布和回滚

### 10.1 发布顺序

1. 发布 public contract library 与 fake fixtures。
2. 部署兼容新 contract 的私有 Control Plane/Hub/Relay staging。
3. 发布 daemon/TUI/App opt-in preview。
4. 完成 direct、Relay、quota、revocation 和 region failure 测试。
5. 切换默认 managed endpoint 到新服务。
6. 删除旧服务和旧客户端路径。
7. 发布前从选定 private commit 复制 public snapshot 到全新 Git 仓库并完成审计。

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
- 发布 spec 明确同一 private monorepo、独立 artifact、Cloud Companion 和未来新仓库复制流程。
- 本计划明确公开/私有/归档边界、旧资产映射和 RP002-RP007 顺序。
- `workflow.md` 与根 `AGENTS.md` 使用相同术语，不再把现有 Hub/Web Controller runtime 当目标 contract。
- 文档通过 `git diff --check` 并作为单独中文提交落库。
