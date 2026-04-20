package termx

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lozzow/termx/fanout"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/transport"
	"github.com/lozzow/termx/transport/memory"
	"github.com/lozzow/termx/vterm"
)

type slowSendTransport struct {
	inner     transport.Transport
	sendDelay time.Duration
	sentBytes atomic.Int64
}

func (t *slowSendTransport) Send(frame []byte) error {
	if t.sendDelay > 0 {
		time.Sleep(t.sendDelay)
	}
	t.sentBytes.Add(int64(len(frame)))
	return t.inner.Send(frame)
}

func (t *slowSendTransport) Recv() ([]byte, error) { return t.inner.Recv() }
func (t *slowSendTransport) Close() error          { return t.inner.Close() }
func (t *slowSendTransport) Done() <-chan struct{} { return t.inner.Done() }

type slowStreamScenario struct {
	name        string
	setup       func(*testing.T) *Terminal
	inputs      [][]byte
	expectFewer bool
}

type slowStreamResult struct {
	wireBytes          int
	screenUpdateFrames int
	state              *streamScreenState
	metrics            perftrace.Snapshot
}

func TestTransportSlowConsumerAltScreenCoalescesToLatestState(t *testing.T) {
	scenario := slowStreamScenario{
		name: "alt_screen_churn",
		setup: func(t *testing.T) *Terminal {
			t.Helper()
			vt := vterm.New(60, 18, 512, nil)
			vt.LoadSnapshot(
				benchmarkFilledScreen(60, 18, "seed"),
				vterm.CursorState{Row: 0, Col: 0, Visible: true},
				vterm.TerminalModes{AlternateScreen: true, AutoWrap: true},
			)
			return newSyntheticStreamTerminal("term-alt", vt, "alt-demo")
		},
		inputs: func() [][]byte {
			out := make([][]byte, 0, 10)
			for _, fill := range []byte{'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J'} {
				out = append(out, benchmarkFullScreenPayload(60, 18, fill))
			}
			return out
		}(),
		expectFewer: true,
	}
	fast := runSlowStreamScenario(t, scenario, 0)
	slow := runSlowStreamScenario(t, scenario, 18*time.Millisecond)
	assertSlowScenarioResult(t, scenario, fast, slow)
}

func TestTransportSlowConsumerNormalScreenPreservesScrollback(t *testing.T) {
	scenario := slowStreamScenario{
		name: "normal_screen_scroll",
		setup: func(t *testing.T) *Terminal {
			t.Helper()
			vt := vterm.New(28, 5, 512, nil)
			vt.LoadSnapshot(
				benchmarkFilledScreen(28, 5, "log"),
				vterm.CursorState{Row: 4, Col: 0, Visible: true},
				vterm.TerminalModes{AutoWrap: true},
			)
			return newSyntheticStreamTerminal("term-normal", vt, "log-demo")
		},
		inputs: func() [][]byte {
			out := make([][]byte, 0, 14)
			for i := 0; i < 14; i++ {
				out = append(out, []byte(fmt.Sprintf("entry-%02d\r\n", i)))
			}
			return out
		}(),
		expectFewer: true,
	}
	fast := runSlowStreamScenario(t, scenario, 0)
	slow := runSlowStreamScenario(t, scenario, 18*time.Millisecond)
	assertSlowScenarioResult(t, scenario, fast, slow)
	if slow.state == nil || slow.state.snapshot == nil {
		t.Fatal("expected slow-path state snapshot")
	}
	for i := 0; i < 14; i++ {
		needle := fmt.Sprintf("entry-%02d", i)
		if !protocolSnapshotContains(slow.state.snapshot, needle) {
			t.Fatalf("slow-path snapshot lost scrollback row %q", needle)
		}
	}
}

func runSlowStreamScenario(t *testing.T, scenario slowStreamScenario, sendDelay time.Duration) slowStreamResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	recorder := perftrace.Enable()
	defer func() {
		perftrace.Disable()
		if recorder != nil {
			recorder.Reset()
		}
	}()

	term := scenario.setup(t)
	expected := cloneStreamScreenState(term.currentStreamScreenStateLocked())
	if expected == nil || expected.snapshot == nil {
		t.Fatal("expected initial terminal snapshot")
	}

	srv := NewServer()
	srv.terminals[term.id] = term

	clientTransport, serverTransportBase := memory.NewPair()
	defer clientTransport.Close()
	serverTransport := &slowSendTransport{inner: serverTransportBase, sendDelay: sendDelay}
	defer serverTransport.Close()

	go func() {
		_ = srv.handleTransport(ctx, serverTransport, scenario.name)
	}()

	client := protocol.NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "slow-test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}
	attach, err := client.Attach(ctx, term.id, string(ModeCollaborator))
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	stream, stop := client.Stream(attach.Channel)
	defer stop()

	for _, input := range scenario.inputs {
		if err := writeSyntheticTerminalUpdate(term, input); err != nil {
			t.Fatalf("broadcast update: %v", err)
		}
		expected = cloneStreamScreenState(term.currentStreamScreenStateLocked())
	}
	term.stream.Close(nil)

	result := slowStreamResult{
		state: &streamScreenState{snapshot: nil},
	}
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for stream completion: frames=%d bytes=%d", result.screenUpdateFrames, result.wireBytes)
		case frame, ok := <-stream:
			if !ok {
				t.Fatal("stream closed before closed frame")
			}
			switch frame.Type {
			case protocol.TypeScreenUpdate:
				result.screenUpdateFrames++
				update, err := protocol.DecodeScreenUpdatePayload(frame.Payload)
				if err != nil {
					t.Fatalf("decode screen update: %v", err)
				}
				result.state = applyStreamScreenUpdateState(result.state, term.id, update)
			case protocol.TypeResize:
				cols, rows, err := protocol.DecodeResizePayload(frame.Payload)
				if err != nil {
					t.Fatalf("decode resize: %v", err)
				}
				result.state = resizeStreamScreenState(result.state, term.id, cols, rows)
			case protocol.TypeClosed:
				result.metrics = perftrace.SnapshotCurrent()
				assertStreamStateMatchesExpected(t, expected, result.state)
				assertWireMetricsPresent(t, result.metrics)
				result.wireBytes = int(serverTransport.sentBytes.Load())
				return result
			}
		}
	}
}

func assertSlowScenarioResult(t *testing.T, scenario slowStreamScenario, fast, slow slowStreamResult) {
	t.Helper()
	if scenario.expectFewer && slow.screenUpdateFrames >= fast.screenUpdateFrames {
		t.Fatalf("%s: expected fewer screen updates on slow link, fast=%d slow=%d", scenario.name, fast.screenUpdateFrames, slow.screenUpdateFrames)
	}
	if slow.wireBytes >= fast.wireBytes {
		t.Fatalf("%s: expected fewer wire bytes on slow link, fast=%d slow=%d", scenario.name, fast.wireBytes, slow.wireBytes)
	}
	t.Logf("%s fast_bytes=%d slow_bytes=%d fast_screen_updates=%d slow_screen_updates=%d",
		scenario.name,
		fast.wireBytes,
		slow.wireBytes,
		fast.screenUpdateFrames,
		slow.screenUpdateFrames,
	)
}

func assertStreamStateMatchesExpected(t *testing.T, expected, got *streamScreenState) {
	t.Helper()
	if expected == nil || expected.snapshot == nil {
		t.Fatal("expected snapshot missing")
	}
	if got == nil || got.snapshot == nil {
		t.Fatal("stream snapshot missing")
	}
	if got.title != expected.title {
		t.Fatalf("unexpected title: got=%q want=%q", got.title, expected.title)
	}
	assertProtocolSnapshotsEqual(t, expected.snapshot, got.snapshot)
}

func assertProtocolSnapshotsEqual(t *testing.T, want, got *protocol.Snapshot) {
	t.Helper()
	if want == nil || got == nil {
		t.Fatalf("snapshot mismatch: want=%#v got=%#v", want, got)
	}
	if want.Size != got.Size {
		t.Fatalf("snapshot size mismatch: want=%#v got=%#v", want.Size, got.Size)
	}
	if !protocolCursorStatesEquivalent(want.Cursor, got.Cursor) {
		t.Fatalf("snapshot cursor mismatch: want=%#v got=%#v", want.Cursor, got.Cursor)
	}
	if want.Modes != got.Modes {
		t.Fatalf("snapshot modes mismatch: want=%#v got=%#v", want.Modes, got.Modes)
	}
	if len(want.Scrollback) != len(got.Scrollback) {
		t.Fatalf("snapshot scrollback length mismatch: want=%d got=%d", len(want.Scrollback), len(got.Scrollback))
	}
	for i := range want.Scrollback {
		if protocolRowToString(want.Scrollback[i]) != protocolRowToString(got.Scrollback[i]) {
			t.Fatalf("snapshot scrollback row mismatch at %d: want=%q got=%q", i, protocolRowToString(want.Scrollback[i]), protocolRowToString(got.Scrollback[i]))
		}
	}
	if len(want.Screen.Cells) != len(got.Screen.Cells) {
		t.Fatalf("snapshot screen rows mismatch: want=%d got=%d", len(want.Screen.Cells), len(got.Screen.Cells))
	}
	for i := range want.Screen.Cells {
		if protocolRowToString(want.Screen.Cells[i]) != protocolRowToString(got.Screen.Cells[i]) {
			t.Fatalf("snapshot screen row mismatch at %d: want=%q got=%q", i, protocolRowToString(want.Screen.Cells[i]), protocolRowToString(got.Screen.Cells[i]))
		}
	}
}

func protocolCursorStatesEquivalent(want, got protocol.CursorState) bool {
	if want.Row != got.Row || want.Col != got.Col || want.Visible != got.Visible || want.Blink != got.Blink {
		return false
	}
	if want.Shape == got.Shape {
		return true
	}
	return (want.Shape == "" && got.Shape == "block") || (want.Shape == "block" && got.Shape == "")
}

func assertWireMetricsPresent(t *testing.T, snapshot perftrace.Snapshot) {
	t.Helper()
	if event, ok := snapshot.Event("transport.bytes_over_wire"); !ok || event.Bytes == 0 {
		t.Fatalf("expected transport.bytes_over_wire metric, got %#v", event)
	}
	if event, ok := snapshot.Event("transport.stream.bytes_over_wire"); !ok || event.Bytes == 0 {
		t.Fatalf("expected transport.stream.bytes_over_wire metric, got %#v", event)
	}
}

func newSyntheticStreamTerminal(id string, vt *vterm.VTerm, title string) *Terminal {
	cols, rows := vt.Size()
	return &Terminal{
		id:          id,
		size:        Size{Cols: uint16(cols), Rows: uint16(rows)},
		state:       StateRunning,
		title:       title,
		vterm:       vt,
		stream:      fanout.New(),
		attachments: make(map[string]AttachInfo),
		done:        make(chan struct{}),
		readDone:    make(chan struct{}),
	}
}

func writeSyntheticTerminalUpdate(term *Terminal, input []byte) error {
	if term == nil || term.vterm == nil || term.stream == nil {
		return fmt.Errorf("synthetic terminal not initialized")
	}
	term.streamMu.Lock()
	defer term.streamMu.Unlock()
	if len(input) == 0 {
		return nil
	}
	if _, err, damage := term.vterm.WriteWithDamage(input); err != nil {
		return err
	} else {
		payload, ok := term.screenUpdatePayloadFromDamageLocked(damage)
		if !ok {
			return fmt.Errorf("encode screen update payload")
		}
		term.stream.BroadcastMessage(fanout.StreamMessage{Type: fanout.StreamScreenUpdate, Payload: payload})
	}
	return nil
}
