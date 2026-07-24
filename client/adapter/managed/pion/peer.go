// Package pion 提供 native Go 平台的 managed WebRTC primitive adapter。
// signaling、remote auth、protocol Hello 和 session generation 均由上层 managed/runtime 持有，本包只操作 Pion peer 与 DataChannel。
package pion

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/client/port"
	"github.com/muxvia/muxvia/proto/cloudpb"
	remotewebrtc "github.com/muxvia/muxvia/remote/webrtc"
	pionwebrtc "github.com/pion/webrtc/v4"
)

const (
	protocolChannelLabel    = "protocol"
	managedPeerReadyTimeout = 15 * time.Second
)

// Factory 创建当前 native 进程使用的 Pion PeerConnection。
// 它不持有 endpoint、credential、Cloud client 或 route winner，因此可被桌面与 Android Go library 共同复用。
type Factory struct {
	// PeerConnections 只覆盖底层 Pion primitive 创建策略；nil 使用当前生产默认配置。
	PeerConnections remotewebrtc.PeerConnectionFactory
}

// OpenDirectPeer 创建只启用 ICE-TCP 的 native Pion peer，供 Direct 与后续 SSH tunnel connector 使用。
// 默认 factory 不发布 UDP candidate；测试可以通过 PeerConnections 注入受控 API，但不能改变上层 Route 或授权语义。
func (factory Factory) OpenDirectPeer(_ context.Context) (port.ManagedPeer, error) {
	peerFactory := factory.PeerConnections
	if peerFactory == nil {
		settings := pionwebrtc.SettingEngine{}
		settings.SetNetworkTypes([]pionwebrtc.NetworkType{pionwebrtc.NetworkTypeTCP4, pionwebrtc.NetworkTypeTCP6})
		settings.SetIncludeLoopbackCandidate(true)
		peerFactory = pionwebrtc.NewAPI(pionwebrtc.WithSettingEngine(settings)).NewPeerConnection
	}
	return openPeer(peerFactory, pionwebrtc.Configuration{})
}

// OpenManagedPeer 按已验证 ICE material 创建可靠有序 protocol DataChannel。
// direct-only 会剔除 TURN URL，relay-only 会启用 Pion relay policy；非法组合直接失败，不能降级为其他策略。
func (factory Factory) OpenManagedPeer(_ context.Context, servers []*cloudpb.IceServer, preference cloudpb.RoutePreference, relayOnly bool) (port.ManagedPeer, error) {
	configuration, err := peerConfiguration(servers, preference, relayOnly)
	if err != nil {
		return nil, err
	}
	peerFactory := factory.PeerConnections
	if peerFactory == nil {
		peerFactory = remotewebrtc.NewPeerConnection
	}
	return openPeer(peerFactory, configuration)
}

func openPeer(peerFactory remotewebrtc.PeerConnectionFactory, configuration pionwebrtc.Configuration) (port.ManagedPeer, error) {
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
	value := &managedPeer{
		peer: peer, channel: channelAdapter, ready: ready, channelClosed: closed,
		connectionFailed: connectionFailed, readyTimeout: managedPeerReadyTimeout,
	}
	channel.OnOpen(func() { value.readyOnce.Do(func() { close(ready) }) })
	channelAdapter.SetCloseHandler(func() { value.channelClosedOnce.Do(func() { close(closed) }) })
	peer.OnConnectionStateChange(func(state pionwebrtc.PeerConnectionState) {
		value.handleConnectionState(state)
	})
	return value, nil
}

type managedPeer struct {
	peer                  *pionwebrtc.PeerConnection
	channel               port.ManagedMessageChannel
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
func (peer *managedPeer) handleConnectionState(state pionwebrtc.PeerConnectionState) {
	var failure error
	switch state {
	case pionwebrtc.PeerConnectionStateFailed:
		failure = fmt.Errorf("managed endpoint WebRTC peer failed")
	case pionwebrtc.PeerConnectionStateClosed:
		failure = fmt.Errorf("managed endpoint WebRTC peer closed")
	default:
		return
	}
	peer.connectionFailureOnce.Do(func() { peer.connectionFailed <- failure })
	// Pion 的状态回调不能同步等待 PeerConnection teardown；channel close 会让现有 protocol read loop 自行完成。
	go peer.closeProtocolChannel()
}

func (peer *managedPeer) closeProtocolChannel() {
	peer.channelCloseOnce.Do(func() {
		if peer.channel != nil {
			peer.channelCloseErr = peer.channel.Close()
		}
	})
}

func (peer *managedPeer) Channel() port.ManagedMessageChannel { return peer.channel }

func (peer *managedPeer) CreateOffer(ctx context.Context) (string, error) {
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
		return "", fmt.Errorf("managed endpoint offer has no local description")
	}
	return description.SDP, nil
}

func (peer *managedPeer) ApplyAnswer(ctx context.Context, answer string, candidates []*cloudpb.IceCandidate) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if strings.TrimSpace(answer) == "" {
		return fmt.Errorf("managed endpoint answer is empty")
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

func (peer *managedPeer) WaitReady(ctx context.Context) error {
	select {
	case <-peer.ready:
		return nil
	default:
	}
	timeout := peer.readyTimeout
	if timeout <= 0 {
		timeout = managedPeerReadyTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-peer.ready:
		return nil
	case <-peer.channelClosed:
		return fmt.Errorf("managed endpoint protocol DataChannel closed before becoming ready")
	case err := <-peer.connectionFailed:
		return err
	case <-timer.C:
		return fmt.Errorf("managed endpoint protocol DataChannel was not ready within %s", timeout)
	}
}

func (peer *managedPeer) RemoteCertificateFingerprint() (string, error) {
	return remotewebrtc.RemoteCertificateFingerprint(peer.peer)
}

func (peer *managedPeer) ObservedPath() endpoint.Path {
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

func (peer *managedPeer) Snapshot(at time.Time) (port.ManagedPeerSnapshot, bool) {
	report := peer.peer.GetStats()
	pair, ok := nominatedCandidatePair(report)
	if !ok {
		return port.ManagedPeerSnapshot{}, false
	}
	local, localOK := report[pair.LocalCandidateID].(pionwebrtc.ICECandidateStats)
	remote, remoteOK := report[pair.RemoteCandidateID].(pionwebrtc.ICECandidateStats)
	if !localOK || !remoteOK {
		return port.ManagedPeerSnapshot{}, false
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
	return port.ManagedPeerSnapshot{
		PairID: pair.ID, Path: candidatePath(local, remote), NetworkClass: strings.ToLower(strings.TrimSpace(local.NetworkType)), At: at.UTC(),
		RoundTrip: rtt, BytesSent: pair.BytesSent, BytesRecv: pair.BytesReceived, PacketsSent: uint64(pair.PacketsSent),
		LossEvents:         saturatingAdd(pair.RetransmissionsSent, uint64(pair.PacketsDiscardedOnSend)),
		Connected:          peer.peer.ConnectionState() == pionwebrtc.PeerConnectionStateConnected,
		LocalCandidateType: strings.ToLower(local.CandidateType.String()), RemoteCandidateType: strings.ToLower(remote.CandidateType.String()),
		LocalProtocol: strings.ToLower(strings.TrimSpace(local.Protocol)), RemoteProtocol: strings.ToLower(strings.TrimSpace(remote.Protocol)),
		RelayProtocol: strings.ToLower(strings.TrimSpace(local.RelayProtocol)),
	}, true
}

func (peer *managedPeer) Close() error {
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

func peerConfiguration(servers []*cloudpb.IceServer, preference cloudpb.RoutePreference, relayOnly bool) (pionwebrtc.Configuration, error) {
	switch preference {
	case cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
		cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY,
		cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE,
		cloudpb.RoutePreference_ROUTE_PREFERENCE_GLOBAL_ACCELERATOR:
	default:
		return pionwebrtc.Configuration{}, fmt.Errorf("unsupported managed WebRTC route preference %s", preference)
	}
	configuration := pionwebrtc.Configuration{}
	if relayOnly {
		if preference == cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY {
			return pionwebrtc.Configuration{}, fmt.Errorf("managed WebRTC direct-only route cannot require relay candidates")
		}
		configuration.ICETransportPolicy = pionwebrtc.ICETransportPolicyRelay
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
		configuration.ICEServers = append(configuration.ICEServers, pionwebrtc.ICEServer{
			URLs: urls, Username: server.GetUsername(), Credential: server.GetCredential(),
		})
	}
	return configuration, nil
}

func toPionCandidate(candidate *cloudpb.IceCandidate) pionwebrtc.ICECandidateInit {
	if candidate == nil {
		return pionwebrtc.ICECandidateInit{}
	}
	mid := candidate.GetSdpMid()
	lineIndex := uint16(candidate.GetSdpMlineIndex())
	usernameFragment := candidate.GetUsernameFragment()
	result := pionwebrtc.ICECandidateInit{Candidate: candidate.GetCandidate(), SDPMLineIndex: &lineIndex}
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

var _ port.ManagedPeerFactory = Factory{}
var _ port.ManagedPeer = (*managedPeer)(nil)
