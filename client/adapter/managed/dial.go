// Package managed 实现跨 native 与浏览器平台复用的 managed WebRTC route attempt。
// 本包拥有 Cloud signaling、remote auth、protocol Hello 与 ReadyPeerSession 装配顺序；具体 RTCPeerConnection 只能通过 client/port 注入。
package managed

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/client/port"
	clientruntime "github.com/lozzow/termx/client/runtime"
	internalprotocol "github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/proto/wire"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/shared/transport"
	"github.com/lozzow/termx/shared/transport/datachannel"
)

const defaultProtocolClientName = "termx-go-client"

// CloudClient 是 managed attempt 实际需要的 Cloud signaling/route 子集。
// CapabilityGrant、ClientAccessIdentity 和 DataChannel payload 永远不能进入该接口。
type CloudClient interface {
	// ResolveEndpoint 获取当前 managed session 与初始 ICE material。
	ResolveEndpoint(context.Context, *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error)
	// CreateSignalingSession 提交 offer 并返回当前 session 的 answer/candidate stream。
	CreateSignalingSession(context.Context, *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error)
	// AcquireRelayLease 仅为显式 relay-only policy 获取 principal-specific TURN material。
	AcquireRelayLease(context.Context, *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error)
	// PlanManagedRoute 获取短期 SmartRoute 计划；客户端仍必须重新校验全部 material。
	PlanManagedRoute(context.Context, *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error)
	// ReportPathQuality 上报匿名质量窗口，失败不得改变当前 route 或授权。
	ReportPathQuality(context.Context, *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error)
	// ReportConnectionOutcome 上报当前 managed session 的脱敏路径结果。
	ReportConnectionOutcome(context.Context, *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error)
}

// PreparedAuthorization 是当前 endpoint/route 已完成本地 credential 校验后的单次认证事务。
// Authenticate 只能绑定当前 peer 的实际 DTLS certificate；失败后调用方必须关闭 peer，不能切换旧授权路径。
type PreparedAuthorization interface {
	// Authenticate 使用当前 peer certificate fingerprint 完成 DataChannel 内 capability proof。
	Authenticate(context.Context, transport.Transport, string) (remoteauth.Claims, error)
}

// Authorizer 在任何 Cloud 请求前验证 endpoint-bound credential，并冻结本次认证事务。
// 平台 secure store 或 signer 适配位于实现侧，managed dialer 不读取、持久化或记录私钥。
type Authorizer interface {
	// Prepare 在 Cloud 请求前冻结 endpoint-bound credential/signer 事务。
	Prepare(context.Context, clientruntime.AttemptRequest) (PreparedAuthorization, error)
}

// Dialer 是 managed WebRTC 单 route adapter。
// runtime 负责选择唯一 AttemptRequest 和 generation；Dialer 不读取 registry、不竞速其他 route，也不建立 fallback。
type Dialer struct {
	Cloud         CloudClient
	Peers         port.ManagedPeerFactory
	Authorization Authorizer
	ClientName    string
	Now           func() time.Time
	Phase         func(clientruntime.EndpointPhase)
	Quality       QualityObservationOptions
}

// Dial 完成 Cloud route、WebRTC、DTLS-bound remote auth、protocol Hello 和 application session 装配。
// 返回值已经是当前 AttemptRequest generation 的 ReadyPeerSession；任一步失败都会关闭已创建的 peer/channel/protocol 资源。
func (dialer *Dialer) Connect(ctx context.Context, request clientruntime.AttemptRequest) (clientruntime.ReadyPeerSession, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	route := request.Route()
	if route.Kind != endpoint.RouteManagedWebRTC {
		return nil, fmt.Errorf("route %q kind %q is not managed WebRTC", route.ID, route.Kind)
	}
	if dialer == nil || dialer.Cloud == nil || dialer.Peers == nil || dialer.Authorization == nil {
		return nil, fmt.Errorf("managed WebRTC dialer dependencies are incomplete")
	}
	qualityOptions, err := normalizeQualityObservationOptions(dialer.Quality)
	if err != nil {
		return nil, err
	}
	prepared, err := dialer.Authorization.Prepare(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("prepare managed endpoint authorization: %w", err)
	}
	if prepared == nil {
		return nil, fmt.Errorf("managed endpoint authorizer returned no transaction")
	}

	dialer.reportPhase(clientruntime.EndpointPhaseResolving)
	resolved, err := dialer.Cloud.ResolveEndpoint(ctx, &cloudpb.ResolveEndpointRequest{
		EndpointId: string(request.EndpointID()), TargetDeviceId: request.DaemonIdentity().DeviceID,
	})
	if err != nil {
		return nil, err
	}
	if err := validateResolution(request, resolved); err != nil {
		return nil, err
	}
	policy, err := cloudcompanion.DialPolicyForRelayMode(route.RelayMode)
	if err != nil {
		return nil, err
	}
	selected, err := resolveDialRoute(ctx, dialer.Cloud, request, resolved, policy, dialer.now())
	if err != nil {
		return nil, err
	}
	peer, err := dialer.Peers.OpenManagedPeer(ctx, selected.iceServers, selected.preference, selected.relayOnly)
	if err != nil {
		return nil, fmt.Errorf("create managed endpoint peer: %w", err)
	}
	if peer == nil || peer.Channel() == nil {
		if peer != nil {
			_ = peer.Close()
		}
		return nil, fmt.Errorf("managed endpoint peer has no protocol DataChannel")
	}
	transportConnection := datachannel.New(peer.Channel())
	closeAttempt := func() {
		_ = transportConnection.Close()
		_ = peer.Close()
	}

	offer, err := peer.CreateOffer(ctx)
	if err != nil {
		closeAttempt()
		return nil, fmt.Errorf("create managed endpoint offer: %w", err)
	}
	dialer.reportPhase(clientruntime.EndpointPhaseSignaling)
	signaling, err := dialer.Cloud.CreateSignalingSession(ctx, &cloudpb.CreateSignalingSessionRequest{
		EndpointId: string(request.EndpointID()), ManagedSessionId: resolved.GetManagedSessionId(),
		TargetDeviceId: request.DaemonIdentity().DeviceID, OfferSdp: offer,
		RoutePreference: selected.preference, RelayOnly: selected.relayOnly,
	})
	if err != nil {
		closeAttempt()
		return nil, err
	}
	if signaling == nil {
		closeAttempt()
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an empty signaling stream")
	}
	answer, candidates, receiveErr := receiveAnswer(signaling)
	closeErr := signaling.Close()
	if receiveErr != nil {
		closeAttempt()
		return nil, receiveErr
	}
	if closeErr != nil {
		closeAttempt()
		return nil, closeErr
	}
	if err := peer.ApplyAnswer(ctx, answer.GetSdp(), append(candidates, answer.GetCandidates()...)); err != nil {
		closeAttempt()
		return nil, fmt.Errorf("apply managed endpoint answer: %w", err)
	}
	dialer.reportPhase(clientruntime.EndpointPhaseConnecting)
	if err := peer.WaitReady(ctx); err != nil {
		closeAttempt()
		return nil, err
	}
	if selected.expectedPath != "" && peer.ObservedPath() != selected.expectedPath {
		closeAttempt()
		return nil, routePlanProtocolError(fmt.Sprintf("managed route plan selected %q but ICE established %q", selected.expectedPath, peer.ObservedPath()))
	}
	fingerprint, err := peer.RemoteCertificateFingerprint()
	if err != nil {
		closeAttempt()
		return nil, fmt.Errorf("read managed endpoint DTLS certificate: %w", err)
	}
	dialer.reportPhase(clientruntime.EndpointPhaseAuthorizing)
	if _, err := prepared.Authenticate(ctx, transportConnection, fingerprint); err != nil {
		closeAttempt()
		return nil, fmt.Errorf("authenticate managed endpoint DataChannel: %w", err)
	}

	protocolClient := internalprotocol.NewClient(transportConnection)
	clientName := strings.TrimSpace(dialer.ClientName)
	if clientName == "" {
		clientName = defaultProtocolClientName
	}
	if err := protocolClient.Hello(ctx, internalprotocol.Hello{Version: wire.Version, Client: clientName}); err != nil {
		_ = protocolClient.Close()
		_ = peer.Close()
		return nil, fmt.Errorf("managed endpoint protocol Hello: %w", err)
	}
	application, err := clientruntime.NewApplicationSession(request.Stamp(), protocolClient)
	if err != nil {
		_ = protocolClient.Close()
		_ = peer.Close()
		return nil, err
	}
	session := &Session{
		stamp: request.Stamp(), observedPath: string(peer.ObservedPath()), selectionReason: string(selected.selectionReason),
		evidence: clientruntime.ReadyPeerSessionEvidence{
			Identity: request.DaemonIdentity(), IdentityVerified: true, AuthorizationVerified: true, ProtocolVersion: wire.Version,
		},
		peer: peer, protocol: protocolClient, application: application,
		observationDone: make(chan struct{}), closeRequested: make(chan struct{}),
	}
	var reporter *qualityReporter
	if qualityOptions.Enabled {
		reporter = &qualityReporter{
			cloud: dialer.Cloud, managedSessionID: resolved.GetManagedSessionId(), options: qualityOptions, startedAt: time.Now().UTC(),
		}
	}
	session.startLifecycle(reporter)
	dialer.reportPhase(clientruntime.EndpointPhaseReady)
	return session, nil
}

func (dialer *Dialer) now() time.Time {
	if dialer != nil && dialer.Now != nil {
		return dialer.Now().UTC()
	}
	return time.Now().UTC()
}

func (dialer *Dialer) reportPhase(phase clientruntime.EndpointPhase) {
	if dialer != nil && dialer.Phase != nil {
		dialer.Phase(phase)
	}
}

func validateResolution(request clientruntime.AttemptRequest, resolved *cloudpb.ResolvedEndpoint) error {
	if resolved == nil || strings.TrimSpace(resolved.GetManagedSessionId()) == "" || strings.TrimSpace(resolved.GetManagedSessionId()) != resolved.GetManagedSessionId() {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an invalid endpoint resolution")
	}
	if resolved.GetEndpointId() != "" && resolved.GetEndpointId() != string(request.EndpointID()) {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion resolved a different endpoint")
	}
	if resolved.GetTargetDeviceId() != "" && resolved.GetTargetDeviceId() != request.DaemonIdentity().DeviceID {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion resolved a different target device")
	}
	return nil
}

func receiveAnswer(stream cloudcompanion.SignalingStream) (*cloudpb.SignalingAnswer, []*cloudpb.IceCandidate, error) {
	var candidates []*cloudpb.IceCandidate
	for {
		event, err := stream.Receive()
		if err != nil {
			return nil, nil, err
		}
		if event == nil {
			return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an empty signaling event")
		}
		switch payload := event.GetPayload().(type) {
		case *cloudpb.SignalingEvent_Answer:
			if payload.Answer == nil || strings.TrimSpace(payload.Answer.GetSdp()) == "" {
				return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an empty signaling answer")
			}
			return payload.Answer, candidates, nil
		case *cloudpb.SignalingEvent_Candidate:
			if payload.Candidate != nil {
				candidates = append(candidates, payload.Candidate)
			}
		case *cloudpb.SignalingEvent_Error:
			return nil, nil, cloudcompanion.ErrorFromWire(payload.Error)
		case *cloudpb.SignalingEvent_Closed:
			reason := "signaling session closed"
			if payload.Closed != nil && strings.TrimSpace(payload.Closed.GetReason()) != "" {
				reason = payload.Closed.GetReason()
			}
			return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE, reason)
		default:
			return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an unknown signaling event")
		}
	}
}

// Session 是 managed adapter 产出的 authenticated protocol session。
// runtime/binding 应只消费 ApplicationReadyPeerSession；FramingClient 仅供当前仓库内 attachment/file stream adapter 使用，不得跨 JNI/WASM 暴露。
type Session struct {
	stamp           clientruntime.EndpointSessionStamp
	observedPath    string
	selectionReason string
	evidence        clientruntime.ReadyPeerSessionEvidence
	peer            port.ManagedPeer
	protocol        *internalprotocol.Client
	application     *clientruntime.ApplicationSession
	closeOnce       sync.Once
	closeErr        error
	observationDone chan struct{}
	closeRequested  chan struct{}
	closeSignalOnce sync.Once
}

// Stamp 返回 runtime 分配且在 attempt 成功后冻结的 endpoint generation。
func (session *Session) Stamp() clientruntime.EndpointSessionStamp { return session.stamp }

// ObservedPath 返回当前 peer 的 direct/single-relay 投影，不改变 route identity。
func (session *Session) ObservedPath() string { return session.observedPath }

// Readiness 返回 remote-auth 与 protocol Hello 已完成的冻结证据。
func (session *Session) Readiness() clientruntime.ReadyPeerSessionEvidence { return session.evidence }

// Done 返回 protocol connection 生命周期终止信号；不能据此推断 daemon terminal lifecycle。
func (session *Session) Done() <-chan struct{} { return session.protocol.Done() }

// Err 返回 protocol/transport 最终错误；session 尚未关闭或正常关闭时返回 nil。
func (session *Session) Err() error { return session.protocol.Err() }

// Close 幂等结束 protocol、质量观测、DataChannel 与 peer 生命周期。
func (session *Session) Close() error {
	if session == nil {
		return nil
	}
	session.closeOnce.Do(func() {
		session.closeSignalOnce.Do(func() { close(session.closeRequested) })
		session.closeErr = session.protocol.Close()
		<-session.observationDone
	})
	return session.closeErr
}

func (session *Session) startLifecycle(reporter *qualityReporter) {
	go func() {
		if reporter == nil {
			select {
			case <-session.protocol.Done():
			case <-session.closeRequested:
			}
		} else {
			reporter.run(session.peer, session.protocol.Done(), session.closeRequested)
		}
		_ = session.peer.Close()
		close(session.observationDone)
	}()
}

// ExecuteApplication 使用当前 generation 的 ApplicationSession 写入 request/operation stamp 后执行 generated Proto command。
func (session *Session) ExecuteApplication(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	return session.application.Execute(ctx, command)
}

// ExecuteApplicationTerminal 使用同一 managed generation 的 ApplicationSession 补齐 correlation stamp，并把取消后的有界 terminal response 交给 protocol。
// 该方法不解释业务 command；是否选择 terminal response 由 binding 的资源 owner 决定。
func (session *Session) ExecuteApplicationTerminal(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	return session.application.ExecuteTerminal(ctx, command)
}

// ApplicationEvents 返回当前 authenticated connection 的 generated Proto event stream。
func (session *Session) ApplicationEvents(ctx context.Context) (<-chan *apipb.EventEnvelope, error) {
	return session.protocol.ApplicationEvents(ctx)
}

// OpenResourceStream 把 Proto resource handle 绑定到当前 connection 的私有 framing channel。
// channel 编号不跨出该 adapter；跨语言调用方只能持有 binding 分配的 opaque stream handle。
func (session *Session) OpenResourceStream(resource *apipb.ResourceHandle) (clientruntime.ResourceStream, error) {
	channel, ok := session.protocol.ApplicationResourceChannel(resource)
	if !ok {
		return nil, fmt.Errorf("application resource is not bound to this session")
	}
	frames, stop := session.protocol.Stream(channel)
	return &managedResourceStream{client: session.protocol, channel: channel, frames: frames, stop: stop}, nil
}

// ApplicationAttachmentChannel 返回当前 managed protocol connection 内 attachment resource 的 channel correlation。
func (session *Session) ApplicationAttachmentChannel(resource *apipb.ResourceHandle) (uint16, bool) {
	return session.protocol.ApplicationAttachmentChannel(resource)
}

// ApplicationAttachment 返回当前 managed protocol connection 内 channel 对应的 attachment resource。
func (session *Session) ApplicationAttachment(channel uint16) (*apipb.ResourceHandle, bool) {
	return session.protocol.ApplicationAttachment(channel)
}

type managedResourceStream struct {
	client  *internalprotocol.Client
	channel uint16
	frames  <-chan internalprotocol.StreamFrame
	stop    func()
	once    sync.Once
}

func (stream *managedResourceStream) Receive(ctx context.Context) (uint8, []byte, error) {
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case frame, ok := <-stream.frames:
		if !ok {
			return 0, nil, io.EOF
		}
		return frame.Type, append([]byte(nil), frame.Payload...), nil
	}
}

func (stream *managedResourceStream) Send(ctx context.Context, typ uint8, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return stream.client.SendFileFrame(stream.channel, typ, payload)
}

func (stream *managedResourceStream) Close() error {
	stream.once.Do(stream.stop)
	return nil
}

// FramingClient 返回当前 authenticated connection 的内部 framing client。
// 该入口只允许 Go attachment/file stream adapter 使用；业务 command 仍必须走 ApplicationReadyPeerSession，平台 binding 不得导出此对象。
func (session *Session) FramingClient() *internalprotocol.Client {
	if session == nil {
		return nil
	}
	return session.protocol
}

var _ clientruntime.ReadyPeerSession = (*Session)(nil)
var _ clientruntime.ProtoApplicationExecutor = (*Session)(nil)
var _ clientruntime.ApplicationReadyPeerSession = (*Session)(nil)
var _ clientruntime.ResourceStreamSession = (*Session)(nil)
var _ clientruntime.ApplicationAttachmentSession = (*Session)(nil)
