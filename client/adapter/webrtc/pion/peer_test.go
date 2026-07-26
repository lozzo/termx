package pion

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	pionwebrtc "github.com/pion/webrtc/v4"
)

func TestWaitReadyReturnsWhenChannelClosesBeforeOpen(t *testing.T) {
	peer := &webRTCPeer{ready: make(chan struct{}), channelClosed: make(chan struct{}), readyTimeout: time.Second}
	close(peer.channelClosed)
	if err := peer.WaitReady(context.Background()); err == nil {
		t.Fatal("closed DataChannel was reported ready")
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
