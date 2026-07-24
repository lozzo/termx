package pion

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	pionwebrtc "github.com/pion/webrtc/v4"
)

func TestPeerConfigurationEnforcesCandidatePolicy(t *testing.T) {
	servers := []*cloudpb.IceServer{{
		Urls: []string{"stun:stun.example.com", "turn:turn.example.com?transport=udp", "turn:turn.example.com?transport=tcp"}, Username: "client", Credential: "secret",
	}}
	relay, err := peerConfiguration(servers, cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY, true)
	if err != nil {
		t.Fatal(err)
	}
	if relay.ICETransportPolicy != pionwebrtc.ICETransportPolicyRelay || len(relay.ICEServers) != 1 || len(relay.ICEServers[0].URLs) != 3 || relay.ICEServers[0].URLs[1] != "turn:turn.example.com?transport=udp" || relay.ICEServers[0].URLs[2] != "turn:turn.example.com?transport=tcp" {
		t.Fatalf("relay configuration = %#v", relay)
	}
	direct, err := peerConfiguration(servers, cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(direct.ICEServers) != 1 || len(direct.ICEServers[0].URLs) != 1 || direct.ICEServers[0].URLs[0] != "stun:stun.example.com" {
		t.Fatalf("direct configuration retained TURN material: %#v", direct)
	}
	if _, err := peerConfiguration(servers, cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY, true); err == nil {
		t.Fatal("direct-only route accepted relay-only policy")
	}
}

func TestWaitReadyReturnsWhenChannelClosesBeforeOpen(t *testing.T) {
	peer := &managedPeer{ready: make(chan struct{}), channelClosed: make(chan struct{}), readyTimeout: time.Second}
	close(peer.channelClosed)
	if err := peer.WaitReady(context.Background()); err == nil {
		t.Fatal("closed DataChannel was reported ready")
	}
}

func TestWaitReadyReturnsPeerFailureAndTimeout(t *testing.T) {
	connectionFailed := make(chan error, 1)
	connectionFailed <- errors.New("ICE failed")
	peer := &managedPeer{ready: make(chan struct{}), channelClosed: make(chan struct{}), connectionFailed: connectionFailed, readyTimeout: time.Second}
	if err := peer.WaitReady(context.Background()); err == nil || err.Error() != "ICE failed" {
		t.Fatalf("peer failure = %v", err)
	}

	peer = &managedPeer{ready: make(chan struct{}), channelClosed: make(chan struct{}), connectionFailed: make(chan error), readyTimeout: time.Millisecond}
	if err := peer.WaitReady(context.Background()); err == nil {
		t.Fatal("WebRTC ready timeout unexpectedly succeeded")
	}
}

func TestPeerFailureAfterReadyClosesProtocolChannel(t *testing.T) {
	channel := &lifecycleChannel{closed: make(chan struct{})}
	peer := &managedPeer{
		channel: channel, ready: make(chan struct{}), channelClosed: channel.closed,
		connectionFailed: make(chan error, 1), readyTimeout: time.Second,
	}
	channel.SetCloseHandler(func() {
		peer.channelClosedOnce.Do(func() { close(peer.channelClosed) })
	})
	close(peer.ready)
	peer.handleConnectionState(pionwebrtc.PeerConnectionStateFailed)

	select {
	case <-channel.closed:
	case <-time.After(time.Second):
		t.Fatal("final peer failure left the ready protocol channel half-open")
	}
	select {
	case err := <-peer.connectionFailed:
		if err == nil || err.Error() != "managed endpoint WebRTC peer failed" {
			t.Fatalf("peer failure = %v", err)
		}
	default:
		t.Fatal("final peer failure did not notify the pre-ready waiter")
	}
	peer.closeProtocolChannel()
	if channel.closeCalls != 1 {
		t.Fatalf("protocol channel close calls = %d, want 1", channel.closeCalls)
	}
}

type lifecycleChannel struct {
	mu         sync.Mutex
	closed     chan struct{}
	closeOnce  sync.Once
	closeCalls int
	onClose    func()
}

func (*lifecycleChannel) SetMessageHandler(func([]byte)) {}
func (channel *lifecycleChannel) SetCloseHandler(handler func()) {
	channel.onClose = handler
}
func (*lifecycleChannel) BufferedAmount() uint64               { return 0 }
func (*lifecycleChannel) SetBufferedAmountLowThreshold(uint64) {}
func (*lifecycleChannel) SetBufferedAmountLowHandler(func())   {}
func (*lifecycleChannel) Send([]byte) error                    { return nil }
func (channel *lifecycleChannel) Close() error {
	channel.closeOnce.Do(func() {
		channel.mu.Lock()
		channel.closeCalls++
		channel.mu.Unlock()
		if channel.onClose != nil {
			channel.onClose()
		} else {
			close(channel.closed)
		}
	})
	return nil
}
