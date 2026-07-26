// Package control 实现 Controller 侧 EdgeControl gRPC admission 和 R1 Hello/Welcome 链路。
package control

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/muxvia/muxvia/cloud/securetransport"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProtocolVersion 是新 Cloud 控制流首发协议版本。
const ProtocolVersion uint32 = 1

// Config 是 EdgeControl service 的 Controller 身份和下发策略。
// ControllerBootID 每次进程启动重新生成，不存数据库。
type Config struct {
	ControllerID           string
	ControllerBootID       string
	HeartbeatInterval      time.Duration
	HeartbeatTimeout       time.Duration
	TicketVerificationKeys []*cloudv1.VerificationKey
}

// Service 拥有 EdgeControl 流的 admission 语义，但不持有 R2 Directory 状态。
type Service struct {
	cloudv1.UnimplementedEdgeControlServer
	config Config
}

// NewService 校验 Controller 身份与心跳策略，失败时不创建部分可用 service。
func NewService(config Config) (*Service, error) {
	config.ControllerID = strings.TrimSpace(config.ControllerID)
	config.ControllerBootID = strings.TrimSpace(config.ControllerBootID)
	if config.ControllerID == "" || config.ControllerBootID == "" {
		return nil, errors.New("controller ID and boot ID are required")
	}
	if config.HeartbeatInterval <= 0 || config.HeartbeatTimeout < config.HeartbeatInterval {
		return nil, errors.New("heartbeat timeout must be greater than or equal to a positive interval")
	}
	config.TicketVerificationKeys = cloneKeys(config.TicketVerificationKeys)
	return &Service{config: config}, nil
}

// Connect 验证 mTLS EdgeIdentity、第一个 envelope 和 EdgeHello，然后发送唯一 EdgeWelcome。
// R1 不接受快照或增量 payload；收到额外事件会明确拒绝，不做旧协议 fallback。
func (service *Service) Connect(stream cloudv1.EdgeControl_ConnectServer) error {
	certificateEdgeID, err := authenticatedEdgeID(stream)
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	event, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "receive EdgeHello: %v", err)
	}
	if _, err := validateHello(event, certificateEdgeID); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	command := &cloudv1.ControllerCommand{
		ProtocolVersion: ProtocolVersion,
		MessageId:       uuid.NewString(),
		SenderId:        service.config.ControllerID,
		BootId:          service.config.ControllerBootID,
		ConnectionId:    event.GetConnectionId(),
		StreamSeq:       1,
		SentAt:          timestamppb.Now(),
		Payload: &cloudv1.ControllerCommand_Welcome{Welcome: &cloudv1.EdgeWelcome{
			AcceptedProtocolVersion: ProtocolVersion,
			Heartbeat: &cloudv1.HeartbeatPolicy{
				Interval: durationpb.New(service.config.HeartbeatInterval),
				Timeout:  durationpb.New(service.config.HeartbeatTimeout),
			},
			TicketVerificationKeys: cloneKeys(service.config.TicketVerificationKeys),
		}},
	}
	if err := stream.Send(command); err != nil {
		return status.Errorf(codes.Unavailable, "send EdgeWelcome: %v", err)
	}
	for {
		if _, err := stream.Recv(); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		return status.Error(codes.Unimplemented, "R1 EdgeControl accepts only the initial EdgeHello")
	}
}

func authenticatedEdgeID(stream cloudv1.EdgeControl_ConnectServer) (string, error) {
	remotePeer, ok := peer.FromContext(stream.Context())
	if !ok {
		return "", errors.New("mTLS peer is missing")
	}
	tlsInfo, ok := remotePeer.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return "", errors.New("verified mTLS client certificate is missing")
	}
	return securetransport.EdgeIDFromCertificate(tlsInfo.State.PeerCertificates[0])
}

func validateHello(event *cloudv1.EdgeEvent, certificateEdgeID string) (*cloudv1.EdgeHello, error) {
	if event == nil || event.GetHello() == nil {
		return nil, errors.New("first EdgeControl payload must be EdgeHello")
	}
	if event.GetProtocolVersion() != ProtocolVersion {
		return nil, fmt.Errorf("unsupported protocol version %d", event.GetProtocolVersion())
	}
	if strings.TrimSpace(event.GetMessageId()) == "" || strings.TrimSpace(event.GetBootId()) == "" || strings.TrimSpace(event.GetConnectionId()) == "" {
		return nil, errors.New("EdgeHello envelope IDs are required")
	}
	if event.GetStreamSeq() != 1 {
		return nil, errors.New("EdgeHello stream_seq must be 1")
	}
	if event.GetSentAt() == nil || event.GetSentAt().CheckValid() != nil {
		return nil, errors.New("EdgeHello sent_at is invalid")
	}
	hello := event.GetHello()
	if hello.GetEdgeId() != certificateEdgeID || event.GetSenderId() != certificateEdgeID {
		return nil, errors.New("EdgeHello identity does not match the mTLS Edge URI SAN")
	}
	if strings.TrimSpace(hello.GetSoftwareVersion()) == "" {
		return nil, errors.New("EdgeHello software version is required")
	}
	if !slices.Contains(hello.GetCapabilities(), cloudv1.EdgeCapability_EDGE_CAPABILITY_CONTROL_STREAM) {
		return nil, errors.New("EdgeHello does not advertise the control stream capability")
	}
	return hello, nil
}

func cloneKeys(keys []*cloudv1.VerificationKey) []*cloudv1.VerificationKey {
	cloned := make([]*cloudv1.VerificationKey, 0, len(keys))
	for _, key := range keys {
		if key == nil {
			continue
		}
		cloned = append(cloned, proto.Clone(key).(*cloudv1.VerificationKey))
	}
	return cloned
}
