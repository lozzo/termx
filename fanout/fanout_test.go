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
