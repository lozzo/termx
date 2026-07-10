// Package client 提供 termx daemon 连接 Hub agent stream 的公开客户端边界。
package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"sync"

	pb "github.com/lozzow/termx/termx-hub/internal/protocol/hubgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Terminal 是 daemon 向 Hub 发布的可发现 terminal 摘要。
// Hub 只保存在线发现投影，不拥有 terminal lifecycle 或 remote grant scope。
type Terminal struct {
	ID            string
	Name          string
	RemoteEnabled bool
}

// Registration 是 daemon agent 建立 Hub stream 时发送的发现身份。
// DeviceID/MachineID 只用于 Hub 路由，不替代 remoteauth device fingerprint。
type Registration struct {
	AgentID     string
	DeviceID    string
	MachineID   string
	DisplayName string
	Hostname    string
	Platform    string
	Version     string
	Terminals   []Terminal
}

// ICEServer 是 Hub 返回的 STUN/TURN 配置。
// 它只影响新建 peer connection，不承担远端设备身份或 capability 授权。
type ICEServer struct {
	URLs       []string
	Username   string
	Credential string
}

// RelayPolicy 是 Hub 发布的 NAT traversal/relay 能力策略。
// Hub 只能提供路径能力，不能扩大 capability grant 的 protocol scope。
type RelayPolicy struct {
	AllowRelay         bool
	AllowRelayTransfer bool
}

// RegistrationAck 是 Hub 接受 agent 后返回的 session 配置。
type RegistrationAck struct {
	SessionID        string
	ICEServers       []ICEServer
	HeartbeatSeconds int
	RelayPolicy      RelayPolicy
}

// Offer 是 Hub 从客户端中继给 daemon 的 WebRTC offer。
// CapabilityGrant 是不透明 bearer payload，Hub 不验证；daemon 必须在创建 core-v2 session 前自行校验。
type Offer struct {
	SessionID       string
	MachineID       string
	TerminalID      string
	Path            string
	SDP             string
	Candidates      []string
	CapabilityGrant string
}

// Answer 是 daemon 经 Hub 返回给发起客户端的 WebRTC answer。
type Answer struct {
	SessionID  string
	SDP        string
	Candidates []string
	Error      string
}

// Message 是 Hub agent stream 的单条下行消息。
// 当前新主线消费 offer 与 kick；旧 pairing claim 仍留在冻结产品面，不进入 remote-v2 runtime。
type Message struct {
	Offer *Offer
	Kick  string
}

// DialOptions 描述 daemon 到 Hub 管理/信令 stream 的连接参数。
// BearerToken 只认证 agent 到 Hub 的 stream，不是终端 capability grant。
type DialOptions struct {
	URL       string
	TLSConfig *tls.Config
}

// Client 是 Hub agent gRPC 连接工厂。
type Client struct {
	conn *grpc.ClientConn
}

// Dial 建立到 Hub gRPC endpoint 的连接。
// http 使用明文 HTTP/2，https 使用 TLS；未知 scheme 直接失败，不 fallback 到旧 HTTP remote API。
func Dial(ctx context.Context, options DialOptions) (*Client, error) {
	target, credentialsOption, err := dialTarget(options)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(target, credentialsOption)
	if err != nil {
		return nil, fmt.Errorf("dial termx hub: %w", err)
	}
	return &Client{conn: conn}, nil
}

// Connect 注册 daemon agent 并返回长连接 stream。
func (client *Client) Connect(ctx context.Context, bearerToken string, registration Registration) (*Stream, RegistrationAck, error) {
	if client == nil || client.conn == nil {
		return nil, RegistrationAck{}, fmt.Errorf("termx hub client is not connected")
	}
	if token := strings.TrimSpace(bearerToken); token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	}
	stream, err := pb.NewAgentHubClient(client.conn).Connect(ctx)
	if err != nil {
		return nil, RegistrationAck{}, fmt.Errorf("connect termx hub agent stream: %w", err)
	}
	wrapper := &Stream{stream: stream}
	if err := wrapper.send(registerMessage(registration)); err != nil {
		_ = wrapper.Close()
		return nil, RegistrationAck{}, err
	}
	message, err := stream.Recv()
	if err != nil {
		_ = wrapper.Close()
		return nil, RegistrationAck{}, fmt.Errorf("receive termx hub registration ack: %w", err)
	}
	ack := message.GetRegisterAck()
	if ack == nil {
		_ = wrapper.Close()
		return nil, RegistrationAck{}, fmt.Errorf("termx hub did not return registration ack")
	}
	return wrapper, registrationAck(ack), nil
}

// Close 关闭底层 Hub gRPC 连接。
func (client *Client) Close() error {
	if client == nil || client.conn == nil {
		return nil
	}
	return client.conn.Close()
}

// Stream 是单 daemon agent 的 Hub 双向消息流。
type Stream struct {
	stream pb.AgentHub_ConnectClient
	sendMu sync.Mutex
}

// Receive 等待下一条 offer 或 kick。
func (stream *Stream) Receive() (Message, error) {
	message, err := stream.stream.Recv()
	if err != nil {
		return Message{}, err
	}
	if offer := message.GetSignalingOffer(); offer != nil {
		return Message{Offer: &Offer{
			SessionID: offer.GetSessionId(), MachineID: offer.GetMachineId(), TerminalID: offer.GetTerminalId(), Path: offer.GetPath(),
			SDP: offer.GetSdp(), Candidates: append([]string(nil), offer.GetIceCandidates()...), CapabilityGrant: offer.GetSessionToken(),
		}}, nil
	}
	if kick := message.GetKick(); kick != nil {
		return Message{Kick: kick.GetReason()}, nil
	}
	return Message{}, nil
}

// Heartbeat 更新 agent session 和 terminal 发现投影。
func (stream *Stream) Heartbeat(sessionID string, terminals []Terminal) error {
	items := make([]*pb.Terminal, 0, len(terminals))
	for _, terminal := range terminals {
		items = append(items, terminalMessage(terminal))
	}
	return stream.send(&pb.AgentToHub{Payload: &pb.AgentToHub_Heartbeat{Heartbeat: &pb.HeartbeatRequest{AgentSessionId: sessionID, Terminals: items}}})
}

// SendAnswer 把 daemon WebRTC answer 中继回发起客户端。
func (stream *Stream) SendAnswer(answer Answer) error {
	return stream.send(&pb.AgentToHub{Payload: &pb.AgentToHub_SignalingAnswer{SignalingAnswer: &pb.SignalingAnswer{
		SessionId: answer.SessionID, Sdp: answer.SDP, IceCandidates: append([]string(nil), answer.Candidates...), Error: answer.Error,
	}}})
}

// Close 关闭 agent 发送方向；底层连接生命周期由 Client.Close 或 context 管理。
func (stream *Stream) Close() error {
	if stream == nil || stream.stream == nil {
		return nil
	}
	return stream.stream.CloseSend()
}

func (stream *Stream) send(message *pb.AgentToHub) error {
	stream.sendMu.Lock()
	defer stream.sendMu.Unlock()
	if err := stream.stream.Send(message); err != nil {
		return fmt.Errorf("send termx hub agent message: %w", err)
	}
	return nil
}

func registerMessage(registration Registration) *pb.AgentToHub {
	terminals := make([]*pb.Terminal, 0, len(registration.Terminals))
	for _, terminal := range registration.Terminals {
		terminals = append(terminals, terminalMessage(terminal))
	}
	return &pb.AgentToHub{Payload: &pb.AgentToHub_Register{Register: &pb.RegisterRequest{
		AgentId: registration.AgentID, DeviceId: registration.DeviceID, MachineId: registration.MachineID,
		DisplayName: registration.DisplayName, Hostname: registration.Hostname, Platform: registration.Platform,
		Version: registration.Version, Terminals: terminals,
	}}}
}

func terminalMessage(terminal Terminal) *pb.Terminal {
	return &pb.Terminal{TerminalId: terminal.ID, Name: terminal.Name, RemoteEnabled: terminal.RemoteEnabled}
}

func registrationAck(ack *pb.RegisterResponse) RegistrationAck {
	servers := make([]ICEServer, 0, len(ack.GetIceServers()))
	for _, server := range ack.GetIceServers() {
		servers = append(servers, ICEServer{URLs: append([]string(nil), server.GetUrls()...), Username: server.GetUsername(), Credential: server.GetCredential()})
	}
	policy := ack.GetRelayPolicy()
	return RegistrationAck{SessionID: ack.GetAgentSessionId(), ICEServers: servers, HeartbeatSeconds: int(ack.GetHeartbeatIntervalSeconds()), RelayPolicy: RelayPolicy{AllowRelay: policy.GetAllowRelay(), AllowRelayTransfer: policy.GetAllowRelayTransfer()}}
}

func dialTarget(options DialOptions) (string, grpc.DialOption, error) {
	parsed, err := url.Parse(strings.TrimSpace(options.URL))
	if err != nil || parsed.Host == "" {
		return "", nil, fmt.Errorf("invalid termx hub URL %q", options.URL)
	}
	switch parsed.Scheme {
	case "http":
		return parsed.Host, grpc.WithTransportCredentials(insecure.NewCredentials()), nil
	case "https":
		config := options.TLSConfig
		if config == nil {
			config = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: parsed.Hostname()}
		} else {
			config = config.Clone()
			if config.ServerName == "" {
				config.ServerName = parsed.Hostname()
			}
		}
		return parsed.Host, grpc.WithTransportCredentials(credentials.NewTLS(config)), nil
	default:
		return "", nil, fmt.Errorf("unsupported termx hub URL scheme %q", parsed.Scheme)
	}
}
