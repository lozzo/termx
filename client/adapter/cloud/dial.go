// Package cloud 把 Muxvia Cloud 发现/信令组装成与 Direct/SSH 相同的 Go-owned ReadyPeerSession。
// Endpoint planning 和 generation 属于 client/runtime；Controller/Edge 结果不能替代最终 DataChannel remote auth。
package cloud

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	peeradapter "github.com/muxvia/muxvia/client/adapter/peer"
	protocoladapter "github.com/muxvia/muxvia/client/adapter/protocol"
	"github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/client/port"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
	cloudclient "github.com/muxvia/muxvia/cloud/client"
	internalprotocol "github.com/muxvia/muxvia/internal/protocol"
	"github.com/muxvia/muxvia/proto/apipb"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"github.com/muxvia/muxvia/proto/wire"
	"github.com/muxvia/muxvia/shared/transport/datachannel"
)

const defaultClientName = "muxvia-go-cloud"

// PeerFactory 根据本次 Controller/Edge 决策创建 direct 或 single-Relay WebRTC primitive。
type PeerFactory interface {
	OpenCloudPeer(context.Context, port.WebRTCConfig) (port.WebRTCPeer, error)
}

// Dialer 是 managed-webrtc Route 的 Go-owned connector。
type Dialer struct {
	Peers         PeerFactory
	Cloud         *cloudclient.Client
	Authorization peeradapter.Authorizer
	Product       cloudv1.ClientProduct
	ClientName    string
	Phase         func(clientruntime.EndpointPhase)
}

// Connect 依次完成 grant resolve、ClientGateway、P2P、DTLS capability auth、protocol Hello，并返回同一 session contract。
func (dialer *Dialer) Connect(ctx context.Context, request clientruntime.AttemptRequest) (clientruntime.ReadyPeerSession, error) {
	if dialer == nil || dialer.Peers == nil || dialer.Cloud == nil || dialer.Authorization == nil || dialer.Product == cloudv1.ClientProduct_CLIENT_PRODUCT_UNSPECIFIED {
		return nil, errors.New("Cloud connector dependencies are incomplete")
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if request.Route().Kind != endpoint.RouteManagedWebRTC {
		return nil, fmt.Errorf("route %q is not managed WebRTC", request.Route().ID)
	}
	prepared, err := dialer.Authorization.Prepare(ctx, request)
	if err != nil {
		return nil, err
	}
	signaling, ok := prepared.(peeradapter.PreparedSignalingAuthorization)
	if !ok || len(signaling.CloudRouteGrant()) == 0 {
		return nil, errors.New("Cloud route credential is missing its signed discovery grant")
	}
	dialer.report(clientruntime.EndpointPhaseSignaling)
	resolved, err := dialer.Cloud.Resolve(ctx, signaling.CloudRouteGrant(), signaling)
	if err != nil {
		return nil, err
	}
	preference, icePolicy, err := relayPreference(request.Route().RelayMode)
	if err != nil {
		return nil, err
	}
	var peer port.WebRTCPeer
	closePeer := func() {
		if peer != nil {
			_ = peer.Close()
		}
	}
	dialer.report(clientruntime.EndpointPhaseConnecting)
	signalSession, err := dialer.Cloud.Exchange(ctx, resolved, signaling.ClientIdentity(), signaling, dialer.Product, uint64(request.Stamp().Generation), preference, func(ctx context.Context, ready *cloudv1.ClientReady) (string, error) {
		peerConfig := port.WebRTCConfig{Policy: icePolicy}
		if relay := ready.GetRelay(); relay != nil {
			peerConfig.Servers = append(peerConfig.Servers, port.ICEServer{URLs: append([]string(nil), relay.GetUrls()...), Username: relay.GetUsername(), Credential: relay.GetCredential()})
		}
		if icePolicy == port.ICETransportRelayOnly && len(peerConfig.Servers) == 0 {
			return "", errors.New("Cloud Relay-only attempt did not receive TURN material")
		}
		var openErr error
		peer, openErr = dialer.Peers.OpenCloudPeer(ctx, peerConfig)
		if openErr != nil {
			return "", fmt.Errorf("create Cloud WebRTC peer: %w", openErr)
		}
		if peer.Channel() == nil {
			closePeer()
			return "", errors.New("Cloud WebRTC peer has no protocol DataChannel")
		}
		return peer.CreateOffer(ctx)
	})
	if err != nil {
		closePeer()
		return nil, err
	}
	closeAttempt := func() {
		_ = signalSession.Close()
		closePeer()
	}
	answer := signalSession.Answer()
	candidates := make([]port.ICECandidate, 0, len(answer.GetCandidates()))
	for _, candidate := range answer.GetCandidates() {
		if candidate != nil {
			candidates = append(candidates, port.ICECandidate{Candidate: candidate.GetCandidate(), SDPMid: candidate.GetSdpMid(), SDPMLineIndex: candidate.GetSdpMlineIndex(), UsernameFragment: candidate.GetUsernameFragment()})
		}
	}
	if err := peer.ApplyAnswer(ctx, answer.GetAnswerSdp(), candidates); err != nil {
		closeAttempt()
		return nil, fmt.Errorf("apply Cloud WebRTC answer: %w", err)
	}
	if err := peer.WaitReady(ctx); err != nil {
		closeAttempt()
		return nil, fmt.Errorf("wait Cloud WebRTC DataChannel: %w", err)
	}
	observedPath := peer.ObservedPath()
	if observedPath != endpoint.PathDirect && observedPath != endpoint.PathSingleRelay || icePolicy == port.ICETransportRelayOnly && observedPath != endpoint.PathSingleRelay {
		closeAttempt()
		return nil, fmt.Errorf("Cloud connector established a path that violates Relay policy: %q", observedPath)
	}
	fingerprint, err := peer.RemoteCertificateFingerprint()
	if err != nil {
		closeAttempt()
		return nil, err
	}
	dialer.report(clientruntime.EndpointPhaseAuthorizing)
	connection := datachannel.New(peer.Channel())
	if _, err := prepared.Authenticate(ctx, connection, fingerprint); err != nil {
		_ = connection.Close()
		closeAttempt()
		return nil, fmt.Errorf("authenticate Cloud DataChannel: %w", err)
	}
	protocolClient := internalprotocol.NewClient(connection)
	clientName := strings.TrimSpace(dialer.ClientName)
	if clientName == "" {
		clientName = defaultClientName
	}
	if err := protocolClient.Hello(ctx, internalprotocol.Hello{Version: wire.Version, Client: clientName}); err != nil {
		_ = protocolClient.Close()
		closeAttempt()
		return nil, fmt.Errorf("Cloud protocol Hello: %w", err)
	}
	application, err := protocoladapter.NewApplicationClientWithObservedPath(protocolClient, request.Stamp(), string(observedPath))
	if err != nil {
		_ = protocolClient.Close()
		closeAttempt()
		return nil, err
	}
	if err := application.MarkReady(clientruntime.ReadyPeerSessionEvidence{Identity: request.DaemonIdentity(), IdentityVerified: true, AuthorizationVerified: true, ProtocolVersion: wire.Version}); err != nil {
		_ = application.Close()
		closeAttempt()
		return nil, err
	}
	dialer.report(clientruntime.EndpointPhaseReady)
	return newSession(application, peer, signalSession), nil
}

func relayPreference(mode endpoint.RelayMode) (cloudv1.RelayPreference, port.ICETransportPolicy, error) {
	switch mode {
	case "", endpoint.RelayAuto, endpoint.RelaySmart:
		return cloudv1.RelayPreference_RELAY_PREFERENCE_AUTO, port.ICETransportAll, nil
	case endpoint.RelayDirect:
		return cloudv1.RelayPreference_RELAY_PREFERENCE_DIRECT_ONLY, port.ICETransportAll, nil
	case endpoint.RelayOnly:
		return cloudv1.RelayPreference_RELAY_PREFERENCE_RELAY_ONLY, port.ICETransportRelayOnly, nil
	default:
		return cloudv1.RelayPreference_RELAY_PREFERENCE_UNSPECIFIED, port.ICETransportAll, fmt.Errorf("unsupported Cloud relay mode %q", mode)
	}
}

func (dialer *Dialer) report(phase clientruntime.EndpointPhase) {
	if dialer != nil && dialer.Phase != nil {
		dialer.Phase(phase)
	}
}

// Session 把 authenticated application client 与获胜的 Cloud P2P peer 绑定到同一 generation。
type Session struct {
	*protocoladapter.ApplicationClient
	peer      port.WebRTCPeer
	signaling *cloudclient.SignalSession
	closeOnce sync.Once
	closeErr  error
}

func newSession(application *protocoladapter.ApplicationClient, peer port.WebRTCPeer, signaling *cloudclient.SignalSession) *Session {
	session := &Session{ApplicationClient: application, peer: peer, signaling: signaling}
	go func() { <-application.Done(); _ = session.Close() }()
	return session
}

// ExecuteApplication 执行 generated Proto application command。
func (session *Session) ExecuteApplication(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	return session.ApplicationSession.Execute(ctx, command)
}

// ExecuteApplicationTerminal 为 resource-producing command 保留 terminal result。
func (session *Session) ExecuteApplicationTerminal(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	return session.ApplicationSession.ExecuteTerminal(ctx, command)
}

// ConnectionSnapshot 返回 P2P selected pair 的脱敏网络计数。
func (session *Session) ConnectionSnapshot(at time.Time) (clientruntime.ConnectionSnapshot, bool) {
	if session == nil || session.ApplicationClient == nil {
		return clientruntime.ConnectionSnapshot{}, false
	}
	result := clientruntime.ConnectionSnapshot{RouteID: session.Stamp().RouteID, RouteKind: endpoint.RouteManagedWebRTC, ObservedPath: session.ObservedPath(), SampledAt: at.UTC(), Connected: true}
	if snapshot, ok := session.peer.Snapshot(at); ok {
		result.SampledAt, result.RoundTrip = snapshot.At, snapshot.RoundTrip
		result.LocalCandidateType, result.RemoteCandidateType = snapshot.LocalCandidateType, snapshot.RemoteCandidateType
		result.LocalProtocol, result.RemoteProtocol, result.RelayTransport = snapshot.LocalProtocol, snapshot.RemoteProtocol, snapshot.RelayProtocol
		result.NetworkClass, result.BytesSent, result.BytesReceived = snapshot.NetworkClass, snapshot.BytesSent, snapshot.BytesRecv
		result.PacketsSent, result.LossEvents, result.Connected = snapshot.PacketsSent, snapshot.LossEvents, snapshot.Connected
	}
	return result, true
}

// Close 幂等释放 protocol、DataChannel、ICE、DTLS 和 Pion peer。
func (session *Session) Close() error {
	if session == nil {
		return nil
	}
	session.closeOnce.Do(func() {
		if session.ApplicationClient != nil {
			session.closeErr = session.ApplicationClient.Close()
		}
		if session.peer != nil {
			if err := session.peer.Close(); session.closeErr == nil {
				session.closeErr = err
			}
		}
		if session.signaling != nil {
			if err := session.signaling.Close(); session.closeErr == nil {
				session.closeErr = err
			}
		}
	})
	return session.closeErr
}

var _ clientruntime.PeerConnector = (*Dialer)(nil)
var _ clientruntime.ApplicationReadyPeerSession = (*Session)(nil)
