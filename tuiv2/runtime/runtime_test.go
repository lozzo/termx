package runtime

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/terminalmeta"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/shared"
	localvterm "github.com/lozzow/termx/vterm"
)

func newTestRuntime(t *testing.T) (*Runtime, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	socketPath := filepath.Join(t.TempDir(), "termx.sock")
	srv := termx.NewServer(termx.WithSocketPath(socketPath))
	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = srv.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not stop in time")
		}
	})

	var transport *unixtransport.Transport
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		transport, err = unixtransport.Dial(socketPath)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	client := protocol.NewClient(transport)
	t.Cleanup(func() { _ = client.Close() })

	helloCtx, helloCancel := context.WithTimeout(ctx, 2*time.Second)
	defer helloCancel()
	if err := client.Hello(helloCtx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	return New(bridge.NewProtocolClient(client)), ctx
}

func TestRuntimeListTerminalsDoesNotPopulateRegistry(t *testing.T) {
	rt, ctx := newTestRuntime(t)

	created, err := rt.client.Create(ctx, protocol.CreateParams{
		Command: []string{"sh"},
		Name:    "demo",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	terminals, err := rt.ListTerminals(ctx)
	if err != nil {
		t.Fatalf("list terminals: %v", err)
	}
	if len(terminals) != 1 {
		t.Fatalf("expected 1 terminal, got %d", len(terminals))
	}
	stored := rt.Registry().Get(created.TerminalID)
	if stored != nil {
		t.Fatalf("expected list to avoid populating registry, got %#v", stored)
	}
}

func TestRuntimeAttachAndLoadSnapshotInitializesVTermCache(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 7, Mode: "collaborator"}
	client.listResult = &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
		ID:    "term-1",
		Name:  "shell",
		State: "running",
	}}}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 6, 3, []string{
		"hello",
		"world",
	})

	rt := New(client)

	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal.VTerm == nil {
		t.Fatal("expected attach to initialize a vterm")
	}

	snapshot, err := rt.LoadSnapshot(ctx, "term-1", 0, 10)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}

	stored := rt.Registry().Get("term-1")
	if stored == nil || stored.Snapshot == nil {
		t.Fatal("expected snapshot cached on terminal runtime")
	}
	if stored.Name != "shell" {
		t.Fatalf("expected attach to hydrate terminal metadata name, got %q", stored.Name)
	}
	screen := stored.VTerm.ScreenContent()
	if len(screen.Cells) < 2 || len(screen.Cells[0]) < 5 || len(screen.Cells[1]) < 5 {
		t.Fatalf("unexpected vterm screen dimensions: %#v", screen.Cells)
	}
	if got := screen.Cells[0][0].Content + screen.Cells[0][1].Content + screen.Cells[0][2].Content + screen.Cells[0][3].Content + screen.Cells[0][4].Content; got != "hello" {
		t.Fatalf("expected first row to contain hello, got %q", got)
	}
	if got := screen.Cells[1][0].Content + screen.Cells[1][1].Content + screen.Cells[1][2].Content + screen.Cells[1][3].Content + screen.Cells[1][4].Content; got != "world" {
		t.Fatalf("expected second row to contain world, got %q", got)
	}
}

func TestRuntimeAttachTerminalDoesNotStartStreamBeforeSnapshotLoad(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 7, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}

	if got := client.subscriptionCount(7); got != 0 {
		t.Fatalf("expected attach to avoid starting stream before snapshot load, got %d subscriptions", got)
	}
}

func TestRuntimeLoadSnapshotDoesNotRaceWithAlternateScreenExit(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen: protocol.ScreenData{
			Cells:             [][]protocol.Cell{{{Content: "v", Width: 1}, {Content: "i", Width: 1}}},
			IsAlternateScreen: true,
		},
		Cursor:    protocol.CursorState{Visible: false},
		Modes:     protocol.TerminalModes{AutoWrap: true, AlternateScreen: true, MouseTracking: true},
		Timestamp: time.Now(),
	}
	client.snapshotHook = func() {
		if client.subscriptionCount(9) != 0 {
			t.Fatalf("snapshot load should run before stream subscription")
		}
	}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("\x1b[?1049l\x1b[?25h\x1b[?1002l$\x20")})
	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "$ ")
	})

	stored := rt.Registry().Get("term-1")
	if stored == nil || stored.VTerm == nil {
		t.Fatalf("expected terminal vterm after stream, got %#v", stored)
	}
	if !stored.VTerm.CursorState().Visible {
		t.Fatalf("expected streamed cursor show to win over older state, got %#v", stored.VTerm.CursorState())
	}
	if stored.VTerm.Modes().MouseTracking {
		t.Fatalf("expected streamed mouse disable to win over older state, got %#v", stored.VTerm.Modes())
	}
	if stored.VTerm.Modes().AlternateScreen || stored.VTerm.ScreenContent().IsAlternateScreen {
		t.Fatalf("expected alternate screen exit to win over older state, got modes=%#v alt=%v", stored.VTerm.Modes(), stored.VTerm.ScreenContent().IsAlternateScreen)
	}
}

func TestRuntimeStartStreamUpdatesSurfaceAndInvalidates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	var invalidateCount atomic.Int32
	rt := New(client, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal.VTerm == nil {
		t.Fatal("expected attach to initialize a vterm")
	}

	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("hi")})

	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "hi")
	})

	if invalidateCount.Load() == 0 {
		t.Fatal("expected stream refresh to invalidate rendering")
	}
	if !vtermContains(rt.Registry().Get("term-1").VTerm, "hi") {
		t.Fatal("expected live surface to contain streamed output")
	}
	if rt.Registry().Get("term-1").SurfaceVersion == 0 {
		t.Fatal("expected surface version to advance after stream output")
	}
}

func TestSnapshotFromVTermPrefersRowViewsOverWholeContentCopies(t *testing.T) {
	vt := &countingSnapshotVTerm{VTerm: localvterm.New(8, 2, 16, nil)}
	if _, err := vt.Write([]byte("hello\r\nworld")); err != nil {
		t.Fatalf("seed vterm: %v", err)
	}

	snapshot := snapshotFromVTerm("term-1", vt)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if got := vt.screenContentCalls.Load(); got != 0 {
		t.Fatalf("expected snapshotFromVTerm to avoid ScreenContent copies, got %d calls", got)
	}
	if got := vt.scrollbackContentCalls.Load(); got != 0 {
		t.Fatalf("expected snapshotFromVTerm to avoid ScrollbackContent copies, got %d calls", got)
	}
	if !snapshotContains(snapshot, "hello") || !snapshotContains(snapshot, "world") {
		t.Fatalf("expected row-view snapshot content, got %#v", snapshot)
	}
}

func TestRuntimeScreenUpdateAlsoRefreshesLocalVTermSurface(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil {
		t.Fatal("expected terminal")
	}

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		FullReplace: true,
		Size:        protocol.Size{Cols: 6, Rows: 2},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}},
		},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: protocol.TypeScreenUpdate, Payload: updatePayload})

	if terminal.PreferSnapshot {
		t.Fatalf("expected structured screen update to refresh local vterm surface, got %#v", terminal)
	}
	if terminal.SurfaceVersion == 0 {
		t.Fatalf("expected structured screen update to bump surface version, got %#v", terminal)
	}
	if terminal.VTerm == nil || !vtermContains(terminal.VTerm, "ok") {
		t.Fatalf("expected local vterm to receive structured screen update, got %#v", terminal.VTerm)
	}
	if terminal.Snapshot == nil || !snapshotContains(terminal.Snapshot, "ok") {
		t.Fatalf("expected snapshot to stay synchronized with structured update, got %#v", terminal.Snapshot)
	}
}

func TestRuntimeScreenUpdateUsesIncrementalVTermApplyWhenSupported(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	var counted *incrementalCountingVTerm
	rt := New(client, WithVTermFactory(func(channel uint16) VTermLike {
		counted = &incrementalCountingVTerm{VTerm: localvterm.New(80, 24, 10000, nil)}
		return counted
	}))
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil {
		t.Fatal("expected terminal")
	}
	if counted == nil {
		t.Fatal("expected incremental counting vterm")
	}

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 80, Rows: 24},
		ChangedRows: []protocol.ScreenRowUpdate{{
			Row: 0,
			Cells: []protocol.Cell{
				{Content: "o", Width: 1},
				{Content: "k", Width: 1},
			},
			Timestamp: time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC),
		}},
		Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: protocol.TypeScreenUpdate, Payload: updatePayload})

	if got := counted.partialCalls.Load(); got == 0 {
		t.Fatalf("expected incremental apply path, got partialCalls=%d", got)
	}
	if got := counted.fullLoadCalls.Load(); got != 0 {
		t.Fatalf("expected incremental apply to avoid full snapshot reload, got fullLoadCalls=%d", got)
	}
	if terminal.VTerm == nil || !vtermContains(terminal.VTerm, "ok") {
		t.Fatalf("expected local vterm to receive incremental structured update, got %#v", terminal.VTerm)
	}
}

func TestRuntimeScreenUpdateAppliesScreenScrollShiftToLocalVTerm(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil {
		t.Fatal("expected terminal")
	}

	terminal.Snapshot = snapshotWithLines("term-1", 4, 3, []string{"row1", "row2", "row3"})
	loadSnapshotIntoVTerm(terminal.VTerm, terminal.Snapshot)

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Size:         protocol.Size{Cols: 4, Rows: 3},
		ScreenScroll: 1,
		ChangedRows: []protocol.ScreenRowUpdate{{
			Row:   2,
			Cells: []protocol.Cell{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}, {Content: "4", Width: 1}},
		}},
		Cursor: protocol.CursorState{Row: 2, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: protocol.TypeScreenUpdate, Payload: updatePayload})

	screen := terminal.VTerm.ScreenContent()
	got := []string{
		screen.Cells[0][0].Content + screen.Cells[0][1].Content + screen.Cells[0][2].Content + screen.Cells[0][3].Content,
		screen.Cells[1][0].Content + screen.Cells[1][1].Content + screen.Cells[1][2].Content + screen.Cells[1][3].Content,
		screen.Cells[2][0].Content + screen.Cells[2][1].Content + screen.Cells[2][2].Content + screen.Cells[2][3].Content,
	}
	if !reflect.DeepEqual(got, []string{"row2", "row3", "row4"}) {
		t.Fatalf("expected local vterm screen scroll shift applied, got %#v", got)
	}
}

func TestRuntimeScreenUpdateAppliesOpcodeScrollRectToLocalVTerm(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil {
		t.Fatal("expected terminal")
	}

	terminal.Snapshot = snapshotWithLines("term-1", 4, 4, []string{"row1", "row2", "row3", "row4"})
	loadSnapshotIntoVTerm(terminal.VTerm, terminal.Snapshot)

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Size:         protocol.Size{Cols: 4, Rows: 4},
		ScreenScroll: 1,
		Ops: []protocol.ScreenOp{
			{Code: protocol.ScreenOpScrollRect, Rect: protocol.ScreenRect{X: 0, Y: 0, Width: 4, Height: 4}, Dy: -1},
			{Code: protocol.ScreenOpWriteSpan, Row: 3, Col: 0, Cells: []protocol.Cell{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}, {Content: "5", Width: 1}}},
			{Code: protocol.ScreenOpCursor, Cursor: protocol.CursorState{Row: 3, Col: 0, Visible: true}},
			{Code: protocol.ScreenOpModes, Modes: protocol.TerminalModes{AutoWrap: true}},
		},
		Cursor: protocol.CursorState{Row: 3, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: protocol.TypeScreenUpdate, Payload: updatePayload})

	screen := terminal.VTerm.ScreenContent()
	got := []string{
		screen.Cells[0][0].Content + screen.Cells[0][1].Content + screen.Cells[0][2].Content + screen.Cells[0][3].Content,
		screen.Cells[1][0].Content + screen.Cells[1][1].Content + screen.Cells[1][2].Content + screen.Cells[1][3].Content,
		screen.Cells[2][0].Content + screen.Cells[2][1].Content + screen.Cells[2][2].Content + screen.Cells[2][3].Content,
		screen.Cells[3][0].Content + screen.Cells[3][1].Content + screen.Cells[3][2].Content + screen.Cells[3][3].Content,
	}
	if !reflect.DeepEqual(got, []string{"row2", "row3", "row4", "row5"}) {
		t.Fatalf("expected local vterm opcode scrollrect applied, got %#v", got)
	}
}

func TestRuntimeScreenUpdateTitleOnlyKeepsBootstrapPending(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}
	terminal.Snapshot = snapshotWithLines("term-1", 6, 2, []string{"seed"})
	loadSnapshotIntoVTerm(terminal.VTerm, terminal.Snapshot)
	terminal.BootstrapPending = true

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Title:  "renamed",
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: protocol.TypeScreenUpdate, Payload: updatePayload})

	if !terminal.BootstrapPending {
		t.Fatalf("expected title-only screen update to keep bootstrap pending, got %#v", terminal)
	}
	if terminal.Title != "renamed" {
		t.Fatalf("expected title-only screen update to update title, got %#v", terminal)
	}
	if terminal.Snapshot == nil || !snapshotContains(terminal.Snapshot, "seed") {
		t.Fatalf("expected title-only screen update to preserve existing snapshot content, got %#v", terminal.Snapshot)
	}
}

func TestRuntimeScreenUpdateTitleOnlyKeepsRecoveryState(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}
	terminal.Snapshot = snapshotWithLines("term-1", 6, 2, []string{"seed"})
	loadSnapshotIntoVTerm(terminal.VTerm, terminal.Snapshot)
	terminal.Recovery = RecoveryState{SyncLost: true, DroppedBytes: 9}

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Title:  "renamed",
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: protocol.TypeScreenUpdate, Payload: updatePayload})

	if terminal.Recovery != (RecoveryState{SyncLost: true, DroppedBytes: 9}) {
		t.Fatalf("expected title-only screen update to preserve recovery state, got %#v", terminal.Recovery)
	}
	if terminal.Title != "renamed" {
		t.Fatalf("expected title-only screen update to update title, got %#v", terminal)
	}
}

func TestRuntimeScreenUpdateFullReplaceClearsRecoveryState(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}
	terminal.Snapshot = snapshotWithLines("term-1", 6, 2, []string{"seed"})
	loadSnapshotIntoVTerm(terminal.VTerm, terminal.Snapshot)
	terminal.Recovery = RecoveryState{SyncLost: true, DroppedBytes: 9}

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		FullReplace: true,
		Size:        protocol.Size{Cols: 6, Rows: 2},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			{{Content: "o", Width: 1}, {Content: "k", Width: 1}},
		}},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: protocol.TypeScreenUpdate, Payload: updatePayload})

	if terminal.Recovery != (RecoveryState{}) {
		t.Fatalf("expected full-replace screen update to clear recovery, got %#v", terminal.Recovery)
	}
	if terminal.Snapshot == nil || !snapshotContains(terminal.Snapshot, "ok") {
		t.Fatalf("expected full-replace screen update to refresh snapshot content, got %#v", terminal.Snapshot)
	}
	if !terminal.ScreenUpdate.FullReplace {
		t.Fatalf("expected full-replace screen update summary, got %#v", terminal.ScreenUpdate)
	}
}

func TestNewScreenUpdateContractDeduplicatesChangedRows(t *testing.T) {
	contract := NewScreenUpdateContract(protocol.ScreenUpdate{
		ChangedRows: []protocol.ScreenRowUpdate{
			{Row: 4},
			{Row: 4},
			{Row: 1},
		},
	})

	if !reflect.DeepEqual(contract.Summary.ChangedRows, []int{4, 1}) {
		t.Fatalf("expected changed row summary to deduplicate in wire order, got %#v", contract.Summary.ChangedRows)
	}
}

func TestRuntimeStartStreamCoalescesBurstOutputFrames(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	var invalidateCount atomic.Int32
	var counted *countingVTerm
	rt := New(
		client,
		WithInvalidate(func() {
			invalidateCount.Add(1)
		}),
		WithVTermFactory(func(channel uint16) VTermLike {
			counted = &countingVTerm{VTermLike: localvterm.New(80, 24, 10000, nil)}
			return counted
		}),
	)

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if counted == nil {
		t.Fatal("expected counting vterm to be installed")
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("a")})
	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("b")})
	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("c")})

	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "abc")
	})

	if got := counted.writeCalls.Load(); got != 1 {
		t.Fatalf("expected one coalesced vterm write, got %d", got)
	}
	if got := invalidateCount.Load(); got != 1 {
		t.Fatalf("expected one invalidate after coalesced output, got %d", got)
	}
}

func TestRuntimeHandleStreamFrameDefersSnapshotRefreshDuringSynchronizedOutput(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.Snapshot = snapshotWithLines("term-1", 12, 4, []string{"old state"})
	if vt := rt.ensureVTerm(terminal); vt == nil {
		t.Fatal("expected vterm cache")
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{
		Type:    protocol.TypeOutput,
		Payload: []byte("\x1b[?2026h\x1b[H\x1b[J"),
	})

	if terminal.Snapshot == nil || !snapshotContains(terminal.Snapshot, "old state") {
		t.Fatalf("expected old snapshot to stay visible during synchronized output, got %#v", terminal.Snapshot)
	}
	if invalidateCount.Load() != 0 {
		t.Fatalf("expected no redraw invalidation during synchronized output, got %d", invalidateCount.Load())
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{
		Type:    protocol.TypeOutput,
		Payload: []byte("new state\x1b[?2026l"),
	})

	if !vtermContains(terminal.VTerm, "new state") {
		t.Fatalf("expected synchronized output flush to update live surface, got %#v", terminal.VTerm)
	}
	if invalidateCount.Load() != 1 {
		t.Fatalf("expected exactly one redraw invalidation after synchronized output flush, got %d", invalidateCount.Load())
	}
}

func TestCoalesceClientOutputFramesMergesBurstOutput(t *testing.T) {
	stream := make(chan protocol.StreamFrame, 4)
	stream <- protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("b")}
	stream <- protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("c")}
	close(stream)

	merged, pending, hasPending, ok := coalesceClientOutputFrames(
		protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("a")},
		stream,
		clientOutputBatchDelay,
		StreamState{},
	)

	if merged.Type != protocol.TypeOutput || string(merged.Payload) != "abc" {
		t.Fatalf("expected merged output %q, got %#v", "abc", merged)
	}
	if hasPending {
		t.Fatalf("expected no pending frame, got %#v", pending)
	}
	if ok {
		t.Fatal("expected closed source stream after draining burst output")
	}
}

func TestCoalesceClientOutputFramesPreservesNonOutputBoundary(t *testing.T) {
	stream := make(chan protocol.StreamFrame, 4)
	stream <- protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("b")}
	stream <- protocol.StreamFrame{Type: protocol.TypeResize, Payload: protocol.EncodeResizePayload(120, 40)}
	stream <- protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("c")}

	merged, pending, hasPending, ok := coalesceClientOutputFrames(
		protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("a")},
		stream,
		clientOutputBatchDelay,
		StreamState{},
	)

	if merged.Type != protocol.TypeOutput || string(merged.Payload) != "ab" {
		t.Fatalf("expected merged output %q, got %#v", "ab", merged)
	}
	if !hasPending {
		t.Fatal("expected resize frame to stay pending")
	}
	if pending.Type != protocol.TypeResize {
		t.Fatalf("expected pending resize frame, got %#v", pending)
	}
	if !ok {
		t.Fatal("expected source stream to remain open after boundary frame")
	}
}

func TestRuntimeClientOutputBatchDelayBypassesAfterRecentInput(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "0")
	t.Setenv("TERMX_CLIENT_OUTPUT_BATCH_DELAY", "")
	t.Setenv("TERMX_INTERACTIVE_OUTPUT_BATCH_DELAY", "")

	rt := New(nil)
	if got := rt.clientOutputBatchDelay(); got != clientOutputBatchDelay {
		t.Fatalf("expected default client output batch delay %v, got %v", clientOutputBatchDelay, got)
	}

	rt.noteLocalInput()
	// Interactive bypass reduces delay for all batches during the latency window.
	if got := rt.clientOutputBatchDelay(); got != interactiveOutputBatchDelay {
		t.Fatalf("expected recent local input to shrink output batch delay to %v, got %v", interactiveOutputBatchDelay, got)
	}
	if got := rt.clientOutputBatchDelay(); got != interactiveOutputBatchDelay {
		t.Fatalf("expected interactive bypass to remain active during window, got %v", got)
	}
}

func TestRuntimeClientOutputBatchDelayUsesRemoteProfile(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	t.Setenv("TERMX_CLIENT_OUTPUT_BATCH_DELAY", "")
	t.Setenv("TERMX_INTERACTIVE_OUTPUT_BATCH_DELAY", "")

	rt := New(nil)
	if got := rt.clientOutputBatchDelay(); got != remoteClientOutputBatchDelay {
		t.Fatalf("expected remote client output batch delay %v, got %v", remoteClientOutputBatchDelay, got)
	}

	rt.noteLocalInput()
	// Remote interactive bypass applies to all batches during the latency window.
	if got := rt.clientOutputBatchDelay(); got != remoteInteractiveOutputBatchDelay {
		t.Fatalf("expected remote interactive output batch delay %v, got %v", remoteInteractiveOutputBatchDelay, got)
	}
	if got := rt.clientOutputBatchDelay(); got != remoteInteractiveOutputBatchDelay {
		t.Fatalf("expected remote interactive bypass to remain active during window, got %v", got)
	}
}

func TestEffectiveInteractiveLatencyWindowUsesRemoteProfile(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	t.Setenv("TERMX_INTERACTIVE_LATENCY_WINDOW", "")
	if got := effectiveInteractiveLatencyWindow(); got != remoteInteractiveLatencyWindow {
		t.Fatalf("expected remote interactive latency window %v, got %v", remoteInteractiveLatencyWindow, got)
	}
}

func TestCoalesceClientOutputFramesExitsEarlyOnSynchronizedOutputEnd(t *testing.T) {
	// Sync begin in first frame, sync end arrives mid-batch-window. The coalesce
	// function should merge both frames and return as soon as the group closes,
	// without waiting for the full batch timer to expire.
	stream := make(chan protocol.StreamFrame, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(2 * time.Millisecond)
		stream <- protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("body\x1b[?2026l")}
	}()

	start := time.Now()
	merged, pending, hasPending, _ := coalesceClientOutputFrames(
		protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("\x1b[?2026h")},
		stream,
		10*time.Millisecond, // long batch window — early exit must fire before this
		StreamState{},
	)
	elapsed := time.Since(start)

	<-done
	if merged.Type != protocol.TypeOutput || string(merged.Payload) != "\x1b[?2026hbody\x1b[?2026l" {
		t.Fatalf("expected synchronized output burst to merge, got %#v", merged)
	}
	if hasPending {
		t.Fatalf("expected no pending frame, got %#v", pending)
	}
	// Early exit should fire within a few ms of the sync end arriving, well
	// before the 10ms batch timer would expire.
	if elapsed > 6*time.Millisecond {
		t.Fatalf("expected early exit within ~3ms of sync end, took %v", elapsed)
	}
}

func TestCoalesceClientOutputFramesEarlyExitOnCompleteGroupInFirstFrame(t *testing.T) {
	// Complete sync group (begin + content + end) in the first frame:
	// must return immediately without starting the batch timer.
	stream := make(chan protocol.StreamFrame)
	start := time.Now()
	merged, pending, hasPending, ok := coalesceClientOutputFrames(
		protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("\x1b[?2026hcontent\x1b[?2026l")},
		stream,
		10*time.Millisecond,
		StreamState{},
	)
	elapsed := time.Since(start)

	if string(merged.Payload) != "\x1b[?2026hcontent\x1b[?2026l" {
		t.Fatalf("expected complete sync group, got %#v", merged)
	}
	if hasPending {
		t.Fatalf("unexpected pending: %#v", pending)
	}
	if !ok {
		t.Fatal("expected stream still open after early exit")
	}
	if elapsed > 2*time.Millisecond {
		t.Fatalf("expected immediate return for complete sync group, took %v", elapsed)
	}
}

func TestRuntimeHandleStreamFrameTracksSynchronizedOutputAcrossFrameBoundaries(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.Snapshot = snapshotWithLines("term-1", 12, 4, []string{"steady"})
	if vt := rt.ensureVTerm(terminal); vt == nil {
		t.Fatal("expected vterm cache")
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{
		Type:    protocol.TypeOutput,
		Payload: []byte("\x1b[?20"),
	})
	rt.handleStreamFrame("term-1", protocol.StreamFrame{
		Type:    protocol.TypeOutput,
		Payload: []byte("26h\x1b[H\x1b[J"),
	})

	if terminal.Snapshot == nil || !snapshotContains(terminal.Snapshot, "steady") {
		t.Fatalf("expected split synchronized-output begin to keep old snapshot visible, got %#v", terminal.Snapshot)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{
		Type:    protocol.TypeOutput,
		Payload: []byte("done\x1b[?202"),
	})
	if terminal.Snapshot == nil || !snapshotContains(terminal.Snapshot, "steady") {
		t.Fatalf("expected partial synchronized-output end to keep old snapshot visible, got %#v", terminal.Snapshot)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{
		Type:    protocol.TypeOutput,
		Payload: []byte("6l"),
	})

	if !vtermContains(terminal.VTerm, "done") {
		t.Fatalf("expected split synchronized-output end to flush live surface, got %#v", terminal.VTerm)
	}
	if invalidateCount.Load() == 0 {
		t.Fatal("expected synchronized-output flush to invalidate rendering")
	}
}

func TestRuntimeStreamOutputPreservesAuthoritativeSnapshotSize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 118, 36, []string{"ready"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	stored := rt.Registry().Get("term-1")
	if stored == nil || stored.Snapshot == nil {
		t.Fatalf("expected cached snapshot, got %#v", stored)
	}
	if stored.Snapshot.Size.Cols != 118 || stored.Snapshot.Size.Rows != 36 {
		t.Fatalf("expected loaded snapshot size 118x36, got %#v", stored.Snapshot.Size)
	}

	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}
	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("x")})

	waitFor(t, func() bool {
		current := rt.Registry().Get("term-1")
		return current != nil && vtermContains(current.VTerm, "x")
	})

	current := rt.Registry().Get("term-1")
	if current == nil || current.VTerm == nil {
		t.Fatalf("expected refreshed live surface, got %#v", current)
	}
	cols, rows := current.VTerm.Size()
	if cols != 118 || rows != 36 {
		t.Fatalf("expected streamed output to preserve surface size 118x36, got %dx%d", cols, rows)
	}
}

func TestRuntimeStreamOutputPreservesWideSnapshotCellsAfterReattach(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 8, Rows: 2},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			{
				{Content: "你", Width: 2},
				{Content: "", Width: 0},
				{Content: "好", Width: 2},
				{Content: "", Width: 0},
				{Content: "A", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
			},
			{
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
			},
		}},
		Cursor: protocol.CursorState{Row: 0, Col: 5, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("!")})

	waitFor(t, func() bool {
		current := rt.Registry().Get("term-1")
		if current == nil || current.VTerm == nil {
			return false
		}
		row := current.VTerm.ScreenContent().Cells[0]
		return len(row) > 5 && row[5].Content == "!"
	})

	current := rt.Registry().Get("term-1")
	if current == nil || current.VTerm == nil {
		t.Fatalf("expected refreshed live surface, got %#v", current)
	}
	row := current.VTerm.ScreenContent().Cells[0]
	if got := row[0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected first wide snapshot cell preserved after stream output, got %#v", got)
	}
	if got := row[1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected first continuation preserved after stream output, got %#v", got)
	}
	if got := row[2]; got.Content != "好" || got.Width != 2 {
		t.Fatalf("expected second wide snapshot cell preserved after stream output, got %#v", got)
	}
	if got := row[3]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected second continuation preserved after stream output, got %#v", got)
	}
	if got := row[4]; got.Content != "A" || got.Width != 1 {
		t.Fatalf("expected ASCII cell before stream output preserved, got %#v", got)
	}
	if got := row[5]; got.Content != "!" || got.Width != 1 {
		t.Fatalf("expected streamed output appended after preserved wide cells, got %#v", got)
	}
}

func TestRuntimeSetHostDefaultColorsRefreshesVisibleState(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	initial := rt.Visible()
	if initial == nil {
		t.Fatal("expected visible runtime")
	}
	if initial.HostDefaultFG != "" || initial.HostDefaultBG != "" {
		t.Fatalf("expected empty initial host colors, got fg=%q bg=%q", initial.HostDefaultFG, initial.HostDefaultBG)
	}

	rt.SetHostDefaultColors(color.RGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}, color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff})

	visible := rt.Visible()
	if visible.HostDefaultFG != "#aabbcc" || visible.HostDefaultBG != "#112233" {
		t.Fatalf("expected visible host colors to refresh, got fg=%q bg=%q", visible.HostDefaultFG, visible.HostDefaultBG)
	}
	if invalidateCount.Load() == 0 {
		t.Fatal("expected host default color update to invalidate rendering")
	}
}

func TestRuntimeSetHostPaletteColorRefreshesVisibleState(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	if visible := rt.Visible(); visible == nil {
		t.Fatal("expected visible runtime")
	}

	rt.SetHostPaletteColor(5, color.RGBA{R: 0x44, G: 0x88, B: 0xcc, A: 0xff})

	visible := rt.Visible()
	if got := visible.HostPalette[5]; got != "#4488cc" {
		t.Fatalf("expected visible host palette to refresh, got %q", got)
	}
	if invalidateCount.Load() == 0 {
		t.Fatal("expected host palette update to invalidate rendering")
	}
}

func TestRuntimeApplyHostThemeInvalidatesOnceForBatchedTheme(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	rt.ApplyHostTheme(
		color.RGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff},
		color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff},
		map[int]color.Color{
			1: color.RGBA{R: 0x44, G: 0x88, B: 0xcc, A: 0xff},
			2: color.RGBA{R: 0x55, G: 0x99, B: 0xdd, A: 0xff},
		},
	)

	if got := invalidateCount.Load(); got != 1 {
		t.Fatalf("expected batched host theme apply to invalidate once, got %d", got)
	}
	visible := rt.Visible()
	if visible == nil {
		t.Fatal("expected visible runtime")
	}
	if visible.HostDefaultFG != "#aabbcc" || visible.HostDefaultBG != "#112233" {
		t.Fatalf("unexpected host default colors %#v", visible)
	}
	if visible.HostPalette[1] != "#4488cc" || visible.HostPalette[2] != "#5599dd" {
		t.Fatalf("unexpected host palette %#v", visible.HostPalette)
	}
}

func TestRuntimeApplyHostThemeSilentlyRefreshesVisibleStateWithoutInvalidation(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	rt.ApplyHostThemeSilently(
		color.RGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff},
		color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff},
		map[int]color.Color{5: color.RGBA{R: 0x44, G: 0x88, B: 0xcc, A: 0xff}},
	)

	if got := invalidateCount.Load(); got != 0 {
		t.Fatalf("expected silent host theme apply not to invalidate, got %d", got)
	}
	visible := rt.Visible()
	if visible == nil {
		t.Fatal("expected visible runtime")
	}
	if visible.HostDefaultFG != "#aabbcc" || visible.HostDefaultBG != "#112233" {
		t.Fatalf("unexpected host default colors %#v", visible)
	}
	if visible.HostPalette[5] != "#4488cc" {
		t.Fatalf("unexpected host palette %#v", visible.HostPalette)
	}
}

func TestRuntimeSetHostAmbiguousEmojiVariationSelectorModeRefreshesVisibleState(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	if visible := rt.Visible(); visible == nil {
		t.Fatal("expected visible runtime")
	} else if visible.HostEmojiVS16Mode != shared.AmbiguousEmojiVariationSelectorRaw {
		t.Fatalf("expected raw host emoji mode by default, got %q", visible.HostEmojiVS16Mode)
	}

	rt.SetHostAmbiguousEmojiVariationSelectorMode(shared.AmbiguousEmojiVariationSelectorAdvance)

	visible := rt.Visible()
	if visible.HostEmojiVS16Mode != shared.AmbiguousEmojiVariationSelectorAdvance {
		t.Fatalf("expected visible host emoji mode to refresh, got %q", visible.HostEmojiVS16Mode)
	}
	if invalidateCount.Load() == 0 {
		t.Fatal("expected host emoji mode update to invalidate rendering")
	}
}

func TestRuntimeResizePaneUsesBindingChannelAndRefreshesSnapshot(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 6, 3, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 100, 40); err != nil {
		t.Fatalf("resize pane: %v", err)
	}

	if len(client.resizeCalls) != 1 {
		t.Fatalf("expected 1 resize call, got %d", len(client.resizeCalls))
	}
	call := client.resizeCalls[0]
	if call.channel != 11 || call.cols != 100 || call.rows != 40 {
		t.Fatalf("unexpected resize call: %+v", call)
	}
	stored := rt.Registry().Get("term-1")
	if stored == nil || stored.Snapshot == nil {
		t.Fatal("expected terminal snapshot after resize")
	}
	if stored.Snapshot.Size.Cols != 100 || stored.Snapshot.Size.Rows != 40 {
		t.Fatalf("expected resized snapshot size 100x40, got %dx%d", stored.Snapshot.Size.Cols, stored.Snapshot.Size.Rows)
	}
}

func TestRuntimeResizePaneSkipsFollowerBindings(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 100, 40, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}
	client.resizeCalls = nil

	if err := rt.ResizePane(ctx, "pane-2", "term-1", 50, 16); err != nil {
		t.Fatalf("resize follower: %v", err)
	}

	if len(client.resizeCalls) != 0 {
		t.Fatalf("expected follower resize to be ignored, got %#v", client.resizeCalls)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleOwner {
		t.Fatalf("expected pane-1 to remain owner, got %#v", binding)
	}
	if binding := rt.Binding("pane-2"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-2 to remain follower, got %#v", binding)
	}
}

func TestRuntimeResizePaneSkipsSizeLockedTerminal(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 80, 24, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}
	terminal.Tags = map[string]string{terminalmeta.SizeLockTag: terminalmeta.SizeLockLock}

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 100, 40); err != nil {
		t.Fatalf("resize pane: %v", err)
	}
	if len(client.resizeCalls) != 0 {
		t.Fatalf("expected locked terminal to skip resize call, got %#v", client.resizeCalls)
	}

	visible := rt.Visible()
	if len(visible.Terminals) != 1 || !visible.Terminals[0].SizeLocked {
		t.Fatalf("expected visible runtime to expose locked terminal, got %#v", visible.Terminals)
	}
}

func TestRuntimeResizePaneRefreshesSnapshotWhileBootstrapPending(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 80, 24, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}
	terminal.BootstrapPending = true
	client.resizeCalls = nil

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 57, 36); err != nil {
		t.Fatalf("resize pane: %v", err)
	}

	if len(client.resizeCalls) != 1 {
		t.Fatalf("expected bootstrap-pending resize to reach bridge client, got %#v", client.resizeCalls)
	}
	if terminal.Snapshot == nil || terminal.Snapshot.Size.Cols != 57 || terminal.Snapshot.Size.Rows != 36 {
		t.Fatalf("expected snapshot size refreshed during bootstrap pending resize, got %#v", terminal.Snapshot)
	}
	if cols, rows := terminal.VTerm.Size(); cols != 57 || rows != 36 {
		t.Fatalf("expected live vterm resized during bootstrap pending resize, got %dx%d", cols, rows)
	}
	if terminal.PendingOwnerResize {
		t.Fatalf("expected pending owner resize cleared after resize, got %#v", terminal)
	}
}

func TestRuntimeResizePaneShrinkKeepsRenderOnSnapshotUntilOutput(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 80, 24, []string{"top", "middle", "bottom"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 57, 20); err != nil {
		t.Fatalf("resize pane shrink: %v", err)
	}

	if !terminal.PreferSnapshot {
		t.Fatalf("expected shrink preview to prefer snapshot, got %#v", terminal)
	}
	if terminal.Snapshot == nil || terminal.Snapshot.Size.Cols != 57 || terminal.Snapshot.Size.Rows != 20 {
		t.Fatalf("expected provisional shrink snapshot size 57x20, got %#v", terminal.Snapshot)
	}
	if cols, rows := terminal.VTerm.Size(); cols != 57 || rows != 20 {
		t.Fatalf("expected live vterm resized to 57x20, got %dx%d", cols, rows)
	}
	visible := rt.Visible()
	if len(visible.Terminals) != 1 || visible.Terminals[0].Surface != nil {
		t.Fatalf("expected visible runtime to hide live surface during shrink preview, got %#v", visible.Terminals)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("x")})

	if !terminal.PreferSnapshot {
		t.Fatalf("expected post-resize output to keep shrink preview during resize burst, got %#v", terminal)
	}
	visible = rt.Visible()
	if len(visible.Terminals) != 1 || visible.Terminals[0].Surface != nil {
		t.Fatalf("expected visible runtime to keep live surface hidden during resize burst, got %#v", visible.Terminals)
	}
	if err := rt.SendInput(ctx, "pane-1", []byte("q")); err != nil {
		t.Fatalf("send input: %v", err)
	}
	visible = rt.Visible()
	if len(visible.Terminals) != 1 || visible.Terminals[0].Surface == nil {
		t.Fatalf("expected visible runtime to restore live surface after input clears resize source, got %#v", visible.Terminals)
	}
	if terminal.Snapshot == nil || terminal.Snapshot.Size.Cols != 57 || terminal.Snapshot.Size.Rows != 20 {
		t.Fatalf("expected refreshed snapshot to keep resized geometry, got %#v", terminal.Snapshot)
	}
}

func TestRuntimeResizePaneHeightGrowDoesNotExtendNonBlankBottomRowBackground(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	const statusBG = "#0055aa"
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 4, Rows: 2},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells: [][]protocol.Cell{
				{
					{Content: " ", Width: 1},
					{Content: " ", Width: 1},
					{Content: " ", Width: 1},
					{Content: " ", Width: 1},
				},
				{
					{Content: "S", Width: 1, Style: protocol.CellStyle{BG: statusBG}},
					{Content: "T", Width: 1, Style: protocol.CellStyle{BG: statusBG}},
					{Content: "A", Width: 1, Style: protocol.CellStyle{BG: statusBG}},
					{Content: "T", Width: 1, Style: protocol.CellStyle{BG: statusBG}},
				},
			},
		},
		Cursor:    protocol.CursorState{Row: 1, Col: 4, Visible: true},
		Modes:     protocol.TerminalModes{AutoWrap: true, AlternateScreen: true, MouseTracking: true},
		Timestamp: time.Now(),
	}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 4, 4); err != nil {
		t.Fatalf("resize pane grow: %v", err)
	}

	if len(client.resizeCalls) != 1 {
		t.Fatalf("expected one resize call, got %#v", client.resizeCalls)
	}
	screen := terminal.VTerm.ScreenContent()
	if len(screen.Cells) < 4 {
		t.Fatalf("expected resized vterm height, got %#v", screen.Cells)
	}
	if got := screen.Cells[1][0].Style.BG; got != statusBG {
		t.Fatalf("expected status row to keep background %q, got %#v", statusBG, screen.Cells[1][0])
	}
	for _, point := range []struct {
		row int
		col int
	}{
		{row: 2, col: 0},
		{row: 2, col: 3},
		{row: 3, col: 0},
		{row: 3, col: 3},
	} {
		if got := screen.Cells[point.row][point.col].Style.BG; got != "" {
			t.Fatalf("expected grown row %d col %d to stay unfilled, got %#v", point.row, point.col, screen.Cells[point.row][point.col])
		}
	}
	if terminal.Snapshot == nil || terminal.Snapshot.Size.Rows != 4 {
		t.Fatalf("expected snapshot size refreshed to height 4, got %#v", terminal.Snapshot)
	}
	if got := terminal.Snapshot.Screen.Cells[2][0].Style.BG; got != "" {
		t.Fatalf("expected refreshed snapshot not to extend status background, got %#v", terminal.Snapshot.Screen.Cells[2][0])
	}
}

func TestRuntimeResizeFrameDoesNotExposeLocalShrinkMidStateBeforeOutput(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 80, 24, []string{"top", "middle", "bottom"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 57, 20); err != nil {
		t.Fatalf("resize pane shrink: %v", err)
	}
	rt.handleStreamFrame("term-1", protocol.StreamFrame{
		Type:    protocol.TypeResize,
		Payload: protocol.EncodeResizePayload(57, 20),
	})

	if !terminal.PreferSnapshot {
		t.Fatalf("expected resize frame alone to keep shrink preview on snapshot, got %#v", terminal)
	}
	visible := rt.Visible()
	if len(visible.Terminals) != 1 || visible.Terminals[0].Surface != nil {
		t.Fatalf("expected resize echo not to expose provisional shrink surface, got %#v", visible.Terminals)
	}
}

func TestRuntimeUnbindOwnerLeavesTerminalWithoutOwner(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.UnbindPane("pane-1", "term-1")

	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}
	if terminal.OwnerPaneID != "" {
		t.Fatalf("expected terminal owner cleared, got %q", terminal.OwnerPaneID)
	}
	if !reflect.DeepEqual(terminal.BoundPaneIDs, []string{"pane-2"}) {
		t.Fatalf("expected only pane-2 to remain bound, got %#v", terminal.BoundPaneIDs)
	}
	if binding := rt.Binding("pane-2"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-2 binding to remain follower, got %#v", binding)
	}
	if binding := rt.Binding("pane-1"); binding != nil {
		t.Fatalf("expected pane-1 binding removed, got %#v", binding)
	}
}

func TestRuntimeAcquireTerminalOwnershipPromotesRequestedPane(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	if err := rt.AcquireTerminalOwnership("pane-2", "term-1"); err != nil {
		t.Fatalf("acquire ownership: %v", err)
	}

	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 as owner, got %q", terminal.OwnerPaneID)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-1 demoted to follower, got %#v", binding)
	}
	if binding := rt.Binding("pane-2"); binding == nil || binding.Role != BindingRoleOwner {
		t.Fatalf("expected pane-2 promoted to owner, got %#v", binding)
	}
}

func TestRuntimeAcquireTerminalOwnershipForcesNextOwnerResize(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 50, 16, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}
	if err := rt.AcquireTerminalOwnership("pane-2", "term-1"); err != nil {
		t.Fatalf("acquire ownership: %v", err)
	}

	client.resizeCalls = nil
	if err := rt.ResizePane(ctx, "pane-2", "term-1", 50, 16); err != nil {
		t.Fatalf("resize pane: %v", err)
	}

	if len(client.resizeCalls) != 1 {
		t.Fatalf("expected forced resize after owner handoff, got %#v", client.resizeCalls)
	}
	if got := rt.Registry().Get("term-1"); got == nil || got.PendingOwnerResize {
		t.Fatalf("expected pending owner resize cleared after resize, got %#v", got)
	}
}

func TestRuntimeResizeDoesNothingWithoutExplicitOwner(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.UnbindPane("pane-1", "term-1")

	if err := rt.ResizePane(ctx, "pane-2", "term-1", 100, 30); err != nil {
		t.Fatalf("resize pane: %v", err)
	}
	if len(client.resizeCalls) != 0 {
		t.Fatalf("expected no resize calls without explicit owner, got %#v", client.resizeCalls)
	}
}

func TestRuntimeApplySessionLeasesDemotesForeignLeaseAndPromotesLocalLease(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.ApplySessionLeases("view-local", []protocol.LeaseInfo{{
		TerminalID: "term-1",
		ViewID:     "view-remote",
		PaneID:     "pane-9",
	}})

	if terminal := rt.Registry().Get("term-1"); terminal == nil || terminal.OwnerPaneID != "pane-9" || terminal.ControlPaneID != "" || !terminal.RequiresExplicitOwner {
		t.Fatalf("expected foreign lease to demote local panes, got %#v", terminal)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-1 follower under foreign lease, got %#v", binding)
	}

	rt.ApplySessionLeases("view-local", []protocol.LeaseInfo{{
		TerminalID: "term-1",
		ViewID:     "view-local",
		PaneID:     "pane-2",
	}})

	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.OwnerPaneID != "pane-2" || terminal.RequiresExplicitOwner {
		t.Fatalf("expected local lease to promote pane-2 owner, got %#v", terminal)
	}
	if !terminal.PendingOwnerResize {
		t.Fatalf("expected local lease promotion to force next resize, got %#v", terminal)
	}
	if binding := rt.Binding("pane-2"); binding == nil || binding.Role != BindingRoleOwner {
		t.Fatalf("expected pane-2 owner under local lease, got %#v", binding)
	}
}

func TestRuntimeApplySessionLeasesDemotesLocalPaneWhenForeignLeaseReusesSamePaneID(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}

	rt.ApplySessionLeases("view-local", []protocol.LeaseInfo{{
		TerminalID: "term-1",
		ViewID:     "view-remote",
		PaneID:     "pane-1",
	}})

	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.OwnerPaneID != "pane-1" || terminal.ControlPaneID != "" || !terminal.RequiresExplicitOwner {
		t.Fatalf("expected foreign lease on same pane id to demote local control, got %#v", terminal)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-1 demoted to follower under foreign lease, got %#v", binding)
	}
}

func TestRuntimeApplySessionLeasesRefreshesVisibleOwnerWhenForeignLeaseOwnerChanges(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}

	rt.ApplySessionLeases("view-local", []protocol.LeaseInfo{{
		TerminalID: "term-1",
		ViewID:     "view-remote",
		PaneID:     "pane-9",
	}})
	visible := rt.Visible()
	if len(visible.Terminals) == 0 || visible.Terminals[0].OwnerPaneID != "pane-9" {
		t.Fatalf("expected visible runtime owner pane-9 after first foreign lease, got %#v", visible.Terminals)
	}

	rt.ApplySessionLeases("view-local", []protocol.LeaseInfo{{
		TerminalID: "term-1",
		ViewID:     "view-remote",
		PaneID:     "pane-10",
	}})
	visible = rt.Visible()
	if len(visible.Terminals) == 0 || visible.Terminals[0].OwnerPaneID != "pane-10" {
		t.Fatalf("expected visible runtime owner pane-10 after foreign lease owner change, got %#v", visible.Terminals)
	}
}

func TestRuntimeApplySessionLeasesPreservesFirstLocalOwnerWithoutLease(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}

	rt.ApplySessionLeases("view-local", nil)

	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}
	if terminal.OwnerPaneID != "pane-1" || terminal.ControlPaneID != "pane-1" {
		t.Fatalf("expected first local attach to stay owner without lease, got %#v", terminal)
	}
	if terminal.RequiresExplicitOwner {
		t.Fatalf("expected first local attach not to require explicit owner, got %#v", terminal)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleOwner {
		t.Fatalf("expected pane-1 to remain owner, got %#v", binding)
	}
}

func TestRuntimeShouldAcquireTerminalOwnershipRequiresExplicitTakeoverAfterOwnerRelease(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.UnbindPane("pane-1", "term-1")

	if !rt.ShouldAcquireTerminalOwnership("term-1", TerminalOwnershipRequest{
		PaneID:           "pane-2",
		ExplicitTakeover: true,
	}) {
		t.Fatalf("expected explicit control reclaim to require ownership acquire, got %#v", rt.TerminalControlStatus("term-1"))
	}
}

func TestRuntimeResizeDecisionRequiresExplicitOwnerAfterOwnerRelease(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.UnbindPane("pane-1", "term-1")

	decision := rt.ResizeDecision("pane-2", "term-1")
	if decision.Allowed {
		t.Fatalf("expected resize decision to deny pane-2 without explicit owner, got %#v", decision)
	}
	if !decision.Status.RequiresExplicitOwner {
		t.Fatalf("expected resize decision to surface explicit-owner requirement, got %#v", decision)
	}
}

func TestRuntimeAttachSnapshotInputAndResize(t *testing.T) {
	rt, ctx := newTestRuntime(t)

	created, err := rt.client.Create(ctx, protocol.CreateParams{
		Command: []string{"sh"},
		Name:    "demo",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	terminal, err := rt.AttachTerminal(ctx, "pane-1", created.TerminalID, "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal.Channel == 0 {
		t.Fatal("expected non-zero channel")
	}
	binding := rt.Binding("pane-1")
	if binding == nil || !binding.Connected {
		t.Fatal("expected connected pane binding")
	}

	snapshot, err := rt.LoadSnapshot(ctx, created.TerminalID, 0, 10)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if terminal.Snapshot == nil {
		t.Fatal("expected snapshot cached on terminal runtime")
	}

	if err := rt.SendInput(ctx, "pane-1", []byte("echo hi\n")); err != nil {
		t.Fatalf("send input: %v", err)
	}
	if err := rt.ResizePane(ctx, "pane-1", created.TerminalID, 100, 40); err != nil {
		t.Fatalf("resize terminal: %v", err)
	}
}

func TestRuntimeAttachDoesNotRetainStructuralTerminalIDInBinding(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}

	if _, ok := reflect.TypeOf(PaneBinding{}).FieldByName("TerminalID"); ok {
		t.Fatal("expected PaneBinding to stop storing structural TerminalID")
	}
	binding := rt.Binding("pane-1")
	if binding == nil || !binding.Connected || binding.Channel != 11 {
		t.Fatalf("expected connected binding with channel only, got %#v", binding)
	}
}

func TestRuntimeReattachSamePaneCleansPreviousTerminalBindingState(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach term-1: %v", err)
	}

	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-2", "collaborator"); err != nil {
		t.Fatalf("attach term-2: %v", err)
	}

	oldTerminal := rt.Registry().Get("term-1")
	if oldTerminal == nil {
		t.Fatal("expected old terminal runtime to remain present")
	}
	if oldTerminal.OwnerPaneID != "" {
		t.Fatalf("expected old terminal owner to be cleared, got %q", oldTerminal.OwnerPaneID)
	}
	if len(oldTerminal.BoundPaneIDs) != 0 {
		t.Fatalf("expected old terminal bound panes cleared, got %#v", oldTerminal.BoundPaneIDs)
	}

	newTerminal := rt.Registry().Get("term-2")
	if newTerminal == nil {
		t.Fatal("expected new terminal runtime")
	}
	if newTerminal.OwnerPaneID != "pane-1" {
		t.Fatalf("expected new terminal owner pane-1, got %q", newTerminal.OwnerPaneID)
	}
	if !reflect.DeepEqual(newTerminal.BoundPaneIDs, []string{"pane-1"}) {
		t.Fatalf("expected new terminal bound panes [pane-1], got %#v", newTerminal.BoundPaneIDs)
	}

	binding := rt.Binding("pane-1")
	if binding == nil || binding.Channel != 12 || binding.Role != BindingRoleOwner || !binding.Connected {
		t.Fatalf("expected binding reassigned to new channel/owner state, got %#v", binding)
	}
}

type fakeBridgeClient struct {
	mu                  sync.Mutex
	attachResult        *protocol.AttachResult
	listResult          *protocol.ListResult
	snapshotByTerminal  map[string]*protocol.Snapshot
	snapshotHook        func()
	streams             map[uint16]chan protocol.StreamFrame
	streamSubscriptions map[uint16]int
	streamStops         map[uint16]int
	inputCalls          []inputCall
	resizeCalls         []resizeCall
}

type countingVTerm struct {
	VTermLike
	writeCalls atomic.Int32
}

func (v *countingVTerm) Write(data []byte) (int, error) {
	if v == nil || v.VTermLike == nil {
		return 0, nil
	}
	v.writeCalls.Add(1)
	return v.VTermLike.Write(data)
}

type incrementalCountingVTerm struct {
	*localvterm.VTerm
	partialCalls  atomic.Int32
	fullLoadCalls atomic.Int32
}

func (v *incrementalCountingVTerm) ApplyScreenUpdate(update protocol.ScreenUpdate) bool {
	if v == nil || v.VTerm == nil {
		return false
	}
	v.partialCalls.Add(1)
	return v.VTerm.ApplyScreenUpdate(update)
}

func (v *incrementalCountingVTerm) LoadSnapshotWithMetadata(scrollback [][]localvterm.Cell, scrollbackTimestamps []time.Time, scrollbackRowKinds []string, screen localvterm.ScreenData, screenTimestamps []time.Time, screenRowKinds []string, cursor localvterm.CursorState, modes localvterm.TerminalModes) {
	if v == nil || v.VTerm == nil {
		return
	}
	v.fullLoadCalls.Add(1)
	v.VTerm.LoadSnapshotWithMetadata(scrollback, scrollbackTimestamps, scrollbackRowKinds, screen, screenTimestamps, screenRowKinds, cursor, modes)
}

type countingSnapshotVTerm struct {
	*localvterm.VTerm
	screenContentCalls     atomic.Int32
	scrollbackContentCalls atomic.Int32
}

func (v *countingSnapshotVTerm) ScreenContent() localvterm.ScreenData {
	v.screenContentCalls.Add(1)
	return v.VTerm.ScreenContent()
}

func (v *countingSnapshotVTerm) ScrollbackContent() [][]localvterm.Cell {
	v.scrollbackContentCalls.Add(1)
	return v.VTerm.ScrollbackContent()
}

type inputCall struct {
	channel uint16
	data    []byte
}

type resizeCall struct {
	channel uint16
	cols    uint16
	rows    uint16
}

func newFakeBridgeClient() *fakeBridgeClient {
	return &fakeBridgeClient{
		listResult:          &protocol.ListResult{},
		snapshotByTerminal:  make(map[string]*protocol.Snapshot),
		streams:             make(map[uint16]chan protocol.StreamFrame),
		streamSubscriptions: make(map[uint16]int),
		streamStops:         make(map[uint16]int),
	}
}

func (f *fakeBridgeClient) Close() error { return nil }

func (f *fakeBridgeClient) Create(context.Context, protocol.CreateParams) (*protocol.CreateResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) SetTags(context.Context, string, map[string]string) error { return nil }

func (f *fakeBridgeClient) SetMetadata(context.Context, string, string, map[string]string) error {
	return nil
}

func (f *fakeBridgeClient) List(context.Context) (*protocol.ListResult, error) {
	return f.listResult, nil
}

func (f *fakeBridgeClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) Attach(context.Context, string, string) (*protocol.AttachResult, error) {
	if f.attachResult == nil {
		return nil, fmt.Errorf("attach result not configured")
	}
	return f.attachResult, nil
}

func (f *fakeBridgeClient) Snapshot(context.Context, string, int, int) (*protocol.Snapshot, error) {
	f.mu.Lock()
	var snapshot *protocol.Snapshot
	hook := f.snapshotHook
	for _, candidate := range f.snapshotByTerminal {
		snapshot = cloneSnapshot(candidate)
		break
	}
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if snapshot != nil {
		return snapshot, nil
	}
	return nil, fmt.Errorf("snapshot not configured")
}

func (f *fakeBridgeClient) Input(_ context.Context, channel uint16, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputCalls = append(f.inputCalls, inputCall{channel: channel, data: append([]byte(nil), data...)})
	return nil
}

func (f *fakeBridgeClient) Resize(_ context.Context, channel uint16, cols, rows uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resizeCalls = append(f.resizeCalls, resizeCall{channel: channel, cols: cols, rows: rows})
	return nil
}

func (f *fakeBridgeClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stream := f.streams[channel]
	if stream == nil {
		stream = make(chan protocol.StreamFrame, 16)
		f.streams[channel] = stream
	}
	f.streamSubscriptions[channel]++
	return stream, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.streamStops[channel]++
	}
}

func (f *fakeBridgeClient) Kill(context.Context, string) error { return nil }

func (f *fakeBridgeClient) Restart(context.Context, string) error { return nil }

func (f *fakeBridgeClient) CreateSession(context.Context, protocol.CreateSessionParams) (*protocol.SessionSnapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) ListSessions(context.Context) (*protocol.ListSessionsResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) GetSession(context.Context, string) (*protocol.SessionSnapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) AttachSession(context.Context, protocol.AttachSessionParams) (*protocol.SessionSnapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) DetachSession(context.Context, string, string) error {
	return fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) ApplySession(context.Context, protocol.ApplySessionParams) (*protocol.SessionSnapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) ReplaceSession(context.Context, protocol.ReplaceSessionParams) (*protocol.SessionSnapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) UpdateSessionView(context.Context, protocol.UpdateSessionViewParams) (*protocol.ViewInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) AcquireSessionLease(context.Context, protocol.AcquireSessionLeaseParams) (*protocol.LeaseInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) ReleaseSessionLease(context.Context, protocol.ReleaseSessionLeaseParams) error {
	return fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) sendFrame(channel uint16, frame protocol.StreamFrame) {
	f.mu.Lock()
	stream := f.streams[channel]
	f.mu.Unlock()
	if stream == nil {
		panic("stream not initialized")
	}
	stream <- frame
}

func (f *fakeBridgeClient) closeStream(channel uint16) {
	f.mu.Lock()
	stream := f.streams[channel]
	delete(f.streams, channel)
	f.mu.Unlock()
	if stream != nil {
		close(stream)
	}
}

func (f *fakeBridgeClient) subscriptionCount(channel uint16) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streamSubscriptions[channel]
}

func (f *fakeBridgeClient) stopCount(channel uint16) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streamStops[channel]
}

func snapshotWithLines(terminalID string, cols, rows uint16, lines []string) *protocol.Snapshot {
	grid := make([][]protocol.Cell, rows)
	for y := range rows {
		grid[y] = make([]protocol.Cell, cols)
		for x := range cols {
			grid[y][x] = protocol.Cell{Content: " ", Width: 1}
		}
	}
	for y, line := range lines {
		if y >= int(rows) {
			break
		}
		for x := 0; x < len(line) && x < int(cols); x++ {
			grid[y][x] = protocol.Cell{Content: string(line[x]), Width: 1}
		}
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: cols, Rows: rows},
		Screen:     protocol.ScreenData{Cells: grid},
		Cursor:     protocol.CursorState{Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
		Timestamp:  time.Now(),
	}
}

func cloneSnapshot(snapshot *protocol.Snapshot) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.Screen.Cells = make([][]protocol.Cell, len(snapshot.Screen.Cells))
	for y, row := range snapshot.Screen.Cells {
		cloned.Screen.Cells[y] = append([]protocol.Cell(nil), row...)
	}
	cloned.Scrollback = make([][]protocol.Cell, len(snapshot.Scrollback))
	for y, row := range snapshot.Scrollback {
		cloned.Scrollback[y] = append([]protocol.Cell(nil), row...)
	}
	return &cloned
}

func snapshotContains(snapshot *protocol.Snapshot, want string) bool {
	if snapshot == nil {
		return false
	}
	for _, row := range snapshot.Screen.Cells {
		var buf bytes.Buffer
		for _, cell := range row {
			buf.WriteString(cell.Content)
		}
		if bytes.Contains(buf.Bytes(), []byte(want)) {
			return true
		}
	}
	return false
}

func vtermContains(vt VTermLike, want string) bool {
	if vt == nil {
		return false
	}
	screen := vt.ScreenContent()
	for _, row := range screen.Cells {
		var buf bytes.Buffer
		for _, cell := range row {
			buf.WriteString(cell.Content)
		}
		if bytes.Contains(buf.Bytes(), []byte(want)) {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestRuntimeStartStreamReconnectsAfterChannelClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	rt := New(client)

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("one")})
	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "one")
	})

	client.closeStream(9)

	waitFor(t, func() bool {
		return client.subscriptionCount(9) >= 2
	})

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("two")})
	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "two")
	})

	stored := rt.Registry().Get("term-1")
	if stored == nil {
		t.Fatal("expected terminal runtime after reconnect")
	}
	if stored.Stream.RetryCount != 0 {
		t.Fatalf("expected retry count reset after successful frame, got %d", stored.Stream.RetryCount)
	}
	if !stored.Stream.Active {
		t.Fatal("expected stream to be active after reconnect")
	}
}

func TestRuntimeReattachIgnoresLateFramesFromPreviousStreamGeneration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	client.listResult = &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
		ID:    "term-1",
		Name:  "shared",
		State: "running",
	}}}
	rt := New(client)

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start initial stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("seed")})
	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "seed")
	})

	client.attachResult = &protocol.AttachResult{Channel: 10, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("reattach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start replacement stream: %v", err)
	}

	waitFor(t, func() bool { return client.stopCount(9) > 0 })
	waitFor(t, func() bool { return client.subscriptionCount(10) > 0 })

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("stale")})
	client.sendFrame(10, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("fresh")})

	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "fresh")
	})

	stored := rt.Registry().Get("term-1")
	if stored == nil {
		t.Fatal("expected terminal runtime after reattach")
	}
	if vtermContains(stored.VTerm, "stale") {
		t.Fatalf("expected stale frame from previous stream generation to be ignored, got %#v", stored.VTerm.ScreenContent())
	}
}

func TestRuntimeStreamResizeFrameRefreshesSnapshotGeometryDuringBootstrap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 80, 24, []string{"initial"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil {
		t.Fatal("expected terminal with VTerm")
	}
	cols, rows := terminal.VTerm.Size()
	if cols != 80 || rows != 24 {
		t.Fatalf("expected initial VTerm size 80x24, got %dx%d", cols, rows)
	}

	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	// Simulate server sending a resize frame (as if the owner resized the PTY)
	client.sendFrame(9, protocol.StreamFrame{
		Type:    protocol.TypeResize,
		Payload: protocol.EncodeResizePayload(120, 40),
	})

	waitFor(t, func() bool {
		c, r := terminal.VTerm.Size()
		return c == 120 && r == 40
	})

	cols, rows = terminal.VTerm.Size()
	if cols != 120 || rows != 40 {
		t.Fatalf("expected VTerm resized to 120x40 after resize frame, got %dx%d", cols, rows)
	}

	waitFor(t, func() bool {
		return terminal.Snapshot != nil && terminal.Snapshot.Size.Cols == 120 && terminal.Snapshot.Size.Rows == 40
	})
	if !snapshotContains(terminal.Snapshot, "initial") {
		t.Fatalf("expected bootstrap resize to preserve provisional snapshot content, got %#v", terminal.Snapshot)
	}

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeBootstrapDone})
	waitFor(t, func() bool {
		return terminal.Snapshot != nil && terminal.Snapshot.Size.Cols == 120 && terminal.Snapshot.Size.Rows == 40
	})

	// Subsequent output should be processed correctly at the new size.
	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("after-resize")})
	waitFor(t, func() bool {
		return vtermContains(terminal.VTerm, "after-resize")
	})
}

func TestRuntimeClosedStreamStopsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	rt := New(client)

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeClosed, Payload: protocol.EncodeClosedPayload(0)})

	waitFor(t, func() bool {
		terminal := rt.Registry().Get("term-1")
		return terminal != nil && terminal.State == "exited"
	})
	waitFor(t, func() bool {
		return client.stopCount(9) == 1
	})
}
