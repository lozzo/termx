// Package controllerlink 实现 Edge 到 Controller 的唯一 mTLS EdgeControl 客户端连接。
package controllerlink

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/muxvia/muxvia/cloud/controller/control"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Config 是一次 EdgeControl 连接尝试的完整输入。
// BootID 由 Edge 进程拥有，ConnectionID 由 Open 每次生成。
type Config struct {
	ControllerAddress    string
	TLSConfig            *tls.Config
	EdgeID               string
	BootID               string
	SoftwareVersion      string
	DesiredConfigVersion uint64
	CertificateVersion   uint64
}

// Session 拥有一个已完成 Hello/Welcome 的 EdgeControl stream 与底层 gRPC connection。
// 该类型不维护 Directory 或 runtime topology。
type Session struct {
	connectionID string
	welcome      *cloudv1.EdgeWelcome
	stream       cloudv1.EdgeControl_ConnectClient
	connection   *grpc.ClientConn
}

// Open 建立真实 mTLS gRPC 流，发送 EdgeHello 并等待已校验的 EdgeWelcome。
// TLS、协议版本、envelope 或 connection generation 不匹配时不返回部分 Session。
func Open(ctx context.Context, config Config) (*Session, error) {
	config.ControllerAddress = strings.TrimSpace(config.ControllerAddress)
	config.EdgeID = strings.TrimSpace(config.EdgeID)
	config.BootID = strings.TrimSpace(config.BootID)
	config.SoftwareVersion = strings.TrimSpace(config.SoftwareVersion)
	if config.ControllerAddress == "" || config.EdgeID == "" || config.BootID == "" || config.SoftwareVersion == "" || config.TLSConfig == nil {
		return nil, errors.New("controller address, TLS config, Edge ID, boot ID, and software version are required")
	}
	connection, err := grpc.NewClient(config.ControllerAddress, grpc.WithTransportCredentials(credentials.NewTLS(config.TLSConfig.Clone())))
	if err != nil {
		return nil, fmt.Errorf("create EdgeControl client: %w", err)
	}
	stream, err := cloudv1.NewEdgeControlClient(connection).Connect(ctx)
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("open EdgeControl stream: %w", err)
	}
	connectionID := uuid.NewString()
	event := &cloudv1.EdgeEvent{
		ProtocolVersion: control.ProtocolVersion,
		MessageId:       uuid.NewString(),
		SenderId:        config.EdgeID,
		BootId:          config.BootID,
		ConnectionId:    connectionID,
		StreamSeq:       1,
		SentAt:          timestamppb.Now(),
		Payload: &cloudv1.EdgeEvent_Hello{Hello: &cloudv1.EdgeHello{
			EdgeId:               config.EdgeID,
			SoftwareVersion:      config.SoftwareVersion,
			Capabilities:         []cloudv1.EdgeCapability{cloudv1.EdgeCapability_EDGE_CAPABILITY_CONTROL_STREAM},
			DesiredConfigVersion: config.DesiredConfigVersion,
			CertificateVersion:   config.CertificateVersion,
		}},
	}
	if err := stream.Send(event); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("send EdgeHello: %w", err)
	}
	command, err := stream.Recv()
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("receive EdgeWelcome: %w", err)
	}
	welcome, err := validateWelcome(command, connectionID)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	return &Session{connectionID: connectionID, welcome: welcome, stream: stream, connection: connection}, nil
}

// ConnectionID 返回当前 Edge 连接 generation，该值只在进程内存中生效。
func (session *Session) ConnectionID() string {
	return session.connectionID
}

// Welcome 返回 Controller 接受的心跳和公钥策略投影。
func (session *Session) Welcome() *cloudv1.EdgeWelcome {
	return session.welcome
}

// Wait 由当前连接的唯一 reader 调用，直到 Controller 关闭或发送 R1 不支持的命令。
func (session *Session) Wait() error {
	command, err := session.stream.Recv()
	if err != nil {
		return err
	}
	return fmt.Errorf("unsupported R1 Controller command payload %T", command.GetPayload())
}

// Close 关闭发送方向和底层 gRPC connection，迟到消息不会被新 generation 复用。
func (session *Session) Close() error {
	_ = session.stream.CloseSend()
	return session.connection.Close()
}

func validateWelcome(command *cloudv1.ControllerCommand, connectionID string) (*cloudv1.EdgeWelcome, error) {
	if command == nil || command.GetWelcome() == nil {
		return nil, errors.New("first Controller payload must be EdgeWelcome")
	}
	if command.GetProtocolVersion() != control.ProtocolVersion || command.GetWelcome().GetAcceptedProtocolVersion() != control.ProtocolVersion {
		return nil, fmt.Errorf("Controller accepted unsupported protocol version %d", command.GetWelcome().GetAcceptedProtocolVersion())
	}
	if strings.TrimSpace(command.GetMessageId()) == "" || strings.TrimSpace(command.GetSenderId()) == "" || strings.TrimSpace(command.GetBootId()) == "" {
		return nil, errors.New("EdgeWelcome envelope IDs are required")
	}
	if command.GetConnectionId() != connectionID || command.GetStreamSeq() != 1 {
		return nil, errors.New("EdgeWelcome connection generation or stream sequence does not match")
	}
	if command.GetSentAt() == nil || command.GetSentAt().CheckValid() != nil {
		return nil, errors.New("EdgeWelcome sent_at is invalid")
	}
	heartbeat := command.GetWelcome().GetHeartbeat()
	if heartbeat == nil || heartbeat.GetInterval() == nil || heartbeat.GetTimeout() == nil || heartbeat.GetInterval().AsDuration() <= 0 || heartbeat.GetTimeout().AsDuration() < heartbeat.GetInterval().AsDuration() {
		return nil, errors.New("EdgeWelcome heartbeat policy is invalid")
	}
	return command.GetWelcome(), nil
}

// IsExpectedClosure 区分 shutdown/cancel 与需要重连的 Controller 故障。
func IsExpectedClosure(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	code := status.Code(err)
	return code == codes.Canceled || code == codes.DeadlineExceeded
}
