package managed

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/client/port"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/shared/transport"
	"github.com/lozzow/termx/shared/transport/datachannel"
)

// PairingConnector 只建立一次 managed WebRTC PairingExchange 通道。
// 它不执行 protocol Hello、不创建 application session；grant 返回后当前 DTLS/DataChannel 必须关闭。
type PairingConnector struct {
	Cloud CloudClient
	Peers port.ManagedPeerFactory
	Now   func() time.Time
}

// Redeem 使用当前 attempt 的 endpoint pin、route policy 和实际 DTLS fingerprint 兑换 pairing ticket。
// Identity/Signer 必须已由平台 secure store 准备；任何 Cloud、ICE、DTLS 或 remote-auth 失败都会关闭 peer 且不返回 grant。
func (dialer *PairingConnector) Redeem(ctx context.Context, request clientruntime.AttemptRequest, pairing remoteauth.ClientPairingRequest) (remoteauth.PairingExchangeResult, error) {
	if err := clientruntime.ValidatePairingAttempt(request, pairing); err != nil {
		return remoteauth.PairingExchangeResult{}, err
	}
	if dialer == nil || dialer.Cloud == nil || dialer.Peers == nil {
		return remoteauth.PairingExchangeResult{}, fmt.Errorf("managed pairing dialer dependencies are incomplete")
	}
	if request.Route().Kind != endpoint.RouteManagedWebRTC {
		return remoteauth.PairingExchangeResult{}, fmt.Errorf("managed pairing requires a managed WebRTC route")
	}
	resolved, err := dialer.Cloud.ResolveEndpoint(ctx, &cloudpb.ResolveEndpointRequest{
		EndpointId: string(request.EndpointID()), TargetDeviceId: request.DaemonIdentity().DeviceID,
	})
	if err != nil {
		return remoteauth.PairingExchangeResult{}, err
	}
	if err := validateResolution(request, resolved); err != nil {
		return remoteauth.PairingExchangeResult{}, err
	}
	policy, err := cloudcompanion.DialPolicyForRelayMode(request.Route().RelayMode)
	if err != nil {
		return remoteauth.PairingExchangeResult{}, err
	}
	selected, err := resolveDialRoute(ctx, dialer.Cloud, request, resolved, policy, dialer.now())
	if err != nil {
		return remoteauth.PairingExchangeResult{}, err
	}
	peer, err := dialer.Peers.OpenManagedPeer(ctx, selected.iceServers, selected.preference, selected.relayOnly)
	if err != nil {
		return remoteauth.PairingExchangeResult{}, fmt.Errorf("create managed pairing peer: %w", err)
	}
	if peer == nil || peer.Channel() == nil {
		if peer != nil {
			_ = peer.Close()
		}
		return remoteauth.PairingExchangeResult{}, fmt.Errorf("managed pairing peer has no DataChannel")
	}
	connection := datachannel.New(peer.Channel())
	pairingPeer := &managedPairingPeerSession{peer: peer, connection: connection}

	offer, err := peer.CreateOffer(ctx)
	if err != nil {
		_ = pairingPeer.Close()
		return remoteauth.PairingExchangeResult{}, fmt.Errorf("create managed pairing offer: %w", err)
	}
	signaling, err := dialer.Cloud.CreateSignalingSession(ctx, &cloudpb.CreateSignalingSessionRequest{
		EndpointId: string(request.EndpointID()), ManagedSessionId: resolved.GetManagedSessionId(),
		TargetDeviceId: request.DaemonIdentity().DeviceID, OfferSdp: offer,
		RoutePreference: selected.preference, RelayOnly: selected.relayOnly,
	})
	if err != nil {
		_ = pairingPeer.Close()
		return remoteauth.PairingExchangeResult{}, err
	}
	if signaling == nil {
		_ = pairingPeer.Close()
		return remoteauth.PairingExchangeResult{}, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an empty pairing signaling stream")
	}
	answer, candidates, receiveErr := receiveAnswer(signaling)
	closeErr := signaling.Close()
	if receiveErr != nil {
		_ = pairingPeer.Close()
		return remoteauth.PairingExchangeResult{}, receiveErr
	}
	if closeErr != nil {
		_ = pairingPeer.Close()
		return remoteauth.PairingExchangeResult{}, closeErr
	}
	if err := peer.ApplyAnswer(ctx, answer.GetSdp(), append(candidates, answer.GetCandidates()...)); err != nil {
		_ = pairingPeer.Close()
		return remoteauth.PairingExchangeResult{}, fmt.Errorf("apply managed pairing answer: %w", err)
	}
	if err := peer.WaitReady(ctx); err != nil {
		_ = pairingPeer.Close()
		return remoteauth.PairingExchangeResult{}, err
	}
	if selected.expectedPath != "" && peer.ObservedPath() != selected.expectedPath {
		_ = pairingPeer.Close()
		return remoteauth.PairingExchangeResult{}, routePlanProtocolError(fmt.Sprintf("managed pairing route selected %q but ICE established %q", selected.expectedPath, peer.ObservedPath()))
	}
	return (clientruntime.PairingService{Now: dialer.Now}).Redeem(ctx, request, pairingPeer, pairing)
}

func (dialer *PairingConnector) now() time.Time {
	if dialer != nil && dialer.Now != nil {
		return dialer.Now().UTC()
	}
	return time.Now().UTC()
}

type managedPairingPeerSession struct {
	peer       port.ManagedPeer
	connection transport.Transport
	closeOnce  sync.Once
	closeErr   error
}

func (session *managedPairingPeerSession) Transport() transport.Transport {
	if session == nil {
		return nil
	}
	return session.connection
}

func (session *managedPairingPeerSession) RemoteCertificateFingerprint() (string, error) {
	if session == nil || session.peer == nil {
		return "", fmt.Errorf("managed pairing peer is unavailable")
	}
	return session.peer.RemoteCertificateFingerprint()
}

func (session *managedPairingPeerSession) Close() error {
	if session == nil {
		return nil
	}
	session.closeOnce.Do(func() {
		if session.connection != nil {
			session.closeErr = session.connection.Close()
		}
		if session.peer != nil {
			if err := session.peer.Close(); session.closeErr == nil {
				session.closeErr = err
			}
		}
	})
	return session.closeErr
}

var _ clientruntime.PairingPeerSession = (*managedPairingPeerSession)(nil)
