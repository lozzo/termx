package webrtc

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/transport"
	"github.com/muxvia/muxvia/shared/transport/datachannel"
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

// ManagedSessionContext 是 Hub 已验证并随 signaling offer 传入 daemon 的 session fencing metadata。
// 它不包含 terminal、CapabilityGrant、SDP、ICE credential 或 payload；零值不能进入 managed handler。
type ManagedSessionContext struct {
	ManagedSessionID   string
	SessionIncarnation uint64
	ClientDeviceID     string
	PresenceSessionID  string
	AssignmentEpoch    uint64
	ObservedPath       cloudpb.ObservedPath
}

// ManagedSessionOwner 是 Answerer 持有的 PeerConnection 关闭边界。
// RequestClose 必须幂等；Done 只在 DataChannel/PeerConnection teardown 完成后关闭。
type ManagedSessionOwner interface {
	RequestClose()
	Done() <-chan struct{}
}

// ManagedDataChannelSessionHandler 在 managed Cloud route 中接收精确 SessionContext 与 peer owner。
// 实现仍必须先完成 DataChannel 内 remote auth，不能把 Hub metadata 当作 terminal capability。
type ManagedDataChannelSessionHandler interface {
	ServeManagedDataChannel(context.Context, transport.Transport, string, ManagedSessionContext, ManagedSessionOwner) error
}

type peerSessionOwner struct {
	requestOnce sync.Once
	doneOnce    sync.Once
	cancel      context.CancelFunc
	closePeer   func()
	done        chan struct{}
}

func (owner *peerSessionOwner) RequestClose() {
	if owner == nil {
		return
	}
	owner.requestOnce.Do(func() {
		owner.cancel()
		owner.closePeer()
		owner.markDone()
	})
}

func (owner *peerSessionOwner) Done() <-chan struct{} { return owner.done }

func (owner *peerSessionOwner) peerClosed() {
	if owner == nil {
		return
	}
	owner.cancel()
	owner.markDone()
}

func (owner *peerSessionOwner) markDone() {
	owner.doneOnce.Do(func() { close(owner.done) })
}

// Answerer 把 Cloud Companion 中继的公开 WebRTC offer 协商成 daemon answer。
// PeerConnection 只负责 ICE/DTLS/SCTP；它不接收 grant、terminal payload 或 Hub 私有 runtime 类型。
type Answerer struct {
	Handler DataChannelSessionHandler
	// PeerConnections 只允许注入 Pion primitive 创建策略；nil 保持当前生产默认配置。
	PeerConnections PeerConnectionFactory
	// CloseOnDisconnected 只用于 Direct/SSH ICE-TCP 的短连接 owner：对端关闭后立即释放共享 TCPMux ufrag。
	// managed ICE-UDP 保持 false，让其继续使用 Pion 默认的短暂断连恢复策略。
	CloseOnDisconnected bool
	// OnPeerClosed 在 Pion peer 真正进入 closed 后调用一次，供 daemon listener 释放有界 admission token。
	// callback 不能拥有 session truth，也不能启动替代 Route。
	OnPeerClosed func()
}

// Answer 创建 WebRTC answer，并把唯一可靠有序的 muxvia DataChannel 交给端到端授权 handler。
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
	managedContext, managed := managedContextFromOffer(offer)
	if managed {
		if _, ok := answerer.Handler.(ManagedDataChannelSessionHandler); !ok {
			return nil, fmt.Errorf("remote daemon managed data channel handler is not configured")
		}
		if managedContext.ManagedSessionID == "" || managedContext.SessionIncarnation == 0 || managedContext.ClientDeviceID == "" || managedContext.PresenceSessionID == "" || managedContext.AssignmentEpoch == 0 || managedContext.ObservedPath == cloudpb.ObservedPath_OBSERVED_PATH_UNSPECIFIED {
			return nil, fmt.Errorf("remote daemon managed signaling context is incomplete")
		}
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
	peerFactory := answerer.PeerConnections
	if peerFactory == nil {
		peerFactory = NewPeerConnection
	}
	peer, err := peerFactory(configuration)
	if err != nil {
		return nil, fmt.Errorf("create remote daemon peer connection: %w", err)
	}
	var closePeerOnce sync.Once
	closePeer := func() {
		closePeerOnce.Do(func() {
			_ = peer.Close()
			if answerer.OnPeerClosed != nil {
				answerer.OnPeerClosed()
			}
		})
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	owner := &peerSessionOwner{cancel: cancel, closePeer: closePeer, done: make(chan struct{})}
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
		if state == pion.PeerConnectionStateClosed {
			owner.peerClosed()
			return
		}
		if state == pion.PeerConnectionStateFailed || answerer.CloseOnDisconnected && state == pion.PeerConnectionStateDisconnected {
			go owner.RequestClose()
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
					if managed {
						err = answerer.Handler.(ManagedDataChannelSessionHandler).ServeManagedDataChannel(sessionCtx, protocolTransport, dtlsFingerprint, managedContext, owner)
					} else {
						err = answerer.Handler.ServeDataChannel(sessionCtx, protocolTransport, dtlsFingerprint)
					}
				} else {
					_ = protocolTransport.Close()
				}
				owner.RequestClose()
			}()
		})
	})
	if err := peer.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeOffer, SDP: offer.GetSdp()}); err != nil {
		owner.RequestClose()
		return nil, fmt.Errorf("set remote daemon offer: %w", err)
	}
	localAnswer, err := peer.CreateAnswer(nil)
	if err != nil {
		owner.RequestClose()
		return nil, fmt.Errorf("create remote daemon answer: %w", err)
	}
	gatherComplete := pion.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(localAnswer); err != nil {
		owner.RequestClose()
		return nil, fmt.Errorf("set remote daemon answer: %w", err)
	}
	select {
	case <-ctx.Done():
		owner.RequestClose()
		return nil, ctx.Err()
	case <-gatherComplete:
	}
	description := peer.LocalDescription()
	if description == nil || strings.TrimSpace(description.SDP) == "" {
		owner.RequestClose()
		return nil, fmt.Errorf("remote daemon answer has no local description")
	}
	candidateMu.Lock()
	wireCandidates := append([]*cloudpb.IceCandidate(nil), candidates...)
	candidateMu.Unlock()
	return &cloudpb.SignalingAnswer{
		SignalingSessionId: offer.GetSignalingSessionId(), Sdp: description.SDP, Candidates: wireCandidates,
	}, nil
}

func managedContextFromOffer(offer *cloudpb.SignalingOffer) (ManagedSessionContext, bool) {
	if offer == nil {
		return ManagedSessionContext{}, false
	}
	// managed_session_id 也用于客户端本地 correlation；只有 Hub server fencing metadata 出现时才进入 daemon topology lifecycle。
	managed := offer.GetSessionIncarnation() != 0 || offer.GetPresenceSessionId() != "" || offer.GetAssignmentEpoch() != 0
	path := cloudpb.ObservedPath_OBSERVED_PATH_DIRECT
	if offer.GetRelayOnly() {
		path = cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY
	}
	return ManagedSessionContext{ManagedSessionID: offer.GetManagedSessionId(), SessionIncarnation: offer.GetSessionIncarnation(), ClientDeviceID: offer.GetSourceDeviceId(), PresenceSessionID: offer.GetPresenceSessionId(), AssignmentEpoch: offer.GetAssignmentEpoch(), ObservedPath: path}, managed
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
