# TermX Remote Platform 网络图解

状态：目标架构图；当前真实实现与差异见 `cloud-end-to-end-swimlanes.md`

日期：2026-07-11

## 1. 阅读方式

这些图描述目标架构，不描述当前 ME012 原型实现。核心结论：

- local 和 SSH 完全绕过 TermX 托管云。
- Direct 使用 daemon embedded signaling，SSH 使用 tunnel 内 signaling，只有 managed WebRTC 使用 Hub signaling；任何 signaling 服务都不承载 terminal protocol。
- 基础 WebRTC 策略优先 direct；失败且存在有效 `RelayLease` 时才走 Relay。启用 SmartRoute 后，direct 可达但质量较差时也可以主动选择 Relay。
- direct、single Relay 和 Relay Mesh 最终都建立同一种 DTLS DataChannel，运行同一种端到端 capability handshake 和 termx protocol。
- Control Plane、Hub 和 Relay 都不能看到原始 `CapabilityGrant`。

## 2. 全部连接方式

```mermaid
flowchart LR
    subgraph Client["客户端（公开源码）"]
        TUI["TUI / CLI"]
        APP["手机 App"]
        CR["client/runtime"]
        TUI --> CR
        APP --> CR
    end

    subgraph FreePaths["免费路径：不依赖 TermX 云"]
        LOCAL["local unix socket"]
        DIRECT["daemon embedded signaling<br/>ICE-TCP"]
        SSH["Go SSH direct-tcpip<br/>loopback ICE-TCP"]
    end

    subgraph ManagedCloud["TermX 托管云（私有源码）"]
        CP["Control Plane<br/>账号 / 设备目录 / Entitlement"]
        HUB["Regional Hub<br/>Presence / SDP / ICE"]
        RELAY["Managed Relay / TURN<br/>租约校验 / 加密字节转发 / 计量"]
    end

    subgraph Daemons["目标设备（公开源码）"]
        D1["本机 termx daemon<br/>独立 core-v2"]
        D2["SSH 可达 termx daemon<br/>独立 core-v2"]
        D3["WebRTC 可达 termx daemon<br/>DeviceIdentity / Capability owner<br/>独立 core-v2"]
    end

    EM -->|"local transport"| LOCAL --> D1
    EM -->|"Direct WebRTC TCP"| DIRECT --> D2
    EM -->|"SSH WebRTC TCP"| SSH --> D2

    EM -->|"TLS：登录 / 申请托管 session"| CP
    D3 -->|"TLS：设备注册 / presence 申请"| CP
    CP -->|"client admission / RelayLease"| EM
    CP -->|"daemon admission / RelayLease"| D3
    EM <-->|"TLS：offer / answer / ICE + admission"| HUB
    D3 <-->|"TLS：presence / offer / answer / ICE + admission"| HUB
    CP -.->|"admission 验签 keyset"| HUB
    CP -.->|"lease 验签 keyset / quota policy"| RELAY

    EM -.->|"优先：WebRTC direct + DTLS"| D3
    EM ==>|"直连失败：WebRTC DTLS"| RELAY
    RELAY ==>|"仅转发端到端加密字节"| D3
    RELAY -->|"签名 UsageEvent"| CP

    EM -.->|"逻辑 E2E：DeviceHello / CapabilityOpen / termx protocol"| D3

    classDef openNode fill:#ecfdf5,stroke:#15803d,color:#052e16;
    classDef privateNode fill:#fff7ed,stroke:#c2410c,color:#431407;
    classDef pathNode fill:#eff6ff,stroke:#1d4ed8,color:#172554;
    class TUI,APP,EM,D1,D2,D3 openNode;
    class CP,HUB,RELAY privateNode;
    class LOCAL,DIRECT,SSH pathNode;
```

图中最后一条逻辑 E2E 连接不是额外网络通道。它运行在 direct path 或 Relay path 已建立的同一个 DTLS DataChannel 上。

## 3. Managed WebRTC 网络拓扑

```mermaid
flowchart LR
    subgraph ClientNet["客户端网络 / NAT A"]
        C["TUI 或 App<br/>WebRTC Adapter"]
        CS["本地安全存储<br/>grant_ref -> CapabilityGrant"]
        CS --> C
    end

    subgraph PrivateCloud["TermX 托管网络"]
        CP["Control Plane"]
        H["Hub"]
        R["Relay / TURN"]
    end

    subgraph DaemonNet["目标网络 / NAT B"]
        D["termx daemon<br/>DeviceIdentity + Grant verifier"]
        V["core-v2<br/>ServeScopedTransport"]
        D --> V
    end

    C -->|"AccountAccessToken / session request"| CP
    D -->|"设备注册证明 / presence request"| CP
    CP -->|"HubAdmissionTicket"| C
    CP -->|"HubAdmissionTicket"| D
    C <-->|"SDP / ICE + admission"| H
    D <-->|"Presence / SDP / ICE + admission"| H

    C -.->|"Path A：direct candidate"| D
    CP -->|"RelayLease"| C
    CP -->|"RelayLease"| D
    CP -->|"Relay policy"| R
    C ==>|"Path B：relayed candidate / DTLS"| R
    R ==>|"不可解密的 WebRTC 流量"| D
    R -->|"UsageEvent"| CP

    C -.->|"仅在 DTLS 内提交 CapabilityOpen"| D

    classDef openNode fill:#ecfdf5,stroke:#15803d,color:#052e16;
    classDef privateNode fill:#fff7ed,stroke:#c2410c,color:#431407;
    class C,CS,D,V openNode;
    class CP,H,R privateNode;
```

### 服务可见性

| 组件 | 可以看到 | 绝不能看到或决定 |
| --- | --- | --- |
| Control Plane | 账号、设备目录、entitlement、ManagedSession、Relay 用量 | 原始 grant、terminal list/content、terminal scope |
| Hub | admission、DeviceID、presence、SDP/ICE、signaling session | 原始 grant、terminal inventory、termx protocol frame |
| Relay | RelayLease、连接 metadata、加密字节数和时长 | DataChannel 明文、grant、terminal scope |
| Client | endpoint pin、原始 grant、daemon 返回的 protocol 数据 | daemon 私钥 |
| Daemon | DeviceIdentity 私钥、grant 签发/撤销、terminal truth | 用户计费数据库 |

## 4. Direct P2P 连接时序

```mermaid
sequenceDiagram
    autonumber
    participant C as TUI / App
    participant CP as Control Plane
    participant H as Hub
    participant D as termx daemon
    participant Core as core-v2

    C->>CP: ResolveEndpoint(target DeviceID)
    CP-->>C: Hub assignment + target presence metadata
    C->>CP: IssueHubAdmission(ManagedSession)
    CP-->>C: short-lived client admission
    D->>CP: refresh daemon admission
    CP-->>D: short-lived daemon admission

    C->>H: offer + ICE + client admission
    H->>D: route offer + ICE
    D->>H: answer + ICE + daemon admission
    H-->>C: route answer + ICE

    C->>D: WebRTC direct ICE + DTLS
    D-->>C: DeviceHello + signed DTLS fingerprint binding
    C->>C: verify pinned DeviceFingerprint and actual DTLS peer
    C->>D: CapabilityOpen(grant + one-time challenge proof)
    D->>D: verify signature / expiry / revoke / proof / scope
    D-->>C: CapabilityAccepted
    D->>Core: ServeScopedTransport(scope, DataChannel)
    C->>D: termx protocol Hello / List / Attach
    D->>Core: dispatch requests inside accepted scope

    Note over C,D: CapabilityGrant only appears inside this E2E DTLS DataChannel
    Note over CP,H: Control Plane and Hub never receive terminal capability
```

## 5. Relay fallback 连接时序

```mermaid
sequenceDiagram
    autonumber
    participant C as TUI / App
    participant CP as Control Plane
    participant H as Hub
    participant R as Relay / TURN
    participant D as termx daemon

    C->>H: exchange offer / answer / ICE
    C-xD: direct ICE candidates fail within policy window
    C->>CP: IssueRelayLease(ManagedSession, region)

    alt entitlement and quota allow Relay
        CP-->>C: client RelayLease credentials
        CP-->>D: daemon RelayLease credentials
        C->>R: allocate with lease-derived TURN credential
        D->>R: accept matching relayed candidate
        C->>R: WebRTC DTLS ciphertext
        R->>D: forward unchanged DTLS ciphertext
        D-->>C: DeviceHello
        C->>D: CapabilityOpen
        D-->>C: CapabilityAccepted
        C->>D: termx protocol over encrypted DataChannel
        R->>CP: signed idempotent UsageEvent
    else no entitlement, expired lease, or quota exhausted
        CP-->>C: stable Relay entitlement error
        C->>C: endpoint offline, keep local / SSH / other endpoints unchanged
    end

    Note over C,D: Relay forwards ciphertext only, E2E auth is identical to direct path
```

## 6. 凭据边界图

```mermaid
flowchart LR
    A["AccountAccessToken"] -->|"只用于"| CP["Control Plane"]
    HA["HubAdmissionTicket"] -->|"只用于"| H["Hub"]
    RL["RelayLease"] -->|"只用于"| R["Relay / TURN"]

    D["Daemon"] -->|"签名"| DI["DeviceIdentity proof"]
    DI -->|"DataChannel E2E 验证"| C["Client"]
    C -->|"从安全存储读取"| CG["CapabilityGrant"]
    CG -->|"仅在 DataChannel E2E 提交"| D
    C <-->|"DeviceHello / CapabilityOpen"| D

    H -.->|"不能替代"| CG
    R -.->|"不能替代"| CG
    CP -.->|"不能替代"| CG

    classDef cloud fill:#fff7ed,stroke:#c2410c,color:#431407;
    classDef e2e fill:#ecfdf5,stroke:#15803d,color:#052e16;
    class CP,H,R cloud;
    class C,D,DI,CG e2e;
```

## 7. 全球网络加速

SmartRoute、双 Edge Relay 和受控 inter-region backbone 的网络图见 `global-acceleration-spec.md` 的“Relay Mesh 网络图”。该路径仍承载同一个端到端 DTLS DataChannel；加速节点增加不会扩大 terminal capability，也不会创建新的 endpoint。

## 8. 一句话总结

Hub 帮两端找到彼此，Relay 在直连失败时搬运密文，Control Plane 决定谁能使用这些托管服务；只有 daemon 能决定客户端最终可以访问哪些 terminal 能力。
