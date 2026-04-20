package termx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/fanout"
	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
)

func TestTerminalLifecycleAndSnapshot(t *testing.T) {
	ctx := context.Background()
	bus := NewEventBus(nil)

	term, err := newTerminal(ctx, bus, terminalConfig{
		ID:             "abc12345",
		Name:           "shell",
		Command:        []string{"bash", "--noprofile", "--norc"},
		Size:           Size{Cols: 80, Rows: 24},
		ScrollbackSize: 128,
		KeepAfterExit:  time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("new terminal failed: %v", err)
	}
	defer term.Close()

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := term.Subscribe(streamCtx)

	if err := term.WriteInput([]byte("echo hello-termx\n")); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msg := <-stream
		if streamMessageContainsText(msg, 80, 24, "hello-termx") {
			break
		}
	}

	snap := term.Snapshot(0, 50)
	if !snapshotContains(snap, "hello-termx") {
		t.Fatalf("snapshot missing output: %#v", snap)
	}

	if err := term.Resize(100, 40); err != nil {
		t.Fatalf("resize failed: %v", err)
	}

	if got := term.Info(); got.Size != (Size{Cols: 100, Rows: 40}) {
		t.Fatalf("unexpected size: %#v", got.Size)
	}

	if err := term.Kill(); err != nil {
		t.Fatalf("kill failed: %v", err)
	}

	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for terminal exit")
	}

	if got := term.Info(); got.State != StateExited {
		t.Fatalf("unexpected state: %s", got.State)
	}
}

func TestTerminalResizeRejectsSizeLockedTerminal(t *testing.T) {
	ctx := context.Background()
	bus := NewEventBus(nil)

	term, err := newTerminal(ctx, bus, terminalConfig{
		ID:             "lock1234",
		Name:           "shell",
		Command:        []string{"bash", "--noprofile", "--norc"},
		Tags:           map[string]string{"termx.size_lock": "lock"},
		Size:           Size{Cols: 80, Rows: 24},
		ScrollbackSize: 128,
		KeepAfterExit:  time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("new terminal failed: %v", err)
	}
	defer term.Close()

	if err := term.Resize(100, 40); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied, got %v", err)
	}
	if got := term.Info().Size; got != (Size{Cols: 80, Rows: 24}) {
		t.Fatalf("expected size to remain locked at 80x24, got %#v", got)
	}
}

func TestSubscribeAfterExitReplaysSnapshotAndClosed(t *testing.T) {
	ctx := context.Background()
	bus := NewEventBus(nil)

	term, err := newTerminal(ctx, bus, terminalConfig{
		ID:             "exit1234",
		Name:           "env",
		Command:        []string{"sh", "-c", "echo replay-me; sleep 0.1; exit 0"},
		Size:           Size{Cols: 80, Rows: 24},
		ScrollbackSize: 128,
		KeepAfterExit:  time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("new terminal failed: %v", err)
	}
	defer term.Close()

	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for terminal exit")
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := term.Subscribe(streamCtx)

	select {
	case msg, ok := <-stream:
		if !ok {
			t.Fatal("expected resize bootstrap frame")
		}
		if msg.Type != StreamResize || msg.Cols != 80 || msg.Rows != 24 {
			t.Fatalf("expected resize bootstrap, got %#v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for resize bootstrap")
	}

	select {
	case msg, ok := <-stream:
		if !ok {
			t.Fatal("expected replay output frame")
		}
		if !streamMessageContainsText(msg, 80, 24, "replay-me") {
			t.Fatalf("expected replay output, got %#v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for replay output")
	}

	select {
	case msg, ok := <-stream:
		if !ok {
			t.Fatal("expected closed frame")
		}
		if msg.Type != StreamBootstrapDone {
			t.Fatalf("expected bootstrap-done frame, got %#v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for bootstrap-done frame")
	}

	select {
	case msg, ok := <-stream:
		if !ok {
			t.Fatal("expected closed frame")
		}
		if msg.Type != StreamClosed {
			t.Fatalf("expected closed frame, got %#v", msg)
		}
		if msg.ExitCode == nil || *msg.ExitCode != 0 {
			t.Fatalf("expected exit code 0, got %#v", msg.ExitCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for closed frame")
	}
}

func TestSubscribeRunningTerminalBootstrapsResizeReplayThenLiveOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	vt := localvterm.New(6, 2, 16, nil)
	if _, err := vt.Write([]byte("hello\r\nworld")); err != nil {
		t.Fatalf("seed vterm: %v", err)
	}

	term := &Terminal{
		size:   Size{Cols: 6, Rows: 2},
		state:  StateRunning,
		vterm:  vt,
		stream: fanout.New(),
	}

	stream := term.Subscribe(ctx)

	select {
	case msg := <-stream:
		if msg.Type != StreamResize || msg.Cols != 6 || msg.Rows != 2 {
			t.Fatalf("expected resize bootstrap, got %#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resize bootstrap")
	}

	select {
	case msg := <-stream:
		if !streamMessageContainsText(msg, 6, 2, "hello") || !streamMessageContainsText(msg, 6, 2, "world") {
			t.Fatalf("expected replay bootstrap output, got %#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for replay bootstrap")
	}

	term.stream.Broadcast([]byte("later"))

	select {
	case msg := <-stream:
		if msg.Type != StreamBootstrapDone {
			t.Fatalf("expected bootstrap-done before live output, got %#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bootstrap-done frame")
	}

	select {
	case msg := <-stream:
		if !streamMessageContainsText(msg, 6, 2, "later") {
			t.Fatalf("expected live output after bootstrap, got %#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live output")
	}
}

func TestTerminalSnapshotFlushesPendingVTermOutput(t *testing.T) {
	vt := localvterm.New(8, 2, 16, nil)
	term := &Terminal{
		id:           "snap-pending",
		size:         Size{Cols: 8, Rows: 2},
		state:        StateRunning,
		vterm:        vt,
		processEpoch: 1,
	}

	term.streamMu.Lock()
	term.queuePendingVTermOutputLocked(1, []byte("hello"))
	term.streamMu.Unlock()
	if replayContainsText(vt.EncodeReplay(16), 8, 2, "hello") {
		t.Fatal("expected pending output to remain buffered until a flush boundary")
	}

	snap := term.Snapshot(0, 10)
	if !snapshotContains(snap, "hello") {
		t.Fatalf("expected snapshot to flush pending vterm output, got %#v", snap)
	}
}

func TestSubscribeFlushesPendingVTermOutputBeforeBootstrap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	vt := localvterm.New(8, 2, 16, nil)
	term := &Terminal{
		size:         Size{Cols: 8, Rows: 2},
		state:        StateRunning,
		vterm:        vt,
		stream:       fanout.New(),
		processEpoch: 1,
	}

	term.streamMu.Lock()
	term.queuePendingVTermOutputLocked(1, []byte("hello"))
	term.streamMu.Unlock()

	stream := term.Subscribe(ctx)

	select {
	case msg := <-stream:
		if msg.Type != StreamResize || msg.Cols != 8 || msg.Rows != 2 {
			t.Fatalf("expected resize bootstrap, got %#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resize bootstrap")
	}

	select {
	case msg := <-stream:
		if !streamMessageContainsText(msg, 8, 2, "hello") {
			t.Fatalf("expected replay bootstrap output with pending bytes, got %#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for replay bootstrap")
	}
}

func TestTerminalIdleFlushesPendingVTermOutput(t *testing.T) {
	originalDelay := serverVTermFlushIdleDelay
	serverVTermFlushIdleDelay = 2 * time.Millisecond
	defer func() { serverVTermFlushIdleDelay = originalDelay }()

	vt := localvterm.New(8, 2, 16, nil)
	term := &Terminal{
		id:           "idle-flush",
		size:         Size{Cols: 8, Rows: 2},
		state:        StateRunning,
		vterm:        vt,
		processEpoch: 1,
	}

	term.streamMu.Lock()
	term.queuePendingVTermOutputLocked(1, []byte("hello"))
	term.armPendingVTermFlushLocked(1)
	term.streamMu.Unlock()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if replayContainsText(vt.EncodeReplay(16), 8, 2, "hello") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("expected idle flush to apply pending output to the server vterm")
}

func TestForwardLiveStreamMessagesCoalescesBurstOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := make(chan fanout.StreamMessage, 8)
	dst := make(chan StreamMessage, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		forwardLiveStreamMessages(ctx, src, dst)
		close(dst)
	}()

	src <- fanout.StreamMessage{Type: fanout.StreamOutput, Output: []byte("a")}
	src <- fanout.StreamMessage{Type: fanout.StreamOutput, Output: []byte("b")}
	src <- fanout.StreamMessage{Type: fanout.StreamOutput, Output: []byte("c")}
	close(src)

	received := collectStreamMessages(t, dst)
	<-done
	if len(received) != 1 {
		t.Fatalf("expected one merged output frame, got %#v", received)
	}
	if received[0].Type != StreamOutput || string(received[0].Output) != "abc" {
		t.Fatalf("expected merged output %q, got %#v", "abc", received[0])
	}
}

func TestForwardLiveStreamMessagesPreservesSingleOutputBuffer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := make(chan fanout.StreamMessage, 2)
	dst := make(chan StreamMessage, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		forwardLiveStreamMessages(ctx, src, dst)
		close(dst)
	}()

	payload := []byte("solo")
	src <- fanout.StreamMessage{Type: fanout.StreamOutput, Output: payload}
	close(src)

	received := collectStreamMessages(t, dst)
	<-done
	if len(received) != 1 {
		t.Fatalf("expected one output frame, got %#v", received)
	}
	if received[0].Type != StreamOutput || string(received[0].Output) != "solo" {
		t.Fatalf("unexpected output %#v", received[0])
	}
	if len(received[0].Output) == 0 || &received[0].Output[0] != &payload[0] {
		t.Fatal("expected single-message coalescing path to preserve the original buffer")
	}
}

func TestForwardLiveStreamMessagesPreservesSyncLostBoundaries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := make(chan fanout.StreamMessage, 8)
	dst := make(chan StreamMessage, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		forwardLiveStreamMessages(ctx, src, dst)
		close(dst)
	}()

	src <- fanout.StreamMessage{Type: fanout.StreamOutput, Output: []byte("ab")}
	src <- fanout.StreamMessage{Type: fanout.StreamSyncLost, DroppedBytes: 7}
	src <- fanout.StreamMessage{Type: fanout.StreamOutput, Output: []byte("cd")}
	src <- fanout.StreamMessage{Type: fanout.StreamResize, Cols: 80, Rows: 24}
	close(src)

	received := collectStreamMessages(t, dst)
	<-done
	if len(received) != 4 {
		t.Fatalf("expected output, sync-lost, output, resize; got %#v", received)
	}
	if received[0].Type != StreamOutput || string(received[0].Output) != "ab" {
		t.Fatalf("unexpected first frame %#v", received[0])
	}
	if received[1].Type != StreamSyncLost || received[1].DroppedBytes != 7 {
		t.Fatalf("unexpected sync-lost frame %#v", received[1])
	}
	if received[2].Type != StreamOutput || string(received[2].Output) != "cd" {
		t.Fatalf("unexpected second output %#v", received[2])
	}
	if received[3].Type != StreamResize || received[3].Cols != 80 || received[3].Rows != 24 {
		t.Fatalf("unexpected resize frame %#v", received[3])
	}
}

func TestTerminalSnapshotReturnsNewestScrollbackWindow(t *testing.T) {
	vt := localvterm.New(4, 2, 16, nil)
	if _, err := vt.Write([]byte("1\n2\n3\n4\n5\n")); err != nil {
		t.Fatalf("write scrollback seed failed: %v", err)
	}

	term := &Terminal{
		id:    "snap-1",
		size:  Size{Cols: 4, Rows: 2},
		vterm: vt,
	}

	latest := term.Snapshot(0, 2)
	if len(latest.Scrollback) != 2 {
		t.Fatalf("expected 2 latest scrollback rows, got %d", len(latest.Scrollback))
	}
	if got := snapshotRowString(latest.Scrollback[0]); !strings.Contains(got, "3") {
		t.Fatalf("expected latest window to start near newest history, got %q", got)
	}
	if got := snapshotRowString(latest.Scrollback[1]); !strings.Contains(got, "4") {
		t.Fatalf("expected latest window to end at newest history, got %q", got)
	}

	older := term.Snapshot(2, 2)
	if len(older.Scrollback) != 2 {
		t.Fatalf("expected 2 older scrollback rows, got %d", len(older.Scrollback))
	}
	if got := snapshotRowString(older.Scrollback[0]); !strings.Contains(got, "1") {
		t.Fatalf("expected older window to include oldest history, got %q", got)
	}
	if got := snapshotRowString(older.Scrollback[1]); !strings.Contains(got, "2") {
		t.Fatalf("expected older window to include next history row, got %q", got)
	}
}

func TestTerminalRestartPreservesScrollbackAcrossRestart(t *testing.T) {
	ctx := context.Background()
	bus := NewEventBus(nil)
	flagPath := t.TempDir() + "/restart-flag"

	term, err := newTerminal(ctx, bus, terminalConfig{
		ID:             "restart123",
		Name:           "restart",
		Command:        []string{"bash", "-lc", "if [ -f " + shellQuote(flagPath) + " ]; then printf 'second-pass\\n'; sleep 5; else touch " + shellQuote(flagPath) + "; printf 'first-pass\\n'; exit 0; fi"},
		Size:           Size{Cols: 80, Rows: 24},
		ScrollbackSize: 128,
		KeepAfterExit:  time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("new terminal failed: %v", err)
	}
	defer term.Close()

	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first terminal exit")
	}

	firstSnap := term.Snapshot(0, 50)
	if !snapshotContains(firstSnap, "first-pass") {
		t.Fatalf("expected first run output before restart, got %#v", firstSnap)
	}
	if ts, ok := snapshotTimestampForNeedle(firstSnap, "first-pass"); !ok || ts.IsZero() {
		t.Fatalf("expected first run output to have a timestamp before restart, got %v (ok=%v)", ts, ok)
	}

	if err := term.Restart(); err != nil {
		t.Fatalf("restart failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := term.Snapshot(0, 100)
		if snapshotContains(snap, "second-pass") {
			if !snapshotContains(snap, "first-pass") {
				t.Fatalf("expected restart snapshot to preserve first run output, got %#v", snap)
			}
			if ts, ok := snapshotTimestampForNeedle(snap, "first-pass"); !ok || ts.IsZero() {
				t.Fatalf("expected preserved first run output to retain a timestamp after restart, got %v (ok=%v)", ts, ok)
			}
			if ts, ok := snapshotTimestampForRowKind(snap, SnapshotRowKindRestart); !ok || ts.IsZero() {
				t.Fatalf("expected restart snapshot to include a restart marker with timestamp, got %v (ok=%v)", ts, ok)
			}
			if err := term.Kill(); err != nil {
				t.Fatalf("kill restarted terminal failed: %v", err)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatal("timed out waiting for restarted terminal output")
}

func TestTerminalDeliversTrailingOutputBeforeClosedFrame(t *testing.T) {
	ctx := context.Background()
	bus := NewEventBus(nil)

	term, err := newTerminal(ctx, bus, terminalConfig{
		ID:             "trail123",
		Name:           "cat",
		Command:        []string{"cat", "-vet"},
		Size:           Size{Cols: 80, Rows: 24},
		ScrollbackSize: 128,
		KeepAfterExit:  time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("new terminal failed: %v", err)
	}
	defer term.Close()

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := term.Subscribe(streamCtx)

	if err := term.WriteInput([]byte("A\t\x1bB\n\x04")); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	sawOutput := false
	for time.Now().Before(deadline) {
		msg, ok := <-stream
		if !ok {
			break
		}
		switch msg.Type {
		case StreamOutput, StreamScreenUpdate:
			if streamMessageContainsText(msg, 80, 24, "A^I^[B$") {
				sawOutput = true
			}
		case StreamClosed:
			if !sawOutput {
				t.Fatalf("stream closed before trailing output arrived")
			}
			return
		}
	}
	if !sawOutput {
		t.Fatal("expected trailing output before close")
	}
	t.Fatal("timed out waiting for closed frame")
}

func snapshotContains(s *Snapshot, needle string) bool {
	for _, row := range s.Scrollback {
		if rowToString(row) == needle {
			return true
		}
	}
	for _, row := range s.Screen.Cells {
		if strings.Contains(rowToString(row), needle) {
			return true
		}
	}
	return false
}

func collectStreamMessages(t *testing.T, ch <-chan StreamMessage) []StreamMessage {
	t.Helper()
	out := make([]StreamMessage, 0, 4)
	timeout := time.After(3 * time.Second)
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, msg)
		case <-timeout:
			t.Fatalf("timed out collecting stream messages: %#v", out)
		}
	}
}

func snapshotTimestampForNeedle(s *Snapshot, needle string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	for i, row := range s.Scrollback {
		if strings.Contains(rowToString(row), needle) {
			if i < len(s.ScrollbackTimestamps) {
				return s.ScrollbackTimestamps[i], true
			}
			return time.Time{}, false
		}
	}
	for i, row := range s.Screen.Cells {
		if strings.Contains(rowToString(row), needle) {
			if i < len(s.ScreenTimestamps) {
				return s.ScreenTimestamps[i], true
			}
			return time.Time{}, false
		}
	}
	return time.Time{}, false
}

func replayContainsText(payload []byte, cols, rows int, needle string) bool {
	if len(payload) == 0 {
		return false
	}
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	vt := localvterm.New(cols, rows, 256, nil)
	if _, err := vt.Write(payload); err != nil {
		return false
	}
	for _, row := range vt.ScrollbackContent() {
		if strings.Contains(vtermRowToString(row), needle) {
			return true
		}
	}
	for _, row := range vt.ScreenContent().Cells {
		if strings.Contains(vtermRowToString(row), needle) {
			return true
		}
	}
	return false
}

func streamMessageContainsText(msg StreamMessage, cols, rows int, needle string) bool {
	switch msg.Type {
	case StreamOutput:
		if cols > 0 && rows > 0 {
			return replayContainsText(msg.Output, cols, rows, needle)
		}
		return strings.Contains(string(msg.Output), needle)
	case StreamScreenUpdate:
		update, err := protocol.DecodeScreenUpdatePayload(msg.Payload)
		if err != nil {
			return false
		}
		return screenUpdateContainsText(update, needle)
	default:
		return false
	}
}

func screenUpdateContainsText(update protocol.ScreenUpdate, needle string) bool {
	if update.FullReplace {
		for _, row := range update.Screen.Cells {
			if strings.Contains(protocolRowToString(row), needle) {
				return true
			}
		}
	}
	for _, row := range update.ChangedRows {
		if strings.Contains(protocolRowToString(row.Cells), needle) {
			return true
		}
	}
	for _, span := range update.ChangedSpans {
		if strings.Contains(protocolRowToString(span.Cells), needle) {
			return true
		}
	}
	for _, row := range update.ScrollbackAppend {
		if strings.Contains(protocolRowToString(row.Cells), needle) {
			return true
		}
	}
	for _, op := range update.Ops {
		if op.Code != protocol.ScreenOpWriteSpan {
			continue
		}
		if strings.Contains(protocolRowToString(op.Cells), needle) {
			return true
		}
	}
	return false
}

func TestScreenUpdatePayloadFromDamageOmitsRedundantControlOps(t *testing.T) {
	vt := localvterm.New(8, 2, 32, nil)
	term := &Terminal{
		vterm: vt,
		title: "demo",
	}
	_, err, damage := vt.WriteWithDamage([]byte("ok"))
	if err != nil {
		t.Fatalf("WriteWithDamage failed: %v", err)
	}
	payload, ok := term.screenUpdatePayloadFromDamageLocked(damage)
	if !ok {
		t.Fatal("expected payload")
	}
	update, err := protocol.DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got := update.Title; got != "demo" {
		t.Fatalf("expected title in top-level header, got %q", got)
	}
	for _, op := range update.Ops {
		switch op.Code {
		case protocol.ScreenOpCursor, protocol.ScreenOpModes, protocol.ScreenOpTitle:
			t.Fatalf("expected server payload to omit redundant control op, got %#v", op)
		}
	}
	if !screenUpdateContainsText(update, "ok") {
		t.Fatalf("expected payload to preserve content op, got %#v", update)
	}
}

func TestScreenUpdateShouldEncodeDeltaOnly(t *testing.T) {
	fullRows := make([]protocol.ScreenOp, 0, 24)
	for row := 0; row < 24; row++ {
		fullRows = append(fullRows, protocol.ScreenOp{
			Code: protocol.ScreenOpWriteSpan,
			Row:  row,
			Col:  0,
			Cells: []protocol.Cell{
				{Content: strings.Repeat("x", 1), Width: 1},
			},
		})
		fullRows[row].Cells = make([]protocol.Cell, 80)
		for col := range fullRows[row].Cells {
			fullRows[row].Cells[col] = protocol.Cell{Content: "x", Width: 1}
		}
	}
	tests := []struct {
		name                    string
		update                  protocol.ScreenUpdate
		preferAggressiveFullRep bool
		want                    bool
	}{
		{
			name: "screen_scroll",
			update: protocol.ScreenUpdate{
				Size:         protocol.Size{Cols: 80, Rows: 24},
				ScreenScroll: 1,
			},
			want: true,
		},
		{
			name: "scrollback_append",
			update: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 80, Rows: 24},
				ScrollbackAppend: []protocol.ScrollbackRowAppend{{
					Cells: []protocol.Cell{{Content: "log", Width: 1}},
				}},
			},
			want: true,
		},
		{
			name: "small_partial_damage",
			update: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 80, Rows: 24},
				Ops: []protocol.ScreenOp{{
					Code: protocol.ScreenOpWriteSpan,
					Row:  0,
					Col:  0,
					Cells: []protocol.Cell{
						{Content: "o", Width: 1},
						{Content: "k", Width: 1},
					},
				}},
			},
			want: true,
		},
		{
			name: "alt_screen_full_damage",
			update: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 80, Rows: 24},
				Ops:  fullRows,
			},
			preferAggressiveFullRep: true,
			want:                    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := screenUpdateShouldEncodeDeltaOnly(tt.update, tt.preferAggressiveFullRep); got != tt.want {
				t.Fatalf("screenUpdateShouldEncodeDeltaOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScreenUpdatePayloadFromDamageScrollUsesDeltaPayload(t *testing.T) {
	vt := localvterm.New(80, 24, 128, nil)
	vt.LoadSnapshot(
		benchmarkFilledScreen(80, 24, "log"),
		localvterm.CursorState{Row: 23, Col: 0, Visible: true},
		localvterm.TerminalModes{AutoWrap: true},
	)
	_, err, damage := vt.WriteWithDamage([]byte("scroll-a\n"))
	if err != nil {
		t.Fatalf("WriteWithDamage failed: %v", err)
	}
	term := &Terminal{
		vterm: vt,
		title: "demo",
	}
	payload, ok := term.screenUpdatePayloadFromDamageLocked(damage)
	if !ok {
		t.Fatal("expected payload")
	}
	update, err := protocol.DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if update.FullReplace {
		t.Fatalf("expected delta payload for scroll update, got full replace %#v", update)
	}
	if update.Title != "demo" {
		t.Fatalf("expected title propagated, got %q", update.Title)
	}
	if update.ScreenScroll == 0 && len(update.Ops) == 0 && len(update.ScrollbackAppend) == 0 {
		t.Fatalf("expected scroll delta operations, got %#v", update)
	}
}

func protocolRowToString(row []protocol.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

func snapshotTimestampForRowKind(s *Snapshot, kind string) (time.Time, bool) {
	if s == nil || kind == "" {
		return time.Time{}, false
	}
	for i, rowKind := range s.ScrollbackRowKinds {
		if rowKind != kind {
			continue
		}
		if i < len(s.ScrollbackTimestamps) {
			return s.ScrollbackTimestamps[i], true
		}
		return time.Time{}, false
	}
	for i, rowKind := range s.ScreenRowKinds {
		if rowKind != kind {
			continue
		}
		if i < len(s.ScreenTimestamps) {
			return s.ScreenTimestamps[i], true
		}
		return time.Time{}, false
	}
	return time.Time{}, false
}

func rowToString(row []Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

func vtermRowToString(row []localvterm.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
