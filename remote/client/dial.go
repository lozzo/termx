// Package client owns the public client-side managed WebRTC dialer.
package client

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	remotev2webrtc "github.com/lozzow/termx/remote/webrtc"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/shared/transport"
	"github.com/lozzow/termx/shared/transport/datachannel"
	pion "github.com/pion/webrtc/v4"
)

const protocolChannelLabel = "protocol"

// DialOptions 是一个 managed cloud endpoint 的公开连接输入。
// Companion 只接收 endpoint、device、signaling 和 route preference；fingerprint 与 grant 始终留在当前公开进程。
type DialOptions struct {
	Companion          cloudcompanion.Client
	EndpointID         string
	TargetDeviceID     string
	DeviceFingerprint  string
	CapabilityGrant    string
	RoutePreference    cloudpb.RoutePreference
	RelayOnly          bool
	AuthRandom         io.Reader
	Now                time.Time
	QualityObservation QualityObservationOptions
	// Phase 把不含 credential 或网络地址的稳定连接阶段回投给 client/TUI。
	// 回调只用于展示，不能修改 route、授权或 fallback 行为。
	Phase func(cloudcompanion.EndpointPhase)
}

// Session 是已完成设备证明和 capability handshake 的 managed WebRTC 连接结果。
// Transport 承载标准 termx protocol；ObservedPath 与 RouteSelectionReason 只投影当前 ICE 路径，不改变 endpoint identity 或授权 scope。
type Session struct {
	Transport            transport.Transport
	ObservedPath         cloudcompanion.Path
	RouteSelectionReason cloudcompanion.RouteSelectionReason
	ObservationDone      <-chan struct{}
}

// DialSession 建立 managed WebRTC session，并返回公开客户端可展示的实际网络路径。
// relay candidate 只能投影为 single_relay；relay_mesh 必须由受信 Companion route metadata 明确报告，不能从本地 stats 猜测。
func DialSession(ctx context.Context, options DialOptions) (Session, error) {
	protocolTransport, err := Dial(ctx, options)
	if err != nil {
		return Session{}, err
	}
	path := cloudcompanion.PathUnknown
	reason := cloudcompanion.RouteSelectionReason("")
	var observationDone <-chan struct{}
	if peer, ok := protocolTransport.(*peerTransport); ok {
		path = observedPath(peer.peer)
		reason = peer.selectionReason
		observationDone = peer.observationDone
	}
	return Session{Transport: protocolTransport, ObservedPath: path, RouteSelectionReason: reason, ObservationDone: observationDone}, nil
}

// Dial 先本地验证 capability 绑定，再通过 Companion 协商 WebRTC，并在 DataChannel 内完成端到端授权。
// Companion、WebRTC 或授权失败只返回当前 endpoint 错误，不 fallback 到 local、SSH、旧 Hub API、remote UI 或原始 shell。
func Dial(ctx context.Context, options DialOptions) (transport.Transport, error) {
	qualityOptions, err := normalizeQualityObservationOptions(options.QualityObservation)
	if err != nil {
		return nil, err
	}
	endpointID := strings.TrimSpace(options.EndpointID)
	targetDeviceID := strings.TrimSpace(options.TargetDeviceID)
	if endpointID == "" || targetDeviceID == "" {
		return nil, fmt.Errorf("managed WebRTC endpoint and target device are required")
	}
	claims, err := remoteauth.Verify(options.CapabilityGrant, options.DeviceFingerprint, options.Now, nil)
	if err != nil {
		return nil, fmt.Errorf("verify managed endpoint capability grant: %w", err)
	}
	if claims.IssuerDeviceID != targetDeviceID {
		return nil, fmt.Errorf("managed endpoint device mismatch: grant %q registry %q", claims.IssuerDeviceID, targetDeviceID)
	}
	if options.Companion == nil {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING, "Cloud Companion is not configured")
	}
	if options.RoutePreference == cloudpb.RoutePreference_ROUTE_PREFERENCE_UNSPECIFIED {
		return nil, fmt.Errorf("managed WebRTC route preference is required")
	}
	connected := false
	reportDialPhase(options.Phase, cloudcompanion.EndpointPhaseResolving)
	defer func() {
		if !connected {
			reportDialPhase(options.Phase, cloudcompanion.EndpointPhaseFailed)
		}
	}()

	resolved, err := options.Companion.ResolveEndpoint(ctx, &cloudpb.ResolveEndpointRequest{
		EndpointId: endpointID, TargetDeviceId: targetDeviceID,
	})
	if err != nil {
		return nil, err
	}
	if resolved == nil || strings.TrimSpace(resolved.GetManagedSessionId()) == "" || strings.TrimSpace(resolved.GetManagedSessionId()) != resolved.GetManagedSessionId() {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an invalid endpoint resolution")
	}
	if resolved.GetTargetDeviceId() != "" && strings.TrimSpace(resolved.GetTargetDeviceId()) != targetDeviceID {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion resolved a different target device")
	}
	route, err := resolveDialRoute(ctx, options, endpointID, targetDeviceID, resolved)
	if err != nil {
		return nil, err
	}
	configuration, err := peerConfiguration(route.iceServers, route.preference, route.relayOnly)
	if err != nil {
		return nil, err
	}
	peer, err := remotev2webrtc.NewPeerConnection(configuration)
	if err != nil {
		return nil, fmt.Errorf("create managed endpoint peer connection: %w", err)
	}
	channel, err := peer.CreateDataChannel(protocolChannelLabel, nil)
	if err != nil {
		_ = peer.Close()
		return nil, fmt.Errorf("create managed endpoint protocol data channel: %w", err)
	}
	// DataChannel 一创建就注册 message/close handler，避免 daemon 在 OnOpen 立即发送的 DeviceHello 早于 client Recv 注册而丢失。
	secured := newPeerTransport(datachannel.New(remotev2webrtc.NewChannel(channel)), peer, route.selectionReason)
	opened := make(chan struct{})
	channel.OnOpen(func() { close(opened) })
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		_ = secured.Close()
		return nil, fmt.Errorf("create managed endpoint offer: %w", err)
	}
	gatherComplete := pion.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		_ = secured.Close()
		return nil, fmt.Errorf("set managed endpoint local offer: %w", err)
	}
	select {
	case <-ctx.Done():
		_ = secured.Close()
		return nil, ctx.Err()
	case <-gatherComplete:
	}
	localDescription := peer.LocalDescription()
	if localDescription == nil || strings.TrimSpace(localDescription.SDP) == "" {
		_ = secured.Close()
		return nil, fmt.Errorf("managed endpoint offer has no local description")
	}
	reportDialPhase(options.Phase, cloudcompanion.EndpointPhaseSignaling)
	signaling, err := options.Companion.CreateSignalingSession(ctx, &cloudpb.CreateSignalingSessionRequest{
		EndpointId:       endpointID,
		ManagedSessionId: resolved.GetManagedSessionId(),
		TargetDeviceId:   targetDeviceID,
		OfferSdp:         localDescription.SDP,
		RoutePreference:  options.RoutePreference,
		RelayOnly:        route.relayOnly,
	})
	if err != nil {
		_ = secured.Close()
		return nil, err
	}
	if signaling == nil {
		_ = secured.Close()
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an empty signaling stream")
	}
	defer signaling.Close()
	answer, candidates, err := receiveAnswer(signaling)
	if err != nil {
		_ = secured.Close()
		return nil, err
	}
	if err := peer.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: answer.GetSdp()}); err != nil {
		_ = secured.Close()
		return nil, fmt.Errorf("set managed endpoint remote answer: %w", err)
	}
	for _, candidate := range append(candidates, answer.GetCandidates()...) {
		if err := peer.AddICECandidate(toPionCandidate(candidate)); err != nil {
			_ = secured.Close()
			return nil, fmt.Errorf("add managed endpoint ICE candidate: %w", err)
		}
	}
	reportDialPhase(options.Phase, cloudcompanion.EndpointPhaseConnecting)
	select {
	case <-ctx.Done():
		_ = secured.Close()
		return nil, ctx.Err()
	case <-opened:
	}
	if route.expectedPath != cloudcompanion.PathUnknown {
		actualPath := observedPath(peer)
		if actualPath != route.expectedPath {
			_ = secured.Close()
			return nil, routePlanProtocolError(fmt.Sprintf("managed route plan selected %q but ICE established %q", route.expectedPath, actualPath))
		}
	}
	dtlsFingerprint, err := remotev2webrtc.RemoteCertificateFingerprint(peer)
	if err != nil {
		_ = secured.Close()
		return nil, fmt.Errorf("read managed endpoint DTLS certificate: %w", err)
	}
	authenticator := remoteauth.ClientHandshake{Random: options.AuthRandom}
	if !options.Now.IsZero() {
		now := options.Now.UTC()
		authenticator.Now = func() time.Time { return now }
	}
	reportDialPhase(options.Phase, cloudcompanion.EndpointPhaseAuthorizing)
	if _, err := authenticator.Authenticate(ctx, secured, remoteauth.ClientHandshakeRequest{
		ExpectedDeviceID: targetDeviceID, ExpectedDeviceFingerprint: options.DeviceFingerprint,
		CapabilityGrant: options.CapabilityGrant, DaemonDTLSCertificateFingerprint: dtlsFingerprint,
	}); err != nil {
		_ = secured.Close()
		return nil, fmt.Errorf("authenticate managed endpoint DataChannel: %w", err)
	}
	connected = true
	reportDialPhase(options.Phase, cloudcompanion.EndpointPhaseConnected)
	var reporter *qualityReporter
	if qualityOptions.Enabled {
		reporter = &qualityReporter{
			companion:        options.Companion,
			managedSessionID: resolved.GetManagedSessionId(),
			options:          qualityOptions,
			startedAt:        time.Now().UTC(),
		}
	}
	secured.startLifecycle(reporter)
	return secured, nil
}

func reportDialPhase(callback func(cloudcompanion.EndpointPhase), phase cloudcompanion.EndpointPhase) {
	if callback != nil {
		callback(phase)
	}
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

func peerConfiguration(servers []*cloudpb.IceServer, preference cloudpb.RoutePreference, relayOnly bool) (pion.Configuration, error) {
	switch preference {
	case cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
		cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY,
		cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE,
		cloudpb.RoutePreference_ROUTE_PREFERENCE_GLOBAL_ACCELERATOR:
	default:
		return pion.Configuration{}, fmt.Errorf("unsupported managed WebRTC route preference %s", preference)
	}
	configuration := pion.Configuration{}
	if relayOnly {
		if preference == cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY {
			return pion.Configuration{}, fmt.Errorf("managed WebRTC direct-only route cannot require relay candidates")
		}
		configuration.ICETransportPolicy = pion.ICETransportPolicyRelay
	}
	for _, server := range servers {
		if server == nil {
			continue
		}
		urls := append([]string(nil), server.GetUrls()...)
		if preference == cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY {
			filtered := urls[:0]
			for _, candidateURL := range urls {
				lower := strings.ToLower(strings.TrimSpace(candidateURL))
				if !strings.HasPrefix(lower, "turn:") && !strings.HasPrefix(lower, "turns:") {
					filtered = append(filtered, candidateURL)
				}
			}
			urls = filtered
		}
		if len(urls) == 0 {
			continue
		}
		configuration.ICEServers = append(configuration.ICEServers, pion.ICEServer{
			URLs: urls, Username: server.GetUsername(), Credential: server.GetCredential(),
		})
	}
	return configuration, nil
}

func observedPath(peer *pion.PeerConnection) cloudcompanion.Path {
	if peer == nil {
		return cloudcompanion.PathUnknown
	}
	if sctp := peer.SCTP(); sctp != nil && sctp.Transport() != nil && sctp.Transport().ICETransport() != nil {
		pair, err := sctp.Transport().ICETransport().GetSelectedCandidatePair()
		if err == nil && pair != nil && pair.Local != nil && pair.Remote != nil {
			if pair.Local.Typ == pion.ICECandidateTypeRelay || pair.Remote.Typ == pion.ICECandidateTypeRelay {
				return cloudcompanion.PathSingleRelay
			}
			return cloudcompanion.PathDirect
		}
	}
	report := peer.GetStats()
	for _, stat := range report {
		pair, ok := stat.(pion.ICECandidatePairStats)
		if !ok || !pair.Nominated || pair.State != pion.StatsICECandidatePairStateSucceeded {
			continue
		}
		local, localOK := report[pair.LocalCandidateID].(pion.ICECandidateStats)
		remote, remoteOK := report[pair.RemoteCandidateID].(pion.ICECandidateStats)
		if (localOK && local.CandidateType == pion.ICECandidateTypeRelay) ||
			(remoteOK && remote.CandidateType == pion.ICECandidateTypeRelay) {
			return cloudcompanion.PathSingleRelay
		}
		return cloudcompanion.PathDirect
	}
	return cloudcompanion.PathUnknown
}

func toPionCandidate(candidate *cloudpb.IceCandidate) pion.ICECandidateInit {
	if candidate == nil {
		return pion.ICECandidateInit{}
	}
	mid := candidate.GetSdpMid()
	lineIndex := uint16(candidate.GetSdpMlineIndex())
	usernameFragment := candidate.GetUsernameFragment()
	result := pion.ICECandidateInit{Candidate: candidate.GetCandidate(), SDPMLineIndex: &lineIndex}
	if mid != "" {
		result.SDPMid = &mid
	}
	if usernameFragment != "" {
		result.UsernameFragment = &usernameFragment
	}
	return result
}

type peerTransport struct {
	transport.Transport
	peer             *pion.PeerConnection
	selectionReason  cloudcompanion.RouteSelectionReason
	observationDone  chan struct{}
	closeRequested   chan struct{}
	lifecycleMu      sync.Mutex
	lifecycleStarted bool
	closeRequestOnce sync.Once
	finishOnce       sync.Once
}

func newPeerTransport(protocolTransport transport.Transport, peer *pion.PeerConnection, selectionReason cloudcompanion.RouteSelectionReason) *peerTransport {
	return &peerTransport{
		Transport: protocolTransport, peer: peer, selectionReason: selectionReason,
		observationDone: make(chan struct{}), closeRequested: make(chan struct{}),
	}
}

func (connection *peerTransport) startLifecycle(reporter *qualityReporter) {
	connection.lifecycleMu.Lock()
	if connection.lifecycleStarted {
		connection.lifecycleMu.Unlock()
		return
	}
	connection.lifecycleStarted = true
	connection.lifecycleMu.Unlock()
	go func() {
		if reporter == nil {
			select {
			case <-connection.Transport.Done():
			case <-connection.closeRequested:
			}
		} else {
			reporter.run(connection.peer, connection.Transport.Done(), connection.closeRequested)
		}
		connection.finish()
	}()
}

func (connection *peerTransport) requestClose() {
	connection.closeRequestOnce.Do(func() { close(connection.closeRequested) })
}

func (connection *peerTransport) finish() {
	connection.finishOnce.Do(func() {
		_ = connection.peer.Close()
		close(connection.observationDone)
	})
}

func (connection *peerTransport) Close() error {
	err := connection.Transport.Close()
	connection.requestClose()
	connection.lifecycleMu.Lock()
	started := connection.lifecycleStarted
	connection.lifecycleMu.Unlock()
	if !started {
		connection.finish()
	}
	return err
}
