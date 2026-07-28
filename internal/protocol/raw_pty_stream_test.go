package protocol

import (
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/proto/wire"
)

func TestClientRawPTYQueueOverflowEmitsSyncLostAndCloses(t *testing.T) {
	stream := &clientStream{
		ch: make(chan StreamFrame, 4), done: make(chan struct{}), queueLimit: 2,
		queue: make([]StreamFrame, 0, 2),
	}
	stream.cond = sync.NewCond(&stream.mu)
	stream.send(StreamFrame{Type: wire.TypePTYOutput, Payload: []byte("one")})
	stream.send(StreamFrame{Type: wire.TypePTYOutput, Payload: []byte("two")})
	stream.send(StreamFrame{Type: wire.TypePTYOutput, Payload: []byte("lost")})
	go stream.run()

	frames := stream.channel()
	for _, want := range []string{"one", "two"} {
		select {
		case frame := <-frames:
			if frame.Type != wire.TypePTYOutput || string(frame.Payload) != want {
				t.Fatalf("raw frame = %#v, want %q", frame, want)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for raw PTY prefix")
		}
	}
	select {
	case frame := <-frames:
		if frame.Type != wire.TypeSyncLost {
			t.Fatalf("overflow frame type=%d want=%d", frame.Type, wire.TypeSyncLost)
		}
		dropped, err := wire.DecodeSyncLostPayload(frame.Payload)
		if err != nil || dropped != uint64(len("lost")) {
			t.Fatalf("sync lost dropped=%d err=%v", dropped, err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for explicit raw PTY sync loss")
	}
	select {
	case _, ok := <-frames:
		if ok {
			t.Fatal("raw PTY stream remained open after sync loss")
		}
	case <-time.After(time.Second):
		t.Fatal("raw PTY stream did not close after sync loss")
	}
}
