package pion

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	pionwebrtc "github.com/pion/webrtc/v4"
)

func TestDefaultRouteNetUsesSocketSelectedAddresses(t *testing.T) {
	dials := make([]string, 0, 2)
	network, err := newDefaultRouteNet(func(network, address string) (net.Conn, error) {
		dials = append(dials, network+" "+address)
		switch network {
		case "udp4":
			return &routeProbeConn{local: &net.UDPAddr{IP: net.ParseIP("10.0.2.16"), Port: 49152}}, nil
		case "udp6":
			return nil, errors.New("IPv6 is unavailable")
		default:
			return nil, errors.New("unexpected probe")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dials) != 2 {
		t.Fatalf("route probes = %v", dials)
	}
	interfaces, err := network.Interfaces()
	if err != nil || len(interfaces) != 1 {
		t.Fatalf("interfaces = %v, err = %v", interfaces, err)
	}
	addresses, err := interfaces[0].Addrs()
	if err != nil || len(addresses) != 1 || addresses[0].String() != "10.0.2.16/32" {
		t.Fatalf("interface addresses = %v, err = %v", addresses, err)
	}
	if _, err := network.InterfaceByName("default-route-v4"); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultRouteNetRejectsMissingRoute(t *testing.T) {
	if _, err := newDefaultRouteNet(func(string, string) (net.Conn, error) {
		return nil, errors.New("route unavailable")
	}); err == nil {
		t.Fatal("missing default route unexpectedly created a Pion network")
	}
}

func TestWaitReadyReturnsWhenChannelClosesBeforeOpen(t *testing.T) {
	peer := &webRTCPeer{ready: make(chan struct{}), channelClosed: make(chan struct{}), readyTimeout: time.Second}
	close(peer.channelClosed)
	if err := peer.WaitReady(context.Background()); err == nil {
		t.Fatal("closed DataChannel was reported ready")
	}
}

func TestCandidatePortRejectsInvalidStatsValues(t *testing.T) {
	if got := candidatePort(41121); got != 41121 {
		t.Fatalf("candidatePort(41121) = %d", got)
	}
	for _, value := range []int32{-1, 0, 65536} {
		if got := candidatePort(value); got != 0 {
			t.Fatalf("candidatePort(%d) = %d, want 0", value, got)
		}
	}
}

func TestWaitReadyReturnsPeerFailureAndTimeout(t *testing.T) {
	connectionFailed := make(chan error, 1)
	connectionFailed <- errors.New("ICE failed")
	peer := &webRTCPeer{ready: make(chan struct{}), channelClosed: make(chan struct{}), connectionFailed: connectionFailed, readyTimeout: time.Second}
	if err := peer.WaitReady(context.Background()); err == nil || err.Error() != "ICE failed" {
		t.Fatalf("peer failure = %v", err)
	}

	peer = &webRTCPeer{ready: make(chan struct{}), channelClosed: make(chan struct{}), connectionFailed: make(chan error), readyTimeout: time.Millisecond}
	if err := peer.WaitReady(context.Background()); err == nil {
		t.Fatal("WebRTC ready timeout unexpectedly succeeded")
	}
}

func TestPeerFailureAfterReadyClosesProtocolChannel(t *testing.T) {
	channel := &lifecycleChannel{closed: make(chan struct{})}
	peer := &webRTCPeer{
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
		if err == nil || err.Error() != "WebRTC peer failed" {
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

func TestPeerDisconnectedKeepsProtocolChannelRecoverable(t *testing.T) {
	channel := &lifecycleChannel{closed: make(chan struct{})}
	peer := &webRTCPeer{
		channel: channel, ready: make(chan struct{}), channelClosed: channel.closed,
		connectionFailed: make(chan error, 1), readyTimeout: time.Second,
	}
	channel.SetCloseHandler(func() {
		peer.channelClosedOnce.Do(func() { close(peer.channelClosed) })
	})
	close(peer.ready)

	peer.handleConnectionState(pionwebrtc.PeerConnectionStateDisconnected)

	if channel.closeCalls != 0 {
		t.Fatalf("recoverable disconnect closed protocol channel %d times", channel.closeCalls)
	}
	select {
	case err := <-peer.connectionFailed:
		t.Fatalf("recoverable disconnect reported final failure: %v", err)
	default:
	}
}

type lifecycleChannel struct {
	mu         sync.Mutex
	closed     chan struct{}
	closeOnce  sync.Once
	closeCalls int
	onClose    func()
}

type routeProbeConn struct{ local net.Addr }

func (*routeProbeConn) Read([]byte) (int, error)          { return 0, errors.New("unused") }
func (*routeProbeConn) Write(payload []byte) (int, error) { return len(payload), nil }
func (*routeProbeConn) Close() error                      { return nil }
func (connection *routeProbeConn) LocalAddr() net.Addr    { return connection.local }
func (*routeProbeConn) RemoteAddr() net.Addr              { return &net.UDPAddr{} }
func (*routeProbeConn) SetDeadline(time.Time) error       { return nil }
func (*routeProbeConn) SetReadDeadline(time.Time) error   { return nil }
func (*routeProbeConn) SetWriteDeadline(time.Time) error  { return nil }

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
