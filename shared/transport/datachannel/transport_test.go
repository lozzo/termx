package datachannel

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestTransportRoundTripsProtocolFrames(t *testing.T) {
	clientChannel, serverChannel := newFakeChannelPair()
	client := New(clientChannel)
	server := New(serverChannel)
	defer client.Close()
	defer server.Close()

	want := []byte("protocol-frame")
	if err := client.Send(want); err != nil {
		t.Fatalf("send protocol frame: %v", err)
	}
	got, err := server.Recv()
	if err != nil {
		t.Fatalf("recv protocol frame: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("protocol frame mismatch: got %q want %q", got, want)
	}
	want[0] = 'X'
	if bytes.Equal(got, want) {
		t.Fatal("transport must copy outbound protocol frames before handing them to the data channel")
	}
}

func TestTransportRemoteCloseUnblocksRecv(t *testing.T) {
	clientChannel, serverChannel := newFakeChannelPair()
	client := New(clientChannel)
	server := New(serverChannel)

	recvDone := make(chan error, 1)
	go func() {
		_, err := server.Recv()
		recvDone <- err
	}()
	if err := client.Close(); err != nil {
		t.Fatalf("close client transport: %v", err)
	}

	select {
	case err := <-recvDone:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("remote close should unblock recv with EOF, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("remote close did not unblock recv")
	}
	select {
	case <-server.Done():
	case <-time.After(time.Second):
		t.Fatal("remote close did not close Done")
	}
}

func TestTransportSendWaitsForBufferedAmountLow(t *testing.T) {
	clientChannel, serverChannel := newFakeChannelPair()
	clientChannel.setBufferedAmount(defaultSendBufferHigh + 1)
	client := New(clientChannel)
	server := New(serverChannel)
	defer client.Close()
	defer server.Close()

	sendDone := make(chan error, 1)
	go func() {
		sendDone <- client.Send([]byte("frame"))
	}()
	select {
	case err := <-sendDone:
		t.Fatalf("send should wait for buffered amount low, got %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	clientChannel.setBufferedAmount(defaultSendBufferLow)
	clientChannel.signalBufferedAmountLow()
	select {
	case err := <-sendDone:
		if err != nil {
			t.Fatalf("send after drain: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("buffer drain did not resume send")
	}
}

func TestTransportCloseUnblocksInFlightChannelSend(t *testing.T) {
	channel := newBlockingSendChannel()
	transport := New(channel)
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- transport.Send([]byte("blocked-auth-frame"))
	}()
	select {
	case <-channel.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("channel Send did not start")
	}
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- transport.Close()
	}()
	select {
	case err := <-sendDone:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("closed in-flight Send error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("transport Close did not unblock in-flight Channel.Send")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("transport Close error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("transport Close remained blocked on sendMu")
	}
}

func TestTransportCloseWaitsForInFlightChannelSendToExit(t *testing.T) {
	channel := newBlockingSendChannel()
	sendRelease := make(chan struct{})
	channel.sendRelease = sendRelease
	var releaseOnce sync.Once
	releaseSend := func() { releaseOnce.Do(func() { close(sendRelease) }) }
	t.Cleanup(releaseSend)

	transport := New(channel)
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- transport.Send([]byte("blocked-auth-frame"))
	}()
	select {
	case <-channel.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("channel Send did not start")
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- transport.Close()
	}()
	select {
	case <-channel.sendUnblocked:
	case <-time.After(time.Second):
		t.Fatal("channel Close did not unblock the in-flight Send")
	}
	select {
	case err := <-closeDone:
		t.Fatalf("transport Close returned before the in-flight Send exited: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	releaseSend()
	select {
	case err := <-sendDone:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("closed in-flight Send error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight Send did not exit after release")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("transport Close error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("transport Close did not return after the in-flight Send exited")
	}
}

func TestTransportDrainWaitsForQueuedOutboundMessages(t *testing.T) {
	clientChannel, serverChannel := newFakeChannelPair()
	clientChannel.setBufferedAmount(1)
	client := New(clientChannel)
	server := New(serverChannel)
	defer client.Close()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	drained := make(chan error, 1)
	go func() { drained <- client.Drain(ctx) }()
	select {
	case err := <-drained:
		t.Fatalf("drain returned before outbound buffer emptied: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	clientChannel.setBufferedAmount(0)
	select {
	case err := <-drained:
		if err != nil {
			t.Fatalf("drain queued outbound messages: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("drain did not observe empty outbound buffer")
	}
}

func TestTransportDrainHasInternalDeadlineWithoutCallerDeadline(t *testing.T) {
	channel, _ := newFakeChannelPair()
	channel.setBufferedAmount(1)
	transport := New(channel)
	defer transport.Close()
	transport.drainTimeout = 20 * time.Millisecond
	started := time.Now()
	if err := transport.Drain(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain deadline error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("drain ignored its internal deadline")
	}
}

type blockingSendChannel struct {
	mu            sync.Mutex
	closeHandler  func()
	sendOnce      sync.Once
	closeOnce     sync.Once
	sendStarted   chan struct{}
	sendUnblocked chan struct{}
	sendRelease   <-chan struct{}
	closed        chan struct{}
}

func newBlockingSendChannel() *blockingSendChannel {
	return &blockingSendChannel{
		sendStarted:   make(chan struct{}),
		sendUnblocked: make(chan struct{}),
		closed:        make(chan struct{}),
	}
}

func (*blockingSendChannel) SetMessageHandler(func([]byte)) {}

func (channel *blockingSendChannel) SetCloseHandler(handler func()) {
	channel.mu.Lock()
	channel.closeHandler = handler
	channel.mu.Unlock()
}

func (*blockingSendChannel) BufferedAmount() uint64 { return 0 }

func (*blockingSendChannel) SetBufferedAmountLowThreshold(uint64) {}

func (*blockingSendChannel) SetBufferedAmountLowHandler(func()) {}

func (channel *blockingSendChannel) Send([]byte) error {
	channel.sendOnce.Do(func() { close(channel.sendStarted) })
	<-channel.closed
	if channel.sendRelease != nil {
		close(channel.sendUnblocked)
		<-channel.sendRelease
	}
	return io.EOF
}

func (channel *blockingSendChannel) Close() error {
	channel.closeOnce.Do(func() {
		close(channel.closed)
		channel.mu.Lock()
		handler := channel.closeHandler
		channel.mu.Unlock()
		if handler != nil {
			handler()
		}
	})
	return nil
}

type fakeChannel struct {
	mu                sync.Mutex
	peer              *fakeChannel
	messageHandler    func([]byte)
	closeHandler      func()
	bufferedLowHandle func()
	bufferedAmount    uint64
	closed            bool
}

func newFakeChannelPair() (*fakeChannel, *fakeChannel) {
	left := &fakeChannel{}
	right := &fakeChannel{}
	left.peer = right
	right.peer = left
	return left, right
}

func (channel *fakeChannel) SetMessageHandler(handler func([]byte)) {
	channel.mu.Lock()
	channel.messageHandler = handler
	channel.mu.Unlock()
}

func (channel *fakeChannel) SetCloseHandler(handler func()) {
	channel.mu.Lock()
	channel.closeHandler = handler
	channel.mu.Unlock()
}

func (channel *fakeChannel) BufferedAmount() uint64 {
	channel.mu.Lock()
	defer channel.mu.Unlock()
	return channel.bufferedAmount
}

func (channel *fakeChannel) SetBufferedAmountLowThreshold(uint64) {}

func (channel *fakeChannel) SetBufferedAmountLowHandler(handler func()) {
	channel.mu.Lock()
	channel.bufferedLowHandle = handler
	channel.mu.Unlock()
}

func (channel *fakeChannel) Send(payload []byte) error {
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

func (channel *fakeChannel) Close() error {
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

func (channel *fakeChannel) setBufferedAmount(amount uint64) {
	channel.mu.Lock()
	channel.bufferedAmount = amount
	channel.mu.Unlock()
}

func (channel *fakeChannel) signalBufferedAmountLow() {
	channel.mu.Lock()
	handler := channel.bufferedLowHandle
	channel.mu.Unlock()
	if handler != nil {
		handler()
	}
}
