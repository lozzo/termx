package webrtc

import (
	"bytes"
	"io"
	"sync"
	"testing"
)

type fakeChannel struct {
	incoming <-chan []byte
	outgoing chan<- []byte
	done     chan struct{}
	once     sync.Once
}

func newFakeChannelPair() (*fakeChannel, *fakeChannel) {
	aToB := make(chan []byte, 256)
	bToA := make(chan []byte, 256)
	aDone := make(chan struct{})
	bDone := make(chan struct{})

	a := &fakeChannel{incoming: bToA, outgoing: aToB, done: aDone}
	b := &fakeChannel{incoming: aToB, outgoing: bToA, done: bDone}
	return a, b
}

func (c *fakeChannel) Send(msg []byte) error {
	select {
	case <-c.done:
		return io.EOF
	default:
	}
	data := append([]byte(nil), msg...)
	select {
	case <-c.done:
		return io.EOF
	case c.outgoing <- data:
		return nil
	}
}

func (c *fakeChannel) Recv() ([]byte, error) {
	select {
	case <-c.done:
		return nil, io.EOF
	case msg, ok := <-c.incoming:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	}
}

func (c *fakeChannel) Close() error {
	c.once.Do(func() {
		close(c.done)
		close(c.outgoing)
	})
	return nil
}

func (c *fakeChannel) Done() <-chan struct{} {
	return c.done
}

func TestTransportRoundTripSmallFrame(t *testing.T) {
	leftRaw, rightRaw := newFakeChannelPair()
	left := NewTransport(leftRaw)
	right := NewTransport(rightRaw)
	defer left.Close()
	defer right.Close()

	payload := []byte("hello over data channel")
	if err := left.Send(payload); err != nil {
		t.Fatalf("left send failed: %v", err)
	}
	got, err := right.Recv()
	if err != nil {
		t.Fatalf("right recv failed: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected payload: got %q want %q", string(got), string(payload))
	}
}

func TestTransportReassemblesFragmentedFrame(t *testing.T) {
	leftRaw, rightRaw := newFakeChannelPair()
	left := NewTransport(leftRaw)
	right := NewTransport(rightRaw)
	defer left.Close()
	defer right.Close()

	payload := bytes.Repeat([]byte("abcdefghij"), maxMessagePayloadBytes/5)
	payload = append(payload, bytes.Repeat([]byte("z"), 137)...)
	if len(payload) <= maxMessagePayloadBytes {
		t.Fatalf("expected payload larger than one message chunk, got %d", len(payload))
	}

	if err := left.Send(payload); err != nil {
		t.Fatalf("left send failed: %v", err)
	}
	got, err := right.Recv()
	if err != nil {
		t.Fatalf("right recv failed: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected reassembled payload length: got %d want %d", len(got), len(payload))
	}
}

func TestTransportRejectsUnexpectedFragmentContinuation(t *testing.T) {
	leftRaw, rightRaw := newFakeChannelPair()
	left := NewTransport(leftRaw)
	right := NewTransport(rightRaw)
	defer left.Close()
	defer right.Close()

	if err := leftRaw.Send(append([]byte{packetKindFragmentContinue}, []byte("oops")...)); err != nil {
		t.Fatalf("inject fragment failed: %v", err)
	}

	if _, err := right.Recv(); err == nil {
		t.Fatal("expected malformed fragment sequence error")
	}
}
