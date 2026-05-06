# TASK_02 — Proto 定义与 gRPC 服务

**Wave**: 1（无依赖，可与 TASK_01/03/04 同时执行）  
**验证**: `cd termx-remote && go build ./protocol/hubgrpc/... ./hub/grpcapi/... ./discovery/...`

---

## 1. 创建目录

```bash
mkdir -p termx-remote/protocol/hubgrpc
mkdir -p termx-remote/hub/grpcapi
```

---

## 2. 新建 `termx-remote/protocol/hubgrpc/hub.proto`

```protobuf
syntax = "proto3";
package termxhub.v1;
option go_package = "github.com/lozzow/termx/termx-remote/protocol/hubgrpc";

service AgentHub {
  rpc Connect(stream AgentToHub) returns (stream HubToAgent);
}

message AgentToHub {
  oneof payload {
    RegisterRequest  register         = 1;
    HeartbeatRequest heartbeat        = 2;
    SignalingAnswer  signaling_answer = 3;
    PairingResult    pairing_result   = 4;
  }
}

message HubToAgent {
  oneof payload {
    RegisterResponse register_ack    = 1;
    SignalingOffer   signaling_offer = 2;
    PairingClaim     pairing_claim   = 3;
    Kick             kick            = 4;
  }
}

message RegisterRequest {
  string agent_id     = 1;
  string device_id    = 2;
  string machine_id   = 3;
  string display_name = 4;
  string hostname     = 5;
  string platform     = 6;
  string version      = 7;
  repeated Terminal terminals = 8;
}

message RegisterResponse {
  string agent_session_id            = 1;
  repeated RTCIceServer ice_servers  = 2;
  int32  heartbeat_interval_seconds  = 3;
  RelayPolicy relay_policy           = 4;
}

message Terminal {
  string terminal_id    = 1;
  string name           = 2;
  bool   remote_enabled = 3;
}

message RTCIceServer {
  repeated string urls       = 1;
  string          username   = 2;
  string          credential = 3;
}

message RelayPolicy {
  bool allow_relay          = 1;
  bool allow_relay_transfer = 2;
}

message HeartbeatRequest {
  string            agent_session_id = 1;
  repeated Terminal terminals        = 2;
}

message SignalingOffer {
  string          session_id     = 1;
  string          machine_id     = 2;
  string          terminal_id    = 3;
  string          sdp            = 4;
  repeated string ice_candidates = 5;
  string          session_token  = 6;
}

message SignalingAnswer {
  string          session_id     = 1;
  string          sdp            = 2;
  repeated string ice_candidates = 3;
}

message PairingClaim {
  string          claim_id               = 1;
  string          pair_session_id        = 2;
  string          pair_secret            = 3;
  string          app_device_id          = 4;
  string          app_name               = 5;
  repeated string requested_capabilities = 6;
}

message PairingResult {
  string claim_id      = 1;
  string session_token = 2;
  string expires_at    = 3;
  string machine_id    = 4;
  string machine_name  = 5;
}

message Kick {
  string reason = 1;
}
```

---

## 3. 添加 Go 依赖并生成代码

```bash
cd termx-remote
go get google.golang.org/grpc@v1.64.0
go get google.golang.org/protobuf@v1.34.0

protoc \
  --go_out=. \
  --go_opt=paths=source_relative \
  --go-grpc_out=. \
  --go-grpc_opt=paths=source_relative \
  protocol/hubgrpc/hub.proto
```

生成后应存在：
- `termx-remote/protocol/hubgrpc/hub.pb.go`
- `termx-remote/protocol/hubgrpc/hub_grpc.pb.go`

---

## 4. 新建 `termx-remote/hub/grpcapi/server.go`

```go
package grpcapi

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
)

// RegistryAdapter 解耦 grpcapi 与 registry/cloud 包。
type RegistryAdapter interface {
	RegisterAgent(RegisterAgentInput) (RegisterAgentOutput, error)
	HeartbeatAgent(sessionID string, terminals []TerminalInput) error
	GetPendingOffer(ctx context.Context, sessionID string) (*PendingOffer, error)
	SubmitAnswer(sessionID, answerSessionID, sdp string, candidates []string) error
	GetPendingPairingClaim(ctx context.Context, sessionID string) (*PendingPairingClaim, error)
	SubmitPairingResult(PairingResultInput) error
}

type RegisterAgentInput struct {
	AgentID, DeviceID, MachineID, DisplayName, Hostname, Platform, Version string
	Terminals []TerminalInput
}
type TerminalInput struct{ TerminalID, Name string; RemoteEnabled bool }
type RegisterAgentOutput struct {
	SessionID                string
	ICEServers               []ICEServer
	HeartbeatIntervalSeconds int32
	AllowRelay, AllowRelayTransfer bool
}
type ICEServer struct{ URLs []string; Username, Credential string }
type PendingOffer struct {
	SessionID, MachineID, TerminalID, SDP, SessionToken string
	Candidates []string
}
type PendingPairingClaim struct {
	ClaimID, PairSessionID, PairSecret, AppDeviceID, AppName string
	RequestedCapabilities []string
}
type PairingResultInput struct {
	ClaimID, SessionToken, ExpiresAt, MachineID, MachineName string
}

type Server struct {
	pb.UnimplementedAgentHubServer
	registry RegistryAdapter
}

func NewServer(r RegistryAdapter) *Server { return &Server{registry: r} }

func (s *Server) Connect(stream pb.AgentHub_ConnectServer) error {
	if err := requireBearer(stream.Context()); err != nil { return err }

	first, err := stream.Recv()
	if err != nil { return err }
	reg := first.GetRegister()
	if reg == nil { return status.Error(codes.InvalidArgument, "first message must be register") }

	terms := make([]TerminalInput, len(reg.Terminals))
	for i, t := range reg.Terminals {
		terms[i] = TerminalInput{TerminalID: t.TerminalId, Name: t.Name, RemoteEnabled: t.RemoteEnabled}
	}
	out, err := s.registry.RegisterAgent(RegisterAgentInput{
		AgentID: reg.AgentId, DeviceID: reg.DeviceId, MachineID: reg.MachineId,
		DisplayName: reg.DisplayName, Hostname: reg.Hostname,
		Platform: reg.Platform, Version: reg.Version, Terminals: terms,
	})
	if err != nil { return status.Errorf(codes.Internal, "register: %v", err) }

	iceServers := make([]*pb.RTCIceServer, len(out.ICEServers))
	for i, s := range out.ICEServers {
		iceServers[i] = &pb.RTCIceServer{Urls: s.URLs, Username: s.Username, Credential: s.Credential}
	}
	if err := stream.Send(&pb.HubToAgent{Payload: &pb.HubToAgent_RegisterAck{
		RegisterAck: &pb.RegisterResponse{
			AgentSessionId: out.SessionID, IceServers: iceServers,
			HeartbeatIntervalSeconds: out.HeartbeatIntervalSeconds,
			RelayPolicy: &pb.RelayPolicy{AllowRelay: out.AllowRelay, AllowRelayTransfer: out.AllowRelayTransfer},
		},
	}}); err != nil { return err }

	ctx := stream.Context()
	go s.pushOffers(ctx, stream, out.SessionID)
	go s.pushPairing(ctx, stream, out.SessionID)

	for {
		msg, err := stream.Recv()
		if err == io.EOF || ctx.Err() != nil { return nil }
		if err != nil { return err }
		switch p := msg.Payload.(type) {
		case *pb.AgentToHub_Heartbeat:
			hb := p.Heartbeat
			ts := make([]TerminalInput, len(hb.Terminals))
			for i, t := range hb.Terminals {
				ts[i] = TerminalInput{TerminalID: t.TerminalId, Name: t.Name, RemoteEnabled: t.RemoteEnabled}
			}
			_ = s.registry.HeartbeatAgent(out.SessionID, ts)
		case *pb.AgentToHub_SignalingAnswer:
			a := p.SignalingAnswer
			_ = s.registry.SubmitAnswer(out.SessionID, a.SessionId, a.Sdp, a.IceCandidates)
		case *pb.AgentToHub_PairingResult:
			pr := p.PairingResult
			_ = s.registry.SubmitPairingResult(PairingResultInput{
				ClaimID: pr.ClaimId, SessionToken: pr.SessionToken,
				ExpiresAt: pr.ExpiresAt, MachineID: pr.MachineId, MachineName: pr.MachineName,
			})
		}
	}
}

func (s *Server) pushOffers(ctx context.Context, stream pb.AgentHub_ConnectServer, sid string) {
	for ctx.Err() == nil {
		offer, err := s.registry.GetPendingOffer(ctx, sid)
		if err != nil || offer == nil {
			select {
			case <-ctx.Done(): return
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		_ = stream.Send(&pb.HubToAgent{Payload: &pb.HubToAgent_SignalingOffer{
			SignalingOffer: &pb.SignalingOffer{
				SessionId: offer.SessionID, MachineId: offer.MachineID,
				TerminalId: offer.TerminalID, Sdp: offer.SDP,
				IceCandidates: offer.Candidates, SessionToken: offer.SessionToken,
			},
		}})
	}
}

func (s *Server) pushPairing(ctx context.Context, stream pb.AgentHub_ConnectServer, sid string) {
	for ctx.Err() == nil {
		claim, err := s.registry.GetPendingPairingClaim(ctx, sid)
		if err != nil || claim == nil {
			select {
			case <-ctx.Done(): return
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		_ = stream.Send(&pb.HubToAgent{Payload: &pb.HubToAgent_PairingClaim{
			PairingClaim: &pb.PairingClaim{
				ClaimId: claim.ClaimID, PairSessionId: claim.PairSessionID,
				PairSecret: claim.PairSecret, AppDeviceId: claim.AppDeviceID,
				AppName: claim.AppName, RequestedCapabilities: claim.RequestedCapabilities,
			},
		}})
	}
}

func requireBearer(ctx context.Context) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok { return status.Error(codes.Unauthenticated, "metadata required") }
	auths := md.Get("authorization")
	if len(auths) == 0 || !strings.HasPrefix(auths[0], "Bearer ") {
		return status.Error(codes.Unauthenticated, "Bearer token required")
	}
	if strings.TrimSpace(strings.TrimPrefix(auths[0], "Bearer ")) == "" {
		return status.Error(codes.Unauthenticated, "token is empty")
	}
	return nil
}

// ExtractBearerToken 提取 Bearer token 值（供 service.go 使用）。
func ExtractBearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok { return "", fmt.Errorf("no metadata") }
	auths := md.Get("authorization")
	if len(auths) == 0 { return "", fmt.Errorf("no authorization") }
	return strings.TrimSpace(strings.TrimPrefix(auths[0], "Bearer ")), nil
}
```

---

## 5. 新建 `termx-remote/discovery/grpc_hub_client.go`

```go
package discovery

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
)

type GRPCHubClient struct {
	conn   *grpc.ClientConn
	client pb.AgentHubClient
	token  string
}

// NewGRPCHubClient 建立 gRPC 连接。https:// 用 TLS，其他用明文。
func NewGRPCHubClient(hubURL, token string) (*GRPCHubClient, error) {
	var opts []grpc.DialOption
	if strings.HasPrefix(hubURL, "https://") {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	target := strings.TrimPrefix(strings.TrimPrefix(hubURL, "https://"), "http://")
	conn, err := grpc.NewClient(target, opts...)
	if err != nil { return nil, fmt.Errorf("grpc dial %s: %w", target, err) }
	return &GRPCHubClient{conn: conn, client: pb.NewAgentHubClient(conn), token: token}, nil
}

func (c *GRPCHubClient) Connect(ctx context.Context) (pb.AgentHub_ConnectClient, error) {
	md := metadata.Pairs("authorization", "Bearer "+c.token)
	return c.client.Connect(metadata.NewOutgoingContext(ctx, md))
}

func (c *GRPCHubClient) Close() error { return c.conn.Close() }
```
