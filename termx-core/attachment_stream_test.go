package termx

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
)

func TestAttachmentStreamPumpReadyGatesScreenUpdatesToLatestFrame(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := make(chan StreamMessage, 16)
	sent := make(chan protocol.StreamFrame, 16)
	var latestMu sync.Mutex
	latestSeq := 0
	pump := newAttachmentStreamPump(
		ctx,
		cancel,
		"term-1",
		7,
		"test",
		src,
		func() StreamMessage {
			latestMu.Lock()
			defer latestMu.Unlock()
			latestSeq++
			return StreamMessage{Type: StreamScreenUpdate, Payload: []byte(fmt.Sprintf("frame-%d", latestSeq))}
		},
		func(channel uint16, typ uint8, payload []byte) error {
			sent <- protocol.StreamFrame{Type: typ, Payload: append([]byte(nil), payload...)}
			return nil
		},
		nil,
	)

	done := make(chan error, 1)
	go func() { done <- pump.run() }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("pump returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for pump shutdown")
		}
	}()

	pump.screenReady()
	src <- StreamMessage{Type: StreamScreenUpdate}
	first := waitSentFrame(t, sent)
	if first.Type != protocol.TypeScreenUpdate || string(first.Payload) != "frame-1" {
		t.Fatalf("unexpected first frame: %#v", first)
	}

	for i := 0; i < 5; i++ {
		src <- StreamMessage{Type: StreamScreenUpdate}
	}
	assertNoSentFrame(t, sent, 75*time.Millisecond)

	pump.screenReady()
	second := waitSentFrame(t, sent)
	if second.Type != protocol.TypeScreenUpdate || string(second.Payload) != "frame-2" {
		t.Fatalf("unexpected second frame: %#v", second)
	}
	assertNoSentFrame(t, sent, 75*time.Millisecond)
}

func TestAttachmentStreamPumpCoalescesReadyUpdatesWhileFrameInFlight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := make(chan StreamMessage, 16)
	sent := make(chan protocol.StreamFrame, 16)
	var latestMu sync.Mutex
	latestSeq := 0
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	pump := newAttachmentStreamPump(
		ctx,
		cancel,
		"term-1",
		7,
		"test",
		src,
		func() StreamMessage {
			latestMu.Lock()
			defer latestMu.Unlock()
			latestSeq++
			return StreamMessage{Type: StreamScreenUpdate, Payload: []byte(fmt.Sprintf("frame-%d", latestSeq))}
		},
		func(channel uint16, typ uint8, payload []byte) error {
			if typ == protocol.TypeScreenUpdate {
				select {
				case sendStarted <- struct{}{}:
				default:
				}
				<-releaseSend
			}
			sent <- protocol.StreamFrame{Type: typ, Payload: append([]byte(nil), payload...)}
			return nil
		},
		nil,
	)

	done := make(chan error, 1)
	go func() { done <- pump.run() }()
	defer func() {
		cancel()
		close(releaseSend)
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("pump returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for pump shutdown")
		}
	}()

	pump.screenReady()
	src <- StreamMessage{Type: StreamScreenUpdate}
	select {
	case <-sendStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first send")
	}
	for i := 0; i < 8; i++ {
		src <- StreamMessage{Type: StreamScreenUpdate}
	}
	assertNoSentFrame(t, sent, 75*time.Millisecond)

	releaseSend <- struct{}{}
	first := waitSentFrame(t, sent)
	if first.Type != protocol.TypeScreenUpdate || string(first.Payload) != "frame-1" {
		t.Fatalf("unexpected first frame: %#v", first)
	}
	assertNoSentFrame(t, sent, 75*time.Millisecond)

	pump.screenReady()
	releaseSend <- struct{}{}
	second := waitSentFrame(t, sent)
	if second.Type != protocol.TypeScreenUpdate || string(second.Payload) != "frame-2" {
		t.Fatalf("unexpected second frame: %#v", second)
	}
	assertNoSentFrame(t, sent, 75*time.Millisecond)
}

func waitSentFrame(t *testing.T, frames <-chan protocol.StreamFrame) protocol.StreamFrame {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sent frame")
		return protocol.StreamFrame{}
	}
}

func assertNoSentFrame(t *testing.T, frames <-chan protocol.StreamFrame, timeout time.Duration) {
	t.Helper()
	select {
	case frame := <-frames:
		t.Fatalf("unexpected sent frame: %#v", frame)
	case <-time.After(timeout):
	}
}
