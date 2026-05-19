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
	if replay.Limit != maxGridReplayRows {
		t.Fatalf("expected replay limit clamp to %d, got %d", maxGridReplayRows, replay.Limit)
	}
	if replay.Rows != maxGridReplayRows {
		t.Fatalf("expected replay rows clamp to %d, got %d", maxGridReplayRows, replay.Rows)
	}
	if replay.Replay == "" {
		t.Fatal("expected clamped replay payload")
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
		t.Fatalf("expected retained row count 3, got %d", got)
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

func TestTerminalGridStoreRowsReflowSoftWrappedColdBuffer(t *testing.T) {
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
		t.Fatalf("expected cold buffer to reflow by requested width, got %#v", gotRows)
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

func TestTerminalGridResizeDamagePreservesStressTailRows(t *testing.T) {
	for _, lineCount := range []int{100, 1000} {
		t.Run(fmt.Sprintf("lines_%d", lineCount), func(t *testing.T) {
			vt := localvterm.New(32, 8, 0, nil)
			vt.DisableEmulatorScrollback()
			store := newMemoryTerminalGridStoreForTest(t)
			defer store.Close()
			term := &Terminal{id: "resize-stress", grid: store}

			for i := 1; i <= lineCount; i++ {
				suffix := "\r\n"
				if i == lineCount {
					suffix = ""
				}
				writeVTermDamageToGrid(t, term, vt, fmt.Sprintf("stress-%06d payload%s", i, suffix))
			}
			term.appendGridFromDamageLocked(vt.ResizeWithDamage(16, 4))

			rows := gridAndScreenRowTexts(t, store, vt, 16, 250)
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
	term := &Terminal{id: "resize-wrapped", grid: store}

	writeVTermDamageToGrid(t, term, vt, "abcdefghij")
	term.appendGridFromDamageLocked(vt.ResizeWithDamage(4, 1))

	screenRows := vtermRowsToStrings(vt.ScreenContent().Cells)
	if !reflect.DeepEqual(screenRows, []string{"ij"}) {
		t.Fatalf("test setup expected visible suffix on screen, got %#v wrapped=%#v", screenRows, vt.ScreenWrapped())
	}

	viewport, err := store.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("read resize grid viewport: %v", err)
	}
	gotRows := vtermRowsToStrings(viewport.Rows)
	if !reflect.DeepEqual(gotRows, []string{"abcd", "efgh"}) {
		t.Fatalf("expected grid store to contain only wrapped prefix displaced from screen, got rows=%#v wrapped=%#v", gotRows, viewport.Wrapped)
	}
	if len(viewport.Wrapped) < 2 || !viewport.Wrapped[0] || !viewport.Wrapped[1] {
		t.Fatalf("expected wrapped metadata to keep logical line continuity, got %#v", viewport.Wrapped)
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

	writeVTermDamageToGrid(t, term, vt, "abcdefghij")
	term.appendGridFromDamageLocked(vt.ResizeWithDamage(4, 2))

	viewport, err := store.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("read resize grid viewport: %v", err)
	}
	gotRows := vtermRowsToStrings(viewport.Rows)
	if !reflect.DeepEqual(gotRows, []string{"abcd"}) {
		t.Fatalf("expected grid store to contain only rows displaced from screen, got rows=%#v wrapped=%#v", gotRows, viewport.Wrapped)
	}
	if len(viewport.Wrapped) < 1 || !viewport.Wrapped[0] {
		t.Fatalf("expected stored prefix to keep wrapped continuation into screen, got %#v", viewport.Wrapped)
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
	if !reflect.DeepEqual(combinedRows, []string{"abcd", "efgh", "ij"}) {
		t.Fatalf("expected snapshot to join stored prefix with visible suffix, got %#v", combinedRows)
	}
	combinedWrapped := append(append([]bool(nil), snapshot.ScrollbackWrapped...), snapshot.ScreenWrapped...)
	if len(combinedWrapped) < 3 || !combinedWrapped[0] || !combinedWrapped[1] || combinedWrapped[2] {
		t.Fatalf("expected wrapped metadata across scrollback/screen boundary, got %#v", combinedWrapped)
	}
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
	term := &Terminal{id: "resize-wide", grid: store}

	term.appendGridFromDamageLocked(vt.ResizeWithDamage(4, 1))
	viewport, err := store.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("read resize grid viewport: %v", err)
	}
	gotRows := vtermRowsToStrings(viewport.Rows)
	if !stringRowsContain(gotRows, "你好") || !stringRowsContain(gotRows, "A") || !stringRowsContain(gotRows, "qr-#") {
		t.Fatalf("expected wide and qr-like content in grid store, got %#v", gotRows)
	}
	if len(viewport.Rows) == 0 || len(viewport.Rows[0]) < 4 {
		t.Fatalf("expected decoded wide row cells, got %#v", viewport.Rows)
	}
	if got := viewport.Rows[0][0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected wide anchor preserved in grid store, got %#v", got)
	}
	if got := viewport.Rows[0][1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected wide continuation preserved in grid store, got %#v", got)
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

func gridAndScreenRowTexts(t *testing.T, store *terminalGridStore, vt *localvterm.VTerm, cols int, limit int) []string {
	t.Helper()
	var rows []string
	viewport, err := store.Viewport(0, limit, cols)
	if err != nil {
		t.Fatalf("read grid viewport: %v", err)
	}
	rows = append(rows, vtermRowsToStrings(viewport.Rows)...)
	if vt != nil {
		screen := vt.ScreenContent()
		rows = append(rows, vtermRowsToStrings(screen.Cells)...)
	}
	return rows
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
