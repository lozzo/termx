package render

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestRenderBodyAltScreenFastPathBypassesCanvasForSingleFramelessPane(t *testing.T) {
	recorder := perftrace.Enable()
	defer perftrace.Disable()

	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			ZoomedPaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "vim", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Name = "vim"
	rt.Registry().Get("term-1").State = "running"
	rt.Registry().Get("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 6},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells: [][]protocol.Cell{
				repeatCells("alpha"),
				repeatCells("bravo"),
				repeatCells("charl"),
				repeatCells("delta"),
				repeatCells("echo "),
				repeatCells("foxt "),
			},
		},
		Cursor: protocol.CursorState{Row: 1, Col: 1, Visible: true, Shape: "block"},
		Modes:  protocol.TerminalModes{AlternateScreen: true, MouseTracking: true, BracketedPaste: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 6), 20, 8)
	vm := RenderVMFromVisibleState(state)
	lookup := newRuntimeLookup(vm.Runtime)
	tab := vm.Workbench.Tabs[vm.Workbench.ActiveTab]
	entries := paneEntriesForTab(tab, vm.Workbench.FloatingPanes, 20, 6, lookup, bodyProjectionOptionsForVM(vm, true), uiThemeForRuntime(vm.Runtime))
	if len(entries) != 1 {
		t.Fatalf("expected one render entry, got %#v", entries)
	}
	if !entries[0].Frameless {
		t.Fatalf("expected zoomed pane entry to be frameless, got %#v", entries[0])
	}
	if _, ok := renderAltScreenFastPathVM(vm, entries, 0); !ok {
		t.Fatalf("expected helper fast path to activate, entry=%#v", entries[0])
	}
	body := renderBodyFrameWithCoordinatorVM(nil, vm, 20, 6)

	snapshot := recorder.Snapshot()
	if event, ok := snapshot.Event("render.body.alt_screen_fast_path"); !ok || event.Count == 0 {
		t.Fatalf("expected alt-screen fast path metric, got %#v", snapshot.Events)
	}
	if _, ok := snapshot.Event("render.body.canvas"); ok {
		t.Fatalf("expected single-pane alt-screen fast path to bypass body canvas, got %#v", snapshot.Events)
	}

	if got := len(body.lines); got != 6 {
		t.Fatalf("expected 6 body lines, got %d", got)
	}
	frame := xansi.Strip(strings.Join(body.lines, "\n"))
	if strings.Contains(frame, "vim") {
		t.Fatalf("expected frameless fast path to omit pane chrome, got:\n%s", frame)
	}
	if !strings.Contains(frame, "alpha") || !strings.Contains(frame, "delta") || !strings.Contains(frame, "foxt") {
		t.Fatalf("expected fast path body to preserve content rows, got:\n%s", frame)
	}

	target, ok := activeEntryCursorRenderTarget(entries, vm.Runtime)
	if !ok {
		t.Fatal("expected active cursor target")
	}
	if got, want := body.cursor, hostHiddenCursorANSI(target.X, target.Y, target.Shape, target.Blink); got != want {
		t.Fatalf("expected fast path cursor %q, got %q", want, got)
	}
}

func TestRenderBodyAltScreenFastPathStaysDisabledForSingleFramedPane(t *testing.T) {
	recorder := perftrace.Enable()
	defer perftrace.Disable()

	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "vim", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Name = "vim"
	rt.Registry().Get("term-1").State = "running"
	rt.Registry().Get("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 17, Rows: 4},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells: [][]protocol.Cell{
				repeatCells("alpha"),
				repeatCells("bravo"),
				repeatCells("charl"),
				repeatCells("delta"),
			},
		},
		Cursor: protocol.CursorState{Row: 1, Col: 1, Visible: true, Shape: "block"},
		Modes:  protocol.TerminalModes{AlternateScreen: true, MouseTracking: true, BracketedPaste: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 6), 20, 8)
	vm := RenderVMFromVisibleState(state)
	lookup := newRuntimeLookup(vm.Runtime)
	tab := vm.Workbench.Tabs[vm.Workbench.ActiveTab]
	entries := paneEntriesForTab(tab, vm.Workbench.FloatingPanes, 20, 6, lookup, bodyProjectionOptionsForVM(vm, true), uiThemeForRuntime(vm.Runtime))
	if len(entries) != 1 {
		t.Fatalf("expected one render entry, got %#v", entries)
	}
	if entries[0].Frameless {
		t.Fatalf("expected framed pane entry, got %#v", entries[0])
	}
	if _, ok := renderAltScreenFastPathVM(vm, entries, TopChromeRows); ok {
		t.Fatalf("expected framed single-pane alt-screen to stay on canvas path, entry=%#v", entries[0])
	}
	body := xansi.Strip(strings.Join(renderBodyFrameWithCoordinatorVM(nil, vm, 20, 6).lines, "\n"))

	snapshot := recorder.Snapshot()
	if _, ok := snapshot.Event("render.body.alt_screen_fast_path"); ok {
		t.Fatalf("expected framed single pane to bypass alt-screen fast path, got %#v", snapshot.Events)
	}
	if _, ok := snapshot.Event("render.body.canvas"); !ok {
		t.Fatalf("expected framed single pane to use body canvas, got %#v", snapshot.Events)
	}

	lines := strings.Split(body, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected rendered body lines, got %q", body)
	}
	if got := strings.Count(lines[1], "│"); got != 2 {
		t.Fatalf("expected framed single-pane content row to keep one left/right border pair, got %d in %q", got, lines[1])
	}
}

func TestRenderBodyAltScreenFastPathStaysDisabledForSplitPanes(t *testing.T) {
	recorder := perftrace.Enable()
	defer perftrace.Disable()

	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-2"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").State = "running"
	rt.Registry().Get("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 7, Rows: 4},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells:             [][]protocol.Cell{repeatCells("left")},
		},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AlternateScreen: true, MouseTracking: true},
	}
	rt.Registry().GetOrCreate("term-2").State = "running"
	rt.Registry().Get("term-2").Snapshot = &protocol.Snapshot{
		TerminalID: "term-2",
		Size:       protocol.Size{Cols: 7, Rows: 4},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells:             [][]protocol.Cell{repeatCells("right")},
		},
		Cursor: protocol.CursorState{Visible: false},
		Modes:  protocol.TerminalModes{AlternateScreen: true, MouseTracking: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 6), 20, 8)
	_ = renderBodyFrameWithCoordinatorVM(nil, RenderVMFromVisibleState(state), 20, 6)

	snapshot := recorder.Snapshot()
	if _, ok := snapshot.Event("render.body.alt_screen_fast_path"); ok {
		t.Fatalf("expected split panes to stay on the regular body path, got %#v", snapshot.Events)
	}
	if _, ok := snapshot.Event("render.body.canvas"); !ok {
		t.Fatalf("expected split panes to continue using body canvas, got %#v", snapshot.Events)
	}
}
