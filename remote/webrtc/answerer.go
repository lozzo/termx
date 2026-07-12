package webrtc

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/transport"
	"github.com/lozzow/termx/shared/transport/datachannel"
	pion "github.com/pion/webrtc/v4"
)

const protocolChannelLabel = "protocol"

// DataChannelSessionHandler 是 daemon 侧 DTLS DataChannel 的端到端授权 owner。
// 实现必须先在 DataChannel 内完成 DeviceIdentity proof 与 CapabilityGrant 校验，再把受限 scope 交给 core-v2；云信令不能替代该步骤。
type DataChannelSessionHandler interface {
	// ServeDataChannel 接收尚未授权的可靠有序 DataChannel；实现完成握手前不得调用 core-v2。
	// daemonDTLSFingerprint 必须由 WebRTC adapter 从当前本端 DTLSTransport 读取，不能来自 SDP。
	ServeDataChannel(context.Context, transport.Transport, string) error
}

// Answerer 把 Cloud Companion 中继的公开 WebRTC offer 协商成 daemon answer。
// PeerConnection 只负责 ICE/DTLS/SCTP；它不接收 grant、terminal payload 或 Hub 私有 runtime 类型。
type Answerer struct {
	Handler DataChannelSessionHandler
}

// Answer 创建 WebRTC answer，并把唯一可靠有序的 termx DataChannel 交给端到端授权 handler。
// Handler 缺失时在创建 PeerConnection 前 fail closed；RP003 接入真实握手前不能启动 managed remote runtime。
func (answerer Answerer) Answer(ctx context.Context, offer *cloudpb.SignalingOffer, iceServers []*cloudpb.IceServer) (*cloudpb.SignalingAnswer, error) {
	if answerer.Handler == nil {
		return nil, fmt.Errorf("remote daemon authorized data channel handler is not configured")
	}
	if offer == nil || strings.TrimSpace(offer.GetSdp()) == "" {
		return nil, fmt.Errorf("remote daemon signaling offer is empty")
	}
	if offer.GetRelayOnly() && offer.GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY {
		return nil, fmt.Errorf("remote daemon relay-only offer has invalid route preference")
	}
	if offer.GetRelayOnly() && len(iceServers) == 0 {
		return nil, fmt.Errorf("remote daemon relay-only offer has no TURN material")
	}
	if offer.GetRelayOnly() && !containsOnlyRelayCandidates(offer.GetSdp(), offer.GetCandidates()) {
		return nil, fmt.Errorf("remote daemon relay-only offer contains a non-relay ICE candidate")
	}
	configuration := pion.Configuration{ICEServers: make([]pion.ICEServer, 0, len(iceServers))}
	// single Relay 由 offer 侧 relay-only candidate 保证；daemon 与 TURN 同机时必须发布 host candidate 供 Relay 转发。
	for _, server := range iceServers {
		if server == nil || len(server.GetUrls()) == 0 {
			continue
		}
		configuration.ICEServers = append(configuration.ICEServers, pion.ICEServer{
			URLs: append([]string(nil), server.GetUrls()...), Username: server.GetUsername(), Credential: server.GetCredential(),
		})
	}
	peer, err := NewPeerConnection(configuration)
	if err != nil {
		return nil, fmt.Errorf("create remote daemon peer connection: %w", err)
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	var candidateMu sync.Mutex
	candidates := make([]*cloudpb.IceCandidate, 0, 4)
	// daemon gathering truth 必须显式进入 signaling answer，不能依赖 candidate 是否被内联进 SDP。
	peer.OnICECandidate(func(candidate *pion.ICECandidate) {
		if candidate == nil {
			return
		}
		json := candidate.ToJSON()
		wire := &cloudpb.IceCandidate{Candidate: json.Candidate}
		if json.SDPMid != nil {
			wire.SdpMid = *json.SDPMid
		}
		if json.SDPMLineIndex != nil {
			wire.SdpMlineIndex = uint32(*json.SDPMLineIndex)
		}
		if json.UsernameFragment != nil {
			wire.UsernameFragment = *json.UsernameFragment
		}
		candidateMu.Lock()
		candidates = append(candidates, wire)
		candidateMu.Unlock()
	})
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
		// 先注册 message handler，再等待 open；client 的 CapabilityOpen 不能早于 daemon auth transport 的接收队列。
		protocolTransport := datachannel.New(NewChannel(channel))
		channel.OnOpen(func() {
			go func() {
				dtlsFingerprint, err := LocalCertificateFingerprint(peer)
				if err == nil {
					err = answerer.Handler.ServeDataChannel(sessionCtx, protocolTransport, dtlsFingerprint)
				} else {
					_ = protocolTransport.Close()
				}
				cancel()
				_ = peer.Close()
			}()
		})
	})
	if err := peer.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeOffer, SDP: offer.GetSdp()}); err != nil {
		cancel()
		_ = peer.Close()
		return nil, fmt.Errorf("set remote daemon offer: %w", err)
	}
	localAnswer, err := peer.CreateAnswer(nil)
	if err != nil {
		cancel()
		_ = peer.Close()
		return nil, fmt.Errorf("create remote daemon answer: %w", err)
	}
	gatherComplete := pion.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(localAnswer); err != nil {
		cancel()
		_ = peer.Close()
		return nil, fmt.Errorf("set remote daemon answer: %w", err)
	}
	select {
	case <-ctx.Done():
		cancel()
		_ = peer.Close()
		return nil, ctx.Err()
	case <-gatherComplete:
	}
	description := peer.LocalDescription()
	if description == nil || strings.TrimSpace(description.SDP) == "" {
		cancel()
		_ = peer.Close()
		return nil, fmt.Errorf("remote daemon answer has no local description")
	}
	candidateMu.Lock()
	wireCandidates := append([]*cloudpb.IceCandidate(nil), candidates...)
	candidateMu.Unlock()
	return &cloudpb.SignalingAnswer{
		SignalingSessionId: offer.GetSignalingSessionId(), Sdp: description.SDP, Candidates: wireCandidates,
	}, nil
}

func containsOnlyRelayCandidates(sdp string, extra []*cloudpb.IceCandidate) bool {
	found := false
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "a=candidate:") {
			continue
		}
		found = true
		if !strings.Contains(" "+strings.ToLower(line)+" ", " typ relay ") {
			return false
		}
	}
	for _, candidate := range extra {
		if candidate == nil || strings.TrimSpace(candidate.GetCandidate()) == "" {
			continue
		}
		found = true
		if !strings.Contains(" "+strings.ToLower(candidate.GetCandidate())+" ", " typ relay ") {
			return false
		}
	}
	return found
}
