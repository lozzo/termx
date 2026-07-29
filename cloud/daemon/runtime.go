package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/edge/agentgateway"
	"github.com/anytty/anytty/cloud/securetransport"
	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/proto/remoteauthpb"
	remotedaemon "github.com/anytty/anytty/remote/daemon"
	"github.com/anytty/anytty/remote/webrtc"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Config 是 daemon Cloud owner 的稳定 identity、发现记录和真实 P2P answerer 装配。
type Config struct {
	Record          EnrollmentRecord
	Identity        remoteauth.Identity
	Answerer        webrtc.Answerer
	AccessStore     *remoteauth.AccessStore
	SoftwareVersion string
	RetryMinimum    time.Duration
	RetryMaximum    time.Duration
}

// Runtime 只使用 enrollment 持久化的 binding 和 locator 维持 AgentGateway generation。
type Runtime struct{ config Config }

type authorizedRuntimeOptions struct {
	pionLogger *slog.Logger
}

// AuthorizedRuntimeOption configures process-owned dependencies for the Cloud WebRTC answerer.
type AuthorizedRuntimeOption func(*authorizedRuntimeOptions)

// WithPionLogger routes embedded Pion diagnostics through the daemon logger.
func WithPionLogger(logger *slog.Logger) AuthorizedRuntimeOption {
	return func(options *authorizedRuntimeOptions) {
		options.pionLogger = logger
	}
}

// NewAuthorizedRuntime 把现有 daemon Core/DeviceIdentity/AccessStore 接到真实 WebRTC Answerer。
// cmd composition root 只传 owner 和只读 session 观测回调，不需要依赖 Cloud 内部的 Pion 装配类型；
// 回调不能修改授权、PeerSession 或 Cloud generation 真值。
func NewAuthorizedRuntime(record EnrollmentRecord, identity remoteauth.Identity, accessStore *remoteauth.AccessStore, core remotedaemon.ScopedTransportServer, softwareVersion string, onSessionStart func(), onSessionError func(error), runtimeOptions ...AuthorizedRuntimeOption) (*Runtime, error) {
	if accessStore == nil {
		return nil, errors.New("daemon Cloud runtime requires AccessStore")
	}
	options := authorizedRuntimeOptions{}
	for _, apply := range runtimeOptions {
		if apply != nil {
			apply(&options)
		}
	}
	locator := &cloudv1.EdgeLocator{}
	if err := proto.Unmarshal(record.EdgeLocator, locator); err != nil {
		return nil, errors.New("daemon Cloud runtime Edge locator is invalid")
	}
	caFingerprint, err := securetransport.EdgeCACertificateDERFingerprint(locator.GetCaCertificatePem())
	if err != nil {
		return nil, err
	}
	if err := accessStore.ConfigureManagedRouteGrantIssuer(func(clientPublicKey ed25519.PublicKey, product uint32, issuedAt, expiresAt time.Time) ([]byte, []byte, error) {
		clientProduct := cloudv1.ClientProduct(product)
		if clientProduct == cloudv1.ClientProduct_CLIENT_PRODUCT_UNSPECIFIED || clientProduct > cloudv1.ClientProduct_CLIENT_PRODUCT_DESKTOP_GUI {
			return nil, nil, errors.New("CloudRouteGrant client product is invalid")
		}
		claims := &cloudv1.CloudRouteGrantClaims{GrantId: uuid.NewString(), DaemonId: record.DaemonID, ClientPublicKey: append([]byte(nil), clientPublicKey...), Product: clientProduct, IssuedAt: timestamppb.New(issuedAt.UTC()), ExpiresAt: timestamppb.New(expiresAt.UTC())}
		envelope, err := ticket.SignCloudRouteGrant(identity, claims)
		if err != nil {
			return nil, nil, err
		}
		grant, err := proto.MarshalOptions{Deterministic: true}.Marshal(envelope)
		return grant, append([]byte(nil), record.EdgeLocator...), err
	}); err != nil {
		return nil, err
	}
	if err := accessStore.ConfigureManagedPairingBootstrapIssuer(func() (*remoteauthpb.PairingManagedRouteSeed, error) {
		return &remoteauthpb.PairingManagedRouteSeed{
			DaemonId: record.DaemonID, EdgeId: locator.GetEdgeId(), PublicEndpoint: locator.GetPublicEndpoint(), ServerName: locator.GetServerName(),
			CaCertificateDerSha256: append([]byte(nil), caFingerprint...),
		}, nil
	}); err != nil {
		return nil, err
	}
	answerer := webrtc.Answerer{
		Handler:        remotedaemon.SessionAcceptor{Core: core, Identity: identity, AccessStore: accessStore},
		PionLogger:     options.pionLogger,
		OnSessionStart: onSessionStart,
		OnSessionError: onSessionError,
	}
	return NewRuntime(Config{Record: record, Identity: identity, Answerer: answerer, AccessStore: accessStore, SoftwareVersion: softwareVersion})
}

// NewRuntime 验证 Cloud owner 与 DataChannel 端到端授权 handler 已真实接线。
func NewRuntime(config Config) (*Runtime, error) {
	if err := config.Record.Validate(); err != nil {
		return nil, err
	}
	if err := config.Identity.Validate(); err != nil {
		return nil, err
	}
	config.SoftwareVersion = strings.TrimSpace(config.SoftwareVersion)
	if config.SoftwareVersion == "" || config.Answerer.Handler == nil || config.AccessStore == nil {
		return nil, errors.New("daemon Cloud runtime requires version and authorized WebRTC Answerer")
	}
	if config.RetryMinimum <= 0 {
		config.RetryMinimum = 250 * time.Millisecond
	}
	if config.RetryMaximum < config.RetryMinimum {
		config.RetryMaximum = 5 * time.Second
	}
	return &Runtime{config: config}, nil
}

// Run 维持 AgentGateway 长连接；Controller/Edge 失败只撤销 Presence 并有界重新解析。
func (runtime *Runtime) Run(ctx context.Context) error {
	delay := runtime.config.RetryMinimum
	for ctx.Err() == nil {
		err := runtime.connectOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			delay = runtime.config.RetryMinimum
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < runtime.config.RetryMaximum {
			delay *= 2
			if delay > runtime.config.RetryMaximum {
				delay = runtime.config.RetryMaximum
			}
		}
	}
	return ctx.Err()
}

func (runtime *Runtime) connectOnce(ctx context.Context) error {
	binding := &cloudv1.SignedEnvelope{}
	locator := &cloudv1.EdgeLocator{}
	if proto.Unmarshal(runtime.config.Record.DaemonBinding, binding) != nil || proto.Unmarshal(runtime.config.Record.EdgeLocator, locator) != nil {
		return errors.New("Cloud enrollment binding or Edge locator is invalid")
	}
	return runtime.connectEdge(ctx, binding, locator)
}

func (runtime *Runtime) connectEdge(ctx context.Context, binding *cloudv1.SignedEnvelope, locator *cloudv1.EdgeLocator) error {
	if binding == nil || locator == nil {
		return errors.New("daemon binding or Edge locator is incomplete")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(locator.GetCaCertificatePem()) {
		return errors.New("Edge CA certificate is invalid")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: locator.GetServerName()}
	connection, err := grpc.NewClient(locator.GetPublicEndpoint(), grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return err
	}
	defer connection.Close()
	stream, err := cloudv1.NewAgentGatewayClient(connection).Connect(ctx)
	if err != nil {
		return err
	}
	bootID, connectionID := uuid.NewString(), uuid.NewString()
	proof, err := ticket.SignAgentHelloProof(runtime.config.Identity, binding, runtime.config.Record.DaemonID, bootID, connectionID)
	if err != nil {
		return err
	}
	hello := &cloudv1.AgentEvent{ProtocolVersion: agentgateway.ProtocolVersion, MessageId: uuid.NewString(), SenderId: runtime.config.Record.DaemonID, BootId: bootID, ConnectionId: connectionID, StreamSeq: 1, SentAt: timestamppb.Now(), Payload: &cloudv1.AgentEvent_Hello{Hello: &cloudv1.AgentHello{DaemonBinding: binding, DeviceProof: proof, SoftwareVersion: runtime.config.SoftwareVersion}}}
	if err := stream.Send(hello); err != nil {
		return err
	}
	command, err := stream.Recv()
	if err != nil {
		return err
	}
	if command.GetReady() == nil || command.GetProtocolVersion() != agentgateway.ProtocolVersion || command.GetConnectionId() != connectionID || command.GetStreamSeq() != 1 {
		return errors.New("AgentReady is invalid")
	}
	interval := command.GetReady().GetHeartbeat().GetInterval().AsDuration()
	if interval <= 0 {
		return errors.New("AgentReady heartbeat is invalid")
	}
	connectionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	outbound := make(chan *cloudv1.AgentEvent, 32)
	writerErrors := make(chan error, 1)
	go runtime.runAgentWriter(connectionCtx, stream, bootID, connectionID, 1, outbound, writerErrors)
	receive := make(chan error, 1)
	go runtime.runEdgeCommands(connectionCtx, stream, command.GetReady().GetGeneration(), outbound, receive)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case writeErr := <-writerErrors:
			return writeErr
		case recvErr := <-receive:
			if errors.Is(recvErr, io.EOF) {
				return nil
			}
			return recvErr
		case <-ticker.C:
			event := &cloudv1.AgentEvent{Payload: &cloudv1.AgentEvent_Heartbeat{Heartbeat: &cloudv1.AgentHeartbeat{Generation: command.GetReady().GetGeneration()}}}
			select {
			case outbound <- event:
			default:
				return errors.New("AgentGateway writer queue is full")
			}
		}
	}
}

func (runtime *Runtime) runAgentWriter(ctx context.Context, stream cloudv1.AgentGateway_ConnectClient, bootID, connectionID string, sequence uint64, outbound <-chan *cloudv1.AgentEvent, failures chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-outbound:
			sequence++
			event.ProtocolVersion = agentgateway.ProtocolVersion
			event.MessageId = uuid.NewString()
			event.SenderId = runtime.config.Record.DaemonID
			event.BootId = bootID
			event.ConnectionId = connectionID
			event.StreamSeq = sequence
			event.SentAt = timestamppb.Now()
			if err := stream.Send(event); err != nil {
				select {
				case failures <- err:
				default:
				}
				return
			}
		}
	}
}

func (runtime *Runtime) runEdgeCommands(ctx context.Context, stream cloudv1.AgentGateway_ConnectClient, generation uint64, outbound chan<- *cloudv1.AgentEvent, failures chan<- error) {
	for {
		command, err := stream.Recv()
		if err != nil {
			failures <- err
			return
		}
		var response *cloudv1.AgentEvent
		switch {
		case command.GetAuthorize() != nil:
			authorize := command.GetAuthorize()
			if authorize.GetAgentGeneration() != generation || strings.TrimSpace(authorize.GetCorrelationId()) == "" || strings.TrimSpace(authorize.GetSessionId()) == "" || len(authorize.GetClientPublicKey()) != ed25519.PublicKeySize {
				failures <- errors.New("Edge client authorization command is invalid")
				return
			}
			allowed := runtime.allowsCloudAccess(authorize.GetClientPublicKey(), authorize.GetAccessMode(), authorize.GetPairingClaimSha256(), time.Now().UTC())
			result := &cloudv1.AgentAuthorizationResult{CorrelationId: authorize.GetCorrelationId(), SessionId: authorize.GetSessionId(), Authorized: allowed}
			if !allowed {
				result.Code, result.Message = cloudAccessRejection(authorize.GetAccessMode())
			}
			response = &cloudv1.AgentEvent{Payload: &cloudv1.AgentEvent_Authorization{Authorization: result}}
		case command.GetOffer() != nil:
			offer := command.GetOffer()
			if offer.GetAgentGeneration() != generation || strings.TrimSpace(offer.GetCorrelationId()) == "" || strings.TrimSpace(offer.GetSessionId()) == "" {
				failures <- errors.New("Edge signaling command is invalid")
				return
			}
			response = runtime.answerOffer(ctx, offer)
		default:
			failures <- errors.New("Edge command payload is unsupported")
			return
		}
		select {
		case <-ctx.Done():
			return
		case outbound <- response:
		default:
			failures <- errors.New("AgentGateway writer queue is full")
			return
		}
	}
}

func (runtime *Runtime) answerOffer(ctx context.Context, offer *cloudv1.AgentOffer) *cloudv1.AgentEvent {
	reject := func(code, message string) *cloudv1.AgentEvent {
		return &cloudv1.AgentEvent{Payload: &cloudv1.AgentEvent_Rejected{Rejected: &cloudv1.AgentSignalRejected{CorrelationId: offer.GetCorrelationId(), SessionId: offer.GetSessionId(), Code: code, Message: message}}}
	}
	if !runtime.allowsCloudAccess(offer.GetClientPublicKey(), offer.GetAccessMode(), offer.GetPairingClaimSha256(), time.Now().UTC()) {
		code, message := cloudAccessRejection(offer.GetAccessMode())
		return reject(code, message)
	}
	candidates := make([]webrtc.ICECandidate, 0, len(offer.GetCandidates()))
	for _, candidate := range offer.GetCandidates() {
		if candidate != nil {
			candidates = append(candidates, webrtc.ICECandidate{Candidate: candidate.GetCandidate(), SDPMid: candidate.GetSdpMid(), SDPMLineIndex: candidate.GetSdpMlineIndex(), UsernameFragment: candidate.GetUsernameFragment()})
		}
	}
	iceServers := make([]webrtc.ICEServer, 0, 1)
	if relay := offer.GetRelay(); relay != nil {
		if len(relay.GetUrls()) == 0 || strings.TrimSpace(relay.GetUsername()) == "" || strings.TrimSpace(relay.GetCredential()) == "" {
			return reject("RELAY_INVALID", "Edge supplied incomplete Relay ICE material")
		}
		iceServers = append(iceServers, webrtc.ICEServer{URLs: append([]string(nil), relay.GetUrls()...), Username: relay.GetUsername(), Credential: relay.GetCredential()})
	}
	answer, err := runtime.config.Answerer.Answer(ctx, &webrtc.SignalingOffer{SessionID: offer.GetSessionId(), SDP: offer.GetOfferSdp(), Candidates: candidates}, iceServers)
	if err != nil {
		return reject("ANSWER_FAILED", "daemon could not establish P2P signaling")
	}
	wireCandidates := make([]*cloudv1.CloudICECandidate, 0, len(answer.Candidates))
	for _, candidate := range answer.Candidates {
		wireCandidates = append(wireCandidates, &cloudv1.CloudICECandidate{Candidate: candidate.Candidate, SdpMid: candidate.SDPMid, SdpMlineIndex: candidate.SDPMLineIndex, UsernameFragment: candidate.UsernameFragment})
	}
	return &cloudv1.AgentEvent{Payload: &cloudv1.AgentEvent_Answer{Answer: &cloudv1.AgentAnswer{CorrelationId: offer.GetCorrelationId(), SessionId: offer.GetSessionId(), AnswerSdp: answer.SDP, Candidates: wireCandidates}}}
}

func (runtime *Runtime) allowsCloudAccess(clientPublicKey []byte, mode cloudv1.CloudClientAccessMode, pairingClaimDigest []byte, now time.Time) bool {
	if runtime == nil || runtime.config.AccessStore == nil || len(clientPublicKey) != ed25519.PublicKeySize {
		return false
	}
	publicKey := ed25519.PublicKey(clientPublicKey)
	switch mode {
	case cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_CAPABILITY:
		return len(pairingClaimDigest) == 0 && runtime.config.AccessStore.AllowsClientPublicKey(publicKey, now)
	case cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_PAIRING:
		return runtime.config.AccessStore.AllowsPairingClaimDigest(pairingClaimDigest, publicKey, now)
	default:
		return false
	}
}

func cloudAccessRejection(mode cloudv1.CloudClientAccessMode) (string, string) {
	if mode == cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_CAPABILITY {
		return "CLIENT_REVOKED", "client access is not active"
	}
	return "PAIRING_CLAIM_INVALID", "pairing claim is not active"
}

// Enroll 使用一次性 code 和现有 DeviceIdentity 完成 challenge，并返回可持久化最小记录。
func Enroll(ctx context.Context, controllerAddress, controllerServerName, code string, tlsConfig *tls.Config, identity remoteauth.Identity) (EnrollmentRecord, error) {
	if err := identity.Validate(); err != nil {
		return EnrollmentRecord{}, err
	}
	if tlsConfig == nil {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS13, ServerName: controllerServerName}
	} else {
		tlsConfig = tlsConfig.Clone()
	}
	connection, err := grpc.NewClient(strings.TrimSpace(controllerAddress), grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return EnrollmentRecord{}, err
	}
	defer connection.Close()
	client := cloudv1.NewEnrollmentServiceClient(connection)
	challenge, err := client.BeginDaemonEnrollment(ctx, &cloudv1.BeginDaemonEnrollmentRequest{EnrollmentCode: strings.TrimSpace(code), DeviceId: identity.DeviceID, DeviceFingerprint: identity.Fingerprint, DevicePublicKey: append([]byte(nil), identity.PublicKey...)})
	if err != nil {
		return EnrollmentRecord{}, err
	}
	proof, err := remoteauth.SignDeviceIdentityProof(identity, challenge.GetChallenge())
	if err != nil {
		return EnrollmentRecord{}, err
	}
	completed, err := client.CompleteDaemonEnrollment(ctx, &cloudv1.CompleteDaemonEnrollmentRequest{ChallengeId: challenge.GetChallengeId(), DeviceProof: proof})
	if err != nil {
		return EnrollmentRecord{}, err
	}
	if completed.GetDaemon() == nil || completed.GetDaemonBinding() == nil || completed.GetEdgeLocator() == nil {
		return EnrollmentRecord{}, errors.New("daemon enrollment response is incomplete")
	}
	binding, err := proto.MarshalOptions{Deterministic: true}.Marshal(completed.GetDaemonBinding())
	if err != nil {
		return EnrollmentRecord{}, err
	}
	locator, err := proto.MarshalOptions{Deterministic: true}.Marshal(completed.GetEdgeLocator())
	if err != nil {
		return EnrollmentRecord{}, err
	}
	return EnrollmentRecord{Version: recordVersion, DaemonID: completed.GetDaemon().GetDaemonId(), AccountID: completed.GetDaemon().GetAccountId(), DaemonBinding: binding, EdgeLocator: locator, EnrolledAt: time.Now().UTC()}, nil
}

// EnrollLocal 加载 daemon 已有 DeviceIdentity、完成注册并原子保存最小 Cloud record。
func EnrollLocal(ctx context.Context, controllerAddress, controllerServerName, code, identityDirectory, recordPath string) (EnrollmentRecord, error) {
	identity, err := remoteauth.LoadOrCreateLocalIdentity(identityDirectory)
	if err != nil {
		return EnrollmentRecord{}, err
	}
	record, err := Enroll(ctx, controllerAddress, controllerServerName, code, &tls.Config{MinVersion: tls.VersionTLS13, ServerName: controllerServerName}, identity)
	if err != nil {
		return EnrollmentRecord{}, err
	}
	if err := SaveRecord(recordPath, record); err != nil {
		return EnrollmentRecord{}, err
	}
	return record, nil
}
