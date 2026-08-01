// Package agentgateway 实现 daemon 到 Edge 的认证长连接和 Presence 生命周期。
package agentgateway

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProtocolVersion 是要求 Edge 先发 challenge 的 AgentGateway 协议版本。
const ProtocolVersion uint32 = 3

// Runtime 是 Edge 唯一 State actor 暴露给 AgentGateway 的窄连接边界。
type Runtime interface {
	ResolveDaemonState(context.Context, string) (*cloudv1.DaemonStateRecord, error)
	AttachAuthenticatedAgent(context.Context, *cloudv1.AgentPresence, *cloudv1.DaemonBindingClaims, func(*cloudv1.EdgeCommand) bool, func()) (uint64, *cloudv1.DaemonStateRecord, error)
	DetachAgent(context.Context, string, uint64) error
	ResolveAgentSignal(context.Context, string, uint64, *cloudv1.AgentEvent) error
	ApplyDaemonLifecycleResult(context.Context, string, uint64, *cloudv1.DaemonLifecycleResult) error
}

// Config 提供 Edge identity、动态 Controller binding key set 和心跳策略。
type Config struct {
	EdgeID            string
	EdgeBootID        string
	Runtime           Runtime
	VerificationKeys  func(time.Time) (ticket.KeySet, error)
	Heartbeat         time.Duration
	HeartbeatTimeout  time.Duration
	DeletedAckTimeout time.Duration
	Now               func() time.Time
	ChallengeRandom   io.Reader
}

// Service 验证 daemon binding/DeviceIdentity proof，并把连接生命周期提交给 Runtime actor。
type Service struct {
	cloudv1.UnimplementedAgentGatewayServer
	config Config
}

// NewService 拒绝缺少 key provider、actor 或有界心跳策略的装配。
func NewService(config Config) (*Service, error) {
	config.EdgeID = strings.TrimSpace(config.EdgeID)
	config.EdgeBootID = strings.TrimSpace(config.EdgeBootID)
	if config.DeletedAckTimeout == 0 {
		config.DeletedAckTimeout = config.HeartbeatTimeout
	}
	if config.EdgeID == "" || config.EdgeBootID == "" || config.Runtime == nil || config.VerificationKeys == nil || config.Heartbeat <= 0 || config.HeartbeatTimeout < config.Heartbeat || config.DeletedAckTimeout <= 0 {
		return nil, errors.New("AgentGateway Edge identity, runtime, ticket keys, and heartbeat policy are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ChallengeRandom == nil {
		config.ChallengeRandom = rand.Reader
	}
	return &Service{config: config}, nil
}

// Connect 先发送唯一 EdgeChallenge，再接收唯一 AgentHello，之后只接收连续的 heartbeat 或信令结果。
func (service *Service) Connect(stream cloudv1.AgentGateway_ConnectServer) error {
	challengeCommand, challenge, err := service.challengeCommand()
	if err != nil {
		return status.Errorf(codes.Internal, "create AgentGateway challenge: %v", err)
	}
	if err := stream.Send(challengeCommand); err != nil {
		return status.Errorf(codes.Unavailable, "send AgentGateway challenge: %v", err)
	}
	helloTimeout := challenge.GetExpiresAt().AsTime().Sub(service.config.Now().UTC())
	if helloTimeout <= 0 {
		return status.Error(codes.DeadlineExceeded, "AgentGateway challenge expired before AgentHello")
	}
	helloContext, cancelHello := context.WithTimeout(stream.Context(), helloTimeout)
	event, err := receiveInitialAgentEvent(helloContext, stream)
	cancelHello()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return status.Error(codes.DeadlineExceeded, "AgentGateway challenge expired before AgentHello")
		}
		return status.Errorf(codes.InvalidArgument, "receive AgentHello: %v", err)
	}
	claims, err := service.admit(event, challenge)
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	if _, err := service.config.Runtime.ResolveDaemonState(stream.Context(), claims.GetDaemonId()); err != nil {
		return status.Errorf(codes.Unavailable, "resolve daemon state: %v", err)
	}
	connectionCtx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	writer := newWriter(connectionCtx, cancel, stream, service.config.EdgeID, service.config.EdgeBootID, event.GetConnectionId(), 64)
	go writer.run()
	presence := &cloudv1.AgentPresence{
		DaemonId: claims.GetDaemonId(), AccountId: claims.GetAccountId(), BootId: event.GetBootId(), ConnectionId: event.GetConnectionId(),
		BindingId: claims.GetBindingId(), BindingIssuedAt: claims.GetIssuedAt(),
	}
	generation, daemonState, err := service.config.Runtime.AttachAuthenticatedAgent(connectionCtx, presence, claims, writer.trySend, writer.close)
	if err != nil {
		writer.close()
		return status.Errorf(codes.Aborted, "attach Agent generation: %v", err)
	}
	defer func() {
		detachCtx, detachCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer detachCancel()
		_ = service.config.Runtime.DetachAgent(detachCtx, claims.GetDaemonId(), generation)
	}()
	if !writer.trySend(&cloudv1.EdgeCommand{Payload: &cloudv1.EdgeCommand_Ready{Ready: &cloudv1.AgentReady{
		Generation: generation, Heartbeat: &cloudv1.HeartbeatPolicy{Interval: durationpb.New(service.config.Heartbeat), Timeout: durationpb.New(service.config.HeartbeatTimeout)}, DaemonState: daemonState,
	}}}) {
		return status.Error(codes.Unavailable, "Agent writer is unavailable")
	}

	received := make(chan receiveResult, 1)
	go receiveAgentEvents(stream, received)
	expectedSequence := uint64(2)
	timer := time.NewTimer(service.config.HeartbeatTimeout)
	defer timer.Stop()
	deletedNotice := writer.deleted
	var deletedTimer *time.Timer
	var deletedDeadline <-chan time.Time
	defer func() {
		if deletedTimer != nil {
			deletedTimer.Stop()
		}
	}()
	expiresIn := claims.GetExpiresAt().AsTime().Sub(service.config.Now().UTC())
	if expiresIn <= 0 {
		return status.Error(codes.Unauthenticated, "daemon binding expired after admission")
	}
	expiry := time.NewTimer(expiresIn)
	defer expiry.Stop()
	for {
		select {
		case <-connectionCtx.Done():
			return nil
		case writeErr := <-writer.errors:
			return status.Errorf(codes.Unavailable, "send Edge command: %v", writeErr)
		case <-timer.C:
			return status.Error(codes.DeadlineExceeded, "Agent heartbeat timed out")
		case <-deletedNotice:
			deletedNotice = nil
			deletedTimer = time.NewTimer(service.config.DeletedAckTimeout)
			deletedDeadline = deletedTimer.C
		case <-deletedDeadline:
			return status.Error(codes.DeadlineExceeded, "daemon DELETED lifecycle acknowledgement timed out")
		case <-expiry.C:
			return status.Error(codes.Unauthenticated, "daemon binding expired")
		case result := <-received:
			if errors.Is(result.err, io.EOF) {
				return nil
			}
			if result.err != nil {
				return result.err
			}
			if err := validateAgentEvent(result.event, event, generation, expectedSequence); err != nil {
				return status.Error(codes.InvalidArgument, err.Error())
			}
			if lifecycle := result.event.GetLifecycleResult(); lifecycle != nil {
				if err := service.config.Runtime.ApplyDaemonLifecycleResult(connectionCtx, claims.GetDaemonId(), generation, lifecycle); err != nil {
					return status.Error(codes.FailedPrecondition, err.Error())
				}
			} else if result.event.GetAnswer() != nil || result.event.GetRejected() != nil || result.event.GetAuthorization() != nil {
				if err := service.config.Runtime.ResolveAgentSignal(connectionCtx, claims.GetDaemonId(), generation, result.event); err != nil {
					return status.Error(codes.FailedPrecondition, err.Error())
				}
			}
			expectedSequence++
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(service.config.HeartbeatTimeout)
			go receiveAgentEvents(stream, received)
		}
	}
}

func (service *Service) admit(event *cloudv1.AgentEvent, challenge *cloudv1.EdgeChallenge) (*cloudv1.DaemonBindingClaims, error) {
	if event == nil || event.GetProtocolVersion() != ProtocolVersion || event.GetStreamSeq() != 1 || event.GetHello() == nil ||
		strings.TrimSpace(event.GetMessageId()) == "" || strings.TrimSpace(event.GetSenderId()) == "" || strings.TrimSpace(event.GetBootId()) == "" || strings.TrimSpace(event.GetConnectionId()) == "" ||
		event.GetSentAt() == nil || event.GetSentAt().CheckValid() != nil || strings.TrimSpace(event.GetHello().GetSoftwareVersion()) == "" || event.GetHello().GetAttemptGeneration() == 0 {
		return nil, errors.New("AgentHello envelope is invalid")
	}
	now := service.config.Now().UTC()
	if err := ticket.ValidateEdgeChallenge(challenge, cloudv1.EdgeChallengeTarget_EDGE_CHALLENGE_TARGET_AGENT_GATEWAY, now); err != nil ||
		challenge.GetEdgeId() != service.config.EdgeID || challenge.GetEdgeBootId() != service.config.EdgeBootID {
		return nil, errors.New("AgentGateway challenge is invalid")
	}
	keys, err := service.config.VerificationKeys(now)
	if err != nil {
		return nil, errors.New("binding verification keys are unavailable")
	}
	claims, err := ticket.VerifyDaemonBinding(event.GetHello().GetDaemonBinding(), keys, service.config.EdgeID, now, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if event.GetSenderId() != claims.GetDaemonId() {
		return nil, errors.New("AgentHello sender does not match daemon binding")
	}
	if err := ticket.VerifyAgentHelloProof(claims.GetDevicePublicKey(), event.GetHello().GetDeviceProof(), challenge, event, now); err != nil {
		return nil, err
	}
	return claims, nil
}

func (service *Service) challengeCommand() (*cloudv1.EdgeCommand, *cloudv1.EdgeChallenge, error) {
	nonce := make([]byte, ticket.EdgeChallengeNonceSize)
	if _, err := io.ReadFull(service.config.ChallengeRandom, nonce); err != nil {
		return nil, nil, err
	}
	now := service.config.Now().UTC()
	challenge := &cloudv1.EdgeChallenge{
		Nonce: nonce, EdgeId: service.config.EdgeID, EdgeBootId: service.config.EdgeBootID, StreamId: uuid.NewString(),
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(ticket.EdgeChallengeLifetime)), Target: cloudv1.EdgeChallengeTarget_EDGE_CHALLENGE_TARGET_AGENT_GATEWAY,
	}
	command := &cloudv1.EdgeCommand{
		ProtocolVersion: ProtocolVersion, MessageId: uuid.NewString(), SenderId: service.config.EdgeID, BootId: service.config.EdgeBootID,
		ConnectionId: challenge.GetStreamId(), StreamSeq: 1, SentAt: timestamppb.New(now), Payload: &cloudv1.EdgeCommand_Challenge{Challenge: proto.Clone(challenge).(*cloudv1.EdgeChallenge)},
	}
	return command, challenge, nil
}

func validateAgentEvent(event, hello *cloudv1.AgentEvent, generation, expectedSequence uint64) error {
	if event == nil || event.GetProtocolVersion() != ProtocolVersion || event.GetStreamSeq() != expectedSequence || event.GetSenderId() != hello.GetSenderId() || event.GetBootId() != hello.GetBootId() || event.GetConnectionId() != hello.GetConnectionId() {
		return errors.New("Agent event envelope is invalid")
	}
	switch {
	case event.GetHeartbeat() != nil:
		if event.GetHeartbeat().GetGeneration() != generation {
			return errors.New("Agent heartbeat generation is invalid")
		}
	case event.GetAnswer() != nil:
		if strings.TrimSpace(event.GetAnswer().GetCorrelationId()) == "" || strings.TrimSpace(event.GetAnswer().GetSessionId()) == "" || strings.TrimSpace(event.GetAnswer().GetAnswerSdp()) == "" {
			return errors.New("Agent answer is invalid")
		}
	case event.GetRejected() != nil:
		if strings.TrimSpace(event.GetRejected().GetCorrelationId()) == "" || strings.TrimSpace(event.GetRejected().GetSessionId()) == "" || strings.TrimSpace(event.GetRejected().GetCode()) == "" {
			return errors.New("Agent rejection is invalid")
		}
	case event.GetAuthorization() != nil:
		if strings.TrimSpace(event.GetAuthorization().GetCorrelationId()) == "" || strings.TrimSpace(event.GetAuthorization().GetSessionId()) == "" || (!event.GetAuthorization().GetAuthorized() && strings.TrimSpace(event.GetAuthorization().GetCode()) == "") {
			return errors.New("Agent authorization result is invalid")
		}
	case event.GetLifecycleResult() != nil:
		result := event.GetLifecycleResult()
		state := result.GetDaemonState()
		if result.GetAgentGeneration() != generation || state == nil || state.GetDaemonId() != hello.GetSenderId() || state.GetStateRevision() == 0 ||
			(state.GetState() != cloudv1.DaemonState_DAEMON_STATE_ACTIVE && state.GetState() != cloudv1.DaemonState_DAEMON_STATE_BLOCKED && state.GetState() != cloudv1.DaemonState_DAEMON_STATE_DELETED) ||
			(!result.GetApplied() && strings.TrimSpace(result.GetErrorMessage()) == "") {
			return errors.New("Agent lifecycle result is invalid")
		}
	default:
		return errors.New("Agent event payload is unsupported")
	}
	return nil
}

type receiveResult struct {
	event *cloudv1.AgentEvent
	err   error
}

func receiveInitialAgentEvent(ctx context.Context, stream cloudv1.AgentGateway_ConnectServer) (*cloudv1.AgentEvent, error) {
	received := make(chan receiveResult, 1)
	go receiveAgentEvents(stream, received)
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case result := <-received:
		return result.event, result.err
	}
}

func receiveAgentEvents(stream cloudv1.AgentGateway_ConnectServer, output chan<- receiveResult) {
	event, err := stream.Recv()
	output <- receiveResult{event: event, err: err}
}

type outboundWriter struct {
	ctx                          context.Context
	cancel                       context.CancelFunc
	stream                       cloudv1.AgentGateway_ConnectServer
	edgeID, bootID, connectionID string
	queue                        chan *cloudv1.EdgeCommand
	errors                       chan error
	closed                       chan struct{}
	deleted                      chan struct{}
	closeOnce                    sync.Once
	deletedOnce                  sync.Once
}

func newWriter(ctx context.Context, cancel context.CancelFunc, stream cloudv1.AgentGateway_ConnectServer, edgeID, bootID, connectionID string, size int) *outboundWriter {
	return &outboundWriter{ctx: ctx, cancel: cancel, stream: stream, edgeID: edgeID, bootID: bootID, connectionID: connectionID, queue: make(chan *cloudv1.EdgeCommand, size), errors: make(chan error, 1), closed: make(chan struct{}), deleted: make(chan struct{})}
}

func (writer *outboundWriter) trySend(command *cloudv1.EdgeCommand) bool {
	if command == nil {
		return false
	}
	if commandDaemonState(command) == cloudv1.DaemonState_DAEMON_STATE_DELETED {
		writer.deletedOnce.Do(func() { close(writer.deleted) })
	}
	select {
	case <-writer.closed:
		return false
	case writer.queue <- proto.Clone(command).(*cloudv1.EdgeCommand):
		return true
	default:
		writer.close()
		return false
	}
}

func commandDaemonState(command *cloudv1.EdgeCommand) cloudv1.DaemonState {
	if ready := command.GetReady(); ready != nil && ready.GetDaemonState() != nil {
		return ready.GetDaemonState().GetState()
	}
	if lifecycle := command.GetLifecycle(); lifecycle != nil && lifecycle.GetDaemonState() != nil {
		return lifecycle.GetDaemonState().GetState()
	}
	return cloudv1.DaemonState_DAEMON_STATE_UNSPECIFIED
}

func (writer *outboundWriter) close() {
	writer.closeOnce.Do(func() { close(writer.closed); writer.cancel() })
}

func (writer *outboundWriter) run() {
	sequence := uint64(1)
	for {
		select {
		case <-writer.ctx.Done():
			return
		case command := <-writer.queue:
			sequence++
			command.ProtocolVersion = ProtocolVersion
			command.MessageId = uuid.NewString()
			command.SenderId = writer.edgeID
			command.BootId = writer.bootID
			command.ConnectionId = writer.connectionID
			command.StreamSeq = sequence
			command.SentAt = timestamppb.Now()
			if err := writer.stream.Send(command); err != nil {
				select {
				case writer.errors <- fmt.Errorf("AgentGateway writer: %w", err):
				default:
				}
				writer.close()
				return
			}
		}
	}
}
