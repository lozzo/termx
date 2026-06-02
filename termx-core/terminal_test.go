package termx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-core/fanout"
	"github.com/lozzow/termx/termx-proto/wire"
	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
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

func TestTerminalTracksReportedWorkingDirectory(t *testing.T) {
	ctx := context.Background()
	bus := NewEventBus(nil)

	term, err := newTerminal(ctx, bus, terminalConfig{
		ID:             "cwd-test",
		Name:           "shell",
		Command:        []string{"bash", "--noprofile", "--norc"},
		Size:           Size{Cols: 80, Rows: 24},
		Dir:            "/tmp",
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

	wantInitialCWD, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		wantInitialCWD = "/tmp"
	}
	if got := term.Info().CWD; got != wantInitialCWD {
		t.Fatalf("expected initial cwd from create dir, got %q", got)
	}
	term.installVTermHandlers()
	if _, err := term.vterm.Write([]byte("\x1b]7;file://host/srv/app\x1b\\")); err != nil {
		t.Fatalf("vterm write failed: %v", err)
	}

	reported, ok := term.listInfoSnapshot(ListOptions{})
	if !ok {
		t.Fatal("expected terminal list info")
	}
	if got := reported.CWD; got != "/srv/app" {
		t.Fatalf("expected reported cwd, got %q", got)
	}
	info := term.protocolInfo()
	if info == nil {
		t.Fatal("protocol info failed")
	}
	if info.CWD != "/srv/app" {
		t.Fatalf("expected protocol cwd, got %#v", info)
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

func TestSubscribeRunningTerminalBootstrapsResizeReplayThenLiveScreenUpdate(t *testing.T) {
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

	term.stream.BroadcastMessage(fanout.StreamMessage{
		Type: fanout.StreamScreenUpdate,
		Payload: encodeTestScreenUpdatePayload(t, protocol.ScreenUpdate{
			FullReplace: true,
			Size:        protocol.Size{Cols: 6, Rows: 2},
			Screen: protocol.ScreenData{
				Cells: [][]protocol.Cell{{{Content: "later"}}},
			},
			Cursor: protocol.CursorState{Visible: true},
			Modes:  protocol.TerminalModes{AutoWrap: true},
		}),
	})

	select {
	case msg := <-stream:
		if msg.Type != StreamBootstrapDone {
			t.Fatalf("expected bootstrap-done before live screen update, got %#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bootstrap-done frame")
	}

	select {
	case msg := <-stream:
		if !streamMessageContainsText(msg, 6, 2, "later") {
			t.Fatalf("expected live screen update after bootstrap, got %#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live screen update")
	}
}

func TestWriteAuthoritativeScreenUpdateBroadcastsLiveDeltaPayload(t *testing.T) {
	vt := localvterm.New(80, 24, 128, nil)
	vt.LoadSnapshot(
		benchmarkFilledScreen(80, 24, "log"),
		localvterm.CursorState{Row: 23, Col: 0, Visible: true},
		localvterm.TerminalModes{AutoWrap: true},
	)
	term := &Terminal{
		id:     "term-1",
		size:   Size{Cols: 80, Rows: 24},
		state:  StateRunning,
		title:  "demo",
		vterm:  vt,
		stream: fanout.New(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := term.stream.Subscribe(ctx)

	term.streamMu.Lock()
	term.writeAuthoritativeScreenUpdateLocked(term.stream, []byte("scroll-a\n"))
	term.streamMu.Unlock()

	select {
	case msg := <-stream:
		if msg.Type != fanout.StreamScreenUpdate {
			t.Fatalf("expected screen update, got %#v", msg)
		}
		if len(msg.Payload) == 0 {
			t.Fatal("expected live screen update to carry a delta payload, got empty invalidation")
		}
		update, err := protocol.DecodeScreenUpdatePayload(msg.Payload)
		if err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if update.FullReplace {
			t.Fatalf("expected live scroll delta payload, got full replace %#v", update)
		}
		if update.ScreenScroll == 0 && len(update.Ops) == 0 {
			t.Fatalf("expected live scroll delta operations, got %#v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live screen update")
	}
}

func TestWriteAuthoritativeScreenUpdateLargeChunkBroadcastsLatestInvalidation(t *testing.T) {
	width := 80
	rows := 24
	vt := localvterm.New(width, rows, 256, nil)
	vt.LoadSnapshot(
		benchmarkFilledScreen(width, rows, "seed"),
		localvterm.CursorState{Row: rows - 1, Col: 0, Visible: true},
		localvterm.TerminalModes{AutoWrap: true},
	)
	term := &Terminal{
		id:     "term-large",
		size:   Size{Cols: uint16(width), Rows: uint16(rows)},
		state:  StateRunning,
		vterm:  vt,
		stream: fanout.New(),
		grid:   newMemoryTerminalGridStoreForTest(t),
	}
	defer term.grid.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := term.stream.Subscribe(ctx)

	var b strings.Builder
	lastNeedle := ""
	for i := 0; b.Len() <= terminalInlineDamageMaxBytes; i++ {
		lastNeedle = fmt.Sprintf("large-row-%04d", i)
		fmt.Fprintf(&b, "%s %s\r\n", lastNeedle, strings.Repeat("x", width-len(lastNeedle)-2))
	}

	term.streamMu.Lock()
	term.writeAuthoritativeScreenUpdateLocked(term.stream, []byte(b.String()))
	term.streamMu.Unlock()

	select {
	case msg := <-stream:
		if msg.Type != fanout.StreamScreenUpdate {
			t.Fatalf("expected screen update, got %#v", msg)
		}
		if len(msg.Payload) != 0 {
			t.Fatalf("expected large output to broadcast latest invalidation, got payload bytes=%d", len(msg.Payload))
		}
		if msg.Revision == 0 {
			t.Fatal("expected latest invalidation to carry a screen revision")
		}
		term.streamMu.Lock()
		latest := term.screenSnapshotFallbackMessageLocked()
		term.streamMu.Unlock()
		if latest.Type != StreamScreenUpdate || len(latest.Payload) == 0 {
			t.Fatalf("expected snapshot fallback payload, got %#v", latest)
		}
		if latest.Revision != msg.Revision {
			t.Fatalf("snapshot fallback revision mismatch: got=%d want=%d", latest.Revision, msg.Revision)
		}
		update, err := protocol.DecodeScreenUpdatePayload(latest.Payload)
		if err != nil {
			t.Fatalf("decode snapshot payload: %v", err)
		}
		if !update.FullReplace {
			t.Fatalf("expected latest recovery to use authoritative full screen snapshot, got %#v", update)
		}
		if len(update.ScrollbackAppend) != 0 || update.ScrollbackTrim != 0 {
			t.Fatalf("expected snapshot fallback to be screen-only, got %#v", update)
		}
		if !screenUpdateContainsText(update, lastNeedle) {
			t.Fatalf("expected snapshot fallback to contain final visible row %q, got %#v", lastNeedle, update)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for large output update")
	}

	snap := term.Snapshot(0, 10)
	if snap.ScrollbackTotal == 0 || len(snap.Scrollback) == 0 {
		t.Fatalf("expected latest-frame path to persist scrollback, got total=%d rows=%d", snap.ScrollbackTotal, len(snap.Scrollback))
	}
	if !snapshotContains(snap, "large-row-") {
		t.Fatalf("expected snapshot to include large output rows, got %#v", snap)
	}
}

func TestWriteAuthoritativeScreenUpdateLargeChunkAttachmentPumpSendsFinalTail(t *testing.T) {
	width := 80
	rows := 24
	vt := localvterm.New(width, rows, 512, nil)
	vt.LoadSnapshot(
		benchmarkFilledScreen(width, rows, "seed"),
		localvterm.CursorState{Row: rows - 1, Col: 0, Visible: true},
		localvterm.TerminalModes{AutoWrap: true},
	)
	term := &Terminal{
		id:     "term-large-scrollback",
		size:   Size{Cols: uint16(width), Rows: uint16(rows)},
		state:  StateRunning,
		vterm:  vt,
		stream: fanout.New(),
		grid:   newMemoryTerminalGridStoreForTest(t),
	}
	defer term.grid.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := term.SubscribeLatest(ctx)
	sent := make(chan protocol.StreamFrame, 64)
	pump := newAttachmentStreamPump(
		ctx,
		cancel,
		term.id,
		7,
		"test",
		src,
		term.screenSnapshotFallbackMessage,
		term.currentScreenRevision,
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
	pump.screenReady(0)
	for {
		frame := waitSentFrame(t, sent)
		if frame.Type == wire.TypeScreenUpdate {
			pump.screenReady(1)
		}
		if frame.Type == wire.TypeBootstrapDone {
			break
		}
	}

	var b strings.Builder
	lastNeedle := ""
	for i := 0; b.Len() <= terminalInlineDamageMaxBytes; i++ {
		lastNeedle = fmt.Sprintf("scrollback-row-%04d", i)
		fmt.Fprintf(&b, "%s %s\r\n", lastNeedle, strings.Repeat("x", width-len(lastNeedle)-2))
	}

	term.streamMu.Lock()
	term.writeAuthoritativeScreenUpdateLocked(term.stream, []byte(b.String()))
	term.streamMu.Unlock()

	frame := waitSentFrame(t, sent)
	if frame.Type != wire.TypeScreenUpdate {
		t.Fatalf("expected screen update, got %#v", frame)
	}
	update, err := protocol.DecodeScreenUpdatePayload(frame.Payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !update.FullReplace {
		t.Fatalf("expected latest recovery to use authoritative full screen snapshot, got %#v", update)
	}
	if len(update.ScrollbackAppend) != 0 || update.ScrollbackTrim != 0 {
		t.Fatalf("expected visible-only latest payload without scrollback payload, got %#v", update)
	}
	if !screenUpdateContainsText(update, lastNeedle) {
		t.Fatalf("expected attachment latest payload to contain final visible row %q, got %#v", lastNeedle, update)
	}
	if snapshot := term.Snapshot(0, 10); !snapshotContains(snapshot, lastNeedle) {
		t.Fatalf("expected terminal snapshot to contain final output row %q, got %#v", lastNeedle, snapshot)
	}
	pump.screenReady(0)
	assertNoSentFrame(t, sent, 75*time.Millisecond)
}

func TestSubscribeBootstrapSendsScreenOnlyAndHistoryReplayStaysAvailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scrollbackRows := 136
	vt := localvterm.New(12, 2, scrollbackRows+16, nil)
	for i := 0; i < scrollbackRows+2; i++ {
		if _, err := vt.Write([]byte(fmt.Sprintf("line-%03d\r\n", i))); err != nil {
			t.Fatalf("seed vterm row %d: %v", i, err)
		}
	}

	term := &Terminal{
		size:         Size{Cols: 12, Rows: 2},
		state:        StateRunning,
		vterm:        vt,
		stream:       fanout.New(),
		processEpoch: 1,
	}

	stream := term.Subscribe(ctx)

	select {
	case msg := <-stream:
		if msg.Type != StreamResize || msg.Cols != 12 || msg.Rows != 2 {
			t.Fatalf("expected resize bootstrap, got %#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resize bootstrap")
	}

	select {
	case msg := <-stream:
		if msg.Type != StreamScreenUpdate {
			t.Fatalf("expected screen update bootstrap, got %#v", msg)
		}
		update, err := protocol.DecodeScreenUpdatePayload(msg.Payload)
		if err != nil {
			t.Fatalf("decode screen update: %v", err)
		}
		if update.ResetScrollback || update.ScrollbackTrim != 0 || len(update.ScrollbackAppend) != 0 {
			t.Fatalf("expected bootstrap live screen to omit scrollback, got %#v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for screen update bootstrap")
	}

	replay := term.HistoryReplay(HistoryReplayOptions{BeforeOffset: 0, Limit: 4})
	if replay.Rows == 0 || replay.Replay == "" {
		t.Fatalf("expected history replay to remain available, got %#v", replay)
	}
}

func TestSubscribeBootstrapDoesNotCreateHistoryFromLiveScreenOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	vt := localvterm.New(8, 2, 16, nil)
	vt.LoadSnapshot(localvterm.ScreenData{
		Cells: [][]localvterm.Cell{
			localVTermRowForTest("live-a", 8),
			localVTermRowForTest("live-b", 8),
		},
	}, localvterm.CursorState{Visible: true}, localvterm.TerminalModes{AutoWrap: true})
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:           "bootstrap-no-history-create",
		size:         Size{Cols: 8, Rows: 2},
		state:        StateRunning,
		vterm:        vt,
		stream:       fanout.New(),
		grid:         store,
		processEpoch: 1,
	}

	beforeRows := store.RowCount()
	beforeLines := store.LogicalLineCount()
	stream := term.Subscribe(ctx)

	select {
	case <-stream:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resize bootstrap")
	}
	select {
	case msg := <-stream:
		if msg.Type != StreamScreenUpdate {
			t.Fatalf("expected screen update bootstrap, got %#v", msg)
		}
		update, err := protocol.DecodeScreenUpdatePayload(msg.Payload)
		if err != nil {
			t.Fatalf("decode bootstrap payload: %v", err)
		}
		if !update.FullReplace {
			t.Fatalf("expected bootstrap payload to be authoritative full replace, got %#v", update)
		}
		if len(update.ScrollbackAppend) != 0 || update.ScrollbackTrim != 0 || update.ResetScrollback {
			t.Fatalf("expected bootstrap full replace not to create history rows, got %#v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for screen update bootstrap")
	}

	if got := store.RowCount(); got != beforeRows {
		t.Fatalf("expected bootstrap not to append committed rows, got before=%d after=%d", beforeRows, got)
	}
	if got := store.LogicalLineCount(); got != beforeLines {
		t.Fatalf("expected bootstrap not to create committed logical lines, got before=%d after=%d", beforeLines, got)
	}

	coreViewport, err := term.combinedGridViewport(0, 10, 8, term.primaryLiveTail.clone())
	if err != nil {
		t.Fatalf("bootstrap history window viewport: %v", err)
	}
	window := historyWindowFromCoreGridViewport(term.id, 0, coreViewport)
	if len(window.Rows) != 0 || window.LoadedRows != 0 || window.LoadedLines != 0 || window.TotalRows != 0 || window.LogicalTotal != 0 || window.Token != "" {
		t.Fatalf("expected bootstrap to leave authoritative history window empty, got %#v", window)
	}
	if historyWindowContainsText(window, "live-a") || historyWindowContainsText(window, "live-b") {
		t.Fatalf("expected bootstrap not to expose live screen through history window, got %#v", window.Rows)
	}

	snap := term.Snapshot(0, 0)
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	if snap.ScrollbackTotal != 0 || snap.ScrollbackLoadedRows != 0 || snap.HistoryGeneration != 0 {
		t.Fatalf("expected bootstrap to leave history metadata persisted-empty, got total=%d loaded=%d gen=%d", snap.ScrollbackTotal, snap.ScrollbackLoadedRows, snap.HistoryGeneration)
	}
}

func TestScreenSnapshotFallbackFullReplaceDoesNotCreateHistory(t *testing.T) {
	vt := localvterm.New(8, 2, 16, nil)
	vt.LoadSnapshot(localvterm.ScreenData{
		Cells: [][]localvterm.Cell{
			localVTermRowForTest("seed-a", 8),
			localVTermRowForTest("seed-b", 8),
		},
	}, localvterm.CursorState{Visible: true}, localvterm.TerminalModes{AutoWrap: true})
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:     "fallback-no-history-create",
		size:   Size{Cols: 8, Rows: 2},
		state:  StateRunning,
		vterm:  vt,
		stream: fanout.New(),
		grid:   store,
	}
	if err := store.AppendRows([][]localvterm.Cell{localVTermCellsFromString("hist0")}); err != nil {
		t.Fatalf("append persisted seed: %v", err)
	}
	beforeRows := store.RowCount()
	beforeLines := store.LogicalLineCount()

	term.streamMu.Lock()
	msg := term.screenSnapshotFallbackMessageLocked()
	term.streamMu.Unlock()

	if msg.Type != StreamScreenUpdate || len(msg.Payload) == 0 {
		t.Fatalf("expected fallback full-replace payload, got %#v", msg)
	}
	update, err := protocol.DecodeScreenUpdatePayload(msg.Payload)
	if err != nil {
		t.Fatalf("decode fallback payload: %v", err)
	}
	if !update.FullReplace {
		t.Fatalf("expected fallback to be authoritative full replace, got %#v", update)
	}
	if len(update.ScrollbackAppend) != 0 || update.ScrollbackTrim != 0 || update.ResetScrollback {
		t.Fatalf("expected fallback full replace not to create history rows, got %#v", update)
	}
	if got := store.RowCount(); got != beforeRows {
		t.Fatalf("expected fallback payload generation not to append committed rows, got before=%d after=%d", beforeRows, got)
	}
	if got := store.LogicalLineCount(); got != beforeLines {
		t.Fatalf("expected fallback payload generation not to create committed logical lines, got before=%d after=%d", beforeLines, got)
	}

	coreViewport, err := term.combinedGridViewport(0, 10, 8, term.primaryLiveTail.clone())
	if err != nil {
		t.Fatalf("fallback history window viewport: %v", err)
	}
	window := historyWindowFromCoreGridViewport(term.id, 0, coreViewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"hist0"}) {
		t.Fatalf("expected fallback history window to keep only committed history, got %#v", got)
	}
	if window.LoadedRows != beforeRows || window.LoadedLines != beforeLines || window.LogicalTotal != beforeLines {
		t.Fatalf("expected fallback history window counts unchanged, loaded_rows=%d loaded_lines=%d logical_total=%d", window.LoadedRows, window.LoadedLines, window.LogicalTotal)
	}
	if historyWindowContainsText(window, "seed-a") || historyWindowContainsText(window, "seed-b") {
		t.Fatalf("expected fallback not to expose live screen through history window, got %#v", window.Rows)
	}

	snap := term.Snapshot(0, 0)
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	if snap.ScrollbackTotal != beforeRows || snap.ScrollbackLoadedRows != 0 {
		t.Fatalf("expected fallback to keep committed metadata unchanged, got total=%d loaded=%d", snap.ScrollbackTotal, snap.ScrollbackLoadedRows)
	}
	if snap.HistoryGeneration == 0 {
		t.Fatal("expected committed generation to remain available after fallback")
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
		t.Fatalf("expected older window to include next grid row, got %q", got)
	}
}

func TestTerminalSnapshotPagesLiveHistoryFromDisk(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	for i := 0; i < 12; i++ {
		if err := store.AppendRows([][]localvterm.Cell{{{Content: fmt.Sprintf("row-%02d", i), Width: 1}}}); err != nil {
			t.Fatalf("append grid row %d: %v", i, err)
		}
	}
	vt := localvterm.New(8, 2, 3, nil)
	vt.LoadSnapshot(localvterm.ScreenData{Cells: [][]localvterm.Cell{
		localVTermRowForTest("live-a", 8),
		localVTermRowForTest("live-b", 8),
	}}, localvterm.CursorState{Visible: true}, localvterm.TerminalModes{AutoWrap: true})
	term := &Terminal{
		id:    "disk-snap-1",
		size:  Size{Cols: 8, Rows: 2},
		vterm: vt,
		grid:  store,
	}

	screenOnly := term.Snapshot(0, 0)
	if len(screenOnly.Scrollback) != 0 || screenOnly.ScrollbackTotal != 12 || !screenOnly.ScrollbackHasMore {
		t.Fatalf("expected screen-only snapshot with disk history metadata, got rows=%d total=%d has_more=%v", len(screenOnly.Scrollback), screenOnly.ScrollbackTotal, screenOnly.ScrollbackHasMore)
	}
	if got := snapshotRowString(screenOnly.Screen.Cells[0]); !strings.Contains(got, "live-a") {
		t.Fatalf("expected screen-only snapshot to keep live screen, got %q", got)
	}

	latest := term.Snapshot(0, 5)
	if len(latest.Scrollback) != 5 || latest.ScrollbackTotal != 12 || !latest.ScrollbackHasMore {
		t.Fatalf("expected latest disk snapshot page with more history, got rows=%d total=%d has_more=%v", len(latest.Scrollback), latest.ScrollbackTotal, latest.ScrollbackHasMore)
	}
	if got := snapshotRowString(latest.Scrollback[0]); !strings.Contains(got, "row-07") {
		t.Fatalf("expected latest page to start at row-07, got %q", got)
	}
	if got := snapshotRowString(latest.Scrollback[4]); !strings.Contains(got, "row-11") {
		t.Fatalf("expected latest page to end at row-11, got %q", got)
	}
	if got := snapshotRowString(latest.Screen.Cells[0]); !strings.Contains(got, "live") {
		t.Fatalf("expected current live screen to come from vterm, got %q", got)
	}

	older := term.Snapshot(5, 5)
	if len(older.Scrollback) != 5 || older.ScrollbackTotal != 12 || !older.ScrollbackHasMore {
		t.Fatalf("expected older disk snapshot page with more history, got rows=%d total=%d has_more=%v", len(older.Scrollback), older.ScrollbackTotal, older.ScrollbackHasMore)
	}
	if got := snapshotRowString(older.Scrollback[0]); !strings.Contains(got, "row-02") {
		t.Fatalf("expected older page to start at row-02, got %q", got)
	}
	if got := snapshotRowString(older.Scrollback[4]); !strings.Contains(got, "row-06") {
		t.Fatalf("expected older page to end at row-06, got %q", got)
	}

	oldest := term.Snapshot(10, 5)
	if len(oldest.Scrollback) != 2 || oldest.ScrollbackTotal != 12 || oldest.ScrollbackHasMore {
		t.Fatalf("expected oldest disk snapshot page without more history, got rows=%d total=%d has_more=%v", len(oldest.Scrollback), oldest.ScrollbackTotal, oldest.ScrollbackHasMore)
	}
	if got := snapshotRowString(oldest.Scrollback[0]); !strings.Contains(got, "row-00") {
		t.Fatalf("expected oldest page to start at row-00, got %q", got)
	}
}

func TestTerminalSnapshotMetadataOnlyPreservesCanonicalPersistedWindowAfterRetention(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	store.SetMaxRows(500)
	for i := 0; i < 1000; i++ {
		if err := store.AppendRows([][]localvterm.Cell{{{Content: fmt.Sprintf("row-%04d", i), Width: 1}}}); err != nil {
			t.Fatalf("append grid row %d: %v", i, err)
		}
	}

	vt := localvterm.New(8, 2, 3, nil)
	vt.LoadSnapshot(localvterm.ScreenData{Cells: [][]localvterm.Cell{
		localVTermRowForTest("live-a", 8),
		localVTermRowForTest("live-b", 8),
	}}, localvterm.CursorState{Visible: true}, localvterm.TerminalModes{AutoWrap: true})
	term := &Terminal{
		id:    "disk-snap-retained-metadata",
		size:  Size{Cols: 8, Rows: 2},
		vterm: vt,
		grid:  store,
	}

	latest := term.Snapshot(0, 0)
	if latest == nil {
		t.Fatal("expected latest metadata-only snapshot")
	}
	if latest.ScrollbackTotal != 500 || !latest.ScrollbackHasMore {
		t.Fatalf("expected retained metadata total=500 has_more=true, got total=%d has_more=%v", latest.ScrollbackTotal, latest.ScrollbackHasMore)
	}
	if latest.ScrollbackLoadedRows != 0 {
		t.Fatalf("expected latest metadata-only snapshot to keep metadata-only depth at 0, got %d", latest.ScrollbackLoadedRows)
	}
	if latest.HistoryGeneration == 0 {
		t.Fatal("expected latest metadata-only snapshot to keep history generation")
	}
	if latest.ScrollbackFirstRowID != 500 || latest.ScrollbackLastRowID != 999 {
		t.Fatalf("expected latest metadata-only canonical window 500..999, got %d..%d", latest.ScrollbackFirstRowID, latest.ScrollbackLastRowID)
	}

	older := term.Snapshot(250, 0)
	if older == nil {
		t.Fatal("expected older metadata-only snapshot")
	}
	if older.ScrollbackTotal != 500 || !older.ScrollbackHasMore {
		t.Fatalf("expected older metadata total=500 has_more=true, got total=%d has_more=%v", older.ScrollbackTotal, older.ScrollbackHasMore)
	}
	if older.ScrollbackLoadedRows != 250 {
		t.Fatalf("expected older metadata-only snapshot to keep committed depth 250, got %d", older.ScrollbackLoadedRows)
	}
	if older.HistoryGeneration != latest.HistoryGeneration {
		t.Fatalf("expected older metadata-only snapshot to keep generation %d, got %d", latest.HistoryGeneration, older.HistoryGeneration)
	}
	if older.ScrollbackFirstRowID != 500 || older.ScrollbackLastRowID != 749 {
		t.Fatalf("expected older metadata-only canonical window 500..749, got %d..%d", older.ScrollbackFirstRowID, older.ScrollbackLastRowID)
	}
}

func TestTerminalHistoryReplayReturnsNewestReplayWindow(t *testing.T) {
	vt := localvterm.New(4, 2, 16, nil)
	if _, err := vt.Write([]byte("1\n2\n3\n4\n5\n")); err != nil {
		t.Fatalf("write scrollback seed failed: %v", err)
	}

	term := &Terminal{
		id:    "hist-1",
		size:  Size{Cols: 4, Rows: 2},
		vterm: vt,
	}

	latest := term.HistoryReplay(HistoryReplayOptions{BeforeOffset: 0, Limit: 2})
	if latest.Rows != 2 || latest.Replay == "" {
		t.Fatalf("expected latest replay rows, got %#v", latest)
	}
	if !strings.Contains(latest.Replay, "3") || !strings.Contains(latest.Replay, "4") {
		t.Fatalf("expected latest replay to contain newest scrollback rows, got %q", latest.Replay)
	}

	older := term.HistoryReplay(HistoryReplayOptions{BeforeOffset: 2, Limit: 2})
	if older.Rows != 2 || older.Replay == "" {
		t.Fatalf("expected older replay rows, got %#v", older)
	}
	if !strings.Contains(older.Replay, "1") || !strings.Contains(older.Replay, "2") {
		t.Fatalf("expected older replay to contain earlier scrollback rows, got %q", older.Replay)
	}
}

func TestTerminalHistoryReplayClampsRequestWindow(t *testing.T) {
	const legacyClamp = 250
	vt := localvterm.New(16, 2, maxGridReplayRows+32, nil)
	for i := 0; i < maxGridReplayRows+20; i++ {
		if _, err := vt.Write([]byte(fmt.Sprintf("row-%03d\r\n", i))); err != nil {
			t.Fatalf("write scrollback seed %d failed: %v", i, err)
		}
	}

	term := &Terminal{
		id:    "hist-clamp-1",
		size:  Size{Cols: 16, Rows: 2},
		vterm: vt,
	}

	replay := term.HistoryReplay(HistoryReplayOptions{BeforeOffset: -10, Limit: maxGridReplayRows + 1000})
	if replay.BeforeOffset != 0 {
		t.Fatalf("expected negative beforeOffset to clamp to 0, got %d", replay.BeforeOffset)
	}
	if replay.Limit != maxGridReplayRows+1000 {
		t.Fatalf("expected replay limit to preserve requested window, got %d", replay.Limit)
	}
	if replay.Rows <= legacyClamp {
		t.Fatalf("expected replay rows to exceed legacy clamp %d, got %d", legacyClamp, replay.Rows)
	}
	if replay.Replay == "" {
		t.Fatal("expected clamped replay payload")
	}
}

func TestTerminalGridViewportAllowsLargeHistoryWindow(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	rows := make([][]localvterm.Cell, 0, 1000)
	for i := 0; i < 1000; i++ {
		rows = append(rows, []localvterm.Cell{{Content: fmt.Sprintf("row-%04d", i), Width: 1}})
	}
	if err := store.AppendRows(rows); err != nil {
		t.Fatalf("append rows: %v", err)
	}

	viewport, err := store.Viewport(0, 1000, 16)
	if err != nil {
		t.Fatalf("viewport: %v", err)
	}
	if got := viewport.LoadedRows; got != 1000 {
		t.Fatalf("expected loaded rows 1000, got %d", got)
	}
	if got := viewport.TotalRows; got != 1000 {
		t.Fatalf("expected total rows 1000, got %d", got)
	}
	if len(viewport.Rows) == 0 {
		t.Fatal("expected viewport rows")
	}
	first := ""
	for _, cell := range viewport.Rows[0] {
		if cell.Width > 0 {
			first += cell.Content
		}
	}
	if !strings.Contains(first, "row-0000") {
		t.Fatalf("expected oldest row in viewport, got %q", first)
	}
	last := viewport.Rows[len(viewport.Rows)-1]
	lastText := ""
	for _, cell := range last {
		if cell.Width > 0 {
			lastText += cell.Content
		}
	}
	if !strings.Contains(lastText, "row-0999") {
		t.Fatalf("expected newest row in viewport, got %q", lastText)
	}
}

func TestTerminalGridViewportWithOptionsPreservesLargeRequestedLimit(t *testing.T) {
	const legacyClamp = 250
	vt := localvterm.New(16, 2, 1100, nil)
	for i := 0; i < 1000; i++ {
		if _, err := vt.Write([]byte(fmt.Sprintf("row-%04d\r\n", i))); err != nil {
			t.Fatalf("write scrollback seed %d failed: %v", i, err)
		}
	}
	term := &Terminal{
		id:    "grid-viewport-large-1",
		size:  Size{Cols: 16, Rows: 2},
		vterm: vt,
	}
	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackLimit: 1000, Cols: 16})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	if viewport.ScrollbackLimit != 1000 {
		t.Fatalf("expected requested viewport limit 1000 preserved, got %d", viewport.ScrollbackLimit)
	}
	if viewport.ScrollbackTotal <= legacyClamp {
		t.Fatalf("expected retained rows to exceed legacy clamp %d, got total=%d", legacyClamp, viewport.ScrollbackTotal)
	}
	if len(viewport.Rows) <= legacyClamp {
		t.Fatalf("expected viewport row window to exceed legacy clamp %d, got %d rows", legacyClamp, len(viewport.Rows))
	}
}

func TestTerminalGridStoreViewportLoadedRowsTrackOffsetDepth(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	rows := make([][]localvterm.Cell, 0, 1000)
	for i := 0; i < 1000; i++ {
		rows = append(rows, []localvterm.Cell{{Content: fmt.Sprintf("row-%04d", i), Width: 1}})
	}
	if err := store.AppendRows(rows); err != nil {
		t.Fatalf("append rows: %v", err)
	}

	viewport, err := store.Viewport(502, 500, 16)
	if err != nil {
		t.Fatalf("viewport: %v", err)
	}
	if got, want := viewport.LoadedRows, 1000; got != want {
		t.Fatalf("expected loaded rows to report raw committed depth %d, got %d", want, got)
	}
	if got := len(viewport.Rows); got < 400 {
		t.Fatalf("expected a large materialized row window after offset paging, got %d", got)
	}
}

func TestTerminalHistoryReplayReadsBeyondShortLiveScrollback(t *testing.T) {
	vt := localvterm.New(12, 2, 3, nil)
	term := &Terminal{
		id:     "hist-store-1",
		size:   Size{Cols: 12, Rows: 2},
		vterm:  vt,
		stream: fanout.New(),
		grid:   newMemoryTerminalGridStoreForTest(t),
	}
	defer term.grid.Close()

	for i := 0; i < 12; i++ {
		_, err, damage := vt.WriteWithDamage([]byte(fmt.Sprintf("row-%02d\r\n", i)))
		if err != nil {
			t.Fatalf("write row %d: %v", i, err)
		}
		term.appendGridFromDamageLocked(damage)
	}

	if got := len(vt.ScrollbackContent()); got > 3 {
		t.Fatalf("expected short live scrollback, got %d rows", got)
	}
	older := term.HistoryReplay(HistoryReplayOptions{BeforeOffset: 8, Limit: 2})
	if older.Rows != 2 || older.Replay == "" {
		t.Fatalf("expected older grid rows from store, got %#v", older)
	}
	plainReplay := stripANSIForTest(older.Replay)
	if !strings.Contains(plainReplay, "row-01") || !strings.Contains(plainReplay, "row-02") {
		t.Fatalf("expected replay beyond live scrollback to contain oldest stored rows, got %q", older.Replay)
	}
}

func TestTerminalGridStoreReopensPersistedRows(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "persist-1")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	rows := [][]localvterm.Cell{
		{{Content: "old-1", Width: 1}},
		{{Content: "old-2", Width: 1}},
		{{Content: "old-3", Width: 1}},
	}
	if err := store.AppendRows(rows); err != nil {
		t.Fatalf("append rows: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := openTerminalGridStoreForReplay(root, "persist-1")
	if err != nil {
		t.Fatalf("reopen grid store: %v", err)
	}
	defer reopened.Close()
	replay, count, hasMore, err := reopened.Replay(0, 2)
	if err != nil {
		t.Fatalf("replay reopened grid: %v", err)
	}
	if count != 2 || !hasMore {
		t.Fatalf("expected 2 replay rows with more history, got rows=%d hasMore=%v", count, hasMore)
	}
	plainReplay := stripANSIForTest(string(replay))
	if !strings.Contains(plainReplay, "old-2") || !strings.Contains(plainReplay, "old-3") {
		t.Fatalf("expected reopened replay to contain newest rows, got %q", plainReplay)
	}
	decoded, err := reopened.Viewport(0, 3, 12)
	if err != nil {
		t.Fatalf("decode reopened rows: %v", err)
	}
	if decoded.LoadedRows != 3 || decoded.HasMore {
		t.Fatalf("expected all reopened rows, got rows=%d hasMore=%v", decoded.LoadedRows, decoded.HasMore)
	}
	if got := vtermRowToString(decoded.Rows[0]); !strings.Contains(got, "old-1") {
		t.Fatalf("expected first decoded row from disk, got %q", got)
	}
}

func TestTerminalGridStoreRetentionCapsCommittedRows(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	store.pageMaxBytes = 1
	store.SetMaxRows(3)

	for i := 0; i < 6; i++ {
		if err := store.AppendRows([][]localvterm.Cell{{{Content: fmt.Sprintf("row-%d", i), Width: 1}}}); err != nil {
			t.Fatalf("append row %d: %v", i, err)
		}
	}
	if got := store.RowCount(); got != 3 {
		t.Fatalf("expected retained row count 3 for 3 one-row logical lines, got %d", got)
	}
	viewport, err := store.Viewport(0, 10, 12)
	if err != nil {
		t.Fatalf("viewport after retention: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"row-3", "row-4", "row-5"}) {
		t.Fatalf("expected newest retained rows, got %#v", got)
	}
	if viewport.HasMore || viewport.TotalRows != 3 {
		t.Fatalf("expected retention to expose only committed retained rows, got total=%d hasMore=%v", viewport.TotalRows, viewport.HasMore)
	}
	if viewport.FirstRowID != 3 || viewport.LastRowID != 5 || viewport.Generation == 0 {
		t.Fatalf("expected retained row coordinates to track dropped rows, got generation=%d first=%d last=%d", viewport.Generation, viewport.FirstRowID, viewport.LastRowID)
	}
	lineMetadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read retained line metadata: %v", err)
	}
	if len(lineMetadata.Records) != 3 {
		t.Fatalf("expected retained sidecar to contain 3 logical line records, got %#v", lineMetadata.Records)
	}
	for i, record := range lineMetadata.Records {
		if record.ID != uint64(4+i) || record.StartRow != i || record.EndRow != i || !record.Sealed || record.Origin != terminalLiveTailOriginReclaimed || record.Residency != terminalLogicalLineResidencyPersisted || record.Dirty || record.Generation != viewport.Generation {
			t.Fatalf("unexpected retained sidecar record %d: %#v viewport=%#v", i, record, viewport)
		}
	}

	refs, err := readAllTerminalGridIndexRefs(store.dir)
	if err != nil {
		t.Fatalf("read retained refs: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 retained refs, got %d", len(refs))
	}
	pages := terminalGridPageFilesForTest(t, store.dir)
	if reflect.DeepEqual(pages, []string{"grid-000000.page", "grid-000001.page", "grid-000002.page", "grid-000003.page", "grid-000004.page", "grid-000005.page"}) {
		t.Fatalf("expected unreferenced old pages pruned, got %#v", pages)
	}
	if !reflect.DeepEqual(pages, []string{"grid-000003.page", "grid-000004.page", "grid-000005.page"}) {
		t.Fatalf("expected only retained page files, got %#v", pages)
	}
}

func TestTerminalGridStoreAppendRefreshesPersistedLineMetadata(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	if err := store.AppendRows([][]localvterm.Cell{
		localVTermCellsFromString("one"),
		localVTermCellsFromString("two"),
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}

	_, generation, _ := store.coordinates()
	lineMetadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read line metadata after append: %v", err)
	}
	want := []terminalGridLineRecordMeta{
		{ID: 1, StartRow: 0, EndRow: 0, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyPersisted, Generation: generation},
		{ID: 2, StartRow: 1, EndRow: 1, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyPersisted, Generation: generation},
	}
	if !reflect.DeepEqual(lineMetadata.Records, want) {
		t.Fatalf("expected append to refresh persisted line metadata, got %#v want %#v", lineMetadata.Records, want)
	}
}

func TestTerminalGridStoreLineMetadataKeepsSealedPrefixBeforeWrappedTail(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("sealed")},
		{cells: localVTermCellsFromString("prefix"), wrapped: true},
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}

	_, generation, _ := store.coordinates()
	lineMetadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read line metadata after append: %v", err)
	}
	want := []terminalGridLineRecordMeta{
		{ID: 1, StartRow: 0, EndRow: 0, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyPersisted, Generation: generation},
	}
	if !reflect.DeepEqual(lineMetadata.Records, want) {
		t.Fatalf("expected sidecar to keep sealed persisted prefix only, got %#v want %#v", lineMetadata.Records, want)
	}
}

func TestTerminalGridStoreRetentionDropsPartialWrappedLogicalLine(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	store.SetMaxRows(3)

	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("A"), wrapped: true},
		{cells: localVTermCellsFromString("B"), wrapped: true},
		{cells: localVTermCellsFromString("C")},
		{cells: localVTermCellsFromString("D")},
		{cells: localVTermCellsFromString("E")},
	}); err != nil {
		t.Fatalf("append wrapped rows: %v", err)
	}
	if got := store.RowCount(); got != 5 {
		t.Fatalf("expected retention budget of 3 logical lines to keep the wrapped logical line plus D and E, got %d rows", got)
	}
	viewport, err := store.Viewport(0, 10, 10)
	if err != nil {
		t.Fatalf("viewport after wrapped retention: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"ABC", "D", "E"}) {
		t.Fatalf("expected retained rows to begin at a logical boundary, got %#v", got)
	}
	if viewport.FirstRowID != 0 || viewport.LastRowID != 4 {
		t.Fatalf("expected row IDs to reflect extra dropped wrapped rows, got first=%d last=%d", viewport.FirstRowID, viewport.LastRowID)
	}
}

func TestTerminalGridStoreRetentionCountsLogicalLinesAcrossWrappedRows(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	store.SetMaxRows(3)

	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("A0"), wrapped: true},
		{cells: localVTermCellsFromString("A1")},
		{cells: localVTermCellsFromString("B")},
		{cells: localVTermCellsFromString("C0"), wrapped: true},
		{cells: localVTermCellsFromString("C1")},
		{cells: localVTermCellsFromString("D")},
	}); err != nil {
		t.Fatalf("append wrapped rows: %v", err)
	}

	if got := store.RowCount(); got != 4 {
		t.Fatalf("expected logical-line retention to keep wrapped rows for the newest 3 logical lines, got %d rows", got)
	}
	viewport, err := store.Viewport(0, 10, 10)
	if err != nil {
		t.Fatalf("viewport after logical-line retention: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"B", "C0C1", "D"}) {
		t.Fatalf("expected retention to keep complete newest logical lines, got %#v", got)
	}
	if viewport.FirstRowID != 2 || viewport.LastRowID != 5 {
		t.Fatalf("expected row IDs to reflect dropping only the oldest logical line, got first=%d last=%d", viewport.FirstRowID, viewport.LastRowID)
	}
}

func TestTerminalGridStoreRetentionUsesSealedMetadataPrefix(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("aa")},
		{cells: localVTermCellsFromString("bb")},
		{cells: localVTermCellsFromString("tail")},
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}
	_, generation, _ := store.coordinates()
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{Records: []terminalGridLineRecordMeta{
		{ID: 91, StartRow: 0, EndRow: 1, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyPersisted, Generation: generation},
	}}); err != nil {
		t.Fatalf("write partial line metadata: %v", err)
	}

	store.SetMaxRows(2)
	if err := store.enforceMaxRowsLockedAt(time.Now().UTC()); err != nil {
		t.Fatalf("enforce retention: %v", err)
	}

	viewport, err := store.Viewport(0, 10, 10)
	if err != nil {
		t.Fatalf("viewport after metadata-prefix retention: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"aa", "bb", "tail"}) {
		t.Fatalf("expected retention to keep metadata prefix rows and tail, got %#v", got)
	}
	if viewport.FirstRowID != 0 || viewport.LastRowID != 2 || store.RowCount() != 3 {
		t.Fatalf("expected retention not to cut metadata prefix, rows=%d first=%d last=%d", store.RowCount(), viewport.FirstRowID, viewport.LastRowID)
	}
	prefix, err := store.Viewport(1, 10, 10)
	if err != nil {
		t.Fatalf("prefix viewport after metadata-prefix retention: %v", err)
	}
	if got := vtermRowsToStrings(prefix.Rows); !reflect.DeepEqual(got, []string{"aabb"}) {
		t.Fatalf("expected retained metadata prefix to remain one logical line, got %#v", got)
	}
}

func TestTerminalGridFallbackLogicalLineRecordsExposePersistedBoundaries(t *testing.T) {
	refs := []terminalGridRowRef{
		{flags: terminalGridRowFlagWrapped},
		{},
		{},
		{flags: terminalGridRowFlagWrapped},
	}
	records := terminalGridFallbackLogicalLineRecordsForRefs(refs, 40)
	want := []terminalGridLogicalLineRecord{
		{id: 41, startRow: 0, endRow: 1, sealed: true, origin: terminalLiveTailOriginReclaimed, residency: terminalLogicalLineResidencyPersisted},
		{id: 43, startRow: 2, endRow: 2, sealed: true, origin: terminalLiveTailOriginReclaimed, residency: terminalLogicalLineResidencyPersisted},
		{id: 44, startRow: 3, endRow: 3, sealed: false, origin: terminalLiveTailOriginReclaimed, residency: terminalLogicalLineResidencyPersisted},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("unexpected fallback logical line records got %#v want %#v", records, want)
	}

	generated := terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs, 40, 9)
	if len(generated) != len(want) {
		t.Fatalf("expected generated records count %d, got %#v", len(want), generated)
	}
	for _, record := range generated {
		if record.generation != 9 || record.dirty {
			t.Fatalf("expected persisted record generation=9 dirty=false, got %#v", record)
		}
	}
}

func TestTerminalGridWindowStartUsesLogicalLineRecords(t *testing.T) {
	records := []terminalGridLogicalLineRecord{
		{id: 1, startRow: 0, endRow: 2, sealed: true, residency: terminalLogicalLineResidencyPersisted},
		{id: 4, startRow: 3, endRow: 3, sealed: true, residency: terminalLogicalLineResidencyPersisted},
	}
	if got := terminalGridWindowStartForRecords(records, 2); got != 0 {
		t.Fatalf("expected window inside first logical line to rewind to row 0, got %d", got)
	}
	if got := terminalGridWindowStartForRecords(records, 3); got != 3 {
		t.Fatalf("expected window at independent logical line to stay at row 3, got %d", got)
	}
}

func TestTerminalGridStoreRetentionCountsTrailingWrappedPrefixAsLogicalLine(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	store.SetMaxRows(1)

	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("old")},
		{cells: localVTermCellsFromString("live-prefix"), wrapped: true},
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}

	if got := store.RowCount(); got != 1 {
		t.Fatalf("expected trailing wrapped prefix to count as one retained logical line, got %d rows", got)
	}
	viewport, err := store.Viewport(0, 10, 40)
	if err != nil {
		t.Fatalf("viewport after trailing wrapped retention: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"live-prefix"}) {
		t.Fatalf("expected oldest completed line dropped in favor of trailing wrapped prefix, got %#v", got)
	}
	if viewport.FirstRowID != 1 || viewport.LastRowID != 1 {
		t.Fatalf("expected retained row IDs to point at wrapped prefix row, got first=%d last=%d", viewport.FirstRowID, viewport.LastRowID)
	}
}

func TestTerminalGridStoreViewportReportsLogicalTotals(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("A0"), wrapped: true},
		{cells: localVTermCellsFromString("A1")},
		{cells: localVTermCellsFromString("B")},
		{cells: localVTermCellsFromString("C"), wrapped: true},
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}

	viewport, err := store.Viewport(0, 10, 20)
	if err != nil {
		t.Fatalf("viewport: %v", err)
	}
	if viewport.TotalRows != 4 {
		t.Fatalf("expected committed-row total 4, got %d", viewport.TotalRows)
	}
	if viewport.LogicalTotal != 3 {
		t.Fatalf("expected logical-line total 3, got %d", viewport.LogicalTotal)
	}
}

func TestTerminalGridStoreRetentionByteLimitKeepsNewestRowsWithinBudget(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rowA := terminalGridRow{cells: []localvterm.Cell{{Content: "aaaa", Width: 1}}}
	rowB := terminalGridRow{cells: []localvterm.Cell{{Content: "bbbb", Width: 1}}}
	rowC := terminalGridRow{cells: []localvterm.Cell{{Content: "cccc", Width: 1}}}
	payloadC, err := encodeTerminalGridRow(rowC)
	if err != nil {
		t.Fatalf("encode rowC: %v", err)
	}
	store.SetRetentionPolicy(terminalGridRetentionPolicy{maxRetainedBytes: int64(len(payloadC))})

	if err := store.AppendRows([][]localvterm.Cell{rowA.cells, rowB.cells, rowC.cells}); err != nil {
		t.Fatalf("append rows: %v", err)
	}

	viewport, err := store.Viewport(0, 10, 20)
	if err != nil {
		t.Fatalf("viewport after byte retention: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"cccc"}) {
		t.Fatalf("expected byte retention to keep only newest row within budget, got %#v", got)
	}
}

func TestTerminalGridStoreRetentionByteLimitKeepsCompleteNewestLogicalLine(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	prefix := terminalGridRow{cells: []localvterm.Cell{{Content: "tail-prefix", Width: 1}}, wrapped: true}
	suffix := terminalGridRow{cells: []localvterm.Cell{{Content: "tail-suffix", Width: 1}}}
	payloadSuffix, err := encodeTerminalGridRow(suffix)
	if err != nil {
		t.Fatalf("encode suffix row: %v", err)
	}
	store.SetRetentionPolicy(terminalGridRetentionPolicy{maxRetainedBytes: int64(len(payloadSuffix))})

	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("old")},
		prefix,
		suffix,
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}

	viewport, err := store.Viewport(0, 10, 40)
	if err != nil {
		t.Fatalf("viewport after byte retention: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"tail-prefixtail-suffix"}) {
		t.Fatalf("expected byte retention to keep complete newest logical line, got %#v", got)
	}
	if viewport.FirstRowID != 1 || viewport.LastRowID != 2 {
		t.Fatalf("expected retained row ids for complete logical line, got first=%d last=%d", viewport.FirstRowID, viewport.LastRowID)
	}
}

func TestTerminalGridStoreRetentionPolicyUsesSmallestBudget(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rowA := terminalGridRow{cells: []localvterm.Cell{{Content: "aaaa", Width: 1}}}
	rowB := terminalGridRow{cells: []localvterm.Cell{{Content: "bbbb", Width: 1}}}
	rowC := terminalGridRow{cells: []localvterm.Cell{{Content: "cccc", Width: 1}}}
	payloadC, err := encodeTerminalGridRow(rowC)
	if err != nil {
		t.Fatalf("encode rowC: %v", err)
	}
	store.SetRetentionPolicy(terminalGridRetentionPolicy{
		maxLogicalLines:  2,
		maxRetainedBytes: int64(len(payloadC)),
	})

	if err := store.AppendRows([][]localvterm.Cell{rowA.cells, rowB.cells, rowC.cells}); err != nil {
		t.Fatalf("append rows: %v", err)
	}

	viewport, err := store.Viewport(0, 10, 20)
	if err != nil {
		t.Fatalf("viewport after combined retention: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"cccc"}) {
		t.Fatalf("expected smallest retention budget to win, got %#v", got)
	}
}

func TestTerminalGridStoreRetentionAgeLimitDropsOldLogicalLines(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)
	fresh := now.Add(-30 * time.Minute)
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("old0"), timestamp: old},
		{cells: localVTermCellsFromString("old1"), timestamp: old},
		{cells: localVTermCellsFromString("fresh0"), timestamp: fresh},
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}
	store.SetRetentionPolicy(terminalGridRetentionPolicy{maxAge: time.Hour})
	if err := store.enforceMaxRowsLockedAt(now); err != nil {
		t.Fatalf("enforce retention at time: %v", err)
	}
	viewport, err := store.Viewport(0, 10, 20)
	if err != nil {
		t.Fatalf("viewport after age retention: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"fresh0"}) {
		t.Fatalf("expected age retention to drop old logical lines, got %#v", got)
	}
}

func TestTerminalGridStoreRetentionBumpsGenerationAndRejectsOldCoordinates(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	store.SetMaxRows(3)

	for i := 0; i < 6; i++ {
		if err := store.AppendRows([][]localvterm.Cell{{{Content: fmt.Sprintf("row-%d", i), Width: 1}}}); err != nil {
			t.Fatalf("append row %d: %v", i, err)
		}
	}

	viewport, err := store.Viewport(0, 10, 20)
	if err != nil {
		t.Fatalf("viewport after retention: %v", err)
	}
	if viewport.Generation == 0 {
		t.Fatal("expected non-zero generation after retention rewrite")
	}
	if viewport.FirstRowID != 3 || viewport.LastRowID != 5 {
		t.Fatalf("expected retained coordinates 3..5, got first=%d last=%d", viewport.FirstRowID, viewport.LastRowID)
	}
	if viewport.LoadedRows != 3 {
		t.Fatalf("expected loaded depth to match retained committed rows, got %d", viewport.LoadedRows)
	}
}

func TestTerminalGridResizeDamageAfterTrailingWrappedRetentionKeepsBoundary(t *testing.T) {
	vt := localvterm.New(8, 2, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	store.SetMaxRows(1)
	term := &Terminal{
		id:    "retention-resize-boundary",
		size:  Size{Cols: 8, Rows: 2},
		vterm: vt,
		grid:  store,
	}

	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("old")},
		{cells: localVTermCellsFromString("livepref"), wrapped: true},
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}
	if got := store.RowCount(); got != 1 {
		t.Fatalf("expected retention to leave only trailing wrapped prefix row, got %d", got)
	}

	vt.LoadSnapshot(localvterm.ScreenData{
		Cells: [][]localvterm.Cell{
			localVTermCellsFromString("ix-tail"),
			localVTermCellsFromString("done"),
		},
	}, localvterm.CursorState{Row: 1, Col: 4, Visible: true}, localvterm.TerminalModes{AutoWrap: true})

	term.appendGridFromDamageLocked(vt.ResizeWithDamage(4, 2))
	snapshot := term.Snapshot(0, 10)
	if snapshot == nil {
		t.Fatal("expected snapshot after resize")
	}
	combinedRows := make([]string, 0, len(snapshot.Scrollback)+len(snapshot.Screen.Cells))
	for _, row := range snapshot.Scrollback {
		combinedRows = append(combinedRows, rowToString(row))
	}
	for _, row := range snapshot.Screen.Cells {
		combinedRows = append(combinedRows, rowToString(row))
	}
	if !stringRowsContain(combinedRows, "live") || !stringRowsContain(combinedRows, "pref") {
		t.Fatalf("expected retained wrapped prefix to survive resize boundary, got %#v", combinedRows)
	}
	if len(snapshot.ScrollbackWrapped) == 0 || !snapshot.ScrollbackWrapped[len(snapshot.ScrollbackWrapped)-1] {
		t.Fatalf("expected retained wrapped prefix to continue into live screen after resize, got %#v", snapshot.ScrollbackWrapped)
	}
}

func TestTerminalGridStoreViewportLoadedRowsUseRawCommittedRows(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("abcdefghij"), wrapped: true},
		{cells: localVTermCellsFromString("kl")},
	}); err != nil {
		t.Fatalf("append wrapped rows: %v", err)
	}
	viewport, err := store.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("viewport: %v", err)
	}
	gotRows := vtermRowsToStrings(viewport.Rows)
	if len(gotRows) <= viewport.LoadedRows {
		t.Fatalf("test setup expected reflow to materialize more visual rows than raw rows, got rows=%#v loaded=%d", gotRows, viewport.LoadedRows)
	}
	if viewport.LoadedRows != 2 || viewport.FirstRowID != 0 || viewport.LastRowID != 1 {
		t.Fatalf("expected raw committed row coordinates, got loaded=%d first=%d last=%d rows=%#v", viewport.LoadedRows, viewport.FirstRowID, viewport.LastRowID, gotRows)
	}
}

func TestTerminalGridStoreUsesCompactBinaryRows(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "compact-1")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	row := terminalGridRow{
		cells: []localvterm.Cell{
			{Content: "A", Width: 1},
			{Content: "界", Width: 2, Style: localvterm.CellStyle{FG: "ansi:2", BG: "idx:17", Bold: true, Underline: true}, LinkURL: "https://example.test", LinkParams: "id=grid"},
		},
		timestamp: time.Unix(12, 345).UTC(),
		rowKind:   SnapshotRowKindRestart,
		wrapped:   true,
	}
	if err := store.appendRows([]terminalGridRow{row}); err != nil {
		t.Fatalf("append compact row: %v", err)
	}
	dir := store.dir
	if err := store.Close(); err != nil {
		t.Fatalf("close compact store: %v", err)
	}

	metadata, err := readTerminalGridMetadata(dir)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if metadata.StoreVersion != terminalGridStoreVersion || metadata.RowCodec != terminalGridRowCodec {
		t.Fatalf("unexpected metadata store=%d row_codec=%q", metadata.StoreVersion, metadata.RowCodec)
	}
	lineMetadata, err := readTerminalGridLineMetadata(dir)
	if err != nil {
		t.Fatalf("read line metadata: %v", err)
	}
	if len(lineMetadata.Records) != 0 {
		t.Fatalf("expected unsealed wrapped prefix to omit persisted line metadata, got %#v metadata=%#v", lineMetadata.Records, metadata)
	}
	page, err := os.ReadFile(filepath.Join(dir, terminalGridPageName(0)))
	if err != nil {
		t.Fatalf("read compact page: %v", err)
	}
	if !bytes.HasPrefix(page, []byte{terminalGridRowMagic0, terminalGridRowMagic1, terminalGridRowMagic2, terminalGridRowMagic3}) {
		t.Fatalf("expected compact row magic prefix, got %q", page[:minInt(len(page), 8)])
	}
	if bytes.Contains(page, []byte(`"cells"`)) || bytes.Contains(page, []byte(`"row_kind"`)) {
		t.Fatalf("expected compact binary grid page, got JSON-looking payload %q", page)
	}

	reopened, err := openTerminalGridStoreForReplay(root, "compact-1")
	if err != nil {
		t.Fatalf("reopen compact store: %v", err)
	}
	defer reopened.Close()
	refs, refCount, _, err := reopened.windowRefs(0, 1)
	if err != nil {
		t.Fatalf("read compact row refs: %v", err)
	}
	if refCount != 1 {
		t.Fatalf("expected one compact row ref, got %d", refCount)
	}
	rawRows, err := readTerminalGridRows(reopened.dir, refs)
	if err != nil {
		t.Fatalf("read compact raw rows: %v", err)
	}
	if len(rawRows) != 1 {
		t.Fatalf("expected one compact raw row, got %d", len(rawRows))
	}
	if got := rawRows[0].rowKind; got != SnapshotRowKindRestart {
		t.Fatalf("expected raw row kind round trip, got %q", got)
	}
	if got := rawRows[0].timestamp; !got.Equal(row.timestamp) {
		t.Fatalf("expected raw timestamp round trip, got %v want %v", got, row.timestamp)
	}
	if !rawRows[0].wrapped {
		t.Fatalf("expected raw wrapped flag round trip")
	}

	decoded, err := reopened.Viewport(0, 1, 10)
	if err != nil {
		t.Fatalf("decode compact rows: %v", err)
	}
	if decoded.LoadedRows != 1 {
		t.Fatalf("expected one loaded row, got rows=%d", decoded.LoadedRows)
	}
	if got := vtermRowToString(decoded.Rows[0]); got != "A界" {
		t.Fatalf("expected compact row text round trip, got %q", got)
	}
	if got := decoded.Rows[0][1].Style; got != row.cells[1].Style {
		t.Fatalf("expected style round trip, got %#v want %#v", got, row.cells[1].Style)
	}
	if got := decoded.Rows[0][1]; got.LinkURL != row.cells[1].LinkURL || got.LinkParams != row.cells[1].LinkParams {
		t.Fatalf("expected link round trip, got %#v want %#v", got, row.cells[1])
	}
}

func TestTerminalGridStorePreservesTrailingBlankCells(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	row := terminalGridRow{
		cells: []localvterm.Cell{
			{Content: "A", Width: 1},
			{Content: " ", Width: 1},
			{Content: " ", Width: 1},
		},
	}
	if err := store.appendRows([]terminalGridRow{row}); err != nil {
		t.Fatalf("append row with trailing blanks: %v", err)
	}

	decoded, err := store.Viewport(0, 1, 10)
	if err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	if decoded.LoadedRows != 1 || len(decoded.Rows) != 1 {
		t.Fatalf("expected one decoded row, got loaded=%d rows=%d", decoded.LoadedRows, len(decoded.Rows))
	}
	if got := vtermRowToRawString(decoded.Rows[0]); got != "A  " {
		t.Fatalf("expected grid row to preserve trailing blanks, got %q row=%#v", got, decoded.Rows[0])
	}

	viewport := protocolGridViewportFromCore(&GridViewport{
		Rows: [][]Cell{{
			{Content: "A", Width: 1},
			{Content: " ", Width: 1},
			{Content: " ", Width: 1},
		}},
	})
	if got := protocolRowToRawString(viewport.Rows[0].DecodeCells()); got != "A  " {
		t.Fatalf("expected protocol viewport row to preserve trailing blanks, got %q row=%#v", got, viewport.Rows[0].DecodeCells())
	}
}

func TestTerminalGridStorePreservesQRPlainBlankModules(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	row := terminalGridRow{
		cells: []localvterm.Cell{
			{Content: "█", Width: 1},
			{Content: " ", Width: 1},
			{Content: " ", Width: 1},
			{Content: "▄", Width: 1},
			{Content: " ", Width: 1},
		},
	}
	if err := store.appendRows([]terminalGridRow{row}); err != nil {
		t.Fatalf("append QR-like row with plain blank modules: %v", err)
	}

	decoded, err := store.Viewport(0, 1, 10)
	if err != nil {
		t.Fatalf("decode QR-like row: %v", err)
	}
	if got := vtermRowToRawString(decoded.Rows[0]); got != "█  ▄ " {
		t.Fatalf("expected grid row to preserve QR plain blank modules, got %q row=%#v", got, decoded.Rows[0])
	}

	viewport := protocolGridViewportFromCore(&GridViewport{
		Rows: [][]Cell{{
			{Content: "█", Width: 1},
			{Content: " ", Width: 1},
			{Content: " ", Width: 1},
			{Content: "▄", Width: 1},
			{Content: " ", Width: 1},
		}},
	})
	if got := protocolRowToRawString(viewport.Rows[0].DecodeCells()); got != "█  ▄ " {
		t.Fatalf("expected protocol viewport row to preserve QR plain blank modules, got %q row=%#v", got, viewport.Rows[0].DecodeCells())
	}
}

func TestTerminalGridScrollbackPreservesShellPromptStyleRuns(t *testing.T) {
	vt := localvterm.New(24, 2, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{id: "prompt-style", grid: store}

	writeVTermDamageToGrid(t, term, vt, "\x1b[1;32mtermx%\x1b[0m echo ok\r\nnext\r\n")

	viewport, err := store.Viewport(0, 10, 24)
	if err != nil {
		t.Fatalf("read styled prompt grid viewport: %v", err)
	}
	if len(viewport.Rows) == 0 {
		t.Fatal("expected prompt row in grid viewport")
	}
	row := viewport.Rows[0]
	if got := vtermRowToString(row); got != "termx% echo ok" {
		t.Fatalf("expected prompt text in grid, got %q row=%#v", got, row)
	}
	for i := 0; i < len("termx%"); i++ {
		if got := row[i].Style; got.FG != "ansi:2" || !got.Bold {
			t.Fatalf("expected prompt cell %d to retain bold green style, got %#v", i, row[i])
		}
	}
	if got := row[len("termx%")].Style; got.FG != "" || got.Bold {
		t.Fatalf("expected text after reset to use default style, got %#v", row[len("termx%")])
	}
}

func TestTerminalGridStoreRowsReflowSoftWrappedPersistedBuffer(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	if err := store.appendRows([]terminalGridRow{
		{cells: []localvterm.Cell{{Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "c", Width: 1}, {Content: "d", Width: 1}, {Content: "e", Width: 1}}, wrapped: true},
		{cells: []localvterm.Cell{{Content: "f", Width: 1}, {Content: "g", Width: 1}}},
		{cells: []localvterm.Cell{{Content: "H", Width: 1}, {Content: "I", Width: 1}}},
	}); err != nil {
		t.Fatalf("append structured rows: %v", err)
	}

	wide, err := store.Viewport(0, 3, 10)
	if err != nil {
		t.Fatalf("read wide rows: %v", err)
	}
	if got := vtermRowToString(wide.Rows[0]); got != "abcdefg" {
		t.Fatalf("expected soft-wrapped rows to join when widened, got %q", got)
	}
	if got := vtermRowToString(wide.Rows[1]); got != "HI" {
		t.Fatalf("expected hard newline row to remain separate, got %q", got)
	}

	narrow, err := store.Viewport(0, 3, 3)
	if err != nil {
		t.Fatalf("read narrow rows: %v", err)
	}
	gotRows := make([]string, 0, len(narrow.Rows))
	for _, row := range narrow.Rows {
		gotRows = append(gotRows, vtermRowToString(row))
	}
	if !reflect.DeepEqual(gotRows, []string{"abc", "def", "g", "HI"}) {
		t.Fatalf("expected persisted buffer to reflow by requested width, got %#v", gotRows)
	}
}

func TestTerminalGridStoreRowsLoadsEnoughRawRowsForVisualLimit(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	if err := store.appendRows([]terminalGridRow{
		{cells: []localvterm.Cell{{Content: "A", Width: 1}}, wrapped: true},
		{cells: []localvterm.Cell{{Content: "B", Width: 1}}, wrapped: true},
		{cells: []localvterm.Cell{{Content: "C", Width: 1}}},
		{cells: []localvterm.Cell{{Content: "D", Width: 1}}},
	}); err != nil {
		t.Fatalf("append structured rows: %v", err)
	}

	wide, err := store.Viewport(0, 3, 10)
	if err != nil {
		t.Fatalf("read wide rows: %v", err)
	}
	gotRows := make([]string, 0, len(wide.Rows))
	for _, row := range wide.Rows {
		gotRows = append(gotRows, vtermRowToString(row))
	}
	if !reflect.DeepEqual(gotRows, []string{"ABC", "D"}) {
		t.Fatalf("expected store to read enough raw rows after soft-wrap joins, got %#v", gotRows)
	}
	if wide.HasMore {
		t.Fatal("expected complete soft-wrap group to satisfy request without hidden older rows")
	}
}

func TestTerminalGridStoreWindowStartExpandsToLogicalLine(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()

	if err := store.appendRows([]terminalGridRow{
		{cells: []localvterm.Cell{{Content: "A", Width: 1}}, wrapped: true},
		{cells: []localvterm.Cell{{Content: "B", Width: 1}}, wrapped: true},
		{cells: []localvterm.Cell{{Content: "C", Width: 1}}},
		{cells: []localvterm.Cell{{Content: "D", Width: 1}}},
	}); err != nil {
		t.Fatalf("append structured rows: %v", err)
	}
	viewport, err := store.Viewport(1, 1, 10)
	if err != nil {
		t.Fatalf("read viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"ABC"}) {
		t.Fatalf("expected viewport start to expand to logical line start, got %#v", got)
	}
	if viewport.LoadedRows != 4 {
		t.Fatalf("expected loaded raw rows to include offset depth plus complete logical line, got %d", viewport.LoadedRows)
	}
}

func TestTerminalGridResizeDamagePreservesStressTailRows(t *testing.T) {
	for _, lineCount := range []int{100, 1000} {
		t.Run(fmt.Sprintf("lines_%d", lineCount), func(t *testing.T) {
			vt := localvterm.New(32, 8, 0, nil)
			vt.DisableEmulatorScrollback()
			store := newMemoryTerminalGridStoreForTest(t)
			defer store.Close()
			term := &Terminal{id: "resize-stress", size: Size{Cols: 32, Rows: 8}, vterm: vt, grid: store}

			for i := 1; i <= lineCount; i++ {
				suffix := "\r\n"
				if i == lineCount {
					suffix = ""
				}
				writeVTermDamageToGrid(t, term, vt, fmt.Sprintf("stress-%06d payload%s", i, suffix))
			}
			term.size = Size{Cols: 16, Rows: 4}
			term.appendGridFromDamageLocked(vt.ResizeWithDamage(16, 4))

			rows := terminalProjectionRowTexts(term, 16, 250)
			for i := lineCount - 15; i <= lineCount; i++ {
				needle := fmt.Sprintf("stress-%06d", i)
				if !stringRowsContain(rows, needle) {
					t.Fatalf("expected combined grid/screen rows to contain %q after resize, rows=%#v", needle, rows)
				}
			}
		})
	}
}

func TestTerminalGridResizeDamageKeepsWrappedContinuity(t *testing.T) {
	vt := localvterm.New(5, 2, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{id: "resize-wrapped", size: Size{Cols: 5, Rows: 2}, vterm: vt, grid: store}

	writeVTermDamageToGrid(t, term, vt, "abcdefghij")
	term.size = Size{Cols: 4, Rows: 1}
	term.appendGridFromDamageLocked(vt.ResizeWithDamage(4, 1))

	screenRows := vtermRowsToStrings(vt.ScreenContent().Cells)
	if !reflect.DeepEqual(screenRows, []string{"ij"}) {
		t.Fatalf("test setup expected visible suffix on screen, got %#v wrapped=%#v", screenRows, vt.ScreenWrapped())
	}

	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected resize full-replace not to create persisted history, got %d rows", got)
	}
	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 4})
	if viewport == nil {
		t.Fatal("expected latest viewport")
	}
	gotRows := rowsToStrings(viewport.Rows)
	if !reflect.DeepEqual(gotRows, []string{"abcd", "efgh"}) {
		t.Fatalf("expected latest projection to contain wrapped prefix displaced from screen, got rows=%#v wrapped=%#v", gotRows, viewport.ScrollbackWrapped)
	}
	if len(viewport.ScrollbackWrapped) < 2 || !viewport.ScrollbackWrapped[0] || !viewport.ScrollbackWrapped[1] {
		t.Fatalf("expected wrapped metadata to keep logical line continuity, got %#v", viewport.ScrollbackWrapped)
	}
}

func TestTerminalGridResizeDamageDoesNotPersistVisibleSuffix(t *testing.T) {
	vt := localvterm.New(5, 2, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "resize-visible-suffix",
		size:  Size{Cols: 4, Rows: 2},
		vterm: vt,
		grid:  store,
	}

	writeVTermDamageToGrid(t, term, vt, "abcdefghi")
	term.appendGridFromDamageLocked(vt.ResizeWithDamage(4, 2))

	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected resize full-replace not to create persisted history, got %d rows", got)
	}
	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 4})
	if viewport == nil {
		t.Fatal("expected latest viewport")
	}
	gotRows := rowsToStrings(viewport.Rows)
	if !reflect.DeepEqual(gotRows, []string{"abcd"}) {
		t.Fatalf("expected latest projection to contain rows displaced from screen, got rows=%#v wrapped=%#v", gotRows, viewport.ScrollbackWrapped)
	}
	if len(viewport.ScrollbackWrapped) < 1 || !viewport.ScrollbackWrapped[0] {
		t.Fatalf("expected projected prefix to keep wrapped continuation into screen, got %#v", viewport.ScrollbackWrapped)
	}

	snapshot := term.Snapshot(0, 10)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	combinedRows := make([]string, 0, len(snapshot.Scrollback)+len(snapshot.Screen.Cells))
	for _, row := range snapshot.Scrollback {
		combinedRows = append(combinedRows, rowToString(row))
	}
	for _, row := range snapshot.Screen.Cells {
		combinedRows = append(combinedRows, rowToString(row))
	}
	if !reflect.DeepEqual(combinedRows, []string{"abcd", "efgh", "i"}) {
		t.Fatalf("expected snapshot to join stored prefix with visible suffix, got %#v", combinedRows)
	}
	combinedWrapped := append(append([]bool(nil), snapshot.ScrollbackWrapped...), snapshot.ScreenWrapped...)
	if len(combinedWrapped) < 3 || !combinedWrapped[0] || !combinedWrapped[1] || combinedWrapped[2] {
		t.Fatalf("expected wrapped metadata across scrollback/screen boundary, got %#v", combinedWrapped)
	}
}

func TestTerminalLatestGrowProjectionUsesMinimalCompletePersistedLogicalLineSuffix(t *testing.T) {
	vt := localvterm.New(5, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(
		localvterm.ScreenData{Cells: [][]localvterm.Cell{localVTermCellsFromString("grow2")}},
		localvterm.CursorState{Row: 0, Col: 5, Visible: true},
		localvterm.TerminalModes{AutoWrap: true},
	)
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "grow-minimal-persisted-suffix",
		size:  Size{Cols: 5, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("older")},
		{cells: localVTermCellsFromString("grow0"), wrapped: true},
		{cells: localVTermCellsFromString("grow1"), wrapped: true},
	}); err != nil {
		t.Fatalf("append persisted rows: %v", err)
	}
	term.size = Size{Cols: 5, Rows: 3}
	if err := term.reclaimPrimaryLiveTailForGrowResizeLocked(5); err != nil {
		t.Fatalf("reclaim grow live tail: %v", err)
	}

	snapshot := term.Snapshot(0, 1)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if got := rowsToStrings(snapshot.Scrollback); !reflect.DeepEqual(got, []string{"grow0", "grow1"}) {
		t.Fatalf("expected latest snapshot to reclaim only the newest complete logical-line suffix, got %#v", got)
	}
	var combined []string
	for _, row := range snapshot.Scrollback {
		combined = append(combined, rowToString(row))
	}
	for _, row := range snapshot.Screen.Cells {
		combined = append(combined, rowToString(row))
	}
	if !reflect.DeepEqual(combined, []string{"grow0", "grow1", "grow2"}) {
		t.Fatalf("expected latest snapshot to project the full newest logical line, got %#v", combined)
	}
	if stringRowsContain(combined, "older") {
		t.Fatalf("expected latest snapshot not to reclaim older logical lines, got %#v", combined)
	}
	combinedWrapped := append(append([]bool(nil), snapshot.ScrollbackWrapped...), snapshot.ScreenWrapped...)
	if !reflect.DeepEqual(combinedWrapped, []bool{true, true, false}) {
		t.Fatalf("expected wrapped metadata to preserve the reclaimed logical-line boundary, got %#v", combinedWrapped)
	}
}

func TestTerminalGrowResizeStoresReclaimedSuffixInPrimaryLiveTail(t *testing.T) {
	vt := localvterm.New(5, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(
		localvterm.ScreenData{Cells: [][]localvterm.Cell{localVTermCellsFromString("grow2")}},
		localvterm.CursorState{Row: 0, Col: 5, Visible: true},
		localvterm.TerminalModes{AutoWrap: true},
	)
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "grow-reclaims-to-live-tail",
		size:  Size{Cols: 5, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("older")},
		{cells: localVTermCellsFromString("grow0"), wrapped: true},
		{cells: localVTermCellsFromString("grow1"), wrapped: true},
	}); err != nil {
		t.Fatalf("append persisted rows: %v", err)
	}
	term.size = Size{Cols: 5, Rows: 3}
	if err := term.reclaimPrimaryLiveTailForGrowResizeLocked(5); err != nil {
		t.Fatalf("reclaim grow live tail: %v", err)
	}
	if len(term.primaryLiveTail.segments) == 0 {
		t.Fatal("expected reclaimed live-tail segment")
	}
	reclaimed := term.primaryLiveTail.segments[0]
	if reclaimed.origin != terminalLiveTailOriginReclaimed {
		t.Fatalf("expected first segment to be reclaimed, got %q", reclaimed.origin)
	}
	if reclaimed.sealState != terminalLiveTailSealed {
		t.Fatalf("expected reclaimed segment to stay sealed, got %q", reclaimed.sealState)
	}
	if got := damageRowsToStrings(reclaimed.rows); !reflect.DeepEqual(got, []string{"grow0", "grow1"}) {
		t.Fatalf("expected reclaimed suffix rows, got %#v", got)
	}
	if reclaimed.firstRowID != 1 || reclaimed.lastRowID != 2 {
		t.Fatalf("expected reclaimed row coordinates 1..2, got %d..%d", reclaimed.firstRowID, reclaimed.lastRowID)
	}
	if got := reclaimed.logicalLineIDs; !reflect.DeepEqual(got, []uint64{2, 2}) {
		t.Fatalf("expected reclaimed suffix to keep persisted logical line id, got %#v", got)
	}

	snapshot := term.Snapshot(0, 2)
	if got := rowsToStrings(snapshot.Scrollback); !reflect.DeepEqual(got, []string{"grow0", "grow1"}) {
		t.Fatalf("expected latest snapshot to read reclaimed suffix from live tail exactly once, got %#v", got)
	}
	if snapshot.ScrollbackTotal != 3 {
		t.Fatalf("expected latest total to count older persisted plus reclaimed live tail, got %d", snapshot.ScrollbackTotal)
	}
	if snapshot.ScrollbackLoadedRows != 2 {
		t.Fatalf("expected reclaimed committed depth 2, got %d", snapshot.ScrollbackLoadedRows)
	}
	if snapshot.ScrollbackFirstRowID != 1 || snapshot.ScrollbackLastRowID != 2 {
		t.Fatalf("expected reclaimed snapshot row ids 1..2, got %d..%d", snapshot.ScrollbackFirstRowID, snapshot.ScrollbackLastRowID)
	}
}

func TestTerminalGrowReclaimedLiveTailHistoryWindowKeepsLogicalLineID(t *testing.T) {
	vt := localvterm.New(5, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(
		localvterm.ScreenData{Cells: [][]localvterm.Cell{localVTermCellsFromString("grow2")}},
		localvterm.CursorState{Row: 0, Col: 5, Visible: true},
		localvterm.TerminalModes{AutoWrap: true},
	)
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "grow-reclaimed-history-window-line-id",
		size:  Size{Cols: 5, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("older")},
		{cells: localVTermCellsFromString("grow0"), wrapped: true},
		{cells: localVTermCellsFromString("grow1"), wrapped: true},
	}); err != nil {
		t.Fatalf("append persisted rows: %v", err)
	}
	term.size = Size{Cols: 5, Rows: 3}
	if err := term.reclaimPrimaryLiveTailForGrowResizeLocked(5); err != nil {
		t.Fatalf("reclaim grow live tail: %v", err)
	}

	viewport, err := term.combinedGridViewport(0, 1, 5, term.primaryLiveTail.clone())
	if err != nil {
		t.Fatalf("combined viewport: %v", err)
	}
	if got := viewport.LogicalLineIDs; !reflect.DeepEqual(got, []uint64{2, 2}) {
		t.Fatalf("expected reclaimed live-tail projection to carry stable logical line id, got %#v", got)
	}
	window := historyWindowFromCoreGridViewport(term.id, 0, viewport)
	if len(window.Lines) != 1 {
		t.Fatalf("expected one reclaimed logical line span, got %#v", window.Lines)
	}
	if window.Lines[0].LogicalLineID != 2 {
		t.Fatalf("expected reclaimed history window span to keep logical line id 2, got %#v", window.Lines)
	}
}

func TestTerminalLatestGrowViewportDoesNotPullOlderLogicalLine(t *testing.T) {
	vt := localvterm.New(5, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(
		localvterm.ScreenData{Cells: [][]localvterm.Cell{localVTermCellsFromString("grow2")}},
		localvterm.CursorState{Row: 0, Col: 5, Visible: true},
		localvterm.TerminalModes{AutoWrap: true},
	)
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "grow-minimal-viewport",
		size:  Size{Cols: 5, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("older")},
		{cells: localVTermCellsFromString("grow0"), wrapped: true},
		{cells: localVTermCellsFromString("grow1"), wrapped: true},
	}); err != nil {
		t.Fatalf("append persisted rows: %v", err)
	}
	term.size = Size{Cols: 5, Rows: 3}
	if err := term.reclaimPrimaryLiveTailForGrowResizeLocked(5); err != nil {
		t.Fatalf("reclaim grow live tail: %v", err)
	}

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 1, Cols: 5})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	if got := rowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"grow0", "grow1"}) {
		t.Fatalf("expected latest viewport to materialize only the newest complete logical-line suffix, got %#v", got)
	}
	if viewport.FirstRowID != 1 || viewport.LastRowID != 2 {
		t.Fatalf("expected latest viewport coordinates to stay on the reclaimed logical-line suffix, got %d..%d", viewport.FirstRowID, viewport.LastRowID)
	}
	if !viewport.ScrollbackHasMore {
		t.Fatal("expected viewport to report older committed history still available")
	}
}

func TestTerminalWritePathSplitsPersistedAndLiveTailAppendRows(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{id: "persisted-live-tail-append", grid: store}
	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("hist0"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail0"), WrappedSet: true, Wrapped: true},
			{Cells: localVTermCellsFromString("tail1"), WrappedSet: true, Wrapped: true},
		},
		LiveTailAppendRows: 2,
	}
	term.appendGridFromDamageLocked(damage)

	viewport, err := store.Viewport(0, 10, 8)
	if err != nil {
		t.Fatalf("read persisted viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"hist0"}) {
		t.Fatalf("expected only persisted append rows persisted, got %#v", got)
	}
	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); !reflect.DeepEqual(got, []string{"tail0", "tail1"}) {
		t.Fatalf("expected trailing live-tail append rows retained in working set, got %#v", got)
	}
	window := term.primaryLiveTail.window(0, term.primaryLiveTail.rowCount())
	if got := window.logicalLineIDs; len(got) != 2 || got[0] < terminalLiveTailLogicalLineIDBase || got[0] != got[1] {
		t.Fatalf("expected trailing wrapped live-tail rows to share stable runtime logical line id, got %#v", got)
	}
}

func TestTerminalLatestProjectionUsesPersistedAndLiveTailAppendRows(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(localvterm.ScreenData{Cells: [][]localvterm.Cell{localVTermCellsFromString("live")}}, localvterm.CursorState{Row: 0, Col: 4, Visible: true}, localvterm.TerminalModes{AutoWrap: true})
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "latest-projection",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("hist"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: true},
		},
		LiveTailAppendRows: 1,
	}
	term.appendGridFromDamageLocked(damage)

	snapshot := term.Snapshot(0, 10)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	var combined []string
	for _, row := range snapshot.Scrollback {
		combined = append(combined, rowToString(row))
	}
	for _, row := range snapshot.Screen.Cells {
		combined = append(combined, rowToString(row))
	}
	if !reflect.DeepEqual(combined, []string{"hist", "tail", "live"}) {
		t.Fatalf("expected latest snapshot projection persisted+live-tail+screen, got %#v", combined)
	}
}

func TestTerminalLatestGridViewportDoesNotDuplicatePersistedRowsCoveredByLiveTail(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "latest-no-duplicate-live-tail",
		size:  Size{Cols: 8, Rows: 4},
		vterm: localvterm.New(8, 4, 0, nil),
		grid:  store,
	}
	term.vterm.DisableEmulatorScrollback()
	for i := 0; i <= 100; i++ {
		if err := store.appendRows([]terminalGridRow{{cells: localVTermCellsFromString(fmt.Sprintf("l%06d", i))}}); err != nil {
			t.Fatalf("append row %d: %v", i, err)
		}
	}
	term.primaryLiveTail.replaceReclaimedPrefix([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("l000094"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("l000095"), WrappedSet: true, Wrapped: false},
	}, 1, 94, 95)
	term.primaryLiveTail.replaceLiveRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("l000096"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("l000097"), WrappedSet: true, Wrapped: false},
	}, false)

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 120, Cols: 80})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	gotRows := rowsToStrings(viewport.Rows)
	counts := map[string]int{}
	for _, row := range gotRows {
		if strings.HasPrefix(row, "l") {
			counts[row]++
		}
	}
	for i := 0; i <= 97; i++ {
		label := fmt.Sprintf("l%06d", i)
		if counts[label] != 1 {
			t.Fatalf("expected %s exactly once in latest viewport, got count=%d rows=%#v", label, counts[label], gotRows)
		}
	}
	if counts["l000098"] != 0 || counts["l000099"] != 0 || counts["l000100"] != 0 {
		t.Fatalf("expected live-tail-covered persisted suffix to hide newer persisted rows, counts=%#v rows=%#v", counts, gotRows)
	}
}

func TestTerminalReclaimDropsResizeProjectionSegment(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "reclaim-drops-resize-projection",
		size:  Size{Cols: 8, Rows: 4},
		vterm: localvterm.New(8, 4, 0, nil),
		grid:  store,
	}
	term.vterm.DisableEmulatorScrollback()
	for i := 80; i <= 95; i++ {
		if err := store.appendRows([]terminalGridRow{{cells: localVTermCellsFromString(fmt.Sprintf("l%06d", i))}}); err != nil {
			t.Fatalf("append row %d: %v", i, err)
		}
	}
	term.primaryLiveTail.replaceResizeRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("l000085"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("l000086"), WrappedSet: true, Wrapped: false},
	}, false)
	term.primaryLiveTail.replaceReclaimedPrefix([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("l000083"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("l000084"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("l000085"), WrappedSet: true, Wrapped: false},
	}, 1, 3, 5)

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 40, Cols: 8})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	gotRows := rowsToStrings(viewport.Rows)
	counts := map[string]int{}
	for _, row := range gotRows {
		if strings.HasPrefix(row, "l") {
			counts[row]++
		}
	}
	for _, label := range []string{"l000083", "l000084", "l000085"} {
		if counts[label] != 1 {
			t.Fatalf("expected reclaimed %s exactly once after dropping resize projection, count=%d rows=%#v", label, counts[label], gotRows)
		}
	}
	if counts["l000086"] != 1 {
		t.Fatalf("expected non-overlapping resize projection row to remain once, count=%d rows=%#v", counts["l000086"], gotRows)
	}
}

func TestTerminalOlderViewportIgnoresLatestLiveTailAppendRows(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(localvterm.ScreenData{Cells: [][]localvterm.Cell{localVTermCellsFromString("live")}}, localvterm.CursorState{Row: 0, Col: 4, Visible: true}, localvterm.TerminalModes{AutoWrap: true})
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "older-ignores-live-tail",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("hist"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: true},
		},
		LiveTailAppendRows: 1,
	}
	term.appendGridFromDamageLocked(damage)

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 1, ScrollbackLimit: 10, Cols: 4})
	if viewport == nil {
		t.Fatal("expected viewport metadata")
	}
	gotRows := make([]string, 0, len(viewport.Rows))
	for _, row := range viewport.Rows {
		gotRows = append(gotRows, rowToString(row))
	}
	if len(gotRows) != 0 {
		t.Fatalf("expected offset=1 to mean loaded committed depth already exhausted older rows, got %#v", gotRows)
	}
}

func (t *Terminal) primaryLiveTailRowsToRowsForTest() [][]localvterm.Cell {
	if t == nil || len(t.primaryLiveTail.rows()) == 0 {
		return nil
	}
	out := make([][]localvterm.Cell, 0, len(t.primaryLiveTail.rows()))
	for _, row := range t.primaryLiveTail.rows() {
		out = append(out, damageOpCells(row))
	}
	return out
}

func TestTerminalGridStoreReflowPreservesTrailingWrappedContinuation(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("abcd"), wrapped: true},
		{cells: localVTermCellsFromString("efgh"), wrapped: true},
	}); err != nil {
		t.Fatalf("append grid rows: %v", err)
	}

	viewport, err := store.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("read grid viewport: %v", err)
	}
	gotRows := vtermRowsToStrings(viewport.Rows)
	if !reflect.DeepEqual(gotRows, []string{"abcd", "efgh"}) {
		t.Fatalf("expected trailing wrapped continuation rows, got %#v", gotRows)
	}
	if len(viewport.Wrapped) < 2 || !viewport.Wrapped[0] || !viewport.Wrapped[1] {
		t.Fatalf("expected final row to remain wrapped because it continues outside the store page, got %#v", viewport.Wrapped)
	}
}

func TestTerminalGridResizeDamagePreservesWideAndQRLikeRows(t *testing.T) {
	vt := localvterm.New(8, 3, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(localvterm.ScreenData{
		Cells: [][]localvterm.Cell{
			{
				{Content: "你", Width: 2},
				{Content: "", Width: 0},
				{Content: "好", Width: 2},
				{Content: "", Width: 0},
				{Content: "A", Width: 1},
			},
			localVTermCellsFromString("qr-####"),
			localVTermCellsFromString("tail"),
		},
	}, localvterm.CursorState{Row: 2, Col: 4, Visible: true}, localvterm.TerminalModes{AutoWrap: true})
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{id: "resize-wide", size: Size{Cols: 8, Rows: 3}, vterm: vt, grid: store}

	term.size = Size{Cols: 4, Rows: 1}
	term.appendGridFromDamageLocked(vt.ResizeWithDamage(4, 1))
	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 4})
	if viewport == nil {
		t.Fatal("expected latest viewport")
	}
	gotRows := rowsToStrings(viewport.Rows)
	if !stringRowsContain(gotRows, "你好") || !stringRowsContain(gotRows, "A") || !stringRowsContain(gotRows, "qr-#") {
		t.Fatalf("expected wide and qr-like content in latest projection, got %#v", gotRows)
	}
	if len(viewport.Rows) == 0 || len(viewport.Rows[0]) < 4 {
		t.Fatalf("expected decoded wide row cells, got %#v", viewport.Rows)
	}
	if got := viewport.Rows[0][0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected wide anchor preserved in latest projection, got %#v", got)
	}
	if got := viewport.Rows[0][1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected wide continuation preserved in latest projection, got %#v", got)
	}
}

func TestTerminalGridViewportUsesCanonicalCols(t *testing.T) {
	vt := localvterm.New(10, 2, 0, nil)
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "canonical-cols",
		size:  Size{Cols: 10, Rows: 2},
		vterm: vt,
		grid:  store,
	}
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("abcdefghij"), wrapped: true},
		{cells: localVTermCellsFromString("kl")},
	}); err != nil {
		t.Fatalf("append grid rows: %v", err)
	}

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackLimit: 10, Cols: 4})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	if viewport.Size.Cols != 10 {
		t.Fatalf("expected viewport to report canonical cols 10, got %d", viewport.Size.Cols)
	}
	gotRows := make([]string, 0, len(viewport.Rows))
	for _, row := range viewport.Rows {
		gotRows = append(gotRows, rowToString(row))
	}
	if !reflect.DeepEqual(gotRows, []string{"abcdefghij", "kl"}) {
		t.Fatalf("expected grid rows reflowed at canonical cols, got %#v", gotRows)
	}
}

func TestTerminalSnapshotTrimsResizeGridScreenOverlap(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "resize-overlap",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}

	writeVTermDamageToGrid(t, term, vt, "abcdefghij")
	term.appendGridFromDamageLocked(vt.ResizeWithDamage(4, 1))

	snapshot := term.Snapshot(0, 10)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	var combined []string
	for _, row := range snapshot.Scrollback {
		combined = append(combined, rowToString(row))
	}
	for _, row := range snapshot.Screen.Cells {
		combined = append(combined, rowToString(row))
	}
	want := []string{"abcd", "efgh", "ij"}
	if !reflect.DeepEqual(combined, want) {
		t.Fatalf("expected snapshot to avoid duplicated grid/screen suffix, got %#v", combined)
	}
}

func TestTerminalLatestSnapshotProjectsPersistedLiveTailAndScreen(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(localvterm.ScreenData{Cells: [][]localvterm.Cell{localVTermCellsFromString("live")}}, localvterm.CursorState{Row: 0, Col: 4, Visible: true}, localvterm.TerminalModes{AutoWrap: true})
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "latest-combined",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("hist"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: true},
		},
		LiveTailAppendRows: 1,
	}
	term.appendGridFromDamageLocked(damage)

	snapshot := term.Snapshot(0, 10)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	var combined []string
	for _, row := range snapshot.Scrollback {
		combined = append(combined, rowToString(row))
	}
	for _, row := range snapshot.Screen.Cells {
		combined = append(combined, rowToString(row))
	}
	if !reflect.DeepEqual(combined, []string{"hist", "tail", "live"}) {
		t.Fatalf("expected latest snapshot projection persisted+live-tail+screen, got %#v", combined)
	}
}

func TestTerminalOlderGridViewportIgnoresLiveTailAppendRows(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(localvterm.ScreenData{Cells: [][]localvterm.Cell{localVTermCellsFromString("live")}}, localvterm.CursorState{Row: 0, Col: 4, Visible: true}, localvterm.TerminalModes{AutoWrap: true})
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "older-persisted-only",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("hist"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: true},
		},
		LiveTailAppendRows: 1,
	}
	term.appendGridFromDamageLocked(damage)

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 1, ScrollbackLimit: 10, Cols: 4})
	if viewport == nil {
		t.Fatal("expected viewport metadata")
	}
	gotRows := make([]string, 0, len(viewport.Rows))
	for _, row := range viewport.Rows {
		gotRows = append(gotRows, rowToString(row))
	}
	if len(gotRows) != 0 {
		t.Fatalf("expected offset=1 to mean loaded committed depth already exhausted older rows, got %#v", gotRows)
	}
}

func TestTerminalGridStoreIndexIsReadOnDemandAfterReopen(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "ondemand-1")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	for i := 0; i < 32; i++ {
		wrapped := i == 28 || i == 29
		row := terminalGridRow{
			cells:   []localvterm.Cell{{Content: fmt.Sprintf("row-%02d", i), Width: 1}},
			wrapped: wrapped,
		}
		if err := store.appendRows([]terminalGridRow{row}); err != nil {
			t.Fatalf("append row %d: %v", i, err)
		}
	}
	if got := store.RowCount(); got != 32 {
		t.Fatalf("expected live row count 32, got %d", got)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := openTerminalGridStoreForReplay(root, "ondemand-1")
	if err != nil {
		t.Fatalf("reopen grid store: %v", err)
	}
	defer reopened.Close()
	if got := reopened.RowCount(); got != 32 {
		t.Fatalf("expected reopened row count from index size, got %d", got)
	}
	refs, refCount, hasMore, err := reopened.windowRefs(0, 2)
	if err != nil {
		t.Fatalf("read window refs: %v", err)
	}
	if refCount != 4 || !hasMore {
		t.Fatalf("expected wrapped group to extend latest window, got refs=%d hasMore=%v", refCount, hasMore)
	}
	rawRows, err := readTerminalGridRows(reopened.dir, refs)
	if err != nil {
		t.Fatalf("read raw rows: %v", err)
	}
	gotRows := make([]string, 0, len(rawRows))
	for _, row := range rawRows {
		gotRows = append(gotRows, vtermRowToString(row.cells))
	}
	if !reflect.DeepEqual(gotRows, []string{"row-28", "row-29", "row-30", "row-31"}) {
		t.Fatalf("expected on-demand index window to preserve wrapped prefix, got %#v", gotRows)
	}
}

func TestTerminalGridStoreRecoveryTruncatesShortPage(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "recover-short-page")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := store.AppendRows([][]localvterm.Cell{{{Content: fmt.Sprintf("row-%d", i), Width: 1}}}); err != nil {
			t.Fatalf("append row %d: %v", i, err)
		}
	}
	dir := store.dir
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	refs, err := readAllTerminalGridIndexRefs(dir)
	if err != nil {
		t.Fatalf("read refs: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(refs))
	}
	if err := os.Truncate(filepath.Join(dir, terminalGridPageName(refs[2].seq)), refs[2].offset+refs[2].length-1); err != nil {
		t.Fatalf("truncate page: %v", err)
	}

	reopened, err := openTerminalGridStoreForReplay(root, "recover-short-page")
	if err != nil {
		t.Fatalf("reopen short page store: %v", err)
	}
	defer reopened.Close()
	if got := reopened.RowCount(); got != 2 {
		t.Fatalf("expected recovery to truncate to 2 committed rows, got %d", got)
	}
	viewport, err := reopened.Viewport(0, 10, 12)
	if err != nil {
		t.Fatalf("viewport after recovery: %v", err)
	}
	gotRows := vtermRowsToStrings(viewport.Rows)
	if !reflect.DeepEqual(gotRows, []string{"row-0", "row-1"}) {
		t.Fatalf("expected valid rows after short-page recovery, got %#v", gotRows)
	}
}

func TestTerminalGridStoreRecoveryTruncatesPartialIndexTail(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "recover-partial-index")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := store.AppendRows([][]localvterm.Cell{{{Content: fmt.Sprintf("row-%d", i), Width: 1}}}); err != nil {
			t.Fatalf("append row %d: %v", i, err)
		}
	}
	dir := store.dir
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	indexPath := filepath.Join(dir, terminalGridIndexName)
	file, err := os.OpenFile(indexPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	if _, err := file.Write([]byte{1, 2, 3}); err != nil {
		_ = file.Close()
		t.Fatalf("write partial tail: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close index writer: %v", err)
	}

	reopened, err := openTerminalGridStoreDir(dir, "recover-partial-index", true, false)
	if err != nil {
		t.Fatalf("reopen writable store: %v", err)
	}
	if got := reopened.RowCount(); got != 2 {
		t.Fatalf("expected partial index tail ignored, got row count %d", got)
	}
	if err := reopened.AppendRows([][]localvterm.Cell{{{Content: "row-2", Width: 1}}}); err != nil {
		t.Fatalf("append after partial index recovery: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened store: %v", err)
	}
	indexInfo, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("stat index: %v", err)
	}
	if indexInfo.Size() != 3*terminalGridIndexRecord {
		t.Fatalf("expected index tail repaired before append, size=%d", indexInfo.Size())
	}
	verify, err := openTerminalGridStoreForReplay(root, "recover-partial-index")
	if err != nil {
		t.Fatalf("verify reopen: %v", err)
	}
	defer verify.Close()
	viewport, err := verify.Viewport(0, 10, 12)
	if err != nil {
		t.Fatalf("viewport after append: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"row-0", "row-1", "row-2"}) {
		t.Fatalf("expected rows after repaired append, got %#v", got)
	}
}

func TestTerminalGridStoreRecoveryIgnoresCorruptMetadata(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "recover-metadata")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	dir := store.dir
	if err := store.AppendRows([][]localvterm.Cell{{{Content: "survives", Width: 1}}}); err != nil {
		t.Fatalf("append row: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, terminalGridMetadataName), []byte("bad-metadata"), 0o600); err != nil {
		t.Fatalf("corrupt metadata: %v", err)
	}

	reopened, err := openTerminalGridStoreForReplay(root, "recover-metadata")
	if err != nil {
		t.Fatalf("expected corrupt metadata to be advisory, got %v", err)
	}
	defer reopened.Close()
	viewport, err := reopened.Viewport(0, 10, 12)
	if err != nil {
		t.Fatalf("viewport with corrupt metadata: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"survives"}) {
		t.Fatalf("expected rows from index/page despite corrupt metadata, got %#v", got)
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

func TestTerminalProcessExitForceSealsPrimaryLiveTail(t *testing.T) {
	ctx := context.Background()
	bus := NewEventBus(nil)

	term, err := newTerminal(ctx, bus, terminalConfig{
		ID:             "exit-force-seal",
		Name:           "seal",
		Command:        []string{"bash", "--noprofile", "--norc", "-c", "printf 'abcdefghij'; exit 0"},
		Size:           Size{Cols: 4, Rows: 1},
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

	snap := term.Snapshot(0, 10)
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	var combined []string
	for _, row := range snap.Scrollback {
		combined = append(combined, rowToString(row))
	}
	for _, row := range snap.Screen.Cells {
		combined = append(combined, rowToString(row))
	}
	if !reflect.DeepEqual(combined, []string{"abcd", "efgh", "ij"}) {
		t.Fatalf("expected exited snapshot to expose sealed logical line without duplication, got %#v", combined)
	}
	if got := rowsToStrings(snap.Scrollback); !reflect.DeepEqual(got, []string{"abcd", "efgh"}) {
		t.Fatalf("expected exited snapshot scrollback to carry sealed prefix rows, got %#v", got)
	}
	if !reflect.DeepEqual(snap.ScrollbackWrapped, []bool{true, true}) {
		t.Fatalf("expected sealed logical line wrapped markers, got %#v", snap.ScrollbackWrapped)
	}
	if snap.ScrollbackLoadedRows != 3 {
		t.Fatalf("expected committed loaded rows 3 after exit force seal, got %d", snap.ScrollbackLoadedRows)
	}
	if snap.HistoryGeneration == 0 {
		t.Fatal("expected committed history generation after exit force seal")
	}

	coreViewport, err := term.combinedGridViewport(0, 10, 4, term.primaryLiveTail.clone())
	if err != nil {
		t.Fatalf("exit force-seal history window viewport: %v", err)
	}
	window := historyWindowFromCoreGridViewport(term.id, 0, coreViewport)
	if got := historyWindowTrimmedRowTexts(window); !reflect.DeepEqual(got, []string{"abcd", "efgh", "ij"}) {
		t.Fatalf("expected history window to expose sealed logical line without live-tail duplication, got %#v", got)
	}
	if window.LoadedRows != 3 || window.LoadedLines != 1 || window.LogicalTotal != 1 {
		t.Fatalf("expected history window to count one sealed logical line after exit, loaded_rows=%d loaded_lines=%d total=%d", window.LoadedRows, window.LoadedLines, window.LogicalTotal)
	}
	if len(window.Lines) != 1 || window.Lines[0].StartRow != 0 || window.Lines[0].EndRow != 2 || window.Lines[0].ClippedBefore || window.Lines[0].ClippedAfter {
		t.Fatalf("expected unclipped sealed logical line span after exit, got %#v", window.Lines)
	}
}

func TestTerminalProcessExitClearsPersistedLiveTailMetadata(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "exit-clears-live-tail-metadata",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	term.primaryLiveTail.replaceLiveRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: true},
	}, true)
	term.recordLiveTailMetadataLocked()
	before, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read line metadata before seal: %v", err)
	}
	if len(before.LiveRecords) == 0 || len(before.LiveRows) == 0 {
		t.Fatalf("expected live tail metadata before seal, got %#v", before)
	}

	if err := term.sealLiveTailForProcessExitLocked(); err != nil {
		t.Fatalf("seal live tail for exit: %v", err)
	}
	after, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read line metadata after seal: %v", err)
	}
	if len(after.LiveRecords) != 0 || len(after.LiveRows) != 0 {
		t.Fatalf("expected live tail metadata cleared after process exit seal, got %#v", after)
	}
	if _, ok := store.recoveredLiveTailFromMetadata(); ok {
		t.Fatal("expected process exit seal to remove recoverable live tail metadata")
	}
}

func TestTerminalProcessExitInAltScreenDropsAltAndSealsPrimaryTail(t *testing.T) {
	ctx := context.Background()
	bus := NewEventBus(nil)

	term, err := newTerminal(ctx, bus, terminalConfig{
		ID:             "exit-alt-drop",
		Name:           "alt-exit",
		Command:        []string{"bash", "--noprofile", "--norc", "-c", "printf 'base'; printf '\\033[?1049hALT'; exit 0"},
		Size:           Size{Cols: 8, Rows: 2},
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

	snap := term.Snapshot(0, 10)
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	if snapshotContains(snap, "ALT") {
		t.Fatalf("expected alt-screen content to be dropped on exit, got %#v", snap)
	}
	if !snapshotContains(snap, "base") {
		t.Fatalf("expected primary content to survive exit seal, got %#v", snap)
	}
	if snap.Modes.AlternateScreen {
		t.Fatalf("expected exited snapshot not to remain in alt-screen, got %#v", snap.Modes)
	}
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
		case StreamScreenUpdate:
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

func streamMessageContainsText(msg StreamMessage, cols, rows int, needle string) bool {
	switch msg.Type {
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

func encodeTestScreenUpdatePayload(t *testing.T, update protocol.ScreenUpdate) []byte {
	t.Helper()
	payload, err := protocol.EncodeScreenUpdatePayload(update)
	if err != nil {
		t.Fatalf("encode screen update: %v", err)
	}
	return payload
}

func screenUpdateContainsText(update protocol.ScreenUpdate, needle string) bool {
	if update.FullReplace {
		for _, row := range update.Screen.Cells {
			if strings.Contains(protocolRowToString(row), needle) {
				return true
			}
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

func TestScreenUpdatePayloadFromDamagePreservesStyledErase(t *testing.T) {
	const bg = "#222222"
	vt := localvterm.New(12, 2, 32, nil)
	term := &Terminal{vterm: vt}

	_, err, damage := vt.WriteWithDamage([]byte("\x1b[48;2;34;34;34m\x1b[1;1H\x1b[K"))
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
	if update.FullReplace {
		if len(update.Screen.Cells) == 0 || len(update.Screen.Cells[0]) != 12 {
			t.Fatalf("expected full-row styled erase in full screen payload, got %#v", update.Screen.Cells)
		}
		for i, cell := range update.Screen.Cells[0] {
			if cell.Content != " " || cell.Width != 1 || cell.Style.BG != bg {
				t.Fatalf("expected styled blank cell %d with bg %q, got %#v", i, bg, cell)
			}
		}
		return
	}
	for _, op := range update.Ops {
		if op.Code != protocol.ScreenOpWriteSpan || op.Row != 0 {
			continue
		}
		if len(op.Cells) != 12 {
			t.Fatalf("expected full-row styled erase span, got %#v", op)
		}
		for i, cell := range op.Cells {
			if cell.Content != " " || cell.Width != 1 || cell.Style.BG != bg {
				t.Fatalf("expected styled blank cell %d with bg %q, got %#v", i, bg, cell)
			}
		}
		return
	}
	t.Fatalf("expected styled erase payload, got %#v", update)
}

func TestScreenUpdateFromDamageStatePreservesStyledEraseSpan(t *testing.T) {
	const bg = "#222222"
	vt := localvterm.New(12, 2, 32, nil)

	_, err, damage := vt.WriteWithDamage([]byte("\x1b[48;2;34;34;34m\x1b[1;1H\x1b[K"))
	if err != nil {
		t.Fatalf("WriteWithDamage failed: %v", err)
	}
	update := screenUpdateFromDamageState(damage, "")
	for _, op := range update.Ops {
		if op.Code != protocol.ScreenOpWriteSpan || op.Row != 0 {
			continue
		}
		if len(op.Cells) != 12 {
			t.Fatalf("expected full-row styled erase span, got %#v", op)
		}
		for i, cell := range op.Cells {
			if cell.Content != " " || cell.Width != 1 || cell.Style.BG != bg {
				t.Fatalf("expected styled blank cell %d with bg %q, got %#v", i, bg, cell)
			}
		}
		return
	}
	t.Fatalf("expected styled erase write span, got %#v", update.Ops)
}

func TestTerminalClearScreenDoesNotCreateCommittedHistory(t *testing.T) {
	vt := localvterm.New(12, 2, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "clear-screen-no-history-create",
		size:  Size{Cols: 12, Rows: 2},
		vterm: vt,
		grid:  store,
	}
	if err := store.AppendRows([][]localvterm.Cell{
		localVTermCellsFromString("before-0"),
		localVTermCellsFromString("before-1"),
	}); err != nil {
		t.Fatalf("append committed rows: %v", err)
	}
	vt.LoadSnapshot(localvterm.ScreenData{
		Cells: [][]localvterm.Cell{
			localVTermRowForTest("live-tail", 12),
			localVTermRowForTest("", 12),
		},
	}, localvterm.CursorState{Row: 0, Col: 9, Visible: true}, localvterm.TerminalModes{AutoWrap: true})
	beforeRows := store.RowCount()
	beforeLines := store.LogicalLineCount()
	viewportBefore := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 12})
	if viewportBefore == nil {
		t.Fatal("expected pre-clear viewport")
	}
	if got := rowsToStrings(viewportBefore.Rows); !reflect.DeepEqual(got, []string{"before-0", "before-1"}) {
		t.Fatalf("expected pre-clear committed history, got %#v", got)
	}

	_, err, damage := vt.WriteWithDamage([]byte("\x1b[2J"))
	if err != nil {
		t.Fatalf("clear screen write: %v", err)
	}
	term.appendGridFromDamageLocked(damage)

	if got := store.RowCount(); got != beforeRows {
		t.Fatalf("expected clear screen not to append committed rows, got before=%d after=%d", beforeRows, got)
	}
	if got := store.LogicalLineCount(); got != beforeLines {
		t.Fatalf("expected clear screen not to create committed logical lines, got before=%d after=%d", beforeLines, got)
	}

	viewportAfter := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 12})
	if viewportAfter == nil {
		t.Fatal("expected post-clear viewport")
	}
	if got := rowsToStrings(viewportAfter.Rows); !reflect.DeepEqual(got, []string{"before-0", "before-1"}) {
		t.Fatalf("expected committed history to remain readable after clear screen, got %#v", got)
	}
	if viewportAfter.LoadedRows != beforeRows || viewportAfter.HistoryGeneration == 0 {
		t.Fatalf("expected clear screen not to disturb committed viewport metadata, loaded=%d gen=%d", viewportAfter.LoadedRows, viewportAfter.HistoryGeneration)
	}
	coreViewportAfter, err := term.combinedGridViewport(0, 10, 12, term.primaryLiveTail.clone())
	if err != nil {
		t.Fatalf("post-clear history window viewport: %v", err)
	}
	windowAfter := historyWindowFromCoreGridViewport(term.id, 0, coreViewportAfter)
	if got := historyWindowRowTexts(windowAfter); !reflect.DeepEqual(got, []string{"before-0", "before-1"}) {
		t.Fatalf("expected history window to keep only committed history after clear screen, got %#v", got)
	}
	if windowAfter.LoadedLines != beforeLines || windowAfter.LogicalTotal != beforeLines {
		t.Fatalf("expected history window logical counts unchanged after clear screen, loaded=%d total=%d", windowAfter.LoadedLines, windowAfter.LogicalTotal)
	}
	if historyWindowContainsText(windowAfter, "live-tail") {
		t.Fatalf("expected clear screen not to expose cleared live tail through history window, got %#v", windowAfter.Rows)
	}

	latest := term.Snapshot(0, 10)
	if latest == nil {
		t.Fatal("expected latest snapshot")
	}
	if snapshotContains(latest, "live-tail") {
		t.Fatalf("expected clear screen to clear current surface projection, got %#v", latest)
	}
	if latest.ScrollbackLoadedRows != beforeRows || latest.HistoryGeneration == 0 {
		t.Fatalf("expected latest snapshot committed metadata unchanged after clear screen, loaded=%d gen=%d", latest.ScrollbackLoadedRows, latest.HistoryGeneration)
	}
}

func TestSnapshotFromVTermPreservesWrappedScrollbackTrailingSpaces(t *testing.T) {
	vt := localvterm.New(4, 2, 32, nil)
	if _, err := vt.Write([]byte("AA  BB")); err != nil {
		t.Fatalf("seed wrapped row: %v", err)
	}
	if !vt.ScreenRowWrappedAt(0) {
		t.Fatalf("expected first row to be wrapped before scroll")
	}
	if _, err := vt.Write([]byte("\nnext\nlast")); err != nil {
		t.Fatalf("scroll wrapped row: %v", err)
	}

	snapshot := snapshotFromVTerm(vt)
	if snapshot == nil || len(snapshot.Scrollback) == 0 {
		t.Fatalf("expected scrollback snapshot, got %#v", snapshot)
	}
	if !snapshot.ScrollbackWrapped[0] {
		t.Fatalf("expected wrapped scrollback metadata, got %#v", snapshot.ScrollbackWrapped)
	}
	row := snapshot.Scrollback[0].DecodeCells()
	if got := len(row); got != 4 {
		t.Fatalf("expected wrapped scrollback row to retain trailing blanks, got len=%d row=%#v", got, row)
	}
	if row[2].Content != " " || row[3].Content != " " {
		t.Fatalf("expected trailing blanks preserved, got %#v", row)
	}
}

func TestSnapshotFromVTermPreservesScreenTrailingSpaceColumnsAfterResize(t *testing.T) {
	vt := localvterm.New(4, 6, 32, nil)
	if _, err := vt.Write([]byte("AA  \r\nBB")); err != nil {
		t.Fatalf("seed hard-newline trailing spaces: %v", err)
	}

	vt.Resize(2, 6)
	snapshot := snapshotFromVTerm(vt)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if got := coreProtocolRowText(snapshot.Screen.Cells[0]); got != "AA" {
		t.Fatalf("expected first split row AA, got %q", got)
	}
	if got := coreProtocolRowText(snapshot.Screen.Cells[1]); got != "  " {
		t.Fatalf("expected trailing-space split row to survive snapshot, got %q row=%#v", got, snapshot.Screen.Cells[1])
	}
	if got := coreProtocolRowText(snapshot.Screen.Cells[2]); got != "BB" {
		t.Fatalf("expected following row BB, got %q", got)
	}
	if len(snapshot.ScreenWrapped) < 2 || !snapshot.ScreenWrapped[0] || snapshot.ScreenWrapped[1] {
		t.Fatalf("unexpected wrapped metadata: %#v", snapshot.ScreenWrapped)
	}
}

func TestScreenFullReplaceUpdatePreservesTrailingSpaceColumnsAfterResize(t *testing.T) {
	vt := localvterm.New(4, 6, 32, nil)
	if _, err := vt.Write([]byte("AA  \r\nBB")); err != nil {
		t.Fatalf("seed hard-newline trailing spaces: %v", err)
	}
	vt.Resize(2, 6)

	update := screenFullReplaceUpdateFromVTerm(vt, "demo")
	payload, err := protocol.EncodeScreenUpdatePayload(update)
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}
	decoded, err := protocol.DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode update: %v", err)
	}

	if got := coreProtocolRowText(decoded.Screen.Cells[0]); got != "AA" {
		t.Fatalf("expected first split row AA, got %q", got)
	}
	if got := coreProtocolRowText(decoded.Screen.Cells[1]); got != "  " {
		t.Fatalf("expected trailing-space split row to survive full replace wire payload, got %q row=%#v", got, decoded.Screen.Cells[1])
	}
	if got := coreProtocolRowText(decoded.Screen.Cells[2]); got != "BB" {
		t.Fatalf("expected following row BB, got %q", got)
	}
}

func TestScreenFullReplaceUpdatePreservesCanonicalTrailingBlankCells(t *testing.T) {
	vt := localvterm.New(12, 3, 32, nil)
	if _, err := vt.Write([]byte("████")); err != nil {
		t.Fatalf("seed QR-like row: %v", err)
	}

	update := screenFullReplaceUpdateFromVTerm(vt, "qr")
	payload, err := protocol.EncodeScreenUpdatePayload(update)
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}
	decoded, err := protocol.DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode update: %v", err)
	}

	if len(decoded.Screen.Cells) == 0 {
		t.Fatalf("expected screen rows, got %#v", decoded.Screen)
	}
	if got := len(decoded.Screen.Cells[0]); got != 12 {
		t.Fatalf("expected canonical-width screen row, got len=%d row=%#v", got, decoded.Screen.Cells[0])
	}
	if got := coreProtocolRowText(decoded.Screen.Cells[0]); got != "████        " {
		t.Fatalf("expected QR-like quiet-zone blanks to survive full replace, got %q row=%#v", got, decoded.Screen.Cells[0])
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
	if len(update.ScrollbackAppend) != 0 || update.ScrollbackTrim != 0 || update.ResetScrollback {
		t.Fatalf("expected live delta to omit scrollback history, got %#v", update)
	}
	if update.ScreenScroll == 0 && len(update.Ops) == 0 {
		t.Fatalf("expected scroll delta operations, got %#v", update)
	}
}

func TestScreenUpdatePayloadFromBroadDirectDamageUsesFullReplaceReason(t *testing.T) {
	vt := localvterm.New(8, 2, 32, nil)
	vt.LoadSnapshot(
		localvterm.ScreenData{
			Cells: [][]localvterm.Cell{
				localVTermRowForTest("abcdefgh", 8),
				localVTermRowForTest("ijklmnop", 8),
			},
		},
		localvterm.CursorState{Row: 1, Col: 8, Visible: true},
		localvterm.TerminalModes{AutoWrap: true},
	)
	term := &Terminal{
		vterm: vt,
		title: "demo",
	}
	damage := localvterm.WriteDamage{
		RequiresFullReplace: true,
		FullReplaceReason:   "broad_direct_cell_damage",
		DirectDamageItems:   2,
		DirectDamageRows:    2,
		DirectDamageCells:   16,
		Cursor:              localvterm.CursorState{Row: 1, Col: 8, Visible: true},
		Modes:               localvterm.TerminalModes{AutoWrap: true},
		SizeCols:            8,
		SizeRows:            2,
	}

	payload, ok := term.screenUpdatePayloadFromDamageLocked(damage)
	if !ok {
		t.Fatal("expected payload")
	}
	update, err := protocol.DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !update.FullReplace {
		t.Fatalf("expected full replace payload, got %#v", update)
	}
	if update.Title != "demo" {
		t.Fatalf("expected title propagated, got %q", update.Title)
	}
	if got := protocolRowToString(update.Screen.Cells[0]); got != "abcdefgh" {
		t.Fatalf("expected screen row preserved in full replace, got %q", got)
	}
	if got := fullReplaceDamageReason(damage); got != "requires_full_replace_broad_direct_cell_damage" {
		t.Fatalf("unexpected diagnostic reason %q", got)
	}
}

func protocolRowToString(row []protocol.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

func coreProtocolRowText(row []protocol.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return b.String()
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

func vtermRowToRawString(row []localvterm.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return b.String()
}

func vtermRowsToStrings(rows [][]localvterm.Cell) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, vtermRowToString(row))
	}
	return out
}

func localVTermCellsFromString(value string) []localvterm.Cell {
	cells := make([]localvterm.Cell, 0, len(value))
	for _, r := range value {
		cells = append(cells, localvterm.Cell{Content: string(r), Width: 1})
	}
	return cells
}

func writeVTermDamageToGrid(t *testing.T, term *Terminal, vt *localvterm.VTerm, text string) {
	t.Helper()
	_, err, damage := vt.WriteWithDamage([]byte(text))
	if err != nil {
		t.Fatalf("write vterm damage %q: %v", text, err)
	}
	term.appendGridFromDamageLocked(damage)
}

func readAllTerminalGridIndexRefs(dir string) ([]terminalGridRowRef, error) {
	file, err := os.Open(filepath.Join(dir, terminalGridIndexName))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	count := int(info.Size() / terminalGridIndexRecord)
	return readTerminalGridIndexWindowFromFile(file, 0, count)
}

func terminalGridPageFilesForTest(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read grid dir: %v", err)
	}
	var pages []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := terminalGridPageSeq(entry.Name()); ok {
			pages = append(pages, entry.Name())
		}
	}
	sort.Strings(pages)
	return pages
}

func terminalProjectionRowTexts(term *Terminal, cols int, limit int) []string {
	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: limit, Cols: cols})
	if viewport == nil {
		return nil
	}
	rows := rowsToStrings(viewport.Rows)
	if term != nil && term.vterm != nil {
		screen := term.vterm.ScreenContent()
		rows = append(rows, vtermRowsToStrings(screen.Cells)...)
	}
	return rows
}

func historyWindowRowTexts(window *HistoryWindow) []string {
	if window == nil {
		return nil
	}
	out := make([]string, 0, len(window.Rows))
	for _, row := range window.Rows {
		out = append(out, rowTextFromHistoryRow(row))
	}
	return out
}

func historyWindowTrimmedRowTexts(window *HistoryWindow) []string {
	if window == nil {
		return nil
	}
	out := make([]string, 0, len(window.Rows))
	for _, row := range window.Rows {
		out = append(out, strings.TrimRight(rowTextFromHistoryRow(row), " "))
	}
	return out
}

func historyWindowContainsText(window *HistoryWindow, needle string) bool {
	return stringRowsContain(historyWindowRowTexts(window), needle)
}

func stringRowsContain(rows []string, needle string) bool {
	for _, row := range rows {
		if strings.Contains(row, needle) {
			return true
		}
	}
	return false
}

func protocolRowToRawString(row []protocol.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return b.String()
}

func localVTermRowForTest(text string, cols int) []localvterm.Cell {
	row := make([]localvterm.Cell, cols)
	for i := range row {
		row[i] = localvterm.Cell{Content: " ", Width: 1}
	}
	for i, r := range []rune(text) {
		if i >= len(row) {
			break
		}
		row[i] = localvterm.Cell{Content: string(r), Width: 1}
	}
	return row
}

func stripANSIForTest(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != 0x1b {
			b.WriteByte(value[i])
			continue
		}
		i++
		if i >= len(value) {
			break
		}
		if value[i] == '[' {
			for i+1 < len(value) {
				i++
				if value[i] >= '@' && value[i] <= '~' {
					break
				}
			}
		}
	}
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
