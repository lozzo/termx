package fanout

import (
	"context"
	"testing"
	"time"
)

func TestFanoutSyncLost(t *testing.T) {
	f := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slow := f.Subscribe(ctx)

	for i := 0; i < 257; i++ {
		f.Broadcast([]byte("x"))
	}

	for i := 0; i < 256; i++ {
		<-slow
	}

	f.Broadcast([]byte("third"))

	select {
	case msg := <-slow:
		if msg.Type != StreamSyncLost {
			t.Fatalf("expected sync lost, got %#v", msg)
		}
		if msg.DroppedBytes == 0 {
			t.Fatal("expected dropped bytes")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sync lost")
	}

	select {
	case msg := <-slow:
		if msg.Type != StreamOutput || string(msg.Output) != "third" {
			t.Fatalf("unexpected recovery output: %#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for output after sync lost")
	}
}

func TestFanoutClose(t *testing.T) {
	f := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := f.Subscribe(ctx)
	code := 7
	f.Close(&code)

	msg, ok := <-ch
	if !ok {
		t.Fatal("channel closed before closed message")
	}
	if msg.Type != StreamClosed {
		t.Fatalf("expected closed, got %#v", msg)
	}
	if msg.ExitCode == nil || *msg.ExitCode != 7 {
		t.Fatalf("unexpected exit code: %#v", msg.ExitCode)
	}

	if _, ok := <-ch; ok {
		t.Fatal("expected closed channel")
	}
}

func TestFanoutBroadcastSharesPayloadAcrossSubscribers(t *testing.T) {
	f := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	left := f.Subscribe(ctx)
	right := f.Subscribe(ctx)
	payload := []byte("shared")

	f.Broadcast(payload)

	leftMsg := <-left
	rightMsg := <-right
	if leftMsg.Type != StreamOutput || rightMsg.Type != StreamOutput {
		t.Fatalf("expected output messages, got left=%#v right=%#v", leftMsg, rightMsg)
	}
	if len(leftMsg.Output) == 0 || len(rightMsg.Output) == 0 {
		t.Fatalf("expected payload bytes, got left=%#v right=%#v", leftMsg.Output, rightMsg.Output)
	}
	if &leftMsg.Output[0] != &payload[0] {
		t.Fatal("expected first subscriber to receive the shared payload buffer")
	}
	if &rightMsg.Output[0] != &payload[0] {
		t.Fatal("expected second subscriber to receive the shared payload buffer")
	}
	if &leftMsg.Output[0] != &rightMsg.Output[0] {
		t.Fatal("expected subscribers to share the same payload buffer")
	}
}

func TestFanoutResizePreemptsBufferedOutputWhenSubscriberBackedUp(t *testing.T) {
	f := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := f.Subscribe(ctx)
	for i := 0; i < 256; i++ {
		f.Broadcast([]byte("x"))
	}

	f.BroadcastResize(120, 40)

	sawResize := false
	timeout := time.After(time.Second)
	for !sawResize {
		select {
		case msg := <-ch:
			if msg.Type == StreamResize {
				sawResize = true
				if msg.Cols != 120 || msg.Rows != 40 {
					t.Fatalf("unexpected resize %#v", msg)
				}
			}
		case <-timeout:
			t.Fatal("timed out waiting for priority resize frame")
		}
	}
}

func TestFanoutScreenUpdatePreemptsBufferedOutputWhenSubscriberBackedUp(t *testing.T) {
	f := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := f.Subscribe(ctx)
	for i := 0; i < 256; i++ {
		f.Broadcast([]byte("x"))
	}

	payload := []byte(`{"full_replace":true}`)
	f.BroadcastMessage(StreamMessage{Type: StreamScreenUpdate, Payload: payload})

	sawUpdate := false
	timeout := time.After(time.Second)
	for !sawUpdate {
		select {
		case msg := <-ch:
			if msg.Type == StreamScreenUpdate {
				sawUpdate = true
				if string(msg.Payload) != string(payload) {
					t.Fatalf("unexpected screen update %#v", msg)
				}
			}
		case <-timeout:
			t.Fatal("timed out waiting for priority screen update frame")
		}
	}
}
