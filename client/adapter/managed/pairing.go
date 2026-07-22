package managed

import (
	"context"
	"fmt"
	"time"

	"github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/client/port"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"github.com/muxvia/muxvia/shared/remoteauth"
	"github.com/muxvia/muxvia/shared/transport"
	"github.com/muxvia/muxvia/shared/transport/datachannel"
)

// PairingConnector 通过现有 Cloud signaling 建立一次性 managed WebRTC pairing peer。
// Cloud 只看到 endpoint、target 和 SDP/ICE；claim、ClientAccessIdentity proof、bundle 与 grant 只进入端到端 DataChannel。
type PairingConnector struct {
	Cloud CloudClient
	Peers port.ManagedPeerFactory
	Now   func() time.Time
	Phase func(clientruntime.EndpointPhase)
}

// Redeem 建立 managed peer 后调用 Direct/SSH/Cloud 共用的 PairingService，并在完成后关闭 peer。
func (connector *PairingConnector) Redeem(ctx context.Context, request clientruntime.AttemptRequest, pairing remoteauth.ClientPairingRequest) (remoteauth.PairingExchangeResult, error) {
	if err := clientruntime.ValidatePairingAttempt(request, pairing); err != nil {
		return remoteauth.PairingExchangeResult{}, err
	}
	if connector == nil || connector.Cloud == nil || connector.Peers == nil {
		return remoteauth.PairingExchangeResult{}, fmt.Errorf("managed pairing connector dependencies are incomplete")
	}
	route := request.Route()
	if route.Kind != endpoint.RouteManagedWebRTC {
		return remoteauth.PairingExchangeResult{}, fmt.Errorf("route %q kind %q is not managed WebRTC", route.ID, route.Kind)
	}
	connector.reportPhase(clientruntime.EndpointPhaseResolving)
	resolved, err := connector.Cloud.ResolveEndpoint(ctx, &cloudpb.ResolveEndpointRequest{
		EndpointId: string(request.EndpointID()), TargetDeviceId: request.DaemonIdentity().DeviceID,
	})
	if err != nil {
		return remoteauth.PairingExchangeResult{}, err
	}
	if err := validateResolution(request, resolved); err != nil {
		return remoteauth.PairingExchangeResult{}, err
	}
	policy, err := cloudcompanion.DialPolicyForRelayMode(route.RelayMode)
	if err != nil {
		return remoteauth.PairingExchangeResult{}, err
	}
	selected, err := resolveDialRoute(ctx, connector.Cloud, request, resolved, policy, connector.now())
	if err != nil {
		return remoteauth.PairingExchangeResult{}, err
	}
	peer, err := connector.Peers.OpenManagedPeer(ctx, selected.iceServers, selected.preference, selected.relayOnly)
	if err != nil {
		return remoteauth.PairingExchangeResult{}, fmt.Errorf("create managed pairing peer: %w", err)
	}
	if peer == nil || peer.Channel() == nil {
		if peer != nil {
			_ = peer.Close()
		}
		return remoteauth.PairingExchangeResult{}, fmt.Errorf("managed pairing peer has no DataChannel")
	}
	opened := &managedPairingPeer{peer: peer, connection: datachannel.New(peer.Channel())}
	closeFailure := func(err error) (remoteauth.PairingExchangeResult, error) {
		_ = opened.Close()
		return remoteauth.PairingExchangeResult{}, err
	}
	offer, err := peer.CreateOffer(ctx)
	if err != nil {
		return closeFailure(fmt.Errorf("create managed pairing offer: %w", err))
	}
	connector.reportPhase(clientruntime.EndpointPhaseSignaling)
	signaling, err := connector.Cloud.CreateSignalingSession(ctx, &cloudpb.CreateSignalingSessionRequest{
		EndpointId: string(request.EndpointID()), ManagedSessionId: resolved.GetManagedSessionId(),
		TargetDeviceId: request.DaemonIdentity().DeviceID, OfferSdp: offer,
		RoutePreference: selected.preference, RelayOnly: selected.relayOnly,
	})
	if err != nil {
		return closeFailure(err)
	}
	if signaling == nil {
		return closeFailure(cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an empty signaling stream"))
	}
	answer, candidates, receiveErr := receiveAnswer(signaling)
	closeErr := signaling.Close()
	if receiveErr != nil {
		return closeFailure(receiveErr)
	}
	if closeErr != nil {
		return closeFailure(closeErr)
	}
	if err := peer.ApplyAnswer(ctx, answer.GetSdp(), append(candidates, answer.GetCandidates()...)); err != nil {
		return closeFailure(fmt.Errorf("apply managed pairing answer: %w", err))
	}
	connector.reportPhase(clientruntime.EndpointPhaseConnecting)
	if err := peer.WaitReady(ctx); err != nil {
		return closeFailure(err)
	}
	if selected.expectedPath != "" && peer.ObservedPath() != selected.expectedPath {
		return closeFailure(routePlanProtocolError(fmt.Sprintf("managed pairing route selected %q but ICE established %q", selected.expectedPath, peer.ObservedPath())))
	}
	connector.reportPhase(clientruntime.EndpointPhaseAuthorizing)
	return (clientruntime.PairingService{Now: connector.Now}).Redeem(ctx, request, opened, pairing)
}

func (connector *PairingConnector) now() time.Time {
	if connector != nil && connector.Now != nil {
		return connector.Now().UTC()
	}
	return time.Now().UTC()
}

func (connector *PairingConnector) reportPhase(phase clientruntime.EndpointPhase) {
	if connector != nil && connector.Phase != nil {
		connector.Phase(phase)
	}
}

type managedPairingPeer struct {
	peer       port.ManagedPeer
	connection transport.Transport
}

func (peer *managedPairingPeer) Transport() transport.Transport { return peer.connection }

func (peer *managedPairingPeer) RemoteCertificateFingerprint() (string, error) {
	return peer.peer.RemoteCertificateFingerprint()
}

func (peer *managedPairingPeer) Close() error {
	if peer == nil {
		return nil
	}
	if peer.connection != nil {
		_ = peer.connection.Close()
	}
	if peer.peer != nil {
		return peer.peer.Close()
	}
	return nil
}

var _ clientruntime.PairingPeerSession = (*managedPairingPeer)(nil)
