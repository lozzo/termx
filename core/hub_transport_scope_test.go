package core

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/wire"
	"github.com/lozzow/termx/shared/transport/datachannel"
)

func TestHubDataChannelTransportCannotEscapeCapabilityTerminalScope(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	registerScopedTestTerminal(t, server, "allowed")
	registerScopedTestTerminal(t, server, "denied")

	clientChannel, serverChannel := newHubTestChannelPair()
	clientTransport := datachannel.New(clientChannel)
	serverTransport := datachannel.New(serverChannel)
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ServeScopedTransport(context.Background(), serverTransport, TransportScope{TerminalID: "allowed"})
	}()
	client := protocol.NewClient(clientTransport)
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "hub-scope-test"}); err != nil {
		t.Fatalf("hello over hub data channel: %v", err)
	}
	defer func() {
		_ = client.Close()
		select {
		case err := <-errCh:
			if err != nil && !strings.Contains(err.Error(), "EOF") {
				t.Fatalf("serve hub transport: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("hub transport server did not stop")
		}
	}()

	var info protocol.TerminalInfo
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "allowed"}, &info); err != nil {
		t.Fatalf("get allowed terminal: %v", err)
	}
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "denied"}, &info); err == nil || !strings.Contains(err.Error(), "transport scope") {
		t.Fatalf("expected capability scope denial, got %v", err)
	}
}

type hubTestChannel struct {
	mu             sync.Mutex
	peer           *hubTestChannel
	messageHandler func([]byte)
	closeHandler   func()
	closed         bool
}

func newHubTestChannelPair() (*hubTestChannel, *hubTestChannel) {
	left := &hubTestChannel{}
	right := &hubTestChannel{}
	left.peer = right
	right.peer = left
	return left, right
}

func (channel *hubTestChannel) SetMessageHandler(handler func([]byte)) {
	channel.mu.Lock()
	channel.messageHandler = handler
	channel.mu.Unlock()
}

func (channel *hubTestChannel) SetCloseHandler(handler func()) {
	channel.mu.Lock()
	channel.closeHandler = handler
	channel.mu.Unlock()
}

func (channel *hubTestChannel) BufferedAmount() uint64 { return 0 }

func (channel *hubTestChannel) SetBufferedAmountLowThreshold(uint64) {}

func (channel *hubTestChannel) SetBufferedAmountLowHandler(func()) {}

func (channel *hubTestChannel) Send(payload []byte) error {
	channel.mu.Lock()
	if channel.closed {
		channel.mu.Unlock()
		return io.EOF
	}
	peer := channel.peer
	channel.mu.Unlock()
	peer.mu.Lock()
	handler := peer.messageHandler
	closed := peer.closed
	peer.mu.Unlock()
	if closed || handler == nil {
		return io.EOF
	}
	handler(append([]byte(nil), payload...))
	return nil
}

func (channel *hubTestChannel) Close() error {
	channel.mu.Lock()
	if channel.closed {
		channel.mu.Unlock()
		return nil
	}
	channel.closed = true
	localHandler := channel.closeHandler
	peer := channel.peer
	channel.mu.Unlock()
	if localHandler != nil {
		localHandler()
	}
	peer.mu.Lock()
	peer.closed = true
	peerHandler := peer.closeHandler
	peer.mu.Unlock()
	if peerHandler != nil {
		peerHandler()
	}
	return nil
}
