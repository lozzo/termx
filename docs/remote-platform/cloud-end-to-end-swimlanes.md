# TermX Cloud 全链路泳道与 Control Plane 降载审计

状态：CLOUD017 当前实现审计基线

日期：2026-07-15

## 1. 结论

TermX Cloud 的连接热路径已经基本从 Web Controller 和 Control Plane 移到 Hub：客户端登录后，设备目录、endpoint resolve、direct signaling 和 single Relay lease 都只访问 Hub；WebRTC 建立后，direct 数据完全绕过云，Relay 也只转发 DTLS 密文。

当前仍有三个关键缺口：

1. daemon 每次建立或重建 Presence 都要同步访问 Control Plane 两次，Hub 还没有按既定目标直接使用本地 public key 投影验证 fresh DeviceProof。
2. staging 的 account/device edge session、设备目录和 daemon public key 投影仍主要在进程内存中；Cloud supervisor 重启后需要重新登录和 enrollment，Web SQLite 中的节点展示记录不足以恢复安全真值。
3. edge policy 每 5 分钟刷新，Hub 最大接受 30 分钟陈旧快照。Control Plane 或同步链路中断超过窗口后，新连接按设计 fail closed；这保护撤销传播边界，但可用性窗口仍需产品化配置。

Web Controller 不应、目前也不会参与每次连接。它只应出现在用户主动进行身份与管理操作时。Control Plane 应是低频持久真值和后台同步 owner，不应出现在 Presence 重连、direct 或 Relay 的请求热路径。

## 2. 参与者与部署关系

当前 staging 中，React Web Controller 由 Nginx 静态托管，浏览器 API 与 Control Plane HTTP API 运行在同一个 Cloud supervisor 进程内。图中仍拆成两个泳道，因为它们承担不同产品职责：

- Web Controller：用户可见的登录、批准、账号、订阅和设备管理入口。
- Control Plane：账号、设备 ownership、edge credential、策略 revision 和持久业务真值。
- Hub：本地授权投影、Presence、目录、resolve、signaling 和区域 Relay lease。
- Relay：验证短租约并转发端到端加密字节。
- daemon：DeviceIdentity、CapabilityGrant、terminal/file truth 的 owner。

## 3. 调用频率矩阵

| 场景 | Web Controller | Control Plane | Hub | Relay | daemon |
| --- | --- | --- | --- | --- | --- |
| 浏览器注册/登录 | 用户主动 | 读写账号数据库 | 不参与 | 不参与 | 不参与 |
| TUI/CLI 登录批准 | 用户主动批准一次 | 创建 Flow、签发 edge session | 登录后使用 | 不参与 | 不参与 |
| 手机 QR 激活 | 用户主动创建和批准 | 创建/认领 Flow、签发 edge session | 激活后使用 | 不参与 | 不参与 |
| daemon enrollment | 用户主动生成短码 | 验证 DeviceIdentity、登记 ownership、签发 device session | 接收后台投影 | 不参与 | 生成 proof |
| daemon Presence 首次建立/重连（当前） | 不参与 | **每次两次同步调用** | 打开长连接 | 不参与 | fresh proof |
| 设备列表刷新 | 不参与 | 不参与 | 每次刷新 | 不参与 | 不参与 |
| direct 新连接 | 不参与 | 不参与 | resolve + signaling | 不参与 | answer + E2E auth |
| explicit Relay 新连接 | 不参与 | 不参与 | resolve + lease + signaling | allocate/转发 | answer + E2E auth |
| terminal/file 数据 | 不参与 | 不参与 | 不参与 | direct 时不参与；Relay 时只搬密文 | 每个 protocol 请求 |
| 订阅/撤销/踢线 | 用户或后台动作 | 更新持久真值 | 后台接收签名 revision | 可撤销新 lease | capability 撤销仍由 daemon |
| Relay 用量 | 不参与 | 异步接收幂等事件 | 预算预留 | durable usage event | 不参与 |
| policy 保鲜（当前 staging） | 不参与 | 每 5 分钟发布完整快照 | 原子应用 | 不参与 | 不参与 |

## 4. 当前真实实现泳道

### 4.1 TUI/CLI 账号登录

```mermaid
sequenceDiagram
    autonumber
    participant U as 用户
    participant C as TUI/CLI + Companion
    participant CP as Control Plane
    participant W as Web Controller
    participant DB as Account DB
    participant S as OS Credential Store
    participant H as Hub

    C->>CP: BeginLogin(client metadata)
    CP-->>C: user code + verification URL + private flow ID
    C-->>U: 显示网页登录地址和短码
    U->>W: 登录并核对短码
    W->>CP: ApproveDeviceLogin(browser session, user code)
    CP->>DB: 读取账号并登记 client device
    CP->>CP: 发布新 edge policy revision
    CP-->>W: approved
    loop 客户端有界轮询
        C->>CP: CompleteLogin(private flow ID)
    end
    CP-->>C: signed edge token + signed HubDirectory
    C->>S: 保存 account edge session
    C->>H: 后续目录/连接请求携带 edge token
    H->>H: 离线验签并与本地 policy 取交集
```

Web Controller 只参与用户批准。登录完成后，它不参与设备刷新或连接。当前 edge session TTL 为 8 小时；代码尚未形成独立 refresh-token 闭环，过期后的产品行为仍偏向重新登录。

### 4.2 Official App Web QR 激活

```mermaid
sequenceDiagram
    autonumber
    participant U as 用户
    participant W as Web Controller
    participant CP as Control Plane
    participant A as Official App Native
    participant KS as Android Keystore
    participant H as Hub

    U->>W: 创建手机激活二维码
    W->>CP: CreateMobileActivation(browser account)
    CP-->>W: 一次性 QR locator + user code
    U->>A: 扫描 QR
    A->>CP: ClaimActivation(QR locator, phone metadata)
    CP-->>A: waiting for Web approval
    W->>CP: InspectActivation
    CP-->>W: 手机名称、平台、验证码
    U->>W: 再次批准
    W->>CP: ApproveDeviceLogin
    CP->>CP: 登记 client 并发布 policy revision
    A->>CP: AwaitActivation(private native flow credential)
    CP-->>A: signed edge token + HubDirectory
    A->>KS: 保存 edge session
    A->>H: 后续目录/连接请求
```

二维码只定位活动 Flow。高熵 flow credential 留在原生层，WebView、Web Controller 和二维码都不能单独领取 edge session。

### 4.3 daemon enrollment

```mermaid
sequenceDiagram
    autonumber
    participant U as 用户
    participant W as Web Controller
    participant CP as Control Plane
    participant DB as Device Directory
    participant DC as Daemon Companion
    participant D as Public daemon
    participant S as OS Credential Store
    participant H as Hub Policy Cache

    U->>W: 生成一次性 enrollment code
    W->>CP: CreateEnrollment(account)
    CP-->>W: 两分钟单次短码
    U->>DC: termx cloud enroll CODE
    DC->>CP: BeginEnrollment(code, public key, metadata)
    CP-->>DC: fresh challenge + flow ID
    DC-->>D: challenge
    D->>D: DeviceIdentity private key 签名
    D-->>DC: DeviceProof
    DC->>CP: CompleteEnrollment(flow ID, proof)
    CP->>DB: 保存 DeviceID/account/public key/revoke 状态
    CP->>CP: 发布新 signed policy revision
    CP-->>DC: signed daemon edge session + HubDirectory
    DC->>S: 保存 device session
    CP-->>H: 后台设备授权投影
```

enrollment 是低频 Control Plane 操作，保留在 Control Plane 是正确的。问题不在首次注册，而在注册后 Presence 重连仍同步回到 Control Plane。

### 4.4 daemon Presence 建立与重连：当前实现缺口

```mermaid
sequenceDiagram
    autonumber
    participant D as Public daemon
    participant DC as Daemon Companion
    participant CP as Control Plane
    participant H as Hub
    participant P as Hub Policy Cache

    Note over DC,CP: 当前每次 Presence 建立或重连都走这里
    DC->>CP: BeginPresence(device edge session)
    CP-->>DC: PresenceSessionID + fresh challenge
    DC-->>D: challenge
    D->>D: DeviceIdentity 签名
    D-->>DC: DeviceProof
    DC->>CP: AcquirePresenceAdmission(DeviceProof)
    CP->>CP: 验证 proof 并签 presence-only Hub ticket
    CP-->>DC: short Hub admission
    DC->>H: OpenPresence(ticket, DeviceProof)
    H->>P: 查 device ownership/revoke
    H->>H: 验 ticket 并打开长连接
    H-->>DC: Presence ready + heartbeat interval
    loop Presence 长连接存活
        H-->>DC: signaling offer
        DC-->>H: answer/error
    end
```

这里与既定 `hub-edge-control-plan.md` 目标不一致。Hub 已经持有 DeviceID、AccountID 和 public key 投影，却仍依赖 Control Plane 创建 challenge 和 admission。结果是：

- active Presence 存活时 Control Plane 中断不影响新连接；
- daemon 网络切换、Companion 重启、Hub 重启或 Presence 断线后，重新上线会同步依赖 Control Plane；
- device edge session 过期后缺少独立刷新路径，可能需要重新 enrollment。

### 4.5 CapabilityGrant 配对

```mermaid
sequenceDiagram
    autonumber
    participant D as Owning daemon
    participant O as daemon owner
    participant C as TUI/App client
    participant S as Client Secure Store
    participant W as Web Controller
    participant CP as Control Plane
    participant H as Hub

    O->>D: termx pair create(scope, TTL)
    D->>D: 用 DeviceIdentity 签 CapabilityGrant
    D-->>O: QR 或 owner-only bundle
    O->>C: 本地扫描 / SSH 管道 / 可信文件传递
    C->>C: 验签、校验 fingerprint/TTL/scope
    C->>S: 保存 grant_ref -> raw grant
    C--xW: 不上传 grant
    C--xCP: 不上传 grant
    C--xH: signaling 不携带 grant
```

账号相同只决定 Hub 是否允许发现和建立托管连接，不授予 terminal 权限。CapabilityGrant 始终由 daemon 签发，Web Controller 和 Control Plane 不能代替它。

### 4.6 managed direct 新连接

```mermaid
sequenceDiagram
    autonumber
    participant C as TUI/App
    participant H as Hub
    participant P as Hub Policy Cache
    participant DC as Active daemon Presence
    participant D as Public daemon
    participant Core as core-v2
    participant CP as Control Plane

    C->>H: ListManagedDevices(edge token)
    H->>P: 离线 token/account/device 检查
    H-->>C: 同账号 daemon + Presence 状态
    C->>H: ResolveEndpoint(target DeviceID)
    H->>P: 本地 ownership/revoke/entitlement
    H-->>C: managed session ID + ICE metadata
    C->>H: CreateSignalingSession(offer, target)
    H->>P: 本地准入
    H->>DC: offer on owning Presence
    DC-->>D: offer
    D-->>DC: answer
    DC->>H: CompleteSignalingOffer(answer)
    H-->>C: answer
    C->>D: direct ICE + DTLS DataChannel
    D-->>C: DeviceHello + DTLS binding proof
    C->>D: CapabilityOpen(raw grant，仅 DTLS 内)
    D->>D: 验 grant/expiry/revoke/scope
    D->>Core: ServeScopedTransport(scope)
    C->>Core: terminal/file protocol
    Note over CP: 整个新连接不参与
```

这是当前已经达到的主要降载目标。Control Plane、Web Controller 和数据库都不在 direct 请求热路径。

### 4.7 explicit single Relay 新连接

```mermaid
sequenceDiagram
    autonumber
    participant C as TUI/App
    participant H as Hub
    participant B as Hub Regional Budget
    participant DC as Active daemon Presence
    participant R as Relay/TURN
    participant D as Public daemon
    participant CP as Control Plane

    C->>H: ResolveEndpoint + relay_only intent
    H->>B: 本地账号预算和并发检查
    C->>H: AcquireRelayLease(managed session, target)
    H->>B: 原子预留区域预算
    H-->>C: client-specific TURN credential
    H->>DC: offer + daemon-specific TURN credential
    C->>R: TURN allocate(client credential)
    D->>R: TURN allocate(daemon credential)
    C->>R: DTLS ciphertext
    R->>D: 原样转发 ciphertext
    D-->>C: DeviceHello / CapabilityOpen / protocol
    R-->>CP: durable outbox 异步补报 signed usage
    Note over CP: 不参与 lease 请求；只异步接收用量
```

single Relay lease 已由 Hub 的区域委派 authority 签发。当前 SmartRoute、路径质量上报和 connection outcome API 仍明确 deferred/fail closed，不构成隐藏的逐连接 Control Plane 流量。

### 4.8 订阅、撤销与后台 policy 同步

```mermaid
sequenceDiagram
    autonumber
    participant U as 用户/支付/管理员
    participant W as Web Controller
    participant CP as Control Plane
    participant DB as Persistent DB
    participant H as Hub Sync Worker
    participant M as Hub Memory Policy
    participant C as Client
    participant D as Daemon Presence

    U->>W: 订阅变更 / 移除 client / 移除 daemon
    W->>CP: authenticated mutation
    CP->>DB: 持久事务 + auth epoch/revision
    CP->>H: signed snapshot/delta revision N+1
    H->>H: 验签、schema、单调 revision
    H->>M: 原子发布 N+1
    alt client 被撤销
        H-->>C: 后续目录/resolve/signaling 拒绝
    else daemon 被撤销
        H-->>D: 关闭 Hub-owned Presence/signaling
        H-->>C: 后续 target unavailable
    end
```

当前 staging 不是独立分布式同步服务，而是同进程每 5 分钟重新签发完整 snapshot。生产实现需要真正的 snapshot/delta worker、持久 verified snapshot/WAL 和断档恢复。

## 5. Control Plane 故障窗口

```mermaid
sequenceDiagram
    autonumber
    participant CP as Control Plane unavailable
    participant C as Client
    participant H as Hub
    participant P as Last verified policy
    participant D as Existing daemon Presence

    Note over CP: API/数据库/同步链路中断
    alt edge token 有效 + Presence 存活 + policy 未超过 30 分钟
        C->>H: 新 direct/Relay 请求
        H->>P: 本地授权
        H->>D: signaling offer
        D-->>H: answer
        H-->>C: 连接成功
    else daemon Presence 断线需要重建（当前）
        D->>CP: BeginPresence / Admission
        CP--xD: unavailable
        D-->>H: 无法重新上线
    else client edge session 到期
        C->>CP: 重新登录/刷新
        CP--xC: unavailable
        C-->>C: 无法建立新的 managed 连接
    else policy 超过 max staleness
        H->>P: stale
        H-->>C: fail closed
    end
```

| 故障条件 | 当前结果 | 是否符合目标 |
| --- | --- | --- |
| Web Controller 不可用，已有 edge session | 目录和连接继续 | 符合 |
| Control Plane 不可用，已有 Presence 和有效 policy | 新 direct/Relay 继续 | 符合 |
| Control Plane 不可用，daemon Presence 需要重建 | daemon 无法重新上线 | **不符合** |
| Cloud supervisor/Hub 重启 | 内存目录和 session 丢失，需重新登录/enrollment | **不符合生产目标** |
| policy 同步中断超过 30 分钟 | 新连接 fail closed | 安全正确，窗口需产品决策 |
| 已建立 direct DataChannel | 通常继续，不依赖云 | 符合 |
| 已建立 Relay DataChannel | Relay lease/传输存活期内继续 | 符合 |

## 6. 目标 Hub 自治泳道

```mermaid
sequenceDiagram
    autonumber
    participant DB as Control Plane DB
    participant CP as Control Plane Sync
    participant H as Regional Hub
    participant WAL as Verified Snapshot/WAL
    participant D as Daemon + Companion
    participant C as TUI/App
    participant R as Relay

    Note over CP,D: 低频：登录、refresh、首次 enrollment、订阅/撤销
    CP->>H: signed snapshot/delta(account/device/public key/budget/revoke)
    H->>WAL: 原子保存 verified revision
    H->>H: 发布内存 policy

    D->>H: BeginPresence(DeviceID, daemon edge credential)
    H->>H: 离线验 credential + 本地 public key/revoke
    H-->>D: fresh one-time challenge
    D->>D: DeviceIdentity 签名
    D->>H: OpenPresence(DeviceProof)
    H->>H: 本地验 proof，建立 Presence

    C->>H: directory/resolve/offer(edge token)
    H->>H: 本地授权 + active Presence
    H->>D: offer
    D-->>H: answer
    H-->>C: answer
    C->>D: direct DTLS + daemon capability auth

    opt direct 不可用且预算允许
        C->>H: AcquireRelayLease
        H-->>C: delegated short lease
        C->>R: DTLS ciphertext
        R->>D: ciphertext
    end

    H-->>CP: 异步批量 audit/usage/health
    Note over CP: 不参与 Presence 重连或每次连接
```

目标架构仍保留 Control Plane，但把它限制为：

- 用户身份、账号、首次 enrollment、refresh、订阅、撤销和全局策略的持久 owner；
- 向 Hub 推送签名 snapshot/delta；
- 接收异步批量 audit/usage；
- 不参与 Presence challenge、Presence admission、resolve、signaling 或 Relay lease 请求。

## 7. 改造优先级

### P0：移除 Presence 重连对 Control Plane 的同步依赖

- 把 fresh Presence challenge owner 从 Control Plane 移到 Hub。
- Hub 使用已同步的 daemon public key、DeviceID、AccountID、revoke 和 auth epoch 本地验证 DeviceProof。
- daemon edge credential 只证明云服务身份，不能替代 DeviceIdentity proof。
- cache miss、stale policy、proof replay 和 revoked device 继续 fail closed，禁止请求线程回源。

### P0：持久化 Control Plane 设备安全真值

- 持久保存 DeviceID、account ownership、public key、kind、revoke、auth epoch 和 policy revision。
- Web 节点展示表不能作为设备安全真值；它缺少 public key 和完整 credential lifecycle。
- Cloud/Hub 重启后应能恢复 verified directory/policy，不要求用户重新 enrollment。

### P0：补 account/device session refresh contract

- account 和 daemon device session 都需要明确 refresh/rotation，而不是固定 8 小时后重新登录或 enrollment。
- refresh 访问 Control Plane 是低频允许行为；Hub 不持有 refresh token，也不能自行延长账号权限。
- device refresh 必须重新绑定 DeviceIdentity proof 或安全的 rotation proof，不能仅凭旧 bearer 永久续期。

### P1：生产 Hub snapshot/delta worker

- Control Plane 预计算签名 snapshot/delta，数据库变更时推送；正常保鲜不应每 5 分钟全表重读。
- Hub 持久化 verified snapshot/WAL，重启先恢复再后台追 revision。
- `max_staleness` 是最大撤销传播窗口，需要按套餐/安全等级明确配置和监控，不能无限延长。

### P1：Web Controller 与 Control Plane 资源隔离

- React 静态资源可由 CDN/Nginx 提供，不消耗 Control Plane 进程。
- 浏览器 API 可以保持同一安全域，但应有独立限流、连接池和只读 projection，避免 Landing/账号查询挤占 Hub 同步和 credential 签发资源。
- Web Controller 故障不得影响已有 edge session、Presence 或连接。

### P2：异步 telemetry

- SmartRoute 恢复后，质量与 outcome 先写 Hub/客户端有界队列，再批量异步进入 Control Plane。
- 连接成功不能等待 telemetry ACK；telemetry 失败不能触发 transport fallback 或改变 terminal capability。

## 8. 不应做的“降载”方式

- 不把 Control Plane 数据库复制到每个连接请求的本地 fallback。
- 不让 Hub 在 cache miss 时同步查询 Control Plane。
- 不把 edge token 变成永久 token，也不无限延长 stale policy。
- 不让 Web Controller 代发或保存 CapabilityGrant。
- 不把 terminal inventory、文件 metadata 或 DataChannel payload同步到 Hub。
- 不用客户端自报“订阅有效”或“daemon 在线”替代签名 policy 和 Hub Presence。

## 9. 最终判断

Web Controller 的参与频率已经合理：它只处理用户主动管理动作，不参与连接。Control Plane 的 direct/Relay 热路径降载也已经完成，但 daemon Presence 和重启恢复仍未达到目标架构。

下一阶段最有价值的改造不是继续压缩 Web 页面请求，而是完成“Hub 本地 fresh proof Presence + 持久设备目录 + account/device refresh”。完成这三项后，Control Plane 可以在较长故障窗口内不影响 daemon 重新上线和已有账号的新连接，同时数据库压力仍主要来自低频身份/策略变更和后台批量同步。
