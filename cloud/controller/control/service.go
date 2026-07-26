// Package control 实现 Controller 侧 EdgeControl gRPC admission、同步状态机和 writer 边界。
package control

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/muxvia/muxvia/cloud/controller/directory"
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

// Config 是 EdgeControl service 的 Controller 身份、Directory 和下发策略。
type Config struct {
	ControllerID           string
	ControllerBootID       string
	HeartbeatInterval      time.Duration
	HeartbeatTimeout       time.Duration
	TicketVerificationKeys []*cloudv1.VerificationKey
	Directory              *directory.Directory
}

// Service 只拥有 EdgeControl admission 和 wire 状态机；实时拓扑全部提交给 Directory actor。
type Service struct {
	cloudv1.UnimplementedEdgeControlServer
	config    Config
	draining  atomic.Bool
	drain     chan struct{}
	drainOnce sync.Once
}

// NewService 校验 Controller 身份、心跳和 Directory，失败时不创建部分可用 service。
func NewService(config Config) (*Service, error) {
	config.ControllerID = strings.TrimSpace(config.ControllerID)
	config.ControllerBootID = strings.TrimSpace(config.ControllerBootID)
	if config.ControllerID == "" || config.ControllerBootID == "" || config.Directory == nil {
		return nil, errors.New("controller ID, boot ID, and Directory are required")
	}
	if config.HeartbeatInterval <= 0 || config.HeartbeatTimeout < config.HeartbeatInterval {
		return nil, errors.New("heartbeat timeout must be greater than or equal to a positive interval")
	}
	config.TicketVerificationKeys = cloneKeys(config.TicketVerificationKeys)
	return &Service{config: config, drain: make(chan struct{})}, nil
}

// BeginShutdown 拒绝新控制流并通知现有 Connect handler 主动结束。
func (service *Service) BeginShutdown() {
	service.draining.Store(true)
	service.drainOnce.Do(func() { close(service.drain) })
}

// Connect 验证 mTLS/Hello，然后把快照和增量逐条提交给 Directory。
// 半份快照、序号缺口和摘要错误只触发当前连接重同步，不发布陈旧或不完整投影。
func (service *Service) Connect(stream cloudv1.EdgeControl_ConnectServer) error {
	if service.draining.Load() {
		return status.Error(codes.Unavailable, "Controller is draining")
	}
	certificateEdgeID, err := authenticatedEdgeID(stream)
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	outbound := make(chan *cloudv1.ControllerCommand, 64)
	writerErrors := make(chan error, 1)
	go func() {
		for command := range outbound {
			if err := stream.Send(command); err != nil {
				writerErrors <- err
				return
			}
		}
	}()
	defer close(outbound)
	send := func(command *cloudv1.ControllerCommand) error {
		select {
		case err := <-writerErrors:
			return err
		case <-stream.Context().Done():
			return context.Cause(stream.Context())
		case outbound <- command:
			return nil
		default:
			return errors.New("Controller writer queue is full")
		}
	}

	event, err := service.receive(stream, writerErrors)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "receive EdgeHello: %v", err)
	}
	hello, err := validateHello(event, certificateEdgeID)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if err := service.config.Directory.Attach(stream.Context(), directory.Attachment{
		EdgeID: certificateEdgeID, BootID: event.GetBootId(), ConnectionID: event.GetConnectionId(), SoftwareVersion: hello.GetSoftwareVersion(), ConnectedAt: time.Now().UTC(),
	}); err != nil {
		return status.Errorf(codes.Aborted, "attach Edge generation: %v", err)
	}
	defer service.config.Directory.Detach(event.GetConnectionId())
	bootID := event.GetBootId()
	connectionID := event.GetConnectionId()

	commandSeq := uint64(1)
	if err := send(service.command(connectionID, commandSeq, &cloudv1.ControllerCommand_Welcome{Welcome: &cloudv1.EdgeWelcome{
		AcceptedProtocolVersion: ProtocolVersion,
		Heartbeat:               &cloudv1.HeartbeatPolicy{Interval: durationpb.New(service.config.HeartbeatInterval), Timeout: durationpb.New(service.config.HeartbeatTimeout)},
		TicketVerificationKeys:  cloneKeys(service.config.TicketVerificationKeys),
	}})); err != nil {
		return status.Errorf(codes.Unavailable, "send EdgeWelcome: %v", err)
	}
	expectedEventSeq := uint64(2)
	for {
		event, err = service.receive(stream, writerErrors)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := validateEventEnvelope(event, certificateEdgeID, hello.GetEdgeId(), bootID, connectionID); err != nil {
			return status.Error(codes.InvalidArgument, err.Error())
		}
		if event.GetStreamSeq() != expectedEventSeq {
			expectedEventSeq = event.GetStreamSeq() + 1
			commandSeq++
			if err := send(service.resyncCommand(event.GetConnectionId(), commandSeq, 0, "Edge event stream sequence has a gap")); err != nil {
				return status.Errorf(codes.Unavailable, "send ResyncRequired: %v", err)
			}
			continue
		}
		expectedEventSeq++
		accepted, syncErr := service.applyEvent(stream.Context(), event)
		if syncErr != nil {
			var resync *directory.SyncError
			if !errors.As(syncErr, &resync) {
				return status.Error(codes.InvalidArgument, syncErr.Error())
			}
			commandSeq++
			if err := send(service.resyncCommand(event.GetConnectionId(), commandSeq, resync.ExpectedRevision, resync.Reason)); err != nil {
				return status.Errorf(codes.Unavailable, "send ResyncRequired: %v", err)
			}
			continue
		}
		if accepted != nil {
			commandSeq++
			if err := send(service.command(event.GetConnectionId(), commandSeq, &cloudv1.ControllerCommand_SnapshotAccepted{SnapshotAccepted: accepted})); err != nil {
				return status.Errorf(codes.Unavailable, "send SnapshotAccepted: %v", err)
			}
		}
	}
}

func (service *Service) applyEvent(ctx context.Context, event *cloudv1.EdgeEvent) (*cloudv1.SnapshotAccepted, error) {
	switch payload := event.GetPayload().(type) {
	case *cloudv1.EdgeEvent_SnapshotBegin:
		return nil, service.config.Directory.BeginSnapshot(ctx, event.GetConnectionId(), payload.SnapshotBegin)
	case *cloudv1.EdgeEvent_SnapshotChunk:
		return nil, service.config.Directory.AppendSnapshot(ctx, event.GetConnectionId(), payload.SnapshotChunk)
	case *cloudv1.EdgeEvent_SnapshotEnd:
		if err := service.config.Directory.CommitSnapshot(ctx, event.GetConnectionId(), payload.SnapshotEnd); err != nil {
			return nil, err
		}
		return &cloudv1.SnapshotAccepted{SnapshotId: payload.SnapshotEnd.GetSnapshotId(), Revision: payload.SnapshotEnd.GetRevision()}, nil
	case *cloudv1.EdgeEvent_RuntimeDelta:
		return nil, service.config.Directory.ApplyDelta(ctx, event.GetConnectionId(), payload.RuntimeDelta)
	case *cloudv1.EdgeEvent_Heartbeat:
		return nil, service.config.Directory.Heartbeat(ctx, event.GetConnectionId(), payload.Heartbeat)
	default:
		return nil, errors.New("EdgeHello is only valid as the first EdgeControl payload")
	}
}

func (service *Service) command(connectionID string, sequence uint64, payload any) *cloudv1.ControllerCommand {
	command := &cloudv1.ControllerCommand{ProtocolVersion: ProtocolVersion, MessageId: uuid.NewString(), SenderId: service.config.ControllerID, BootId: service.config.ControllerBootID, ConnectionId: connectionID, StreamSeq: sequence, SentAt: timestamppb.Now()}
	switch typed := payload.(type) {
	case *cloudv1.ControllerCommand_Welcome:
		command.Payload = typed
	case *cloudv1.ControllerCommand_SnapshotAccepted:
		command.Payload = typed
	case *cloudv1.ControllerCommand_ResyncRequired:
		command.Payload = typed
	default:
		panic("unsupported ControllerCommand payload")
	}
	return command
}

func (service *Service) resyncCommand(connectionID string, sequence, expected uint64, reason string) *cloudv1.ControllerCommand {
	return service.command(connectionID, sequence, &cloudv1.ControllerCommand_ResyncRequired{ResyncRequired: &cloudv1.ResyncRequired{ExpectedRevision: expected, Reason: reason}})
}

type receiveResult struct {
	event *cloudv1.EdgeEvent
	err   error
}

func (service *Service) receive(stream cloudv1.EdgeControl_ConnectServer, writerErrors <-chan error) (*cloudv1.EdgeEvent, error) {
	result := make(chan receiveResult, 1)
	go func() {
		event, err := stream.Recv()
		result <- receiveResult{event: event, err: err}
	}()
	select {
	case err := <-writerErrors:
		return nil, status.Errorf(codes.Unavailable, "send Controller command: %v", err)
	case <-service.drain:
		return nil, status.Error(codes.Unavailable, "Controller is draining")
	case <-stream.Context().Done():
		return nil, context.Cause(stream.Context())
	case received := <-result:
		return received.event, received.err
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
	if err := validateEventEnvelope(event, certificateEdgeID, certificateEdgeID, event.GetBootId(), event.GetConnectionId()); err != nil {
		return nil, err
	}
	if event.GetStreamSeq() != 1 {
		return nil, errors.New("EdgeHello stream_seq must be 1")
	}
	hello := event.GetHello()
	if hello.GetEdgeId() != certificateEdgeID {
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

func validateEventEnvelope(event *cloudv1.EdgeEvent, certificateEdgeID, senderID, bootID, connectionID string) error {
	if event == nil || event.GetProtocolVersion() != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version %d", event.GetProtocolVersion())
	}
	if strings.TrimSpace(event.GetMessageId()) == "" || strings.TrimSpace(event.GetBootId()) == "" || strings.TrimSpace(event.GetConnectionId()) == "" {
		return errors.New("Edge event envelope IDs are required")
	}
	if event.GetSenderId() != senderID || senderID != certificateEdgeID || event.GetBootId() != bootID || event.GetConnectionId() != connectionID {
		return errors.New("Edge event identity or generation does not match EdgeHello")
	}
	if event.GetSentAt() == nil || event.GetSentAt().CheckValid() != nil {
		return errors.New("Edge event sent_at is invalid")
	}
	return nil
}

func cloneKeys(keys []*cloudv1.VerificationKey) []*cloudv1.VerificationKey {
	cloned := make([]*cloudv1.VerificationKey, 0, len(keys))
	for _, key := range keys {
		if key != nil {
			cloned = append(cloned, proto.Clone(key).(*cloudv1.VerificationKey))
		}
	}
	return cloned
}
