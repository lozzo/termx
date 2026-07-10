package webrtc

import (
	"context"
	"fmt"
	"time"

	hubclient "github.com/lozzow/termx/termx-hub/client"
	"github.com/lozzow/termx/termx-remote-v2/daemon"
	pion "github.com/pion/webrtc/v4"
)

const protocolChannelLabel = "termx-protocol"

// Answerer 把 Hub offer 协商成受 capability grant 约束的 daemon protocol DataChannel。
// PeerConnection 只负责 ICE/DTLS/SCTP；远端身份、grant 和 core scope 均由 SessionAcceptor 决定。
type Answerer struct {
	Acceptor daemon.SessionAcceptor
}

// Answer 先验证 grant，再创建 WebRTC answer。
// 只有可靠有序且无部分重传配置的 `termx-protocol` channel 会进入 core-v2；其他 channel 会被关闭。
func (answerer Answerer) Answer(ctx context.Context, offer hubclient.Offer, ack hubclient.RegistrationAck) (hubclient.Answer, error) {
	if _, err := answerer.Acceptor.Authorize(offer.CapabilityGrant, time.Now().UTC()); err != nil {
		return hubclient.Answer{}, err
	}
	configuration := pion.Configuration{ICEServers: make([]pion.ICEServer, 0, len(ack.ICEServers))}
	for _, server := range ack.ICEServers {
		configuration.ICEServers = append(configuration.ICEServers, pion.ICEServer{
			URLs: append([]string(nil), server.URLs...), Username: server.Username, Credential: server.Credential,
		})
	}
	peer, err := pion.NewPeerConnection(configuration)
	if err != nil {
		return hubclient.Answer{}, fmt.Errorf("create remote daemon peer connection: %w", err)
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	peer.OnConnectionStateChange(func(state pion.PeerConnectionState) {
		if state == pion.PeerConnectionStateFailed || state == pion.PeerConnectionStateClosed {
			cancel()
		}
	})
	peer.OnDataChannel(func(channel *pion.DataChannel) {
		if channel.Label() != protocolChannelLabel || !channel.Ordered() || channel.MaxPacketLifeTime() != nil || channel.MaxRetransmits() != nil {
			channel.OnOpen(func() { _ = channel.Close() })
			return
		}
		channel.OnOpen(func() {
			go func() {
				_ = answerer.Acceptor.Serve(sessionCtx, daemon.SessionRequest{
					Channel: NewChannel(channel), Grant: offer.CapabilityGrant, Now: time.Now().UTC(),
				})
				cancel()
				_ = peer.Close()
			}()
		})
	})
	if err := peer.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeOffer, SDP: offer.SDP}); err != nil {
		cancel()
		_ = peer.Close()
		return hubclient.Answer{}, fmt.Errorf("set remote daemon offer: %w", err)
	}
	localAnswer, err := peer.CreateAnswer(nil)
	if err != nil {
		cancel()
		_ = peer.Close()
		return hubclient.Answer{}, fmt.Errorf("create remote daemon answer: %w", err)
	}
	gatherComplete := pion.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(localAnswer); err != nil {
		cancel()
		_ = peer.Close()
		return hubclient.Answer{}, fmt.Errorf("set remote daemon answer: %w", err)
	}
	select {
	case <-ctx.Done():
		cancel()
		_ = peer.Close()
		return hubclient.Answer{}, ctx.Err()
	case <-gatherComplete:
	}
	description := peer.LocalDescription()
	if description == nil {
		cancel()
		_ = peer.Close()
		return hubclient.Answer{}, fmt.Errorf("remote daemon answer has no local description")
	}
	return hubclient.Answer{SessionID: offer.SessionID, SDP: description.SDP}, nil
}
