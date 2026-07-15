# 统一 Endpoint、多 Route 与 Transport 竞速重构方案

状态：用户审核草案，已补充私有 TermX Cloud 专项审计，未进入实现

日期：2026-07-15

## 1. 背景

当前产品原则已经明确：用户管理的是 daemon endpoint，不是“SSH 机器”“Cloud 机器”或“直连机器”三套资源；local、SSH、直连 IP 和 managed WebRTC 只是到达同一个 daemon 的不同方式。

当前实现尚未完整落实这个原则：

- `shared/connection.Config` 把一个 endpoint 和一个 transport 绑定在同一个对象里。
- TUI `EndpointManager` 按 endpoint 配置中的唯一 transport 创建唯一 bundle。
- Cloud target device、daemon fingerprint、grant、SSH 参数和 local socket 被放在同一层 dial identity 中。
- Official App 同时存在共享 TypeScript connection orchestrator、TypeScript session manager 和 Android Kotlin managed Cloud `ConnectionStore` 等多套连接状态机。
- 当前 termx protocol Hello 不返回可验证 daemon identity，因此客户端无法安全确认 SSH、指定 IP 和 managed Cloud 是否到达同一个 daemon。

最终需要把“目标身份”“可用连接方式”“一次运行时连接”和“WebRTC 内部网络路径”拆成独立领域概念。

## 2. 重构目标

1. 一个客户端只保存一个逻辑 daemon endpoint，但该 endpoint 可以同时配置多条 route。
2. 同一个 daemon 可以同时开放 local、SSH、指定 IP 和 managed Cloud ingress。
3. TUI 与 App 使用同一套 endpoint、route、竞速、错误和运行时状态语义。
4. 未配置 route 优先级时，所有可用 route 默认并发竞速。
5. 配置 route 优先级时，按优先级分组并采用有界 staggered race。
6. 首个完成身份验证、授权和 protocol Hello 的 route 成为唯一活动 protocol session。
7. transport 切换不改变 `EndpointID`、`TerminalRef`、terminal lifecycle 或 history truth。
8. Cloud Companion 只作为 managed WebRTC route provider，不拥有 endpoint registry、跨 transport 选择或 terminal protocol session。
9. local、SSH 和指定 IP 不依赖账号、订阅、Hub 或 Relay；Cloud 故障只降低 managed route 可用性。

## 3. 非目标

- 不把 route 竞速扩展成通用插件系统。
- 第一阶段不长期保留多个已授权 protocol session 作为热备。
- 第一阶段不实现无中断 live reroute；route 变化通过关闭旧 session、重连和重新 attach 完成。
- 不让 Cloud 服务读取 CapabilityGrant、terminal metadata、history、文件信息或 terminal payload。
- 不使用 hostname、IP、endpoint label、SSH alias 或 Hub device id 作为 daemon 安全身份。
- 不为指定 IP 连接新增用户名密码或长期共享 token 授权体系。

## 4. 术语与领域模型

### 4.1 Endpoint

`Endpoint` 表达客户端要访问的一个逻辑 daemon。

```text
Endpoint
  EndpointID          客户端本地稳定引用
  Label               纯展示名称
  DaemonIdentity      DeviceID + pinned DeviceFingerprint
  ConnectMode         auto | on_demand | manual
  SelectionPolicy     route 竞速策略
  Routes              一组 AccessRoute
```

约束：

- `EndpointID` 只用于客户端持久化、`TerminalRef`、路由和 UI，不是安全身份。
- `DeviceFingerprint` 是跨 transport 合并同一 daemon 的安全锚点。
- `DeviceID` 是 daemon 稳定目录/发现 ID，但不能替代 fingerprint 校验。
- route 返回的 daemon identity 与 endpoint pin 不一致时必须失败，禁止自动覆盖 pin。

### 4.2 AccessRoute

`AccessRoute` 表达一种到达 endpoint 的配置。

首期 route kind：

- `local-unix`：本机 Unix socket。
- `ssh-stdio`：OpenSSH 登录远端后运行固定 `termx daemon stdio-proxy`。
- `direct-tls`：用户指定 IP/DNS + TLS 1.3 + daemon capability authentication。
- `managed-webrtc`：Cloud Companion + Hub signaling + WebRTC DataChannel。

每条 route 有独立的：

- `RouteID`。
- transport-specific address/config。
- `AuthRef`。
- enabled/manual-only 状态。
- 可选 priority。
- dial phase、错误、最近成功时间和观测路径。

route 不拥有 terminal lifecycle、history、workbench 或 endpoint identity。

### 4.3 Transport

`Transport` 是某次 route attempt 建立的运行时载体：

- Unix frame transport。
- SSH stdio frame transport。
- TLS/TCP frame transport。
- WebRTC DataChannel frame transport。

transport 建立不等于 endpoint 已连接。只有完成 daemon identity、authorization 和 termx protocol Hello 后，才能形成 `ReadySession`。

### 4.4 Path

`Path` 只描述某个 transport 内部实际经过的网络路径。

首期只有 `managed-webrtc` 使用：

- `direct`。
- `single_relay`。
- 后续显式阶段才可能有 `relay_mesh`。

SSH、local Unix 和 direct TLS 不使用 `direct/single_relay` 伪装自己的 route kind。

### 4.5 EndpointSession

`EndpointSession` 是 endpoint 当前唯一活动的已认证 termx protocol bundle。

```text
EndpointSession
  EndpointID
  RouteID
  SessionGeneration
  VerifiedDaemonIdentity
  AuthorizationSummary
  ProtocolBundle
  ObservedPath
  Lifecycle
```

同一客户端可以在竞速期间并发持有多个未决 transport，但第一阶段每个 endpoint 最终只保留一个活动 protocol session。

## 5. 建议配置 Schema

桌面 `connections.yaml` 建议升级为破坏性的 v2 schema：

```yaml
version: 2
default: studio

endpoints:
  studio:
    label: Studio
    device_id: device-xxx
    device_fingerprint: SHA256:xxx
    connect_mode: on_demand
    selection:
      hedge_delay: 300ms
    routes:
      lan:
        kind: direct-tls
        priority: 10
        address: 192.168.1.20:41120
        auth_ref: grant:studio

      ssh:
        kind: ssh-stdio
        priority: 20
        address: studio-host
        auth_ref: ssh:studio
        remote_socket: auto

      cloud:
        kind: managed-webrtc
        priority: 30
        target_device_id: device-xxx
        auth_ref: grant:studio
        relay_mode: auto
```

移动端不要求使用 YAML，但 native endpoint store 必须表达同一 versioned domain schema，并通过共享 contract fixture 验证。

## 6. Route 选择与竞速

### 6.1 Eligible route

开始连接前，`RouteSelectionPlanner` 先过滤：

- route 是否 enabled。
- 当前平台是否支持该 route kind。
- route 配置是否完整。
- 必要 credential reference 是否存在。
- 当前连接 intent 是否允许使用该 route。
- 用户是否显式锁定某条 route。

过滤只产生连接计划，不做网络 IO。

### 6.2 显式 route

用户通过 CLI、TUI 或 App 显式选择 route 时，只连接该 route：

```text
termx endpoint connect studio --route ssh
```

显式 route override 在本次 session 内保持 sticky。重连只使用该 route，直到用户解除 override 或重新执行自动连接。

### 6.3 无 priority：全量竞速

当 endpoint 的全部 eligible route 都没有配置 priority 时：

- 所有 route 在 `t=0` 同时启动。
- 首个产生 `ReadySession` 的 route 胜出。
- 其他 attempt 立即取消和清理。

不能因为某条 route 通常更便宜或更接近，就在没有用户配置时隐式改变默认竞速语义。

### 6.4 有 priority：分组 staggered race

只要任一 enabled route 配置 priority，就要求该 endpoint 下所有自动 eligible route 都配置 priority；混合配置直接报错。

规则：

- 数字越小优先级越高。
- 相同 priority 的 route 同时竞速。
- 下一 priority 组在前一组尚未产生 `ReadySession` 且经过 `hedge_delay` 后启动。
- 不要求前一组完整失败或超时，避免高优先级 route 卡住全部连接。
- 如果 `hedge_delay = 0`，所有组同时启动，但同一完成时刻使用 priority 作为稳定 tie-breaker。

示例：

```text
t=0ms     direct-tls(priority=10)
t=300ms   ssh(priority=20)
t=600ms   managed-webrtc(priority=30)
```

### 6.5 胜出边界

route 只有完成以下全部阶段才能胜出：

```text
transport connected
-> daemon identity verified
-> authorization accepted
-> termx protocol Hello succeeded
-> authorization scope 满足 ConnectIntent
-> ReadySession
```

以下状态都不能提前胜出：

- TCP socket connected。
- SSH 子进程已启动。
- TLS handshake completed。
- WebRTC ICE connected。
- DataChannel open。
- Hub signaling answer 已返回。

### 6.6 竞速取消与资源回收

每轮竞速生成唯一 `AttemptID` 和 `SessionGeneration`。

首个 `ReadySession` 通过单次 compare-and-swap 成为 winner，随后：

- cancel 所有 loser context。
- 关闭 loser transport。
- 结束 SSH 子进程和远端 stdio proxy。
- 关闭 loser WebRTC PeerConnection/DataChannel。
- 关闭未使用的 managed signaling session。
- 释放未使用 Relay reservation/lease 或明确报告 cancelled outcome。
- 禁止 loser 创建 protocol attachment、history token 或长期 event subscription。

route dialer 必须完整支持 context cancellation，不能只依赖上层丢弃返回值。

### 6.7 重连

- 自动连接产生的 session 断开后，重新执行完整 eligible route 竞速。
- 用户显式 route override 产生的 session 断开后，只重试该 route。
- route identity mismatch 进入 quarantine，不继续参与自动竞速，直到配置或安全 pin 被用户明确修复。
- auth expired、host key mismatch、grant revoked 等非网络错误进入 route-scoped fail-closed 状态，不进行无界重试。

## 7. 两层选路关系

需要严格区分两个不同层级：

```text
外层 Endpoint route race
  local-unix vs ssh-stdio vs direct-tls vs managed-webrtc

内层 managed-webrtc path selection
  WebRTC direct candidate vs single Relay candidate
```

外层由客户端 `EndpointSessionManager` 统一拥有。

内层由公开 managed WebRTC adapter 根据 Companion 提供的服务准入和 route policy 执行；Cloud Companion 不知道 SSH、local 或 direct TLS 是否存在，也不能替外层选择 transport。

## 8. Daemon Identity 与授权

### 8.1 Identity

daemon `DeviceIdentity` 必须从 Cloud 专用概念提升为 daemon 全局稳定身份：

- daemon 启动时加载一次。
- local、SSH、direct TLS 和 managed WebRTC 使用同一 public identity。
- Cloud enrollment 只登记该 identity，不创建第二份 identity。
- fingerprint 变化属于安全事件，不能通过重新 resolve、修改 label 或连接成功自动接受。

protocol/transport 需要增加可验证 daemon identity challenge，使 SSH 和 direct IP route 能证明自己与 Cloud route 到达同一 daemon。

### 8.2 Route authorization

不同 route 可以使用不同授权来源：

- `local-unix`：当前用户本地 socket 权限。
- `ssh-stdio`：OpenSSH host key + 用户认证 + 远端 socket 权限。
- `direct-tls`：DeviceIdentity + CapabilityGrant。
- `managed-webrtc`：DTLS-bound DeviceIdentity + CapabilityGrant。

Authorization scope 是 session 属性，不是 endpoint 展示属性。竞速 winner 必须满足当前 `ConnectIntent` 所要求的 capability。

## 9. 指定 IP + 鉴权

指定 IP 建议使用 `direct-tls`，不新增用户名密码或长期 API token：

1. daemon 显式启用 TLS 1.3 listener，默认关闭公网监听。
2. TLS certificate 可以短期轮换，但 daemon 必须在认证握手中签名当前 TLS certificate fingerprint。
3. client 先验证 pinned DeviceFingerprint 和实际 TLS peer certificate binding。
4. identity 通过后，client 才提交 CapabilityGrant challenge proof。
5. daemon 验证 grant、expiry、revoke 和 scope 后调用 core-v2 `ServeScopedTransport`。

该模型与 managed WebRTC 的 DTLS DataChannel 授权语义一致，区别只在 transport/channel binding。

如果用户需要用户名密码、SSH key、agent 或 ProxyJump，应使用 SSH route，而不是给 direct TLS 再造一套账号系统。

## 10. 客户端运行时

### 10.1 公共 planner

引入纯领域 `RouteSelectionPlanner`：

- 输入 endpoint routes、priority、platform capability、manual override 和 `ConnectIntent`。
- 输出 race groups、start delay 和稳定 tie-breaker。
- 不做网络 IO，不读取 secure store，不修改 UI state。

Go TUI、CLI 与 Android native 使用同一组机器可读 fixture 验证规划结果。

### 10.2 TUI

TUI `EndpointManager` 应拆成：

- endpoint registry projection。
- route selection/session owner。
- endpoint-aware service router。

service router 继续按 `EndpointID` 路由 terminal/history/input/file；它不应该知道 route priority、Cloud account 或 SSH 参数。

TUI reducer 投影：

- endpoint aggregate state。
- 每条 route 的 phase/error。
- 当前 active route。
- managed route 的 observed path。
- race 中正在参与的 route。

### 10.3 App

Official App native 层拥有：

- endpoint/route store。
- credential reference resolution。
- route dialer。
- route race/session lifecycle。
- Android foreground/background/network recovery。

共享 TypeScript UI 只负责：

- endpoint 和 route 展示。
- 用户发起 connect/reconnect/select route。
- 消费 runtime event。
- 使用唯一活动 protocol session。

需要删除共享 UI 中拥有网络竞速真值的旧 local/hub orchestrator，以及 Kotlin Cloud-only `ConnectionStore` 与 TypeScript native session manager 的重复职责。

## 11. Daemon 多 Ingress 装配

同一个 daemon 进程可以并行运行：

```text
local Unix listener -----------------------> core-v2 full local scope
OpenSSH stdio proxy -> local Unix listener -> core-v2 remote OS-user scope
direct TLS listener -> remote auth --------> core-v2 capability scope
managed WebRTC answerer -> remote auth ----> core-v2 capability scope
```

所有 ingress 最终进入同一个 core-v2 server，因此看到的是同一 terminal lifecycle、history、file truth 和 daemon-local TerminalID。

## 12. Cloud Companion 初步收缩方向

Cloud Companion 只保留：

- account/device edge session 与安全刷新。
- Hub directory 和 managed device discovery。
- managed target resolve。
- signaling stream。
- Relay lease/route plan。
- 脱敏 path quality 和 connection outcome。

Cloud Companion 不应拥有：

- 客户端本地 `EndpointID` registry。
- SSH/local/direct TLS route 配置。
- 外层 route priority 或竞速。
- CapabilityGrant 或 DeviceIdentity private key。
- termx protocol client、terminal inventory 或 reconnect state machine。

后续建议从 Cloud IPC 中删除客户端本地 `EndpointID`，改用短期 `AttemptID + TargetDeviceID`；`ResolveEndpoint` 重命名为更准确的 managed target/session contract。

私有 Control Plane、Hub、Relay、Companion 的认证、离线能力、持久化恢复、写放大和故障窗口需要在本方案后续章节单独审计。

## 13. 迁移切片建议

### CONN001：模型与 contract

- 冻结 Endpoint/Route/Session/Path/ConnectIntent 模型。
- 建立 connections v2 schema。
- 建立 Go/Kotlin/TypeScript fixture。
- 更新现有 endpoint/transport 文档真值。

### CONN002：Daemon identity

- daemon 启动统一加载 DeviceIdentity。
- 增加跨 transport identity challenge/proof。
- 证明 local、SSH、managed WebRTC 返回同一 identity。

### CONN003：Route planner 与 TUI/CLI session manager

- 实现 priority/staggered/default race。
- local/SSH 先接入新 session manager。
- service router 继续保持 endpoint-aware。

### CONN004：Managed Cloud route adapter

- managed Cloud 改成普通 route dialer。
- 外层 race 不进入 Companion。
- 清理 Cloud IPC 中客户端本地 endpoint identity。

### CONN005：Direct TLS

- 实现 TLS frame transport。
- 实现 certificate-bound DeviceIdentity 和 capability handshake。
- daemon 同时开启 local/direct/cloud ingress。

### CONN006：Official App

- Android native 建立统一 endpoint route manager。
- TUI/App planner contract 对齐。
- 共享 TypeScript UI 退出网络 owner 职责。

### CONN007：删除旧路径与真实验收

- 删除 local/hub URL race、旧 session token 和重复 connection store。
- 删除 `local/cloud/local_cloud` 作为持久连接 truth 的分类。
- 完成真实 SSH、direct TLS、managed direct/single Relay 竞速与切换验收。

## 14. 必要 Harness

- 一个 endpoint 配置 SSH、direct TLS、managed WebRTC，只显示一个 machine/endpoint。
- 三条 route 注入不同延迟，TUI 与 App 选择相同 winner。
- 未配置 priority 时所有 eligible route 同时开始。
- 配置 priority 后按分组和 `hedge_delay` 启动。
- TCP/SSH/WebRTC 只完成底层连接但 identity/auth/protocol 未完成时不能胜出。
- loser SSH process、PeerConnection、Relay reservation 和 protocol transport 全部释放。
- route identity mismatch 不覆盖 endpoint pin，并从自动竞速中 quarantine。
- active transport 断开后自动重新竞速。
- 用户显式 route override 后只重连该 route。
- route 切换后 `TerminalRef` 不变，旧 generation 的 live/history/effect 回包全部拒绝。
- 两个客户端分别经 SSH 和 Cloud 操作同一 terminal，看到同一 core lifecycle/history truth。
- Cloud/Control Plane 中断时 local、SSH、direct TLS 不受影响。
- CapabilityGrant 撤销后 direct TLS 与 managed WebRTC 都 fail closed，SSH 仍只服从 SSH/OS 权限。

## 15. 当前审核决策

已经确认：

- 同一逻辑 endpoint 可以持有多条 route。
- 未配置 priority 时默认全量竞速。
- 配置 priority 时使用 priority-aware staggered race。
- winner 必须完成 identity、authorization 和 protocol Hello。
- TUI 与 App 使用同一选择语义。

私有 Cloud 专项审计已确认：

- 现有 credential 分层和 Cloud/DataChannel 安全边界总体正确，应保留。
- 登录、refresh 和 enrollment 都存在“先消费一次性凭据，后完成持久化和响应”的故障窗口，需要优先重构为事务化幂等交换。
- Hub policy 每 5 分钟全量重签和落盘，但有效离线窗口仍只有 30 分钟，且后台刷新失败会使整个 dev runtime 退出。
- Presence 声明了 30 秒 heartbeat，实现却没有 heartbeat contract，而是每 5 分钟重做 challenge/proof/HTTP stream，并反复更新 SQLite online 状态。
- managed route 取消能关闭 signaling，但不能立即释放已领取的 Relay 区域并发预算。
- Relay usage 在 durable outbox 落盘前已清空内存计数，落盘失败可丢用量；outbox 又对每个事件执行全文件重写和 fsync。
- 客户端每次重新登录都生成新 client device，会持续增加设备行、policy 记录和重复审计事件。
- Desktop Companion 每个 Hub 请求都读 OS credential store，且 account/device refresh 共用一把全局锁，需要改为内存会话缓存和按 session kind 的 singleflight。

详细结论和建议见第 16 节及后续章节。

## 16. 私有 TermX Cloud 专项审计

### 16.1 审计范围

本次核对了以下当前实现：

- `private/cloud/control-plane/`：设备安全目录、edge credential、policy、Relay lease 和 usage ledger。
- `private/cloud/hub/`：离线 edge authorization、Presence、signaling 和 verified snapshot store。
- `private/cloud/companion/`：OS credential session、refresh、Hub adapter 和本地 IPC connection。
- `private/cloud/devcloud/`：当前单区域 staging 装配、登录/enrollment flow、Relay 用量和 Web Controller 联动。
- `private/cloud/relay/`：RelayLease 离线验签、TURN credential、allocation 和 usage outbox。
- `private/cloud/mobile/android/`：Official Android account session、Keystore 和 refresh。
- `private/cloud/web-controller/`：浏览器账号、设备码批准、节点展示和 SQLite 写入。
- `remote/client/` 与 `remote/daemon/`：managed WebRTC dial、DataChannel auth、Presence agent 和 route 关闭边界。

### 16.2 应保留的安全边界

以下设计是正确的，重构时不应倒退：

- `EdgeAccessToken`、`RefreshSecret`、`DeviceIdentity`、`CapabilityGrant` 和 `RelayLease` 是不同 credential type，具有不同 audience 和 custody。
- Hub 用本地已验签 policy 与 edge token 取交集，cache miss 和 revoke 直接 fail closed，请求热路径不回查 Control Plane。
- daemon Presence 需要 fresh challenge 和 DeviceIdentity proof，仅有 daemon bearer token 不足以伪造 Presence。
- CapabilityGrant 只在 DTLS DataChannel 内由 owning daemon 验证，Hub/Relay/Control Plane 无权判断 terminal scope。
- RelayLease 绑定 account、managed session、client、daemon、region、route 和 quota，Relay 可离线验签。
- 浏览器 session 使用 HttpOnly + SameSite=Strict Cookie，变更请求还要求 same-origin 和 CSRF token。
- 已建立的 DataChannel 不经 Hub 转发 payload，Cloud 控制面故障不应主动中断已就绪 terminal session。

### 16.3 风险排序

| 优先级 | 当前实现 | 影响 |
| --- | --- | --- |
| P0 | login/refresh/enrollment 先消费一次性状态，再做设备目录、policy、session 和响应 | 中间失败或响应丢失后，用户已批准但拿不到凭据，可被迫重新登录或 enrollment |
| P0 | Relay `DrainUsage` 先清空 pending bytes，后写 durable outbox | outbox 落盘失败时用量消失；付费上线前不可接受 |
| P1 | policy 每 5 分钟无条件新增 revision、全量重签、fsync，有效窗口 30 分钟 | 写放大、同步故障使全部新 Cloud 连接在 30 分钟后失败 |
| P1 | policy 刷新失败会向 runtime error channel 报错并结束 worker | 一次后台持久化/签名故障可触发整个 dev cloud 重启 |
| P1 | Presence 每 5 分钟强制重连，却没有实际 heartbeat 协议 | 每个 daemon 每天约 288 次 challenge/proof/stream 重建，增加网络故障面和 CPU 开销 |
| P1 | Presence open/close 每次都更新 SQLite `nodes.online` | 在线瞬时态被错当持久真值，至少每 5 分钟两次写，重启还会全表置离线 |
| P1 | Relay lease 只在 5 分钟到期时释放区域并发预算 | Cloud route 在外层竞速中落败时，仍可暂时挤占后续连接的 Relay 配额 |
| P1 | 每次账号重新登录都生成新 client device ID | 设备列表膨胀、每次登录写目录/policy/SQLite/audit，且 client 仍只是 bearer token 持有者 |
| P1 | Relay usage outbox 每秒排水，Enqueue 和每个 Ack 都全文件 fsync | 持续 Relay 流量会产生高频存储写和 O(N) 队列重写 |
| P1 | Companion 每次 Hub 操作都读 OS credential store，account/device refresh 共用全局锁 | Keychain/Keystore 短暂异常会放大为所有 Cloud 请求失败，两种 session 互相阻塞 |
| P2 | CLI 的 browser/device-code 方法当前都只打印 URL；客户端每 1 秒轮询完成状态 | 方法语义不真实，故障提示弱，大量并发登录时产生无必要请求 |
| P2 | `/api/status` 固定返回 Control Plane/Hub ready | 用户和运维无法区分 Web、Control Plane、Hub、Relay 和 Companion 的真实故障 |

## 17. 认证与一次性凭据重构

### 17.1 统一 CredentialExchange 真值

login completion、refresh rotation 和 daemon enrollment completion 应共享同一个事务语义，而不是三套“删除旧记录后继续执行”的流程。

```text
CredentialExchange
  ExchangeID
  Kind                  login | refresh | daemon_enrollment
  SubjectBinding        account/device/session family
  ProofDigest
  State                 pending | approved | committed | deliverable | expired | revoked
  ResultRevision
  DeliveryReceipt       短期加密的同一结果
  DeliveryGraceUntil
```

建议事务顺序：

1. 验证 flow/refresh/proof 并锁定对应 exchange/session family。
2. 在 Control Plane 主数据库的同一事务中提交设备归属、auth epoch、refresh child hash、security revision 和 projection outbox。
3. 产生的 access/refresh response 使用短期 sealed delivery receipt 保存，只能被同一 exchange 重试读取。
4. 如果新设备需要 Hub policy 先可见，exchange 保持 `committed`，等目标 Hub 确认最低 security revision 后才进入 `deliverable`。
5. 响应丢失后，客户端使用原 exchange key 重试，服务端返回完全相同的结果，不再生成第二个 child token。
6. delivery grace 过期后删除结果密文，长期只保留 hash、family、expiry 和审计 metadata。

不应为解决响应丢失而长期允许旧 refresh secret 重放。幂等宽限必须绑定同一 session family、同一 exchange 和短期结果收据。

### 17.2 当前三个 P0 故障窗口

#### Login

`private/cloud/devcloud/control.go` 的 `handleCompleteLogin` 在开始设备注册和 session 签发前就删除已批准 flow。后续 directory、policy snapshot、Web projection、refresh store 或响应任一失败，客户端都无法继续原流程。

重构后：浏览器批准只使 exchange 进入 `approved`；设备注册、policy revision 和 session family 在事务中提交；同一 `FlowID` 可幂等取回已提交结果。

#### Refresh

`refreshStore.Rotate` 当前已删除旧 hash 并持久化新 hash，之后才检查设备状态和签发 access token。签发失败时，旧 secret 已失效，新 secret 已存在但没有交付。

重构后：旧 refresh row 锁定、设备/revoke 重验、child hash、access token claims 和 delivery receipt 在同一事务中提交。并发轮换只能观察到同一个 child result。

#### Daemon enrollment

`handleBeginEnrollment` 在 challenge 生成前就把一次性 code 标记为 claimed；`handleCompleteEnrollment` 在校验 proof 和签发 device session 前就删除 flow。

重构后：一次性 code 到 challenge 的转换必须原子提交；proof 失败可终止 exchange，服务端内部或响应故障不得把已正确证明的流程变成不可恢复状态。

### 17.3 稳定 ClientInstallationIdentity

当前 client login 每次生成随机 `client-*` ID，没有客户端公钥连续性。建议为 Desktop Companion 和 Official App 各自生成一次稳定的 `ClientInstallationIdentity`：

- private key 仅保存在 OS credential store/Android Keystore。
- `ClientDeviceID` 从 public key 或稳定 installation record 派生，重新登录不新建设备。
- login approval 页展示 display name、platform 和短 fingerprint，用户批准后 Control Plane 绑定该公钥。
- Hub 请求可增加基于 installation key 的 connection/request proof，使被窃取的 edge bearer 不能单独使用。
- logout 只删除当前 account session；“移除设备”才 revoke installation identity 和全部 refresh family。
- Web `node.enrolled` audit 只在首次建立设备归属时写入，metadata 没变化时不写 UPDATE 或重复 audit。

daemon 仍使用已有 `DeviceIdentity`；两种 identity 不应混为同一个领域类型。

### 17.4 Web 账号验证

当前 password + bcrypt + SQLite 可作为 staging 验收，不应默认当作生产账号平台。

生产建议：

- 优先接入成熟 OIDC 或 passkey/WebAuthn identity provider，不在 TermX 内继续扩展自建密码体系。
- login/register/device-code/enrollment 入口按 IP、account 和 flow family 做有界限流，但错误仍不泄漏账号、code 或设备是否存在。
- 保留 HttpOnly、SameSite=Strict、same-origin 和 CSRF 现有边界。
- 设备批准页必须明确展示请求设备和操作类型，不允许仅凭一个短码盲目批准。

## 18. 离线能力与故障隔离

### 18.1 离线不是单一状态

| 故障域 | 应继续工作 | 允许失败的操作 |
| --- | --- | --- |
| Web Controller 不可用 | 已有 edge session、Presence、Hub directory、managed direct/Relay | 账号登录、管理、付费、设备批准 |
| Control Plane/DB 不可用 | 在 edge token 和已验签 policy 有效窗口内，Hub 的列表、resolve、Presence 重建、direct；Relay 受独立 budget 窗口限制 | 新登录、refresh、enrollment、订阅变更和新的全局撤销 |
| Hub 不可用 | 已就绪 DataChannel 及 local/SSH/direct-TLS route | 新 managed Cloud resolve/signaling |
| Relay 不可用 | managed direct candidate 和非 Cloud route | 显式 relay-only；auto 只能在同一 managed attempt 允许的候选中失败 |
| Cloud Companion 不可用 | 已就绪的公开进程 DataChannel 和其他 route | 新 managed route，不得影响 local/SSH/direct-TLS |
| 客户端无互联网 | local/SSH/LAN direct-TLS 按实际网络可达性参与外层竞速 | managed Cloud route |

### 18.2 拆分 IdentityPolicy 与 RelayBudget

当前一个 `MaxStaleness=30m` 同时控制设备 ownership、client/daemon admission、Presence 和付费 Relay budget，把安全撤销、离线可用性和成本风险绑成了一个开关。

建议拆成两类签名投影：

```text
IdentityPolicy
  account/device ownership
  principal kind
  public key
  auth/revoke epoch
  managed-direct allow
  security revision

RelayBudget
  entitlement revision
  region/pool
  max lease duration/bytes/bitrate/concurrency
  shorter expiry
```

建议基线：

- 保留当前 8 小时 edge access token，`IdentityPolicy` 有效期可设为 24 小时，则 Control Plane 中断时新 managed direct 的有效离线上限仍受 token 的 8 小时限制。
- `RelayBudget` 保持更短窗口，例如 30 分钟，避免 Control Plane 长时间故障时无界消耗付费 Relay。
- 撤销延迟与离线时长不可能同时为零。默认最坏撤销传播上限应明确等于有效 admission 窗口；更高安全等级可显式选择较短离线窗口。
- Control Plane 可在可用时立即推送签名 revoke/auth-epoch delta，但不应在同步链路故障时回退到 Hub 逐请求查库。

### 18.3 Policy 发布不应靠 5 分钟全量改 revision 续命

新 snapshot envelope 建议区分：

- `SecurityRevision`：只在账号、设备、公钥、revoke 或 entitlement 的语义真值改变时递增。
- `ContentDigest`：对 canonical projection 求摘要，防止同 revision 内容被替换。
- `EnvelopeSequence/IssuedAt/ExpiresAt`：只用于对未变内容进行低频续期。

发布规则：

- 业务真值改变时立即发布。
- 内容未变时只在接近 expiry 时续期，并加 jitter，不每 5 分钟全量重写。
- Hub 对同 digest 的更新 envelope 允许新的 sequence，但拒绝 revision/digest 回滚或同 revision 不同 digest。
- snapshot store 只在实际接受新 envelope 时落盘，不做 no-op fsync。
- 后台同步失败进入 degraded 状态并指数退避重试；Hub 继续使用最后已验签投影直到它真正过期，不因一次刷新失败退出整个 runtime。

### 18.4 Hub assignment 可维护性

当前 `HubDirectoryVersion` 在 devcloud 中固定为 `1`，Desktop 和 Android 又在已有 session 时无条件拒绝 HubID 变化。这能防止未授权跳转，但也会把合法的单区域 Hub URL/instance 替换变成强制退出重登。

本阶段不建设多区域调度，但应定义最小的签名 `HubAssignment`：

- monotonic assignment version。
- 当前 HubID/origin/region。
- 可选的短期 previous assignment grace，仅用于可控维护切换。
- Companion/App 只接受签名且 version 递增的切换，不接受请求返回的任意 URL。

## 19. Presence 重构

### 19.1 当前问题

`PresenceReady.HeartbeatSeconds=30` 已返回给 daemon，但 `remote/daemon.Agent` 没有发送 heartbeat。Hub 把 Presence 硬过期设为 5 分钟，`RunContinuously` 于是每轮都重做：

```text
BeginPresence
-> fresh challenge
-> DeviceIdentity signature
-> OpenPresence HTTP stream
-> 5 分钟到期
-> 关闭并重连
```

这不是 heartbeat，而是周期性全量重新认证。

### 19.2 目标 PresenceLease

建议改为两层生命周期：

```text
PresenceSession hard lifetime
  <= min(edge token expiry, IdentityPolicy expiry)
  只在首次建立、流真正断开或 hard renewal 时重做 challenge/proof

PresenceLivenessLease
  heartbeat interval: 30s
  in-memory deadline: 90s
  heartbeat 成功只更新 Hub 内存 deadline
```

当前 HTTP response stream 是 Hub -> daemon 单向事件流，因此需要一个明确的双向语义：

- 最小实现可增加独立 `RenewPresence` RPC，由 daemon 对 `PresenceSessionID + counter + sent_at` 签名，Companion 只转发。
- Hub 按 device/session/counter 拒绝重放，成功时只修改内存 deadline。
- 后续如改用全双工 HTTP/2/WebSocket，仍保留同一签名 heartbeat 语义，不应依赖“TCP 还没关”作为存活证明。
- heartbeat 连续丢失后 Hub 在 90 秒内移除 Presence 并关闭未完成 signaling；客户端后续用 fresh proof 重建。
- policy revoke/auth epoch 更新到达 Hub 时立即关闭对应 Presence，不等 heartbeat timeout。

### 19.3 Online 不落库

Presence 是 Hub runtime truth，`nodes.online` 不应是 SQLite 真值。

建议：

- Web 节点列表查询持久设备目录后，在返回前与 Hub Presence 快照合并。
- 移除 Presence open/close 对 `SetCloudDaemonOnline` 的同步写入。
- 启动时不再执行全表 `MarkDaemonNodesOffline`；Hub 空的 runtime state 自然表示当前无在线 Presence。
- 如产品需要“最后在线”，只持久化 `last_seen_at`，按 15-30 分钟桶化合并写，不在每个 heartbeat 或 stream 重建时写。
- online/offline 变化事件可用于 WebSocket/UI 推送，但不成为第二份 Presence truth。

## 20. 持久化与写入收敛

### 20.1 写入预算表

| 领域 | 当前写入 | 目标写入 |
| --- | --- | --- |
| Hub policy | 每 5 分钟全量 JSON/token + fsync + rename | 语义变更时发布；接近 expiry 才 jittered renewal；digest no-op 不写 |
| Presence | 每 5 分钟 online=true/false 两次 SQLite UPDATE | 稳态 0 次 DB 写；可选 `last_seen` 低频合并 |
| Refresh session | 每次 issue/rotate/revoke 重写所有 hash 记录 | 以 hash/family 为 key 的行级事务；TTL 索引批量清理 |
| Security directory | 每次 account/user/device 变更重写全量 snapshot | 生产使用规范化表 + security revision + transaction outbox；JSON 仅留 staging harness |
| Client login | 每次新 device + policy + node upsert + audit | 稳定 installation device；首次 insert，metadata 实际改变才 update，audit 只记语义事件 |
| Relay usage outbox | 每秒 Enqueue 全量 fsync，每个 Ack 再全量 fsync | append-only/WAL 或 SQLite outbox；按时间/数量批量 append 和 batch ack |
| Companion credential | 每个 Hub 请求读 OS credential store | 启动/首次使用时读，进程内缓存有效 session；refresh/logout 时原子替换或清理 |

### 20.2 Relay usage P0 修正

Relay 计量需要一个可提交的 drain 边界：

```text
PrepareUsageBatch
  冻结本批 counter/sequence，后续流量进入下一批

PersistUsageBatch
  durable append，成功前本批仍可恢复

CommitUsageBatch
  只在 outbox 成功后从 authority 移除本批
```

要求：

- outbox 写失败不得清空 pending bytes 或递增不可恢复的 sequence。
- Control Plane ledger 的 `(relay_id, lease_id, sequence)` 幂等记录和 aggregate 必须在同一持久事务中提交，不能只放内存。
- response/ACK 丢失后 Relay 重发同一 batch，Control Plane 返回 duplicate success。
- 持续流量按 5-10 秒、数量阈值或 termination 触发批量，不固定每秒执行两次全量 fsync。
- 内存和磁盘队列都必须有容量上限、水位指标和明确的拒绝新 Relay allocation 策略，不得静默丢计量。

### 20.3 投影不和安全主事务耦合

Web node table、audit 和 Hub policy 都是主安全真值的 projection。当前 devcloud 在一个请求内交替更新 JSON directory、Hub snapshot、SQLite node 和 refresh file，容易出现部分成功。

生产边界应改为：

- Control Plane 主数据库事务持有 account/device/session family/security revision 真值。
- 同一事务写 projection outbox。
- Hub policy publisher 和 Web read-model worker 分别幂等消费 outbox。
- Web projection 失败只表示展示延迟，不回滚已完成的安全撤销或设备注册。
- Hub policy 未达到 exchange 所需最低 revision 时，不交付一个立即不可用的新 edge session。

## 21. Companion、TUI 与 App 交互收敛

### 21.1 Companion session runtime

- Service 内建立 account/device 分离的内存 session cache，每份会话只有一个 owner，替换和关闭时清理 secret bytes。
- OS credential store 只在进程启动/首次加载、refresh 成功、login/enrollment 完成和 logout 时访问，不作为每个 Hub request 的热路径依赖。
- account 和 device 使用独立 singleflight/mutex，不因账号刷新阻塞 daemon Presence renewal。
- 主动刷新窗口保留“刷新失败但旧 token 仍有效时继续使用”的现有正确语义，并加入 jitter 避免群体同时刷新。
- refresh 网络 timeout 与 route dial timeout 分离，避免一次 Control Plane 慢请求占满整个 route attempt 的时间预算。
- `Status` 区分 `ready`、`ready_offline`、`refresh_degraded`、`login_required`、`companion_unavailable`，但不暴露 token、Hub 内部地址或原始 adapter error。

### 21.2 登录和设备引导

交互应统一为“当前阶段 + 下一个可执行动作”，不只显示泛化 temporary error。

- CLI/TUI `browser` 方法：默认通过平台 opener 打开已验证 HTTPS URL，同时保留 URL 和 code 供手工备用。
- CLI/TUI `device-code` 方法：不自动打开浏览器，只展示短码、地址、到期和等待状态。两种 method 必须有真实行为差异。
- Official App 同时保留两条路：同机使用系统浏览器/Custom Tab 批准；跨设备使用 Web 生成 QR，App 扫码后 Web 显示设备 metadata 再批准。
- 不在 App WebView 内收集账号密码，不把 browser session Cookie 交给原生模块。
- 当前 1 秒 polling 改为 20-30 秒可取消 long-poll，或有界退避 + jitter；pending 是稳定状态，不返回泛化 503。
- flow 取消要有显式 server-side cancel，尽快释放内存容量，TTL 只作为最终保险。
- 错误映射固定到恢复动作：`login_required`、`approval_expired`、`device_revoked`、`policy_stale`、`hub_unavailable`、`relay_quota`、`companion_missing`、`identity_mismatch`。TUI 和 App 必须消费同一 contract fixture。

### 21.3 真实健康状态

`/api/status` 不应固定返回全部 ready。建议返回脱敏的独立组件状态：

- Web/BFF 是否可接受用户管理请求。
- Control Plane DB 是否可写。
- policy publisher 最后成功时间和当前 backlog。
- Hub 当前 verified policy age，以及 direct/Presence admission 是否仍在有效窗口。
- Relay 是否可接受新 lease/allocation，usage outbox 是否超水位。
- Companion 本地 session 是否仍可离线使用。

UI 可将组件状态收敛为用户可执行结果，但 diagnostics 和 metrics 必须保留真实故障域。

## 22. Managed Cloud Route Attempt 取消契约

外层 route race 接入 Cloud 后，每个 managed route 尝试必须有独立 `AttemptID`，并与客户端本地 `EndpointID` 解耦。

```text
PrepareManagedAttempt(AttemptID, TargetDeviceID, policy)
OpenSignaling(AttemptID, offer)
AcquireRelay(AttemptID)              仅在实际需要时
CancelManagedAttempt(AttemptID)
ReportManagedOutcome(AttemptID, outcome)
```

`CancelManagedAttempt` 必须：

- 幂等，只允许 owning account/client principal 取消。
- 关闭对应 Hub signaling session 和未交付事件。
- 立即删除 Hub 的 managed attempt/Relay budget reservation，不等 5 分钟 lease expiry。
- 关闭已建立的 loser PeerConnection 后，Relay 通过 allocation close 释放 TURN concurrency。
- 对已产生流量签发最终 usage event；未分配或零流量 attempt 不计费。
- 不影响同 endpoint 其他 route attempt 或其他已就绪 managed session。

Cloud route 的内部 direct/single-relay 候选仍由 managed adapter 管理；外层 planner 只看到该 route attempt 是否最终生成 `ReadySession`。

## 23. 验收门禁与可观测性

### 23.1 P0 Harness

- login 在事务提交后、HTTP 响应前断开，用同一 FlowID 重试返回同一 device/session/refresh child。
- refresh 在 child hash 提交后丢失响应，使用旧 secret 的宽限重试只取回同一 sealed result；宽限窗口后重放失败。
- 两个并发 refresh 请求只产生一个 child session，不会一个成功、一个把 family 再次旋转。
- enrollment 在 challenge 生成或最终 session 交付的任意注入故障后，不吞掉已正确使用的 code/proof。
- Relay outbox 写失败后 pending bytes 仍在；重试持久化同一 sequence/body，Control Plane 响应丢失后 duplicate apply 不重复计费。

### 23.2 离线与故障 Harness

- Control Plane/DB 停止后，在有效 edge token + IdentityPolicy 窗口内，新的 Hub list/resolve/Presence rebuild/managed direct 仍成功。
- 同样故障下，新 login/refresh/enrollment 明确失败，不伪造本地成功。
- RelayBudget 过期后新 Relay 失败，但 managed direct 不因此失败。
- Hub/Companion 在 DataChannel `ReadySession` 建立后停止，已有 terminal protocol 仍继续；新 managed route 失败并由外层竞速选择其他 route。
- policy renewal 失败不退出 Hub/runtime，指数退避后恢复；只在已验签 policy 真正过期时停止新 admission。

### 23.3 Presence 与写入 Harness

- daemon 稳定运行 24 小时期间不重建 PresenceSession，heartbeat 只修改 Hub 内存 deadline。
- heartbeat 中断后在配置的 liveness window 内移除 Presence，重放 counter 不能延长 deadline。
- Presence 建立、heartbeat、断开和重建过程中 SQLite 写入数为 0；可选 `last_seen` 只按时间桶写一次。
- 无安全语义变更时，policy publisher 不递增 `SecurityRevision`，不每 5 分钟落盘。
- 同一 ClientInstallationIdentity 重新登录 100 次，设备行仍为 1，不产生 100 条 `node.enrolled` audit。
- 外层 route race 取消 managed Relay loser 后，Hub budget reservation 和 TURN allocation 在限定时间内都回到基线。

### 23.4 必要指标

- `cloud_policy_age_seconds{kind=identity|relay_budget}`
- `cloud_policy_publish_total{result,reason}`
- `credential_exchange_total{kind,result}` 与 `credential_exchange_replay_total{result}`
- `cloud_refresh_total{kind,result}` 与 refresh latency
- `hub_presence_sessions`、`hub_presence_reconnect_total{reason}`、heartbeat timeout
- `cloud_store_writes_total{store,operation}` 与 fsync latency
- `relay_usage_outbox_events/bytes/oldest_age_seconds`
- `managed_attempt_total{route,result}` 与 loser cancel latency
- `companion_session_state{kind,state}`

指标 label 不得包含 account ID、device ID、IP、terminal ID、credential body 或用户输入的 endpoint label。

## 24. 建议实现切片

以下切片只是审核后的候选队列；在用户确认前不写入 `workflow.md`，不与当前 `CLOUD018` 混合实现。

### CLOUDR001：幂等交换 contract 与故障注入 harness

- 定义 `CredentialExchange`、session family、delivery receipt 和 stable error code。
- 先建 login/refresh/enrollment 响应丢失与并发重试 harness。

### CLOUDR002：Control Plane 事务真值

- 把 security directory、refresh family、exchange 和 projection outbox 收敛到同一事务 store contract。
- staging 先可使用 SQLite 真实跨进程持久化，不再以多个全量 JSON 文件组合模拟事务。

### CLOUDR003：Stable ClientInstallationIdentity

- Desktop Companion 和 Android 生成稳定 installation key。
- login approval、client device directory、edge token 和 Hub proof 改为公钥绑定。
- 删除每次登录新建随机 client device 的路径。

### CLOUDR004：Policy 分层与离线窗口

- 拆分 IdentityPolicy 和 RelayBudget。
- 引入 semantic revision、content digest、renewal sequence 和 jittered publisher。
- 刷新失败进入 degraded/retry，不停整个 runtime。

### CLOUDR005：Presence heartbeat 与在线投影

- 实现签名 heartbeat + 内存 liveness lease。
- 删除 5 分钟强制重建和 SQLite online 热路径写入。
- Web 读模型在查询时合并 Hub Presence。

### CLOUDR006：Managed attempt 取消与 Relay 计量

- 增加 `CancelManagedAttempt` 和 Relay reservation 即时释放。
- 把 usage drain 改为 prepare/persist/commit，outbox 改为批量 WAL/SQLite。
- Control Plane ledger 持久化幂等键和 aggregate。

### CLOUDR007：Companion/App/TUI session 与交互

- 实现 session cache、per-kind singleflight、refresh jitter 和 degraded status。
- 统一 browser/device-code/QR/long-poll 交互与错误 fixture。
- 确保 Companion/Hub 退出不关闭已就绪 DataChannel。

### CLOUDR008：统一 Endpoint Route 接入与真实验收

- managed Cloud 作为普通 route dialer 接入第 6 节外层竞速。
- 验收 Cloud loser 取消、Control Plane 离线、Hub/Relay 故障和 SSH/direct/local winner。
- 删除 Cloud-only endpoint/session owner 和重复 App/TUI connection state machine。

## 25. 建议审核结论

建议按以下原则通过方案：

1. 同一 daemon 是一个 Endpoint，local、SSH、direct TLS 和 managed Cloud 是多条 Route，默认竞速，显式 priority 时分组 hedge。
2. Cloud 不获得外层 route 选择权，只实现一条可取消的 managed route attempt。
3. 在扩展 route race 前，先修复 login/refresh/enrollment 的事务幂等性和 Relay usage 丢失窗口。
4. 保留现有 credential 分层、Hub 离线验证和 CapabilityGrant 端到端边界。
5. 用 8 小时 edge token 和独立 IdentityPolicy 支持有界 Control Plane 离线，RelayBudget 保持更短窗口；撤销上限作为明确产品安全参数。
6. Presence 稳态只是 Hub 内存 heartbeat lease，不是每 5 分钟重新 enrollment/认证，也不在每次存活变化时写数据库。
7. 客户端使用稳定 ClientInstallationIdentity，不在每次登录创建新 cloud device。
8. policy、refresh、security directory 和 Relay usage 的生产持久化使用行级事务/outbox/WAL，不使用高频全量 JSON fsync。
9. TUI、CLI 和 Official App 共享同一 route planner、Cloud error、login/enrollment 和 reconnect contract fixture。
10. 已就绪 DataChannel 的生命周期不与 Companion、Hub、Web 或 Control Plane 的后续可用性绑定。
