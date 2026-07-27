// Package pion 提供 native Go 平台的 WebRTC primitive adapter。
// signaling、remote auth、protocol Hello 和 session generation 均由上层 Route/runtime 持有，本包只操作 Pion peer 与 DataChannel。
package pion

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anytty/anytty/client/endpoint"
	"github.com/anytty/anytty/client/port"
	remotewebrtc "github.com/anytty/anytty/remote/webrtc"
	pionice "github.com/pion/ice/v4"
	"github.com/pion/transport/v4"
	pionwebrtc "github.com/pion/webrtc/v4"
)

const (
	protocolChannelLabel = "protocol"
	peerReadyTimeout     = 15 * time.Second
)

// Factory 创建当前 native 进程使用的 Pion PeerConnection。
// 它不持有 endpoint、credential、Cloud client 或 route winner，因此可被桌面与 Android Go library 共同复用。
type Factory struct {
	// PeerConnections 只覆盖底层 Pion primitive 创建策略；nil 使用当前生产默认配置。
	PeerConnections remotewebrtc.PeerConnectionFactory
	// Network 是当前 platform generation 的网络接口快照；nil 使用 Pion 默认网络枚举。
	// Android 必须注入不依赖受限 netlink 的实现，网络切换后由平台 lifecycle 重建整个 generation。
	Network transport.Net
}

// OpenDirectPeer 创建只启用 ICE-TCP 的 native Pion peer，供 Direct 与后续 SSH tunnel connector 使用。
// 默认 factory 不发布 UDP candidate；测试可以通过 PeerConnections 注入受控 API，但不能改变上层 Route 或授权语义。
func (factory Factory) OpenDirectPeer(_ context.Context) (port.WebRTCPeer, error) {
	peerFactory := factory.PeerConnections
	if peerFactory == nil {
		settings := pionwebrtc.SettingEngine{}
		settings.SetNetworkTypes([]pionwebrtc.NetworkType{pionwebrtc.NetworkTypeTCP4, pionwebrtc.NetworkTypeTCP6})
		settings.SetIncludeLoopbackCandidate(true)
		if factory.Network != nil {
			settings.SetNet(factory.Network)
			// Android sandbox 禁止 mDNS 内部绕过 transport.Net 再读取系统网卡。
			// Direct 使用 daemon-signed locator，Cloud 使用显式 ICE server，二者均不依赖 mDNS candidate。
			settings.SetICEMulticastDNSMode(pionice.MulticastDNSModeDisabled)
		}
		peerFactory = pionwebrtc.NewAPI(pionwebrtc.WithSettingEngine(settings)).NewPeerConnection
	}
	return openPeer(peerFactory, pionwebrtc.Configuration{})
}

// OpenCloudPeer 创建允许 ICE-UDP host/srflx candidate 的 native Pion peer。
// TURN server 与 ICE transport policy 由 Cloud adapter 按当前 attempt 显式传入。
func (factory Factory) OpenCloudPeer(_ context.Context, config port.WebRTCConfig) (port.WebRTCPeer, error) {
	peerFactory := factory.PeerConnections
	if peerFactory == nil {
		if factory.Network == nil {
			peerFactory = remotewebrtc.NewPeerConnection
		} else {
			settings := pionwebrtc.SettingEngine{}
			settings.SetNet(factory.Network)
			settings.SetICEMulticastDNSMode(pionice.MulticastDNSModeDisabled)
			peerFactory = pionwebrtc.NewAPI(pionwebrtc.WithSettingEngine(settings)).NewPeerConnection
		}
	}
	configuration := pionwebrtc.Configuration{ICEServers: make([]pionwebrtc.ICEServer, 0, len(config.Servers))}
	for _, server := range config.Servers {
		configuration.ICEServers = append(configuration.ICEServers, pionwebrtc.ICEServer{URLs: append([]string(nil), server.URLs...), Username: server.Username, Credential: server.Credential})
	}
	if config.Policy == port.ICETransportRelayOnly {
		configuration.ICETransportPolicy = pionwebrtc.ICETransportPolicyRelay
	}
	return openPeer(peerFactory, configuration)
}

func openPeer(peerFactory remotewebrtc.PeerConnectionFactory, configuration pionwebrtc.Configuration) (port.WebRTCPeer, error) {
	peer, err := peerFactory(configuration)
	if err != nil {
		return nil, err
	}
	channel, err := peer.CreateDataChannel(protocolChannelLabel, nil)
	if err != nil {
		_ = peer.Close()
		return nil, err
	}
	ready := make(chan struct{})
	closed := make(chan struct{})
	connectionFailed := make(chan error, 1)
	channelAdapter := remotewebrtc.NewChannel(channel)
	value := &webRTCPeer{
		peer: peer, channel: channelAdapter, ready: ready, channelClosed: closed,
		connectionFailed: connectionFailed, readyTimeout: peerReadyTimeout,
	}
	channel.OnOpen(func() { value.readyOnce.Do(func() { close(ready) }) })
	channelAdapter.SetCloseHandler(func() { value.channelClosedOnce.Do(func() { close(closed) }) })
	peer.OnConnectionStateChange(func(state pionwebrtc.PeerConnectionState) {
		value.handleConnectionState(state)
	})
	return value, nil
}

type webRTCPeer struct {
	peer                  *pionwebrtc.PeerConnection
	channel               port.WebRTCMessageChannel
	ready                 chan struct{}
	readyOnce             sync.Once
	channelClosed         chan struct{}
	channelClosedOnce     sync.Once
	channelCloseOnce      sync.Once
	channelCloseErr       error
	connectionFailed      chan error
	connectionFailureOnce sync.Once
	readyTimeout          time.Duration
	closeOnce             sync.Once
	closeErr              error
}

// handleConnectionState 把 Pion 的最终失败接回 protocol/DataChannel lifecycle。
// Ready 之前 connectionFailed 结束建连等待；Ready 之后关闭同一 channel，使 protocol Done 和 SessionOwner generation 失效，禁止留下半开 application session。
func (peer *webRTCPeer) handleConnectionState(state pionwebrtc.PeerConnectionState) {
	var failure error
	switch state {
	case pionwebrtc.PeerConnectionStateFailed:
		failure = fmt.Errorf("WebRTC peer failed")
	case pionwebrtc.PeerConnectionStateClosed:
		failure = fmt.Errorf("WebRTC peer closed")
	default:
		return
	}
	peer.connectionFailureOnce.Do(func() { peer.connectionFailed <- failure })
	// Pion 的状态回调不能同步等待 PeerConnection teardown；channel close 会让现有 protocol read loop 自行完成。
	go peer.closeProtocolChannel()
}

func (peer *webRTCPeer) closeProtocolChannel() {
	peer.channelCloseOnce.Do(func() {
		if peer.channel != nil {
			peer.channelCloseErr = peer.channel.Close()
		}
	})
}

func (peer *webRTCPeer) Channel() port.WebRTCMessageChannel { return peer.channel }

func (peer *webRTCPeer) CreateOffer(ctx context.Context) (string, error) {
	offer, err := peer.peer.CreateOffer(nil)
	if err != nil {
		return "", err
	}
	gatherComplete := pionwebrtc.GatheringCompletePromise(peer.peer)
	if err := peer.peer.SetLocalDescription(offer); err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-gatherComplete:
	}
	description := peer.peer.LocalDescription()
	if description == nil || strings.TrimSpace(description.SDP) == "" {
		return "", fmt.Errorf("WebRTC offer has no local description")
	}
	return description.SDP, nil
}

func (peer *webRTCPeer) ApplyAnswer(ctx context.Context, answer string, candidates []port.ICECandidate) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if strings.TrimSpace(answer) == "" {
		return fmt.Errorf("WebRTC answer is empty")
	}
	if err := peer.peer.SetRemoteDescription(pionwebrtc.SessionDescription{Type: pionwebrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		return err
	}
	for _, candidate := range candidates {
		if err := peer.peer.AddICECandidate(toPionCandidate(candidate)); err != nil {
			return err
		}
	}
	return nil
}

func (peer *webRTCPeer) WaitReady(ctx context.Context) error {
	select {
	case <-peer.ready:
		return nil
	default:
	}
	timeout := peer.readyTimeout
	if timeout <= 0 {
		timeout = peerReadyTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-peer.ready:
		return nil
	case <-peer.channelClosed:
		return fmt.Errorf("WebRTC protocol DataChannel closed before becoming ready")
	case err := <-peer.connectionFailed:
		return err
	case <-timer.C:
		return fmt.Errorf("WebRTC protocol DataChannel was not ready within %s", timeout)
	}
}

func (peer *webRTCPeer) RemoteCertificateFingerprint() (string, error) {
	return remotewebrtc.RemoteCertificateFingerprint(peer.peer)
}

func (peer *webRTCPeer) ObservedPath() endpoint.Path {
	if peer == nil || peer.peer == nil {
		return ""
	}
	if sctp := peer.peer.SCTP(); sctp != nil && sctp.Transport() != nil && sctp.Transport().ICETransport() != nil {
		pair, err := sctp.Transport().ICETransport().GetSelectedCandidatePair()
		if err == nil && pair != nil && pair.Local != nil && pair.Remote != nil {
			if pair.Local.Typ == pionwebrtc.ICECandidateTypeRelay || pair.Remote.Typ == pionwebrtc.ICECandidateTypeRelay {
				return endpoint.PathSingleRelay
			}
			return endpoint.PathDirect
		}
	}
	return pathFromStats(peer.peer.GetStats())
}

func (peer *webRTCPeer) Snapshot(at time.Time) (port.WebRTCPeerSnapshot, bool) {
	report := peer.peer.GetStats()
	pair, ok := nominatedCandidatePair(report)
	if !ok {
		return port.WebRTCPeerSnapshot{}, false
	}
	local, localOK := report[pair.LocalCandidateID].(pionwebrtc.ICECandidateStats)
	remote, remoteOK := report[pair.RemoteCandidateID].(pionwebrtc.ICECandidateStats)
	if !localOK || !remoteOK {
		return port.WebRTCPeerSnapshot{}, false
	}
	rtt := secondsDuration(pair.CurrentRoundTripTime)
	if rtt == 0 {
		for _, stat := range report {
			if sctp, ok := stat.(pionwebrtc.SCTPTransportStats); ok {
				rtt = secondsDuration(sctp.SmoothedRoundTripTime)
				break
			}
		}
	}
	return port.WebRTCPeerSnapshot{
		PairID: pair.ID, Path: candidatePath(local, remote), NetworkClass: strings.ToLower(strings.TrimSpace(local.NetworkType)), At: at.UTC(),
		RoundTrip: rtt, BytesSent: pair.BytesSent, BytesRecv: pair.BytesReceived, PacketsSent: uint64(pair.PacketsSent),
		LossEvents:         saturatingAdd(pair.RetransmissionsSent, uint64(pair.PacketsDiscardedOnSend)),
		Connected:          peer.peer.ConnectionState() == pionwebrtc.PeerConnectionStateConnected,
		LocalCandidateType: strings.ToLower(local.CandidateType.String()), RemoteCandidateType: strings.ToLower(remote.CandidateType.String()),
		LocalProtocol: strings.ToLower(strings.TrimSpace(local.Protocol)), RemoteProtocol: strings.ToLower(strings.TrimSpace(remote.Protocol)),
		RelayProtocol: strings.ToLower(strings.TrimSpace(local.RelayProtocol)),
	}, true
}

func (peer *webRTCPeer) Close() error {
	if peer == nil {
		return nil
	}
	peer.closeOnce.Do(func() {
		peer.closeProtocolChannel()
		peer.closeErr = peer.channelCloseErr
		if peer.channelClosed != nil {
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-peer.channelClosed:
				if !timer.Stop() {
					<-timer.C
				}
			case <-timer.C:
			}
		}
		if err := peer.peer.Close(); peer.closeErr == nil {
			peer.closeErr = err
		}
	})
	return peer.closeErr
}

func toPionCandidate(candidate port.ICECandidate) pionwebrtc.ICECandidateInit {
	mid := candidate.SDPMid
	lineIndex := uint16(candidate.SDPMLineIndex)
	usernameFragment := candidate.UsernameFragment
	result := pionwebrtc.ICECandidateInit{Candidate: candidate.Candidate, SDPMLineIndex: &lineIndex}
	if mid != "" {
		result.SDPMid = &mid
	}
	if usernameFragment != "" {
		result.UsernameFragment = &usernameFragment
	}
	return result
}

func pathFromStats(report pionwebrtc.StatsReport) endpoint.Path {
	pair, ok := nominatedCandidatePair(report)
	if !ok {
		return ""
	}
	local, localOK := report[pair.LocalCandidateID].(pionwebrtc.ICECandidateStats)
	remote, remoteOK := report[pair.RemoteCandidateID].(pionwebrtc.ICECandidateStats)
	if !localOK || !remoteOK {
		return ""
	}
	return candidatePath(local, remote)
}

func candidatePath(local, remote pionwebrtc.ICECandidateStats) endpoint.Path {
	if local.CandidateType == pionwebrtc.ICECandidateTypeRelay || remote.CandidateType == pionwebrtc.ICECandidateTypeRelay {
		return endpoint.PathSingleRelay
	}
	return endpoint.PathDirect
}

func nominatedCandidatePair(report pionwebrtc.StatsReport) (pionwebrtc.ICECandidatePairStats, bool) {
	ids := make([]string, 0)
	for id, stat := range report {
		pair, ok := stat.(pionwebrtc.ICECandidatePairStats)
		if ok && pair.Nominated && pair.State == pionwebrtc.StatsICECandidatePairStateSucceeded {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return pionwebrtc.ICECandidatePairStats{}, false
	}
	sort.Strings(ids)
	pair, ok := report[ids[0]].(pionwebrtc.ICECandidatePairStats)
	if ok && pair.ID == "" {
		pair.ID = ids[0]
	}
	return pair, ok
}

func secondsDuration(seconds float64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	if seconds >= float64(math.MaxInt64)/float64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(seconds * float64(time.Second))
}

func saturatingAdd(left, right uint64) uint64 {
	if math.MaxUint64-left < right {
		return math.MaxUint64
	}
	return left + right
}

var _ port.WebRTCPeer = (*webRTCPeer)(nil)
