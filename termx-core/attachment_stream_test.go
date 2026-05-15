package termx

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-proto/wire"

	"github.com/lozzow/termx/internal/protocol"
)

func TestAttachmentStreamPumpReadyGatesScreenUpdatesToLatestFrame(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := make(chan StreamMessage, 16)
	sent := make(chan protocol.StreamFrame, 16)
	latestSeq := 0
	pump := newAttachmentStreamPump(
		ctx,
		cancel,
		"term-1",
		7,
		"test",
		src,
		func(*streamScreenState) StreamMessage {
			latestSeq++
			return StreamMessage{Type: StreamScreenUpdate, Payload: testAttachmentScreenUpdatePayload(t, fmt.Sprintf("frame-%d", latestSeq))}
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
	if first.Type != wire.TypeScreenUpdate || !testAttachmentScreenUpdateContains(t, first.Payload, "frame-1") {
		t.Fatalf("unexpected first frame: %#v", first)
	}

	for i := 0; i < 5; i++ {
		src <- StreamMessage{Type: StreamScreenUpdate}
	}
	assertNoSentFrame(t, sent, 75*time.Millisecond)

	pump.screenReady()
	second := waitSentFrame(t, sent)
	if second.Type != wire.TypeScreenUpdate || !testAttachmentScreenUpdateContains(t, second.Payload, "frame-2") {
		t.Fatalf("unexpected second frame: %#v", second)
	}
	assertNoSentFrame(t, sent, 75*time.Millisecond)
}

func TestAttachmentStreamPumpCoalescesReadyUpdatesWhileFrameInFlight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := make(chan StreamMessage, 16)
	sent := make(chan protocol.StreamFrame, 16)
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
		func(*streamScreenState) StreamMessage {
			latestSeq++
			return StreamMessage{Type: StreamScreenUpdate, Payload: testAttachmentScreenUpdatePayload(t, fmt.Sprintf("frame-%d", latestSeq))}
		},
		func(channel uint16, typ uint8, payload []byte) error {
			if typ == wire.TypeScreenUpdate {
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
	if first.Type != wire.TypeScreenUpdate || !testAttachmentScreenUpdateContains(t, first.Payload, "frame-1") {
		t.Fatalf("unexpected first frame: %#v", first)
	}
	assertNoSentFrame(t, sent, 75*time.Millisecond)

	pump.screenReady()
	releaseSend <- struct{}{}
	second := waitSentFrame(t, sent)
	if second.Type != wire.TypeScreenUpdate || !testAttachmentScreenUpdateContains(t, second.Payload, "frame-2") {
		t.Fatalf("unexpected second frame: %#v", second)
	}
	assertNoSentFrame(t, sent, 75*time.Millisecond)
}

func TestAttachmentStreamPumpCoalescesPayloadDeltasWhileFrameInFlight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := make(chan StreamMessage, 16)
	sent := make(chan protocol.StreamFrame, 16)
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
		func(*streamScreenState) StreamMessage {
			latestSeq++
			return StreamMessage{Type: StreamScreenUpdate, Payload: testAttachmentScreenUpdatePayload(t, fmt.Sprintf("latest-%d", latestSeq))}
		},
		func(channel uint16, typ uint8, payload []byte) error {
			if typ == wire.TypeScreenUpdate {
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
	src <- StreamMessage{Type: StreamScreenUpdate, Payload: testAttachmentScreenUpdatePayload(t, "delta-1")}
	select {
	case <-sendStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first send")
	}
	for i := 2; i <= 8; i++ {
		src <- StreamMessage{Type: StreamScreenUpdate, Payload: testAttachmentScreenUpdatePayload(t, fmt.Sprintf("delta-%d", i))}
	}
	assertNoSentFrame(t, sent, 75*time.Millisecond)

	releaseSend <- struct{}{}
	first := waitSentFrame(t, sent)
	if first.Type != wire.TypeScreenUpdate || !testAttachmentScreenUpdateContains(t, first.Payload, "delta-1") {
		t.Fatalf("unexpected first frame: %#v", first)
	}
	assertNoSentFrame(t, sent, 75*time.Millisecond)

	pump.screenReady()
	releaseSend <- struct{}{}
	second := waitSentFrame(t, sent)
	if second.Type != wire.TypeScreenUpdate || !testAttachmentScreenUpdateContains(t, second.Payload, "latest-1") {
		t.Fatalf("expected coalesced payload deltas to send latest snapshot, got %#v", second)
	}
	assertNoSentFrame(t, sent, 75*time.Millisecond)
}

func testAttachmentScreenUpdatePayload(t *testing.T, text string) []byte {
	t.Helper()
	payload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		FullReplace: true,
		Size:        protocol.Size{Cols: 16, Rows: 1},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: text, Width: 1}}},
		},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode screen update: %v", err)
	}
	return payload
}

func testAttachmentScreenUpdateContains(t *testing.T, payload []byte, needle string) bool {
	t.Helper()
	update, err := protocol.DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode screen update: %v", err)
	}
	return screenUpdateContainsText(update, needle)
}

func TestAttachmentStreamPumpLatestDeltaReceivesSentScreenState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := make(chan StreamMessage, 16)
	sent := make(chan protocol.StreamFrame, 16)
	var baselineRows int
	pump := newAttachmentStreamPump(
		ctx,
		cancel,
		"term-1",
		7,
		"test",
		src,
		func(before *streamScreenState) StreamMessage {
			if before != nil && before.snapshot != nil {
				baselineRows = len(before.snapshot.Screen.Cells)
			}
			return StreamMessage{Type: StreamScreenUpdate, Payload: testAttachmentScreenUpdatePayload(t, "latest")}
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
	src <- StreamMessage{Type: StreamScreenUpdate, Payload: testAttachmentScreenUpdatePayload(t, "seed")}
	first := waitSentFrame(t, sent)
	if first.Type != wire.TypeScreenUpdate || !testAttachmentScreenUpdateContains(t, first.Payload, "seed") {
		t.Fatalf("unexpected first frame: %#v", first)
	}

	src <- StreamMessage{Type: StreamScreenUpdate}
	assertNoSentFrame(t, sent, 75*time.Millisecond)
	pump.screenReady()
	second := waitSentFrame(t, sent)
	if second.Type != wire.TypeScreenUpdate || !testAttachmentScreenUpdateContains(t, second.Payload, "latest") {
		t.Fatalf("unexpected latest frame: %#v", second)
	}
	if baselineRows != 1 {
		t.Fatalf("expected latest fallback to receive sent baseline state, got rows=%d", baselineRows)
	}
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
