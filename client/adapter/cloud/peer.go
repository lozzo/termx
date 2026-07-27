package cloud

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/anytty/anytty/client/endpoint"
	"github.com/anytty/anytty/client/port"
	clientruntime "github.com/anytty/anytty/client/runtime"
	cloudclient "github.com/anytty/anytty/cloud/client"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/anytty/anytty/shared/transport"
	"github.com/anytty/anytty/shared/transport/datachannel"
)

type openedCloudPeer struct {
	peer       port.WebRTCPeer
	signaling  *cloudclient.SignalSession
	connection transport.Transport
	path       endpoint.Path
	closeOnce  sync.Once
	closeErr   error
}

func openResolvedCloudPeer(
	ctx context.Context,
	request clientruntime.AttemptRequest,
	peers PeerFactory,
	cloud *cloudclient.Client,
	resolved *cloudv1.ResolveClientRouteResponse,
	identity remoteauth.ClientAccessIdentity,
	signer cloudclient.Signer,
	product cloudv1.ClientProduct,
	report func(clientruntime.EndpointPhase),
) (*openedCloudPeer, error) {
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
	if report != nil {
		report(clientruntime.EndpointPhaseConnecting)
	}
	signalSession, err := cloud.Exchange(ctx, resolved, identity, signer, product, uint64(request.Stamp().Generation), preference, func(ctx context.Context, ready *cloudv1.ClientReady) (string, error) {
		peerConfig := port.WebRTCConfig{Policy: icePolicy}
		if relay := ready.GetRelay(); relay != nil {
			peerConfig.Servers = append(peerConfig.Servers, port.ICEServer{URLs: append([]string(nil), relay.GetUrls()...), Username: relay.GetUsername(), Credential: relay.GetCredential()})
		}
		if icePolicy == port.ICETransportRelayOnly && len(peerConfig.Servers) == 0 {
			return "", errors.New("Cloud Relay-only attempt did not receive TURN material")
		}
		peer, err = peers.OpenCloudPeer(ctx, peerConfig)
		if err != nil {
			return "", fmt.Errorf("create Cloud WebRTC peer: %w", err)
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
	opened := &openedCloudPeer{peer: peer, signaling: signalSession}
	answer := signalSession.Answer()
	candidates := make([]port.ICECandidate, 0, len(answer.GetCandidates()))
	for _, candidate := range answer.GetCandidates() {
		if candidate != nil {
			candidates = append(candidates, port.ICECandidate{Candidate: candidate.GetCandidate(), SDPMid: candidate.GetSdpMid(), SDPMLineIndex: candidate.GetSdpMlineIndex(), UsernameFragment: candidate.GetUsernameFragment()})
		}
	}
	if err := peer.ApplyAnswer(ctx, answer.GetAnswerSdp(), candidates); err != nil {
		_ = opened.Close()
		return nil, fmt.Errorf("apply Cloud WebRTC answer: %w", err)
	}
	if err := peer.WaitReady(ctx); err != nil {
		_ = opened.Close()
		return nil, fmt.Errorf("wait Cloud WebRTC DataChannel: %w", err)
	}
	opened.path = peer.ObservedPath()
	if opened.path != endpoint.PathDirect && opened.path != endpoint.PathSingleRelay || icePolicy == port.ICETransportRelayOnly && opened.path != endpoint.PathSingleRelay {
		_ = opened.Close()
		return nil, fmt.Errorf("Cloud connector established a path that violates Relay policy: %q", opened.path)
	}
	opened.connection = datachannel.New(peer.Channel())
	return opened, nil
}

func (opened *openedCloudPeer) Transport() transport.Transport {
	if opened == nil {
		return nil
	}
	return opened.connection
}

func (opened *openedCloudPeer) RemoteCertificateFingerprint() (string, error) {
	if opened == nil || opened.peer == nil {
		return "", errors.New("Cloud peer is unavailable")
	}
	return opened.peer.RemoteCertificateFingerprint()
}

func (opened *openedCloudPeer) ObservedPath() endpoint.Path {
	if opened == nil {
		return ""
	}
	return opened.path
}

func (opened *openedCloudPeer) Release() (port.WebRTCPeer, *cloudclient.SignalSession) {
	if opened == nil {
		return nil, nil
	}
	peer, signaling := opened.peer, opened.signaling
	opened.peer, opened.signaling, opened.connection = nil, nil, nil
	return peer, signaling
}

func (opened *openedCloudPeer) Close() error {
	if opened == nil {
		return nil
	}
	opened.closeOnce.Do(func() {
		if opened.connection != nil {
			opened.closeErr = opened.connection.Close()
		}
		if opened.peer != nil {
			opened.closeErr = errors.Join(opened.closeErr, opened.peer.Close())
		}
		if opened.signaling != nil {
			opened.closeErr = errors.Join(opened.closeErr, opened.signaling.Close())
		}
	})
	return opened.closeErr
}

var _ clientruntime.PairingPeerSession = (*openedCloudPeer)(nil)
