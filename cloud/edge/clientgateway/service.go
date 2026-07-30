// Package clientgateway 使用 daemon 长期 RouteGrant 或在线 pairing admission 完成客户端到 Edge 的准入和 P2P WebRTC 信令转发。
// 本包不承载 DataChannel 或 terminal payload。
package clientgateway

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProtocolVersion 是要求 Edge 先发 challenge 的 ClientGateway 协议版本。
const ProtocolVersion uint32 = 2

const (
	maxOfferSDPBytes     = 256 * 1024
	maxICECandidates     = 64
	maxICECandidateBytes = 2 * 1024
)

// Runtime 是 Edge State actor 暴露给 ClientGateway 的唯一状态与 correlation 边界。
type Runtime interface {
	UpsertSession(context.Context, *cloudv1.ClientSessionSummary) error
	RemoveSession(context.Context, string, uint64) error
	BeginAgentSignal(context.Context, string, string, string) (uint64, <-chan *cloudv1.AgentEvent, error)
	CancelAgentSignal(context.Context, string) error
	SendAgentCommand(context.Context, string, uint64, *cloudv1.EdgeCommand) error
	AuthenticatedAgentClaims(context.Context, string) (*cloudv1.DaemonBindingClaims, error)
}

var errRouteStale = errors.New("cached Edge no longer owns the target daemon")

// RelayBroker 从当前 daemon binding 委托创建并登记 Edge 本地短期 ICE 参数。
type RelayBroker interface {
	RequestRelayLease(context.Context, *cloudv1.RelayLeaseSpec) (*cloudv1.RelayICEConfig, error)
	RenewRelayLease(context.Context, *cloudv1.RelayLeaseSpec, *cloudv1.RelayICEConfig) (*cloudv1.RelayICEConfig, error)
}

// RelaySessionCloser 在信令 stream 结束时释放同 session 的 TURN reservation/allocation。
// 它是 RelayBroker 的可选能力，未启用 Relay 的 Edge 不需要提供。
type RelaySessionCloser interface {
	CloseRelaySession(context.Context, string) error
}

// Config 提供 Edge identity、状态 owner 和信令 deadline。
type Config struct {
	EdgeID          string
	EdgeBootID      string
	Runtime         Runtime
	SignalTimeout   time.Duration
	Now             func() time.Time
	Relay           RelayBroker
	ChallengeRandom io.Reader
}

// Service 只在授权材料、proof、产品和 attempt generation 全部一致后发布客户端实时摘要。
type Service struct {
	cloudv1.UnimplementedClientGatewayServer
	config Config
}

// NewService 拒绝没有 State actor 或信令 deadline 的装配。
func NewService(config Config) (*Service, error) {
	config.EdgeID, config.EdgeBootID = strings.TrimSpace(config.EdgeID), strings.TrimSpace(config.EdgeBootID)
	if config.EdgeID == "" || config.EdgeBootID == "" || config.Runtime == nil || config.SignalTimeout <= 0 {
		return nil, errors.New("ClientGateway Edge identity, runtime, and signal timeout are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ChallengeRandom == nil {
		config.ChallengeRandom = rand.Reader
	}
	return &Service{config: config}, nil
}

// Connect 先发送唯一 EdgeChallenge，再完成唯一 ClientHello 和单次 offer/answer correlation。
func (service *Service) Connect(stream cloudv1.ClientGateway_ConnectServer) error {
	challengeSignal, challenge, err := service.challengeSignal()
	if err != nil {
		return status.Errorf(codes.Internal, "create ClientGateway challenge: %v", err)
	}
	if err := stream.Send(challengeSignal); err != nil {
		return status.Errorf(codes.Unavailable, "send ClientGateway challenge: %v", err)
	}
	helloTimeout := challenge.GetExpiresAt().AsTime().Sub(service.config.Now().UTC())
	if helloTimeout <= 0 {
		return status.Error(codes.DeadlineExceeded, "ClientGateway challenge expired before ClientHello")
	}
	helloContext, cancelHello := context.WithTimeout(stream.Context(), helloTimeout)
	helloEvent, err := receiveClientSignal(helloContext, stream)
	cancelHello()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return status.Error(codes.DeadlineExceeded, "ClientGateway challenge expired before ClientHello")
		}
		return status.Errorf(codes.InvalidArgument, "receive ClientHello: %v", err)
	}
	claims, err := service.admit(stream.Context(), helloEvent, challenge)
	if err != nil {
		if errors.Is(err, errRouteStale) {
			return status.Error(codes.NotFound, errRouteStale.Error())
		}
		return status.Error(codes.Unauthenticated, err.Error())
	}
	sessionID := helloEvent.GetConnectionId()
	generation := helloEvent.GetHello().GetAttemptGeneration()
	sessionContext, cancelSession := context.WithCancelCause(stream.Context())
	defer cancelSession(context.Canceled)
	summary := &cloudv1.ClientSessionSummary{SessionId: sessionID, AccountId: claims.accountID, DaemonId: claims.daemonID, ClientId: claims.clientID, Product: claims.product, Generation: generation, AccessMode: claims.accessMode}
	if err := service.config.Runtime.UpsertSession(sessionContext, summary); err != nil {
		return status.Errorf(codes.Aborted, "publish client session: %v", err)
	}
	if registry, ok := service.config.Runtime.(interface {
		RegisterSessionCloser(context.Context, string, uint64, func()) error
	}); ok {
		if err := registry.RegisterSessionCloser(sessionContext, sessionID, generation, func() { cancelSession(context.Canceled) }); err != nil {
			return status.Errorf(codes.Aborted, "register client session owner: %v", err)
		}
	}
	defer service.cleanupSession(sessionID, generation)
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
		if service.config.Relay == nil {
			return status.Error(codes.FailedPrecondition, "Relay is not allowed for this Cloud attempt")
		}
		relay, err = service.config.Relay.RequestRelayLease(sessionContext, &cloudv1.RelayLeaseSpec{
			SessionId: sessionID, AccountId: claims.accountID, DaemonId: claims.daemonID, ClientId: claims.clientID, Preference: preference,
		})
		if err != nil || relay == nil {
			if preference == cloudv1.RelayPreference_RELAY_PREFERENCE_RELAY_ONLY {
				if err == nil {
					err = errors.New("Relay authorization is unavailable")
				}
				return status.Error(codes.Unavailable, err.Error())
			}
			// AUTO 在 Relay 授权不可用时仍允许纯 P2P。
			relay = nil
		}
		if relay != nil {
			renewRequest := &cloudv1.RelayLeaseSpec{
				SessionId: sessionID, AccountId: claims.accountID, DaemonId: claims.daemonID, ClientId: claims.clientID, Preference: preference,
			}
			go service.maintainRelayLease(sessionContext, renewRequest, relay, cancelSession)
		}
	}
	if err := stream.Send(service.edgeSignal(sessionID, 2, &cloudv1.EdgeSignal_Ready{Ready: &cloudv1.ClientReady{SessionId: sessionID, Generation: generation, Relay: relay}})); err != nil {
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
	agentGeneration, response, err := service.config.Runtime.BeginAgentSignal(sessionContext, correlationID, claims.daemonID, sessionID)
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
		CorrelationId: correlationID, SessionId: sessionID, AgentGeneration: agentGeneration, ClientPublicKey: append([]byte(nil), claims.clientPublicKey...), OfferSdp: offer.GetOfferSdp(), Candidates: cloneCandidates(offer.GetCandidates()), Relay: cloneRelay(relay),
		AccessMode: claims.accessMode, PairingClaimSha256: append([]byte(nil), claims.pairingClaimDigest...),
	}}}
	if err := service.config.Runtime.SendAgentCommand(sessionContext, claims.daemonID, agentGeneration, command); err != nil {
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
			return stream.Send(service.edgeSignal(sessionID, 3, &cloudv1.EdgeSignal_Rejected{Rejected: &cloudv1.SignalRejected{SessionId: sessionID, Code: result.GetRejected().GetCode(), Message: result.GetRejected().GetMessage()}}))
		}
		if result.GetAnswer() == nil {
			return status.Error(codes.Internal, "daemon signaling result is empty")
		}
		if err := stream.Send(service.edgeSignal(sessionID, 3, &cloudv1.EdgeSignal_Answer{Answer: &cloudv1.EdgeAnswer{SessionId: sessionID, AnswerSdp: result.GetAnswer().GetAnswerSdp(), Candidates: cloneCandidates(result.GetAnswer().GetCandidates())}})); err != nil {
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

func (service *Service) maintainRelayLease(ctx context.Context, baseRequest *cloudv1.RelayLeaseSpec, initial *cloudv1.RelayICEConfig, cancel context.CancelCauseFunc) {
	current := cloneRelay(initial)
	retryDelay := time.Duration(0)
	for ctx.Err() == nil {
		now := service.config.Now().UTC()
		remaining, err := relayLeaseRemaining(current, now)
		if err != nil {
			cancel(err)
			return
		}
		delay := remaining / 2
		if retryDelay > 0 && retryDelay < delay {
			delay = retryDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		now = service.config.Now().UTC()
		remaining, err = relayLeaseRemaining(current, now)
		if err != nil {
			cancel(err)
			return
		}
		request := proto.Clone(baseRequest).(*cloudv1.RelayLeaseSpec)
		request.RenewLeaseId = current.GetLeaseId()
		timeout := 10 * time.Second
		if half := remaining / 2; half < timeout {
			timeout = half
		}
		renewContext, stopRenewal := context.WithTimeout(ctx, timeout)
		renewed, renewErr := service.config.Relay.RenewRelayLease(renewContext, request, current)
		stopRenewal()
		if renewErr == nil {
			if err := validateRelayRenewal(current, renewed, now); err != nil {
				cancel(err)
				return
			}
			current = cloneRelay(renewed)
			retryDelay = 0
			continue
		}
		retryDelay = nextRelayRenewalRetry(retryDelay, remaining)
	}
}

func relayLeaseRemaining(relay *cloudv1.RelayICEConfig, now time.Time) (time.Duration, error) {
	if relay == nil || strings.TrimSpace(relay.GetLeaseId()) == "" || strings.TrimSpace(relay.GetUsername()) == "" ||
		strings.TrimSpace(relay.GetCredential()) == "" || relay.GetExpiresAt() == nil || relay.GetExpiresAt().CheckValid() != nil {
		return 0, errors.New("RelayLease renewal state is incomplete")
	}
	remaining := relay.GetExpiresAt().AsTime().Sub(now.UTC())
	if remaining <= 0 {
		return 0, errors.New("RelayLease expired before renewal completed")
	}
	return remaining, nil
}

func validateRelayRenewal(current, renewed *cloudv1.RelayICEConfig, now time.Time) error {
	if _, err := relayLeaseRemaining(renewed, now); err != nil {
		return err
	}
	if current == nil || renewed.GetLeaseId() != current.GetLeaseId() || renewed.GetUsername() != current.GetUsername() || renewed.GetCredential() != current.GetCredential() ||
		!renewed.GetExpiresAt().AsTime().After(current.GetExpiresAt().AsTime()) {
		return errors.New("RelayLease renewal changed credential identity or did not extend expiry")
	}
	return nil
}

func nextRelayRenewalRetry(previous, remaining time.Duration) time.Duration {
	next := previous * 2
	if next <= 0 {
		next = time.Second
	}
	if next > 15*time.Second {
		next = 15 * time.Second
	}
	if half := remaining / 2; next > half {
		next = half
	}
	if next <= 0 {
		return time.Nanosecond
	}
	return next
}

func (service *Service) cleanupSession(sessionID string, generation uint64) {
	if closer, ok := service.config.Relay.(RelaySessionCloser); ok {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = closer.CloseRelaySession(cleanup, sessionID)
		cancel()
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = service.config.Runtime.RemoveSession(cleanup, sessionID, generation)
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

func (service *Service) authorizeClient(ctx context.Context, claims *admissionClaims, sessionID string) error {
	correlationID := uuid.NewString()
	agentGeneration, response, err := service.config.Runtime.BeginAgentSignal(ctx, correlationID, claims.daemonID, sessionID)
	if err != nil {
		return errors.New("target daemon is no longer online")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = service.config.Runtime.CancelAgentSignal(cleanup, correlationID)
	}()
	command := &cloudv1.EdgeCommand{Payload: &cloudv1.EdgeCommand_Authorize{Authorize: &cloudv1.AgentAuthorize{
		CorrelationId: correlationID, SessionId: sessionID, AgentGeneration: agentGeneration, ClientPublicKey: append([]byte(nil), claims.clientPublicKey...), Product: claims.product,
		AccessMode: claims.accessMode, PairingClaimSha256: append([]byte(nil), claims.pairingClaimDigest...),
	}}}
	if err := service.config.Runtime.SendAgentCommand(ctx, claims.daemonID, agentGeneration, command); err != nil {
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

type admissionClaims struct {
	accountID, daemonID, clientID string
	clientPublicKey               []byte
	product                       cloudv1.ClientProduct
	accessMode                    cloudv1.CloudClientAccessMode
	pairingClaimDigest            []byte
}

func (service *Service) admit(ctx context.Context, event *cloudv1.ClientSignal, challenge *cloudv1.EdgeChallenge) (*admissionClaims, error) {
	if event == nil || event.GetProtocolVersion() != ProtocolVersion || event.GetStreamSeq() != 1 || event.GetHello() == nil || strings.TrimSpace(event.GetMessageId()) == "" || strings.TrimSpace(event.GetBootId()) == "" || strings.TrimSpace(event.GetConnectionId()) == "" || event.GetSentAt() == nil || event.GetSentAt().CheckValid() != nil {
		return nil, errors.New("ClientHello envelope is invalid")
	}
	now := service.config.Now().UTC()
	if err := ticket.ValidateEdgeChallenge(challenge, cloudv1.EdgeChallengeTarget_EDGE_CHALLENGE_TARGET_CLIENT_GATEWAY, now); err != nil ||
		challenge.GetEdgeId() != service.config.EdgeID || challenge.GetEdgeBootId() != service.config.EdgeBootID {
		return nil, errors.New("ClientGateway challenge is invalid")
	}
	hello := event.GetHello()
	if len(hello.GetClientPublicKey()) != ed25519.PublicKeySize || hello.GetAttemptGeneration() == 0 || hello.GetProduct() < cloudv1.ClientProduct_CLIENT_PRODUCT_TUI || hello.GetProduct() > cloudv1.ClientProduct_CLIENT_PRODUCT_DESKTOP_GUI || strings.TrimSpace(hello.GetSoftwareVersion()) == "" {
		return nil, errors.New("ClientHello identity is incomplete")
	}
	if hello.GetRelayPreference() < cloudv1.RelayPreference_RELAY_PREFERENCE_AUTO || hello.GetRelayPreference() > cloudv1.RelayPreference_RELAY_PREFERENCE_RELAY_ONLY {
		return nil, errors.New("ClientHello Relay preference is invalid")
	}
	var daemonID string
	var accessMode cloudv1.CloudClientAccessMode
	switch authorization := hello.GetAuthorization().(type) {
	case *cloudv1.ClientHello_CloudRouteGrant:
		daemonID = cloudRouteGrantDaemonID(authorization.CloudRouteGrant)
		accessMode = cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_CAPABILITY
	case *cloudv1.ClientHello_PairingAdmission:
		if authorization.PairingAdmission != nil {
			daemonID = strings.TrimSpace(authorization.PairingAdmission.GetDaemonId())
		}
		accessMode = cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_PAIRING
	default:
		return nil, errors.New("ClientHello authorization is missing")
	}
	if daemonID == "" {
		return nil, errors.New("ClientHello authorization payload is invalid")
	}
	agent, err := service.config.Runtime.AuthenticatedAgentClaims(ctx, daemonID)
	if err != nil || agent == nil {
		return nil, errRouteStale
	}
	if agent.GetEdgeId() != service.config.EdgeID || agent.GetDaemonId() != daemonID {
		return nil, errRouteStale
	}
	claims := &admissionClaims{accountID: agent.GetAccountId(), daemonID: daemonID, clientPublicKey: append([]byte(nil), hello.GetClientPublicKey()...), product: hello.GetProduct(), accessMode: accessMode}
	if err := ticket.VerifyClientHelloProof(claims.clientPublicKey, hello.GetClientProof(), challenge, event, now); err != nil {
		return nil, err
	}
	switch authorization := hello.GetAuthorization().(type) {
	case *cloudv1.ClientHello_CloudRouteGrant:
		verified, verifyErr := ticket.VerifyCloudRouteGrant(authorization.CloudRouteGrant, agent.GetDevicePublicKey(), daemonID, service.config.Now().UTC())
		if verifyErr != nil {
			return nil, verifyErr
		}
		if !bytes.Equal(verified.GetClientPublicKey(), hello.GetClientPublicKey()) || verified.GetProduct() != hello.GetProduct() {
			return nil, errors.New("ClientHello identity or product does not match CloudRouteGrant")
		}
	case *cloudv1.ClientHello_PairingAdmission:
		admission := authorization.PairingAdmission
		expiresAt := time.Unix(0, admission.GetExpiresAtUnixNano()).UTC()
		if admission.GetDeviceId() != agent.GetDeviceId() || !bytes.Equal(admission.GetDevicePublicKey(), agent.GetDevicePublicKey()) || len(admission.GetPairingClaimSha256()) != sha256.Size ||
			admission.GetExpiresAtUnixNano() <= 0 || !expiresAt.After(now) || expiresAt.After(now.Add(24*time.Hour)) {
			return nil, errors.New("pairing admission does not match the online daemon or validity window")
		}
		claims.pairingClaimDigest = append([]byte(nil), admission.GetPairingClaimSha256()...)
	default:
		return nil, errors.New("ClientHello authorization mode is invalid")
	}
	claims.clientID = remoteauth.Fingerprint(claims.clientPublicKey)
	if event.GetSenderId() != claims.clientID {
		return nil, errors.New("ClientHello sender does not match client public key")
	}
	return claims, nil
}

func (service *Service) challengeSignal() (*cloudv1.EdgeSignal, *cloudv1.EdgeChallenge, error) {
	nonce := make([]byte, ticket.EdgeChallengeNonceSize)
	if _, err := io.ReadFull(service.config.ChallengeRandom, nonce); err != nil {
		return nil, nil, err
	}
	now := service.config.Now().UTC()
	challenge := &cloudv1.EdgeChallenge{
		Nonce: nonce, EdgeId: service.config.EdgeID, EdgeBootId: service.config.EdgeBootID, StreamId: uuid.NewString(),
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(ticket.EdgeChallengeLifetime)), Target: cloudv1.EdgeChallengeTarget_EDGE_CHALLENGE_TARGET_CLIENT_GATEWAY,
	}
	signal := &cloudv1.EdgeSignal{
		ProtocolVersion: ProtocolVersion, MessageId: uuid.NewString(), SenderId: service.config.EdgeID, BootId: service.config.EdgeBootID,
		ConnectionId: challenge.GetStreamId(), StreamSeq: 1, SentAt: timestamppb.New(now), Payload: &cloudv1.EdgeSignal_Challenge{Challenge: proto.Clone(challenge).(*cloudv1.EdgeChallenge)},
	}
	return signal, challenge, nil
}

func cloudRouteGrantDaemonID(grant *cloudv1.SignedEnvelope) string {
	if grant == nil {
		return ""
	}
	claims := &cloudv1.CloudRouteGrantClaims{}
	if proto.Unmarshal(grant.GetPayload(), claims) == nil {
		return strings.TrimSpace(claims.GetDaemonId())
	}
	return ""
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
	if len(event.GetOffer().GetOfferSdp()) > maxOfferSDPBytes || len(event.GetOffer().GetCandidates()) > maxICECandidates {
		return errors.New("ClientOffer exceeds signaling limits")
	}
	for _, candidate := range event.GetOffer().GetCandidates() {
		if candidate == nil || len(candidate.GetCandidate()) > maxICECandidateBytes || len(candidate.GetSdpMid()) > 256 || len(candidate.GetUsernameFragment()) > 256 {
			return errors.New("ClientOffer ICE candidate exceeds signaling limits")
		}
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
