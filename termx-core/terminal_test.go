package termx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-core/fanout"
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

func TestTerminalGridStoreUsesCompactBinaryRows(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "compact-1")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	row := terminalGridRow{
		cells: []localvterm.Cell{
			{Content: "A", Width: 1},
			{Content: "界", Width: 2, Style: localvterm.CellStyle{FG: "ansi:2", BG: "idx:17", Bold: true, Underline: true}},
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
