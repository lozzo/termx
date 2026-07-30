// Package cloud 把 AnyTTY Cloud 发现/信令组装成与 Direct/SSH 相同的 Go-owned ReadyPeerSession。
// Endpoint planning 和 generation 属于 client/runtime；Controller/Edge 结果不能替代最终 DataChannel remote auth。
package cloud

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	peeradapter "github.com/anytty/anytty/client/adapter/peer"
	protocoladapter "github.com/anytty/anytty/client/adapter/protocol"
	"github.com/anytty/anytty/client/endpoint"
	"github.com/anytty/anytty/client/port"
	clientruntime "github.com/anytty/anytty/client/runtime"
	cloudclient "github.com/anytty/anytty/cloud/client"
	internalprotocol "github.com/anytty/anytty/internal/protocol"
	"github.com/anytty/anytty/proto/apipb"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/proto/wire"
)

const defaultClientName = "anytty-go-cloud"

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

// Connect 优先复用 secure credential 中的 Edge locator；只有 locator 缺失或失效才查询 Controller。
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
	resolved, cachedErr := cloudclient.NewCachedCapabilityRoute(signaling.CloudEdgeLocator(), signaling.CloudRouteGrant())
	discovered := false
	var opened *openedCloudPeer
	if cachedErr == nil {
		opened, err = openResolvedCloudPeer(ctx, request, dialer.Peers, dialer.Cloud, resolved, signaling.ClientIdentity(), signaling, dialer.Product, dialer.report)
		if err != nil && !cloudclient.ShouldRefreshEdgeLocator(err) {
			return nil, err
		}
	}
	if opened == nil {
		resolved, err = dialer.Cloud.Resolve(ctx, signaling.CloudRouteGrant(), signaling)
		if err != nil {
			return nil, err
		}
		opened, err = openResolvedCloudPeer(ctx, request, dialer.Peers, dialer.Cloud, resolved, signaling.ClientIdentity(), signaling, dialer.Product, dialer.report)
		if err != nil {
			return nil, err
		}
		discovered = true
	}
	fingerprint, err := opened.RemoteCertificateFingerprint()
	if err != nil {
		_ = opened.Close()
		return nil, err
	}
	dialer.report(clientruntime.EndpointPhaseAuthorizing)
	connection := opened.Transport()
	if _, err := prepared.Authenticate(ctx, connection, fingerprint); err != nil {
		_ = opened.Close()
		return nil, fmt.Errorf("authenticate Cloud DataChannel: %w", err)
	}
	protocolClient := internalprotocol.NewClient(connection)
	clientName := strings.TrimSpace(dialer.ClientName)
	if clientName == "" {
		clientName = defaultClientName
	}
	if err := protocolClient.Hello(ctx, internalprotocol.Hello{Version: wire.Version, Client: clientName}); err != nil {
		_ = protocolClient.Close()
		_ = opened.Close()
		return nil, fmt.Errorf("Cloud protocol Hello: %w", err)
	}
	application, err := protocoladapter.NewApplicationClientWithObservedPath(protocolClient, request.Stamp(), string(opened.ObservedPath()))
	if err != nil {
		_ = protocolClient.Close()
		_ = opened.Close()
		return nil, err
	}
	if err := application.MarkReady(clientruntime.ReadyPeerSessionEvidence{Identity: request.DaemonIdentity(), IdentityVerified: true, AuthorizationVerified: true, ProtocolVersion: wire.Version}); err != nil {
		_ = application.Close()
		_ = opened.Close()
		return nil, err
	}
	if discovered {
		locator, err := cloudclient.EncodeEdgeLocator(resolved.Locator())
		if err != nil {
			_ = application.Close()
			_ = opened.Close()
			return nil, fmt.Errorf("encode authenticated Cloud Edge locator: %w", err)
		}
		// Locator 是公开位置缓存；持久化故障不能杀死已经完成端到端认证的会话。
		_ = signaling.StoreCloudEdgeLocator(ctx, locator)
	}
	dialer.report(clientruntime.EndpointPhaseReady)
	peer, signalSession := opened.Release()
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

// ConnectionSnapshot 返回 P2P selected pair 的地址与网络计数。
func (session *Session) ConnectionSnapshot(at time.Time) (clientruntime.ConnectionSnapshot, bool) {
	if session == nil || session.ApplicationClient == nil {
		return clientruntime.ConnectionSnapshot{}, false
	}
	result := clientruntime.ConnectionSnapshot{RouteID: session.Stamp().RouteID, RouteKind: endpoint.RouteManagedWebRTC, ObservedPath: session.ObservedPath(), SampledAt: at.UTC(), Connected: true}
	if snapshot, ok := session.peer.Snapshot(at); ok {
		result.SampledAt, result.RoundTrip = snapshot.At, snapshot.RoundTrip
		result.LocalCandidateType, result.RemoteCandidateType = snapshot.LocalCandidateType, snapshot.RemoteCandidateType
		result.LocalAddress, result.RemoteAddress = snapshot.LocalAddress, snapshot.RemoteAddress
		result.LocalPort, result.RemotePort = snapshot.LocalPort, snapshot.RemotePort
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
