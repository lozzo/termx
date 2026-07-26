// Package clientgateway 实现客户端到 Edge 的短票据准入和 P2P WebRTC 信令转发。
// 本包不接收 CapabilityGrant，也不承载 DataChannel 或 terminal payload。
package clientgateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/muxvia/muxvia/cloud/ticket"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProtocolVersion 是 ClientGateway 首发 envelope 版本。
const ProtocolVersion uint32 = 1

// Runtime 是 Edge State actor 暴露给 ClientGateway 的唯一状态与 correlation 边界。
type Runtime interface {
	UpsertSession(context.Context, *cloudv1.ClientSessionSummary) error
	RemoveSession(context.Context, string, uint64) error
	BeginAgentSignal(context.Context, string, string, string) (uint64, <-chan *cloudv1.AgentEvent, error)
	CancelAgentSignal(context.Context, string) error
	SendAgentCommand(context.Context, string, uint64, *cloudv1.EdgeCommand) error
}

// RelayBroker 只通过当前 EdgeControl generation 申请短租约并返回已经验签、登记的临时 ICE 参数。
type RelayBroker interface {
	RequestRelayLease(context.Context, *cloudv1.RelayLeaseRequest) (*cloudv1.RelayICEConfig, error)
}

// Config 提供 Edge identity、动态 Controller ticket key set 和信令 deadline。
type Config struct {
	EdgeID           string
	EdgeBootID       string
	Runtime          Runtime
	VerificationKeys func() ticket.KeySet
	Ready            func() bool
	SignalTimeout    time.Duration
	Now              func() time.Time
	Relay            RelayBroker
}

// Service 只在票据、proof、产品和 attempt generation 全部一致后发布客户端实时摘要。
type Service struct {
	cloudv1.UnimplementedClientGatewayServer
	config Config
}

// NewService 拒绝无法离线验票、无法查询 ready 或没有 State actor 的装配。
func NewService(config Config) (*Service, error) {
	config.EdgeID, config.EdgeBootID = strings.TrimSpace(config.EdgeID), strings.TrimSpace(config.EdgeBootID)
	if config.EdgeID == "" || config.EdgeBootID == "" || config.Runtime == nil || config.VerificationKeys == nil || config.Ready == nil || config.SignalTimeout <= 0 {
		return nil, errors.New("ClientGateway Edge identity, runtime, ticket keys, readiness, and signal timeout are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{config: config}, nil
}

// Connect 完成 ClientHello、单次完整 offer/answer correlation；成功后 P2P DataChannel 不再依赖该 stream。
func (service *Service) Connect(stream cloudv1.ClientGateway_ConnectServer) error {
	if !service.config.Ready() {
		return status.Error(codes.Unavailable, "Edge is not synchronized with Controller")
	}
	helloEvent, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "receive ClientHello: %v", err)
	}
	claims, err := service.admit(helloEvent)
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	sessionID := helloEvent.GetConnectionId()
	generation := helloEvent.GetHello().GetAttemptGeneration()
	sessionContext, cancelSession := context.WithCancel(stream.Context())
	defer cancelSession()
	summary := &cloudv1.ClientSessionSummary{SessionId: sessionID, AccountId: claims.GetAccountId(), DaemonId: claims.GetDaemonId(), ClientId: claims.GetClientId(), Product: claims.GetProduct(), Generation: generation}
	if err := service.config.Runtime.UpsertSession(sessionContext, summary); err != nil {
		return status.Errorf(codes.Aborted, "publish client session: %v", err)
	}
	if registry, ok := service.config.Runtime.(interface {
		RegisterSessionCloser(context.Context, string, uint64, func()) error
	}); ok {
		if err := registry.RegisterSessionCloser(sessionContext, sessionID, generation, cancelSession); err != nil {
			return status.Errorf(codes.Aborted, "register client session owner: %v", err)
		}
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = service.config.Runtime.RemoveSession(cleanup, sessionID, generation)
	}()
	if err := service.authorizeClient(sessionContext, claims, sessionID); err != nil {
		return status.Error(codes.PermissionDenied, err.Error())
	}
	preference := helloEvent.GetHello().GetRelayPreference()
	switch preference {
	case cloudv1.RelayPreference_RELAY_PREFERENCE_AUTO, cloudv1.RelayPreference_RELAY_PREFERENCE_DIRECT_ONLY, cloudv1.RelayPreference_RELAY_PREFERENCE_RELAY_ONLY:
	default:
		return status.Error(codes.InvalidArgument, "Cloud Relay preference is required")
	}
	var relay *cloudv1.RelayICEConfig
	if preference != cloudv1.RelayPreference_RELAY_PREFERENCE_DIRECT_ONLY {
		if claims.GetRoutePolicy() != cloudv1.CloudRoutePolicy_CLOUD_ROUTE_POLICY_P2P_OR_RELAY || service.config.Relay == nil {
			return status.Error(codes.FailedPrecondition, "Relay is not allowed for this Cloud attempt")
		}
		relay, err = service.config.Relay.RequestRelayLease(sessionContext, &cloudv1.RelayLeaseRequest{
			CorrelationId: uuid.NewString(), SessionId: sessionID, AccountId: claims.GetAccountId(), DaemonId: claims.GetDaemonId(), ClientId: claims.GetClientId(), Preference: preference,
		})
		if err != nil {
			return status.Error(codes.ResourceExhausted, err.Error())
		}
	}
	if err := stream.Send(service.edgeSignal(sessionID, 1, &cloudv1.EdgeSignal_Ready{Ready: &cloudv1.ClientReady{SessionId: sessionID, Generation: generation, Relay: relay}})); err != nil {
		return err
	}
	offerEvent, err := receiveClientSignal(sessionContext, stream)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "receive ClientOffer: %v", err)
	}
	if err := validateOffer(offerEvent, helloEvent, sessionID); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	correlationID := uuid.NewString()
	agentGeneration, response, err := service.config.Runtime.BeginAgentSignal(sessionContext, correlationID, claims.GetDaemonId(), sessionID)
	if err != nil {
		return status.Error(codes.FailedPrecondition, "target daemon is no longer online")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = service.config.Runtime.CancelAgentSignal(cleanup, correlationID)
	}()
	offer := offerEvent.GetOffer()
	command := &cloudv1.EdgeCommand{Payload: &cloudv1.EdgeCommand_Offer{Offer: &cloudv1.AgentOffer{
		CorrelationId: correlationID, SessionId: sessionID, AgentGeneration: agentGeneration, ClientPublicKey: append([]byte(nil), claims.GetClientPublicKey()...), OfferSdp: offer.GetOfferSdp(), Candidates: cloneCandidates(offer.GetCandidates()), Relay: cloneRelay(relay),
	}}}
	if err := service.config.Runtime.SendAgentCommand(sessionContext, claims.GetDaemonId(), agentGeneration, command); err != nil {
		return status.Error(codes.FailedPrecondition, "target daemon signaling stream changed")
	}
	timer := time.NewTimer(service.config.SignalTimeout)
	defer timer.Stop()
	select {
	case <-sessionContext.Done():
		return context.Cause(sessionContext)
	case <-timer.C:
		return status.Error(codes.DeadlineExceeded, "daemon signaling answer timed out")
	case result, ok := <-response:
		if !ok {
			return status.Error(codes.FailedPrecondition, "daemon signaling generation ended")
		}
		if result.GetRejected() != nil {
			return stream.Send(service.edgeSignal(sessionID, 2, &cloudv1.EdgeSignal_Rejected{Rejected: &cloudv1.SignalRejected{SessionId: sessionID, Code: result.GetRejected().GetCode(), Message: result.GetRejected().GetMessage()}}))
		}
		if result.GetAnswer() == nil {
			return status.Error(codes.Internal, "daemon signaling result is empty")
		}
		if err := stream.Send(service.edgeSignal(sessionID, 2, &cloudv1.EdgeSignal_Answer{Answer: &cloudv1.EdgeAnswer{SessionId: sessionID, AnswerSdp: result.GetAnswer().GetAnswerSdp(), Candidates: cloneCandidates(result.GetAnswer().GetCandidates())}})); err != nil {
			return err
		}
		// ClientGateway 只观察当前 P2P session 生命周期，不运输任何业务数据。
		// 客户端关闭 ReadyPeerSession 后关闭发送侧，Edge 才删除内存摘要并向 Controller 发布 delta。
		if _, err := receiveClientSignal(sessionContext, stream); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		return status.Error(codes.InvalidArgument, "ClientGateway does not accept messages after signaling")
	}
}

func receiveClientSignal(ctx context.Context, stream cloudv1.ClientGateway_ConnectServer) (*cloudv1.ClientSignal, error) {
	type result struct {
		signal *cloudv1.ClientSignal
		err    error
	}
	reply := make(chan result, 1)
	go func() { signal, err := stream.Recv(); reply <- result{signal: signal, err: err} }()
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case value := <-reply:
		return value.signal, value.err
	}
}

func (service *Service) authorizeClient(ctx context.Context, claims *cloudv1.ClientTicketClaims, sessionID string) error {
	correlationID := uuid.NewString()
	agentGeneration, response, err := service.config.Runtime.BeginAgentSignal(ctx, correlationID, claims.GetDaemonId(), sessionID)
	if err != nil {
		return errors.New("target daemon is no longer online")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = service.config.Runtime.CancelAgentSignal(cleanup, correlationID)
	}()
	command := &cloudv1.EdgeCommand{Payload: &cloudv1.EdgeCommand_Authorize{Authorize: &cloudv1.AgentAuthorize{
		CorrelationId: correlationID, SessionId: sessionID, AgentGeneration: agentGeneration, ClientPublicKey: append([]byte(nil), claims.GetClientPublicKey()...), Product: claims.GetProduct(),
	}}}
	if err := service.config.Runtime.SendAgentCommand(ctx, claims.GetDaemonId(), agentGeneration, command); err != nil {
		return errors.New("target daemon authorization stream changed")
	}
	timer := time.NewTimer(service.config.SignalTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("daemon client authorization timed out")
	case event, ok := <-response:
		if !ok || event.GetAuthorization() == nil {
			return errors.New("daemon client authorization generation ended")
		}
		if !event.GetAuthorization().GetAuthorized() {
			return fmt.Errorf("daemon precheck rejected client access (%s): %s", event.GetAuthorization().GetCode(), event.GetAuthorization().GetMessage())
		}
		return nil
	}
}

func (service *Service) admit(event *cloudv1.ClientSignal) (*cloudv1.ClientTicketClaims, error) {
	if event == nil || event.GetProtocolVersion() != ProtocolVersion || event.GetStreamSeq() != 1 || event.GetHello() == nil || strings.TrimSpace(event.GetMessageId()) == "" || strings.TrimSpace(event.GetBootId()) == "" || strings.TrimSpace(event.GetConnectionId()) == "" {
		return nil, errors.New("ClientHello envelope is invalid")
	}
	hello := event.GetHello()
	claims, err := ticket.VerifyClientTicket(hello.GetClientTicket(), service.config.VerificationKeys(), service.config.EdgeID, service.config.Now().UTC(), 30*time.Second)
	if err != nil {
		return nil, err
	}
	if event.GetSenderId() != claims.GetClientId() || hello.GetProduct() != claims.GetProduct() || hello.GetAttemptGeneration() == 0 {
		return nil, errors.New("ClientHello identity, product, or generation does not match ClientTicket")
	}
	if hello.GetRelayPreference() < cloudv1.RelayPreference_RELAY_PREFERENCE_UNSPECIFIED || hello.GetRelayPreference() > cloudv1.RelayPreference_RELAY_PREFERENCE_RELAY_ONLY {
		return nil, errors.New("ClientHello Relay preference is invalid")
	}
	if err := ticket.VerifyClientHelloProof(claims.GetClientPublicKey(), hello.GetClientProof(), hello.GetClientTicket(), event.GetConnectionId(), hello.GetAttemptGeneration()); err != nil {
		return nil, err
	}
	return claims, nil
}

func cloneRelay(value *cloudv1.RelayICEConfig) *cloudv1.RelayICEConfig {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*cloudv1.RelayICEConfig)
}

func validateOffer(event, hello *cloudv1.ClientSignal, sessionID string) error {
	if event == nil || event.GetOffer() == nil || event.GetProtocolVersion() != ProtocolVersion || event.GetStreamSeq() != 2 || event.GetSenderId() != hello.GetSenderId() || event.GetBootId() != hello.GetBootId() || event.GetConnectionId() != sessionID || event.GetOffer().GetSessionId() != sessionID || strings.TrimSpace(event.GetOffer().GetOfferSdp()) == "" {
		return errors.New("ClientOffer envelope is invalid")
	}
	return nil
}

func (service *Service) edgeSignal(sessionID string, sequence uint64, payload any) *cloudv1.EdgeSignal {
	result := &cloudv1.EdgeSignal{ProtocolVersion: ProtocolVersion, MessageId: uuid.NewString(), SenderId: service.config.EdgeID, BootId: service.config.EdgeBootID, ConnectionId: sessionID, StreamSeq: sequence, SentAt: timestamppb.New(service.config.Now().UTC())}
	switch value := payload.(type) {
	case *cloudv1.EdgeSignal_Ready:
		result.Payload = value
	case *cloudv1.EdgeSignal_Answer:
		result.Payload = value
	case *cloudv1.EdgeSignal_Rejected:
		result.Payload = value
	}
	return result
}

func cloneCandidates(values []*cloudv1.CloudICECandidate) []*cloudv1.CloudICECandidate {
	result := make([]*cloudv1.CloudICECandidate, 0, len(values))
	for _, value := range values {
		if value != nil {
			result = append(result, proto.Clone(value).(*cloudv1.CloudICECandidate))
		}
	}
	return result
}
