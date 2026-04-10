package app

import (
	"io"
	"testing"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestFloatingMoveTraceCompletesMouseDragOnFrameFlush(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	model := setupFloatingTraceModel(t, true)
	model.SetFrameWriter(newOutputCursorWriter(io.Discard))
	_ = model.View()

	recorder := perftrace.Enable()
	defer perftrace.Disable()
	recorder.Reset()

	model.mouseDragPaneID = "float-1"
	model.mouseDragMode = mouseDragMove
	model.mouseDragOffsetX = 5
	model.mouseDragOffsetY = 0

	_ = model.handleMouseDrag(16, screenYForBodyY(model, 5))
	_ = model.View()

	traces := model.moveTrace.Snapshot()
	if len(traces) == 0 {
		t.Fatal("expected floating move trace")
	}
	trace := traces[len(traces)-1]
	if trace.Source != "mouse" {
		t.Fatalf("expected mouse source, got %#v", trace)
	}
	if trace.Action != "drag_move" {
		t.Fatalf("expected drag_move action, got %#v", trace)
	}
	if trace.Outcome != "frame.flushed" {
		t.Fatalf("expected frame.flushed outcome, got %#v", trace)
	}
	if trace.StartRect == trace.EndRect {
		t.Fatalf("expected move trace rect to change, got %#v", trace)
	}
	assertTraceHasStage(t, trace, "mutation.changed")
	assertTraceHasStage(t, trace, "render.invalidate")
	assertTraceHasStage(t, trace, "view.start")
	assertTraceHasStage(t, trace, "render.done")
	assertTraceHasStage(t, trace, "frame.flush.start")

	snapshot := perftrace.SnapshotCurrent()
	if event, ok := snapshot.Event("app.floating_move.mouse.drag_move.latency.total"); !ok || event.Count != 1 {
		t.Fatalf("expected one mouse move total latency metric, got %#v", event)
	}
	for _, name := range []string{"render.body", "render.body.canvas", "cursor_writer.present", "cursor_writer.io_write"} {
		if event, ok := snapshot.Event(name); !ok || event.Count == 0 {
			t.Fatalf("expected perf event %q, got %#v", name, event)
		}
	}
}

func TestFloatingMoveTraceCompletesKeyboardMoveOnFrameFlush(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	model := setupFloatingTraceModel(t, true)
	model.SetFrameWriter(newOutputCursorWriter(io.Discard))
	_ = model.View()

	recorder := perftrace.Enable()
	defer perftrace.Disable()
	recorder.Reset()

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionMoveFloatingRight, PaneID: "float-1"})
	_ = model.View()

	traces := model.moveTrace.Snapshot()
	if len(traces) == 0 {
		t.Fatal("expected floating move trace")
	}
	trace := traces[len(traces)-1]
	if trace.Source != "keyboard" {
		t.Fatalf("expected keyboard source, got %#v", trace)
	}
	if trace.Action != string(input.ActionMoveFloatingRight) {
		t.Fatalf("expected keyboard action %q, got %#v", input.ActionMoveFloatingRight, trace)
	}
	if trace.Outcome != "frame.flushed" {
		t.Fatalf("expected frame.flushed outcome, got %#v", trace)
	}
	if trace.StartRect == trace.EndRect {
		t.Fatalf("expected keyboard move rect to change, got %#v", trace)
	}
	assertTraceHasStage(t, trace, "mutation.begin")
	assertTraceHasStage(t, trace, "mutation.changed")
	assertTraceHasStage(t, trace, "render.invalidate")
	assertTraceHasStage(t, trace, "frame.write.submit")
	assertTraceHasStage(t, trace, "frame.flush.start")

	snapshot := perftrace.SnapshotCurrent()
	if event, ok := snapshot.Event("app.floating_move.keyboard.move_floating_right.latency.total"); !ok || event.Count != 1 {
		t.Fatalf("expected one keyboard move total latency metric, got %#v", event)
	}
}

func BenchmarkFloatingMoveEndToEnd(b *testing.B) {
	for _, tc := range []struct {
		name   string
		styled bool
	}{
		{name: "plain_snapshot", styled: false},
		{name: "styled_snapshot", styled: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			originalDelay := directFrameBatchDelay
			directFrameBatchDelay = 0
			defer func() { directFrameBatchDelay = originalDelay }()

			model := setupFloatingTraceBenchmarkModel(b, tc.styled)
			model.SetFrameWriter(newOutputCursorWriter(io.Discard))
			_ = model.View()

			y := screenYForBodyY(model, 5)
			xs := [2]int{16, 17}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = model.handleMouseDrag(xs[i&1], y)
				_ = model.View()
			}
		})
	}
}

func setupFloatingTraceModel(t testing.TB, styled bool) *Model {
	t.Helper()
	model := setupFloatingTraceBaseModel(t)
	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 48, H: 14}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-float"); err != nil {
		t.Fatalf("bind floating pane: %v", err)
	}
	_ = model.workbench.FocusPane(tab.ID, "float-1")
	model.workbench.ReorderFloatingPane(tab.ID, "float-1", true)

	terminal := model.runtime.Registry().GetOrCreate("term-float")
	terminal.Name = "term-float"
	terminal.State = "running"
	if styled {
		terminal.Snapshot = cursorWriterStyledSnapshot("term-float", 46, 12)
	} else {
		terminal.Snapshot = floatingMovePlainSnapshot("term-float", 46, 12)
	}
	binding := model.runtime.BindPane("float-1")
	binding.Channel = 7
	binding.Connected = true

	model.mouseDragPaneID = "float-1"
	model.mouseDragMode = mouseDragMove
	model.mouseDragOffsetX = 5
	model.mouseDragOffsetY = 0
	return model
}

func setupFloatingTraceBenchmarkModel(b testing.TB, styled bool) *Model {
	b.Helper()
	return setupFloatingTraceModel(b, styled)
}

func floatingMovePlainSnapshot(terminalID string, cols, rows int) *protocol.Snapshot {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	screen := make([][]protocol.Cell, 0, rows)
	for y := 0; y < rows; y++ {
		row := make([]protocol.Cell, 0, cols)
		for x := 0; x < cols; x++ {
			cell := protocol.Cell{Content: " ", Width: 1}
			if x > 1 && x < cols-2 {
				cell.Content = string(rune('a' + (x+y)%26))
			}
			row = append(row, cell)
		}
		screen = append(screen, row)
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen:     protocol.ScreenData{Cells: screen},
		Cursor:     protocol.CursorState{Row: 0, Col: 0, Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
	}
}

func assertTraceHasStage(t *testing.T, trace floatingMoveTraceRecord, want string) {
	t.Helper()
	for _, stage := range trace.Stages {
		if stage.Name == want {
			return
		}
	}
	t.Fatalf("expected trace stage %q, got %#v", want, trace.Stages)
}

func setupFloatingTraceBaseModel(tb testing.TB) *Model {
	tb.Helper()
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	rt := runtime.New(client)
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt.Registry().GetOrCreate("term-1").Name = "shell"
	rt.Registry().Get("term-1").State = "running"
	rt.Registry().Get("term-1").Channel = 1
	binding := rt.BindPane("pane-1")
	binding.Channel = 1
	binding.Connected = true

	model := New(shared.Config{}, wb, rt)
	model.width = 120
	model.height = 40
	return model
}
