# TASK_06 — 集成（Manager + Service.go）

**Wave**: 3（依赖 TASK_02 + TASK_05 完成）  
**验证**: `cd termx-remote && go build ./...`

---

## 06-A: 修改 `termx-remote/agent/runtime/manager.go`（鉴权部分）

### 删除

- `replay *cert.ReplayWindow` 字段和 `cert.NewReplayWindow(...)` 初始化
- `authorizeCloudOffer()` 方法
- `verifyOfferCertificate()` 方法
- `offerReplayWindow()` 方法（如存在）
- 所有 `machineKey` / `LoadOrCreateMachineKey` / `machineKey.Sign` 代码
- `import "github.com/lozzow/termx/termx-remote/cert"`
- `import "github.com/lozzow/termx/termx-remote/session/rtc"`（仅当只用于 offer signature 时）

### 新增 imports

```go
"github.com/lozzow/termx/termx-remote/session/token"
```

### 新增 `verifySessionToken()` 方法

```go
func (m *Manager) verifySessionToken(machineID, sessionToken string) (token.Claims, error) {
    if strings.TrimSpace(sessionToken) == "" {
        return token.Claims{}, fmt.Errorf("session_token is required")
    }
    secret, err := identity.LoadOrCreateMachineSecret(m.cfg.DataDir)
    if err != nil { return token.Claims{}, fmt.Errorf("load machine secret: %w", err) }
    claims, err := token.Verify(sessionToken, secret, time.Now().UTC())
    if err != nil { return token.Claims{}, fmt.Errorf("invalid session token: %w", err) }
    if claims.MachineID != machineID {
        return token.Claims{}, fmt.Errorf("session token machine_id mismatch: got %q want %q",
            claims.MachineID, machineID)
    }
    return claims, nil
}
```

### 替换 offer 授权调用

找到所有调用 `authorizeCloudOffer(ctx, offer)` 的位置，替换为：

```go
claims, err := m.verifySessionToken(m.cfg.MachineID, offer.SessionToken)
if err != nil { /* 返回/记录错误 */ }
_ = claims.Capabilities  // 后续按需使用
```

### 修改 agent 注册（`syncHubPresence()`）

删除如下整个代码块（machineKey 签名）：

```go
machineKey, err := identity.LoadOrCreateMachineKey(dataDir)
// ...
nonce := ...
now := ...
signature := machineKey.Sign(hubv1.CanonicalAgentRegistrationSignatureMessage(hubv1.AgentRegistrationSignatureFields{...}))
// ...
Signature: hubv1.AgentRegistrationSignature{...}
```

`AgentRegistrationRequest` 不再含 `Signature` 字段，直接发送其余字段。

### 修改 pairing.Manager 初始化

找到创建 `pairing.Config{}` 的位置：

```go
// 旧
pairing.Config{MachineKey: machineKey, ...}

// 新
machineSecret, err := identity.LoadOrCreateMachineSecret(m.cfg.DataDir)
if err != nil { return fmt.Errorf("load machine secret for pairing: %w", err) }
pairing.Config{
    MachineSecret:   machineSecret,
    MachineID:       /* 原有值 */,
    MachineName:     /* 原有值 */,
    LocalPairURL:    /* 原有值 */,
    Now:             /* 原有值 */,
}
```

---

## 06-B: 修改 `termx-remote/agent/runtime/manager.go`（gRPC 连接循环）

### 删除

- `hubSignalingLoop()` 方法（HTTP 长轮询）
- `ensureHubSignalingLoop()` 方法
- `SignalingStarted bool` 字段（hubState struct 中）
- `SignalingCancel  context.CancelFunc` 字段（hubState struct 中）
- 调用 `discovery.PollHubOffer()` / `discovery.SubmitHubAnswer()` 的所有代码

### 新增 imports

```go
pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
```

### 新增 `runGRPCHubLoop()` 方法

```go
func (m *Manager) runGRPCHubLoop(ctx context.Context, hubURL string) {
    backoff := time.Second
    for ctx.Err() == nil {
        if err := m.connectAndServeGRPC(ctx, hubURL); err != nil && ctx.Err() == nil {
            select {
            case <-ctx.Done(): return
            case <-time.After(backoff):
            }
            if backoff < 60*time.Second { backoff *= 2 }
            if backoff > 60*time.Second { backoff = 60 * time.Second }
        } else {
            backoff = time.Second
        }
    }
}

func (m *Manager) connectAndServeGRPC(ctx context.Context, hubURL string) error {
    client, err := discovery.NewGRPCHubClient(hubURL, m.cfg.AccessToken)
    if err != nil { return fmt.Errorf("create grpc client: %w", err) }
    defer client.Close()

    stream, err := client.Connect(ctx)
    if err != nil { return fmt.Errorf("connect: %w", err) }

    if err := stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Register{
        Register: m.buildGRPCRegisterRequest(),
    }}); err != nil { return err }

    msg, err := stream.Recv()
    if err != nil { return err }
    ack := msg.GetRegisterAck()
    if ack == nil { return fmt.Errorf("expected register_ack") }
    m.updateFromGRPCRegisterAck(hubURL, ack)

    interval := time.Duration(ack.HeartbeatIntervalSeconds) * time.Second
    if interval <= 0 { interval = 30 * time.Second }
    go m.grpcHeartbeatLoop(ctx, stream, ack.AgentSessionId, interval)

    for {
        msg, err := stream.Recv()
        if err != nil { return err }
        switch p := msg.Payload.(type) {
        case *pb.HubToAgent_SignalingOffer:
            go m.handleGRPCOffer(ctx, hubURL, p.SignalingOffer, stream)
        case *pb.HubToAgent_PairingClaim:
            go m.handleGRPCPairingClaim(ctx, p.PairingClaim, stream)
        case *pb.HubToAgent_Kick:
            return fmt.Errorf("kicked: %s", p.Kick.Reason)
        }
    }
}
```

### `buildGRPCRegisterRequest()` 方法

将现有 `AgentRegistrationRequest`（hubv1）的字段转换为 `pb.RegisterRequest`：

```go
func (m *Manager) buildGRPCRegisterRequest() *pb.RegisterRequest {
    // 从现有 hubv1.AgentRegistrationRequest 字段填充
    req := &pb.RegisterRequest{
        AgentId:     m.agentID,
        DeviceId:    m.deviceID,
        MachineId:   m.cfg.MachineID,
        DisplayName: m.cfg.DeviceName,
        Version:     m.version,
    }
    // 填充 terminals（从现有 terminal 列表逻辑）
    return req
}
```

### `handleGRPCOffer()` 方法

将 `pb.SignalingOffer` 转换为 `hubv1.SignalingOffer`，复用现有 offer 处理逻辑，
将 answer 通过 stream 发回：

```go
func (m *Manager) handleGRPCOffer(ctx context.Context, hubURL string, pbOffer *pb.SignalingOffer, stream pb.AgentHub_ConnectClient) {
    offer := hubv1.SignalingOffer{
        SessionID:    pbOffer.SessionId,
        MachineID:    pbOffer.MachineId,
        TerminalID:   pbOffer.TerminalId,
        SDP:          pbOffer.Sdp,
        Candidates:   pbOffer.IceCandidates,
        SessionToken: pbOffer.SessionToken,
    }
    answer, err := m.answerOffer(ctx, hubURL, offer)
    if err != nil { /* log */ return }
    stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_SignalingAnswer{
        SignalingAnswer: &pb.SignalingAnswer{
            SessionId:     answer.SessionID,
            Sdp:           answer.SDP,
            IceCandidates: answer.Candidates,
        },
    }})
}
```

### `grpcHeartbeatLoop()` 方法

```go
func (m *Manager) grpcHeartbeatLoop(ctx context.Context, stream pb.AgentHub_ConnectClient, sessionID string, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C:
            _ = stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Heartbeat{
                Heartbeat: &pb.HeartbeatRequest{
                    AgentSessionId: sessionID,
                    Terminals:      m.buildGRPCTerminals(),
                },
            }})
        }
    }
}
```

### `updateFromGRPCRegisterAck()` 方法

从 `ack` 更新 manager 中存储的 ICE servers 和 relay policy：

```go
func (m *Manager) updateFromGRPCRegisterAck(hubURL string, ack *pb.RegisterResponse) {
    // 将 ack.IceServers 转换为现有 hubv1.RTCIceServerConfig 格式
    // 存储到 m 的 hubState 中（参照现有 updateHubState 逻辑）
}
```

### 修改 `reconcileLoop`（或等效函数）中的 hub 连接分支

```go
if config.ModeIncludesOnline(m.cfg.Mode) && selectedHubURL != "" {
    go m.runGRPCHubLoop(ctx, selectedHubURL)
}
if config.ModeIncludesLocal(m.cfg.Mode) {
    // 现有 HTTP 注册逻辑（保留不变）
}
```

---

## 06-C: 集成延迟探测到 hub 选择

在 `manager.go` 的 `discoverHub()` 函数中，获取 hub 列表后调用延迟探测：

```go
hubs, err := discovery.DiscoverHubs(ctx, controlURL, accessToken)
if err != nil { return fmt.Errorf("discover hubs: %w", err) }

urls := make([]string, len(hubs))
for i, h := range hubs { urls[i] = h.URL } // 字段名以现有代码为准

probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
defer cancel()
probeResults := discovery.ProbeHubs(probeCtx, urls, 3*time.Second, 3)

// 选延迟最小且可用的 hub
var selectedURL string
for _, r := range probeResults {
    if r.Available { selectedURL = r.URL; break }
}
if selectedURL == "" {
    // 退回现有 selectDiscoveredHub 逻辑
    hub, ok := selectDiscoveredHub(hubs, hubSelectionOptions{
        PreferredRegion: m.cfg.Region, Now: time.Now().UTC(),
    })
    if ok { selectedURL = hub.URL }
}
```

---

## 06-D: 修改 `termx-remote/service.go`（cmux + LAN filter）

### cmux 新增 HTTP/2 分流

找到 `cmux.New(listener)` 之后的分流代码，在 `HTTP1Fast()` 之前插入：

```go
grpcListener := mux.Match(cmux.HTTP2())      // gRPC（HTTP/2）
httpListener := mux.Match(cmux.HTTP1Fast())  // REST API（HTTP/1）
iceListener  := mux.Match(cmux.Any())        // ICE-TCP（现有）
```

### 启动 gRPC server

```go
import (
    "google.golang.org/grpc"
    grpcapi "github.com/lozzow/termx/termx-remote/hub/grpcapi"
    pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
)

// 创建 RegistryAdapter（将已有 registry 和 cloud service 封装进去）
adapter := &hubRegistryAdapter{registry: hubRegistry, cloud: cloudSvc}
grpcSrv := grpc.NewServer(grpc.StreamInterceptor(grpcStreamAuth))
pb.RegisterAgentHubServer(grpcSrv, grpcapi.NewServer(adapter))
go grpcSrv.Serve(grpcListener)
```

stream auth interceptor：

```go
func grpcStreamAuth(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
    if _, err := grpcapi.ExtractBearerToken(ss.Context()); err != nil {
        return status.Error(codes.Unauthenticated, err.Error())
    }
    return handler(srv, ss)
}
```

### 应用 LAN filter

```go
allowedNets, err := httpapi.ParseLANIPs(cfg.LANIPs)
if err != nil { return fmt.Errorf("parse lan_ips: %w", err) }
lanFilter := httpapi.NewLANFilter(cfg.AllowLAN, allowedNets)
httpServer := &http.Server{Handler: lanFilter(existingHandler)}
```

### 实现 `hubRegistryAdapter`

在 service.go 或新文件 `termx-remote/service_grpc_adapter.go` 中实现
`grpcapi.RegistryAdapter` 接口，委托给已有 `registry.Registry` 和 `cloud.Service`：

```go
type hubRegistryAdapter struct {
    registry *registry.Registry  // 类型以实际为准
    cloud    *cloud.Service
}

func (a *hubRegistryAdapter) RegisterAgent(in grpcapi.RegisterAgentInput) (grpcapi.RegisterAgentOutput, error) {
    // 调用 a.registry.Register(...)，转换参数/返回值
}
// 实现其余接口方法...
```
