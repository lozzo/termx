// Package platform 把 bindingpb 平台请求投影为浏览器 WebRTC primitive。
// 本包不解释 remote-auth、Hello 或 application payload；这些真值仍由 managed Dialer 和 protocol client 持有。
package platform

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/muxvia/muxvia/client/binding"
	"github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/client/port"
	"github.com/muxvia/muxvia/proto/bindingpb"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const platformCallTimeout = 5 * time.Second

// Factory 为单个 Go Client Engine 路由浏览器 peer/channel handle。
// handle 由当前浏览器 adapter 分配，只能在该 Factory 所属 engine generation 内使用。
type Factory struct {
	broker *binding.PlatformBroker

	mu       sync.RWMutex
	closed   bool
	peers    map[uint64]*peer
	channels map[uint64]*channel
}

// NewFactory 创建绑定到单个 PlatformBroker 的浏览器 WebRTC factory。
func NewFactory(broker *binding.PlatformBroker) (*Factory, error) {
	if broker == nil {
		return nil, fmt.Errorf("platform broker is required")
	}
	return &Factory{broker: broker, peers: make(map[uint64]*peer), channels: make(map[uint64]*channel)}, nil
}

// OpenManagedPeer 请求浏览器创建一个可靠有序 protocol DataChannel。
// ICE、relay policy 和 route preference 完整来自 Go 已验证的 route plan。
func (factory *Factory) OpenManagedPeer(ctx context.Context, servers []*cloudpb.IceServer, preference cloudpb.RoutePreference, relayOnly bool) (port.ManagedPeer, error) {
	request := &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_WebrtcOpenPeer{WebrtcOpenPeer: &bindingpb.WebRTCOpenPeerRequest{
		IceServers: cloneIceServers(servers), RoutePreference: preference, RelayOnly: relayOnly,
	}}}
	response, err := factory.exchange(ctx, request)
	if err != nil {
		return nil, err
	}
	opened := response.GetWebrtcPeerOpened()
	if opened == nil || opened.GetPeerHandle() == 0 || opened.GetChannelHandle() == 0 {
		return nil, fmt.Errorf("browser platform returned invalid WebRTC handles")
	}
	peerCtx, cancel := context.WithCancel(context.Background())
	value := &peer{factory: factory, handle: opened.GetPeerHandle(), ctx: peerCtx, cancel: cancel}
	value.channel = &channel{peer: value, handle: opened.GetChannelHandle()}
	factory.mu.Lock()
	if factory.closed || factory.peers[value.handle] != nil || factory.channels[value.channel.handle] != nil {
		factory.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("browser platform reused an active WebRTC handle")
	}
	factory.peers[value.handle] = value
	factory.channels[value.channel.handle] = value.channel
	factory.mu.Unlock()
	return value, nil
}

// HandleEvent 接收 serialized bindingpb.PlatformEvent，并只投递到当前 engine 的活动 channel。
// 未知或已关闭 handle 一律失败，防止 tab 恢复后的迟到 callback 命中新 generation。
func (factory *Factory) HandleEvent(payload []byte) error {
	if len(payload) == 0 || len(payload) > binding.MaxPayloadBytes {
		return fmt.Errorf("browser platform event payload is invalid")
	}
	event := &bindingpb.PlatformEvent{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, event); err != nil {
		return fmt.Errorf("decode browser platform event: %w", err)
	}
	switch value := event.GetEvent().(type) {
	case *bindingpb.PlatformEvent_WebrtcChannelMessage:
		channel, err := factory.activeChannel(value.WebrtcChannelMessage.GetChannelHandle())
		if err != nil {
			return err
		}
		channel.publishMessage(value.WebrtcChannelMessage.GetPayload())
		return nil
	case *bindingpb.PlatformEvent_WebrtcChannelClosed:
		channel, err := factory.activeChannel(value.WebrtcChannelClosed.GetChannelHandle())
		if err != nil {
			return err
		}
		channel.publishClosed()
		return nil
	case *bindingpb.PlatformEvent_WebrtcBufferedAmountLow:
		channel, err := factory.activeChannel(value.WebrtcBufferedAmountLow.GetChannelHandle())
		if err != nil {
			return err
		}
		channel.buffered.Store(value.WebrtcBufferedAmountLow.GetBufferedAmount())
		channel.publishBufferedLow()
		return nil
	default:
		return fmt.Errorf("browser platform event is empty or unsupported")
	}
}

// Close 关闭当前 generation 的全部 peer，并使后续平台事件失效。
func (factory *Factory) Close() error {
	if factory == nil {
		return nil
	}
	factory.mu.Lock()
	if factory.closed {
		factory.mu.Unlock()
		return nil
	}
	factory.closed = true
	peers := make([]*peer, 0, len(factory.peers))
	for _, value := range factory.peers {
		peers = append(peers, value)
	}
	factory.mu.Unlock()
	for _, value := range peers {
		_ = value.Close()
	}
	return nil
}

func (factory *Factory) exchange(ctx context.Context, request *bindingpb.PlatformRequest) (*bindingpb.PlatformResponse, error) {
	response, err := factory.broker.Exchange(ctx, request)
	if err != nil {
		return nil, err
	}
	if response.GetError() != nil {
		return nil, fmt.Errorf("browser platform request failed: %s", response.GetError().GetMessage())
	}
	return response, nil
}

func (factory *Factory) activeChannel(handle uint64) (*channel, error) {
	factory.mu.RLock()
	value := factory.channels[handle]
	closed := factory.closed
	factory.mu.RUnlock()
	if closed || value == nil || value.closed.Load() {
		return nil, binding.ErrInvalidHandle
	}
	return value, nil
}

func (factory *Factory) forget(value *peer) {
	factory.mu.Lock()
	delete(factory.peers, value.handle)
	if value.channel != nil {
		delete(factory.channels, value.channel.handle)
	}
	factory.mu.Unlock()
}

type peer struct {
	factory *Factory
	handle  uint64
	channel *channel
	ctx     context.Context
	cancel  context.CancelFunc

	mu          sync.RWMutex
	fingerprint string
	path        endpoint.Path
	closeOnce   sync.Once
}

func (peer *peer) Channel() port.ManagedMessageChannel { return peer.channel }

func (peer *peer) CreateOffer(ctx context.Context) (string, error) {
	response, err := peer.factory.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_WebrtcCreateOffer{
		WebrtcCreateOffer: &bindingpb.WebRTCPeerRequest{PeerHandle: peer.handle},
	}})
	if err != nil {
		return "", err
	}
	offer := response.GetWebrtcOffer().GetOfferSdp()
	if strings.TrimSpace(offer) == "" {
		return "", fmt.Errorf("browser WebRTC offer SDP is empty")
	}
	return offer, nil
}

func (peer *peer) ApplyAnswer(ctx context.Context, answer string, candidates []*cloudpb.IceCandidate) error {
	if strings.TrimSpace(answer) == "" {
		return fmt.Errorf("browser WebRTC answer SDP is empty")
	}
	_, err := peer.factory.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_WebrtcApplyAnswer{
		WebrtcApplyAnswer: &bindingpb.WebRTCApplyAnswerRequest{PeerHandle: peer.handle, AnswerSdp: answer, Candidates: cloneCandidates(candidates)},
	}})
	return err
}

func (peer *peer) WaitReady(ctx context.Context) error {
	response, err := peer.factory.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_WebrtcWaitReady{
		WebrtcWaitReady: &bindingpb.WebRTCPeerRequest{PeerHandle: peer.handle},
	}})
	if err != nil {
		return err
	}
	ready := response.GetWebrtcPeerReady()
	if ready == nil || strings.TrimSpace(ready.GetRemoteCertificateFingerprint()) == "" {
		return fmt.Errorf("browser WebRTC ready proof is incomplete")
	}
	path, err := observedPath(ready.GetObservedPath())
	if err != nil {
		return err
	}
	peer.mu.Lock()
	peer.fingerprint = strings.TrimSpace(ready.GetRemoteCertificateFingerprint())
	peer.path = path
	peer.mu.Unlock()
	return nil
}

func (peer *peer) RemoteCertificateFingerprint() (string, error) {
	peer.mu.RLock()
	value := peer.fingerprint
	peer.mu.RUnlock()
	if value == "" {
		return "", fmt.Errorf("browser WebRTC peer is not ready")
	}
	return value, nil
}

func (peer *peer) ObservedPath() endpoint.Path {
	peer.mu.RLock()
	defer peer.mu.RUnlock()
	return peer.path
}

func (peer *peer) Snapshot(at time.Time) (port.ManagedPeerSnapshot, bool) {
	ctx, cancel := context.WithTimeout(peer.ctx, platformCallTimeout)
	defer cancel()
	response, err := peer.factory.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_WebrtcPeerSnapshot{
		WebrtcPeerSnapshot: &bindingpb.WebRTCPeerSnapshotRequest{PeerHandle: peer.handle, SampledAtUnixNano: at.UTC().UnixNano()},
	}})
	if err != nil || response.GetWebrtcPeerSnapshot() == nil || !response.GetWebrtcPeerSnapshot().GetValid() {
		return port.ManagedPeerSnapshot{}, false
	}
	value := response.GetWebrtcPeerSnapshot()
	path, err := observedPath(value.GetPath())
	if err != nil {
		return port.ManagedPeerSnapshot{}, false
	}
	return port.ManagedPeerSnapshot{
		PairID: value.GetPairId(), Path: path, NetworkClass: value.GetNetworkClass(),
		At: time.Unix(0, value.GetSampledAtUnixNano()).UTC(), RoundTrip: time.Duration(value.GetRoundTripNanos()),
		BytesSent: value.GetBytesSent(), BytesRecv: value.GetBytesReceived(), PacketsSent: value.GetPacketsSent(),
		LossEvents: value.GetLossEvents(), Connected: value.GetConnected(),
		LocalCandidateType: bindingCandidateType(value.GetLocalCandidateType()), RemoteCandidateType: bindingCandidateType(value.GetRemoteCandidateType()),
		LocalProtocol: bindingTransport(value.GetLocalProtocol()), RemoteProtocol: bindingTransport(value.GetRemoteProtocol()),
		RelayProtocol: bindingTransport(value.GetRelayTransport()),
	}, true
}

func bindingCandidateType(value bindingpb.ConnectionCandidateType) string {
	switch value {
	case bindingpb.ConnectionCandidateType_CONNECTION_CANDIDATE_TYPE_HOST:
		return "host"
	case bindingpb.ConnectionCandidateType_CONNECTION_CANDIDATE_TYPE_SERVER_REFLEXIVE:
		return "srflx"
	case bindingpb.ConnectionCandidateType_CONNECTION_CANDIDATE_TYPE_PEER_REFLEXIVE:
		return "prflx"
	case bindingpb.ConnectionCandidateType_CONNECTION_CANDIDATE_TYPE_RELAY:
		return "relay"
	default:
		return ""
	}
}

func bindingTransport(value bindingpb.ConnectionTransport) string {
	switch value {
	case bindingpb.ConnectionTransport_CONNECTION_TRANSPORT_UDP:
		return "udp"
	case bindingpb.ConnectionTransport_CONNECTION_TRANSPORT_TCP:
		return "tcp"
	default:
		return ""
	}
}

func (peer *peer) Close() error {
	if peer == nil {
		return nil
	}
	peer.closeOnce.Do(func() {
		peer.cancel()
		if peer.channel != nil {
			peer.channel.publishClosed()
		}
		ctx, cancel := context.WithTimeout(context.Background(), platformCallTimeout)
		defer cancel()
		_, _ = peer.factory.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_WebrtcClosePeer{
			WebrtcClosePeer: &bindingpb.WebRTCCloseRequest{Handle: peer.handle},
		}})
		peer.factory.forget(peer)
	})
	return nil
}

type channel struct {
	peer   *peer
	handle uint64

	buffered  atomic.Uint64
	closed    atomic.Bool
	mu        sync.RWMutex
	onMessage func([]byte)
	onClose   func()
	onLow     func()
	closeOnce sync.Once
}

func (channel *channel) SetMessageHandler(handler func([]byte)) {
	channel.mu.Lock()
	channel.onMessage = handler
	channel.mu.Unlock()
}

func (channel *channel) SetCloseHandler(handler func()) {
	channel.mu.Lock()
	channel.onClose = handler
	closed := channel.closed.Load()
	channel.mu.Unlock()
	if closed && handler != nil {
		handler()
	}
}

func (channel *channel) BufferedAmount() uint64 { return channel.buffered.Load() }

func (channel *channel) SetBufferedAmountLowThreshold(value uint64) {
	ctx, cancel := context.WithTimeout(channel.peer.ctx, platformCallTimeout)
	defer cancel()
	_, _ = channel.peer.factory.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_WebrtcChannelThreshold{
		WebrtcChannelThreshold: &bindingpb.WebRTCChannelThresholdRequest{ChannelHandle: channel.handle, LowThreshold: value},
	}})
}

func (channel *channel) SetBufferedAmountLowHandler(handler func()) {
	channel.mu.Lock()
	channel.onLow = handler
	channel.mu.Unlock()
}

func (channel *channel) Send(payload []byte) error {
	if channel.closed.Load() {
		return binding.ErrClosed
	}
	response, err := channel.peer.factory.exchange(channel.peer.ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_WebrtcChannelSend{
		WebrtcChannelSend: &bindingpb.WebRTCChannelSendRequest{ChannelHandle: channel.handle, Payload: append([]byte(nil), payload...)},
	}})
	if err != nil {
		return err
	}
	channel.buffered.Store(response.GetWebrtcChannelSent().GetBufferedAmount())
	return nil
}

func (channel *channel) Close() error {
	channel.closeOnce.Do(func() {
		channel.publishClosed()
		ctx, cancel := context.WithTimeout(context.Background(), platformCallTimeout)
		defer cancel()
		_, _ = channel.peer.factory.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_WebrtcCloseChannel{
			WebrtcCloseChannel: &bindingpb.WebRTCCloseRequest{Handle: channel.handle},
		}})
	})
	return nil
}

func (channel *channel) publishMessage(payload []byte) {
	channel.mu.RLock()
	handler := channel.onMessage
	channel.mu.RUnlock()
	if handler != nil {
		handler(append([]byte(nil), payload...))
	}
}

func (channel *channel) publishClosed() {
	if !channel.closed.CompareAndSwap(false, true) {
		return
	}
	channel.mu.RLock()
	handler := channel.onClose
	channel.mu.RUnlock()
	if handler != nil {
		handler()
	}
}

func (channel *channel) publishBufferedLow() {
	channel.mu.RLock()
	handler := channel.onLow
	channel.mu.RUnlock()
	if handler != nil {
		handler()
	}
}

func observedPath(value cloudpb.ObservedPath) (endpoint.Path, error) {
	switch value {
	case cloudpb.ObservedPath_OBSERVED_PATH_DIRECT:
		return endpoint.PathDirect, nil
	case cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY:
		return endpoint.PathSingleRelay, nil
	default:
		return "", fmt.Errorf("browser WebRTC observed path is unsupported: %s", value)
	}
}

func cloneIceServers(values []*cloudpb.IceServer) []*cloudpb.IceServer {
	result := make([]*cloudpb.IceServer, 0, len(values))
	for _, value := range values {
		if value != nil {
			result = append(result, proto.Clone(value).(*cloudpb.IceServer))
		}
	}
	return result
}

func cloneCandidates(values []*cloudpb.IceCandidate) []*cloudpb.IceCandidate {
	result := make([]*cloudpb.IceCandidate, 0, len(values))
	for _, value := range values {
		if value != nil {
			result = append(result, proto.Clone(value).(*cloudpb.IceCandidate))
		}
	}
	return result
}

var _ port.ManagedPeerFactory = (*Factory)(nil)
var _ port.ManagedPeer = (*peer)(nil)
var _ port.ManagedMessageChannel = (*channel)(nil)
