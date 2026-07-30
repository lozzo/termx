package protocol

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/anytty/anytty/proto/apipb"
	"github.com/anytty/anytty/proto/wire"
	"github.com/anytty/anytty/shared/transport/memory"
	"google.golang.org/protobuf/proto"
)

func TestApplicationEventsReplaysCapacityDuringConcurrentPublication(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	client := NewClient(clientTransport)
	defer client.Close()
	defer serverTransport.Close()
	completeClientHello(t, client, serverTransport)

	client.mu.Lock()
	for index := range 64 {
		client.pendingApplicationEvents = append(client.pendingApplicationEvents, reliabilityEvent(fmt.Sprintf("pending-%d", index)))
	}
	client.mu.Unlock()

	published := make(chan error, 1)
	go func() {
		for {
			client.mu.Lock()
			registered := len(client.applicationEventSubscribers) > 0
			client.mu.Unlock()
			if registered {
				break
			}
			time.Sleep(time.Millisecond)
		}
		for index := range 64 {
			if err := client.publishApplicationEvent(reliabilityEvent(fmt.Sprintf("concurrent-%d", index))); err != nil {
				published <- err
				return
			}
		}
		published <- nil
	}()
	events, err := client.ApplicationEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-published:
		if err != nil {
			t.Fatalf("concurrent publication failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent publication blocked during event registration")
	}
	for index := range 64 {
		select {
		case event := <-events:
			if event == nil {
				t.Fatalf("replayed event %d is nil", index)
			}
		case <-time.After(time.Second):
			t.Fatalf("event replay blocked at capacity index %d", index)
		}
	}
}

func TestClientApplicationEventOverflowClosesMaliciousPeer(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	client := NewClient(clientTransport)
	defer client.Close()
	defer serverTransport.Close()
	completeClientHello(t, client, serverTransport)
	for index := range 65 {
		payload, err := proto.Marshal(reliabilityEvent(fmt.Sprintf("event-%d", index)))
		if err != nil {
			t.Fatal(err)
		}
		if err := sendTestFrame(serverTransport, 0, wire.TypeEvent, payload); err != nil {
			t.Fatal(err)
		}
	}
	assertPeerFailure(t, client, ProtocolErrorCodeResourceExhausted)
}

func TestClientPendingStreamBoundsCloseMaliciousPeer(t *testing.T) {
	tests := []struct {
		name   string
		limits clientPendingLimits
		frames []pendingTestFrame
	}{
		{
			name:   "channels",
			limits: clientPendingLimits{channels: 2, frames: 8, framesPerChannel: 8, bytes: 64},
			frames: []pendingTestFrame{{channel: 1, payload: []byte("a")}, {channel: 2, payload: []byte("b")}, {channel: 3, payload: []byte("c")}},
		},
		{
			name:   "frames",
			limits: clientPendingLimits{channels: 2, frames: 2, framesPerChannel: 2, bytes: 64},
			frames: []pendingTestFrame{{channel: 1, payload: []byte("a")}, {channel: 1, payload: []byte("b")}, {channel: 1, payload: []byte("c")}},
		},
		{
			name:   "bytes",
			limits: clientPendingLimits{channels: 2, frames: 8, framesPerChannel: 8, bytes: 3},
			frames: []pendingTestFrame{{channel: 1, payload: []byte("ab")}, {channel: 1, payload: []byte("cd")}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clientTransport, serverTransport := memory.NewPair()
			client := newClientWithPendingLimits(clientTransport, test.limits)
			defer client.Close()
			defer serverTransport.Close()
			completeClientHello(t, client, serverTransport)
			for _, frame := range test.frames {
				if err := sendTestFrame(serverTransport, frame.channel, wire.TypeFileData, frame.payload); err != nil {
					t.Fatal(err)
				}
			}
			assertPeerFailure(t, client, ProtocolErrorCodeResourceExhausted)
		})
	}
}

func TestClientRejectsDuplicateAndMalformedControlFrames(t *testing.T) {
	t.Run("duplicate hello", func(t *testing.T) {
		clientTransport, serverTransport := memory.NewPair()
		client := NewClient(clientTransport)
		defer client.Close()
		defer serverTransport.Close()
		completeClientHello(t, client, serverTransport)
		hello, err := EncodeHelloPayload(Hello{Version: wire.Version, Server: "duplicate"})
		if err != nil {
			t.Fatal(err)
		}
		if err := sendTestFrame(serverTransport, 0, wire.TypeHello, hello); err != nil {
			t.Fatal(err)
		}
		assertPeerFailure(t, client, ProtocolErrorCodeBadRequest)
	})

	t.Run("duplicate response", func(t *testing.T) {
		clientTransport, serverTransport := memory.NewPair()
		client := NewClient(clientTransport)
		defer client.Close()
		defer serverTransport.Close()
		completeClientHello(t, client, serverTransport)
		client.mu.Lock()
		client.waiters[7] = &responseWaiter{ch: make(chan result, 1)}
		client.mu.Unlock()
		payload, err := EncodeResponsePayload(Response{ID: 7, Result: []byte("ok")})
		if err != nil {
			t.Fatal(err)
		}
		if err := sendTestFrame(serverTransport, 0, wire.TypeResponse, payload); err != nil {
			t.Fatal(err)
		}
		if err := sendTestFrame(serverTransport, 0, wire.TypeResponse, payload); err != nil {
			t.Fatal(err)
		}
		assertPeerFailure(t, client, ProtocolErrorCodeBadRequest)
	})

	t.Run("malformed response", func(t *testing.T) {
		clientTransport, serverTransport := memory.NewPair()
		client := NewClient(clientTransport)
		defer client.Close()
		defer serverTransport.Close()
		completeClientHello(t, client, serverTransport)
		if err := sendTestFrame(serverTransport, 0, wire.TypeResponse, []byte{0xff}); err != nil {
			t.Fatal(err)
		}
		assertPeerFailure(t, client, ProtocolErrorCodeBadRequest)
	})
}

func TestClientStreamOverflowClosesAffectedStreamWithTypedError(t *testing.T) {
	stream := newClientStreamWithConfig(1, 0)
	frames := stream.channel()
	stream.send(StreamFrame{Type: wire.TypeFileData, Payload: []byte("first")})
	waitForStreamQueueLength(t, stream, 0)
	stream.send(StreamFrame{Type: wire.TypeFileData, Payload: []byte("second")})
	stream.send(StreamFrame{Type: wire.TypeFileData, Payload: []byte("overflow")})

	if first := receiveReliabilityStreamFrame(t, frames); first.Type != wire.TypeFileData || string(first.Payload) != "first" {
		t.Fatalf("first stream frame = %#v", first)
	}
	failure := receiveReliabilityStreamFrame(t, frames)
	if failure.Type != wire.TypeError {
		t.Fatalf("overflow frame type = %d", failure.Type)
	}
	message, err := DecodeErrorPayload(failure.Payload)
	if err != nil || message.Error.Code != ProtocolErrorCodeResourceExhausted {
		t.Fatalf("overflow error = %#v, %v", message, err)
	}
	select {
	case _, ok := <-frames:
		if ok {
			t.Fatal("affected stream remained open after overflow")
		}
	case <-time.After(time.Second):
		t.Fatal("affected stream did not close after overflow")
	}
}

type pendingTestFrame struct {
	channel uint16
	payload []byte
}

func completeClientHello(t *testing.T, client *Client, serverTransport *memory.Transport) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- client.Hello(context.Background(), Hello{Version: wire.Version, Client: "reliability-test"})
	}()
	if err := expectHelloAndRespond(serverTransport); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("client Hello did not complete")
	}
}

func reliabilityEvent(id string) *apipb.EventEnvelope {
	return &apipb.EventEnvelope{
		EventId: id,
		Event: &apipb.EventEnvelope_TerminalLifecycle{TerminalLifecycle: &apipb.TerminalLifecycleEvent{
			Terminal: &apipb.TerminalInfo{Ref: &apipb.TerminalRef{EndpointId: "local", TerminalId: id}},
		}},
	}
}

func assertPeerFailure(t *testing.T, client *Client, code int) {
	t.Helper()
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Fatal("malicious peer did not close")
	}
	var peerErr *PeerError
	if err := client.Err(); !errors.As(err, &peerErr) || peerErr.Code != code {
		t.Fatalf("client error = %T %v, want peer code %d", err, err, code)
	}
}

func waitForStreamQueueLength(t *testing.T, stream *clientStream, length int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stream.mu.Lock()
		got := len(stream.queue)
		stream.mu.Unlock()
		if got == length {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("stream queue did not reach length %d", length)
}

func receiveReliabilityStreamFrame(t *testing.T, frames <-chan StreamFrame) StreamFrame {
	t.Helper()
	select {
	case frame, ok := <-frames:
		if !ok {
			t.Fatal("stream closed before expected frame")
		}
		return frame
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream frame")
		return StreamFrame{}
	}
}
