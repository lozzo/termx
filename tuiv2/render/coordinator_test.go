package render

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func makeTestState() VisibleRenderState {
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
	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Name = "demo"
	rt.Registry().Get("term-1").State = "running"
	return WithTermSize(AdaptVisibleStateWithSize(wb, rt, 100, 28), 100, 30)
}

func TestRenderFrameNonEmpty(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := c.RenderFrame()
	if frame == "" {
		t.Fatal("RenderFrame() returned empty string")
	}
}

func TestRenderFrameContainsWorkspaceName(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := xansi.Strip(c.RenderFrame())
	if !strings.Contains(frame, "main") {
		t.Fatalf("frame missing workspace name:\n%s", frame)
	}
}

func TestRenderFrameContainsTabInfo(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := xansi.Strip(c.RenderFrame())
	if !strings.Contains(frame, "tab 1") {
		t.Fatalf("frame missing tab info:\n%s", frame)
	}
}

func TestRenderFrameContainsPaneBorder(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := xansi.Strip(c.RenderFrame())
	// Pane border should prefer runtime metadata name over pane title.
	if !strings.Contains(frame, "demo") {
		t.Fatalf("frame missing pane title 'demo':\n%s", frame)
	}
	// Should have box drawing characters
	if !strings.Contains(frame, "┌") || !strings.Contains(frame, "┘") {
		t.Fatalf("frame missing pane border box characters:\n%s", frame)
	}
}

func TestDrawPaneFrameUsesTieredChromeStylesForActivePane(t *testing.T) {
	canvas := newComposedCanvas(40, 6)
	rect := workbench.Rect{X: 0, Y: 0, W: 40, H: 6}
	theme := uiThemeFromHostColors("#0b1020", "#dbeafe", nil)
	border := paneBorderInfo{StateLabel: "●", ShareLabel: "⇄2", RoleLabel: "◆ owner"}

	drawPaneFrame(canvas, rect, "demo", border, theme, paneOverflowHints{}, true, false)
	layout, ok := paneTopBorderLabelsLayout(rect, "demo", border, paneChromeActionTokensForFrame(rect, "demo", border, false))
	if !ok {
		t.Fatal("expected pane chrome layout")
	}
	if len(layout.actionSlots) == 0 {
		t.Fatal("expected action slots in pane chrome")
	}

	titleFG := canvas.cells[rect.Y][layout.titleX].Style.FG
	metaFG := canvas.cells[rect.Y][layout.stateX].Style.FG
	actionFG := canvas.cells[rect.Y][layout.actionSlots[0].X].Style.FG

	if titleFG == "" || metaFG == "" || actionFG == "" {
		t.Fatalf("expected pane chrome styles to set explicit colors, got title=%q meta=%q action=%q", titleFG, metaFG, actionFG)
	}
	if titleFG == metaFG {
		t.Fatalf("expected active pane title to differ from meta, both %q", titleFG)
	}
	if actionFG == metaFG {
		t.Fatalf("expected action slots to differ from meta, both %q", actionFG)
	}
}

func TestDrawPaneFrameKeepsTopRightCornerAlignedWithWideBorderLabels(t *testing.T) {
	canvas := newComposedCanvas(40, 6)
	rect := workbench.Rect{X: 0, Y: 0, W: 40, H: 6}
	theme := uiThemeFromHostColors("#0b1020", "#dbeafe", nil)
	border := paneBorderInfo{StateLabel: paneRunningIcon(), RoleLabel: "◆ owner"}

	drawPaneFrame(canvas, rect, "demo界", border, theme, paneOverflowHints{}, true, false)

	lines := strings.Split(xansi.Strip(canvas.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least two rendered lines, got %d", len(lines))
	}
	if got := xansi.StringWidth(lines[0]); got != rect.W {
		t.Fatalf("expected top border visual width %d, got %d: %q", rect.W, got, lines[0])
	}
	if got := xansi.StringWidth(lines[1]); got != rect.W {
		t.Fatalf("expected second row visual width %d, got %d: %q", rect.W, got, lines[1])
	}
	if !strings.HasSuffix(lines[0], "┐") {
		t.Fatalf("expected top row to end at the right corner, got %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "│") {
		t.Fatalf("expected second row to end at the right border, got %q", lines[1])
	}
}

func TestEmptyPaneActionStylesSeparatePrimarySecondaryAndDanger(t *testing.T) {
	theme := uiThemeFromHostColors("#0b1020", "#dbeafe", nil)

	attach := emptyPaneActionDrawStyle(theme, HitRegionEmptyPaneAttach, false)
	create := emptyPaneActionDrawStyle(theme, HitRegionEmptyPaneCreate, false)
	manager := emptyPaneActionDrawStyle(theme, HitRegionEmptyPaneManager, false)
	close := emptyPaneActionDrawStyle(theme, HitRegionEmptyPaneClose, false)

	if attach.FG == "" || create.FG == "" || manager.FG == "" || close.FG == "" {
		t.Fatalf("expected empty pane action styles to define colors: %#v %#v %#v %#v", attach, create, manager, close)
	}
	if attach.FG == manager.FG {
		t.Fatalf("expected attach and manager to use different emphasis, both %q", attach.FG)
	}
	if close.FG == attach.FG {
		t.Fatalf("expected close to use danger emphasis, both %q", close.FG)
	}
	if !attach.Bold || !create.Bold || close.Bold == false {
		t.Fatalf("expected primary and danger actions to stay bold: attach=%#v create=%#v close=%#v", attach, create, close)
	}
	if manager.Bold {
		t.Fatalf("expected manager action to be secondary emphasis, got %#v", manager)
	}
}

func TestRenderFrameNilCoordinator(t *testing.T) {
	var c *Coordinator
	if got := c.RenderFrame(); got != "" {
		t.Fatalf("nil coordinator must return empty string, got %q", got)
	}
}

func TestRenderFrameNoState(t *testing.T) {
	c := NewCoordinator(func() VisibleRenderState { return VisibleRenderState{} })
	frame := xansi.Strip(c.RenderFrame())
	if !strings.Contains(frame, "tuiv2") {
		t.Fatalf("empty state frame should contain fallback 'tuiv2', got %q", frame)
	}
}

func TestRenderFrameHasTabBarAndStatusBar(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := c.RenderFrame()
	lines := strings.Split(frame, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (tab bar + body + status bar), got %d", len(lines))
	}
	if !strings.Contains(lines[0], "main") {
		t.Fatalf("first line should be tab bar with workspace, got %q", lines[0])
	}
	// Last line should be status bar
	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, "[Ctrl]") && !strings.Contains(lastLine, "[P] PANE") {
		t.Fatalf("last line should be status bar, got %q", lastLine)
	}
}

func TestRenderBodyZoomedPaneOccupiesWholeBody(t *testing.T) {
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
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 100, 28), 100, 30)

	body := renderBody(state, 100, 28)
	if !strings.Contains(body, "left") {
		t.Fatalf("expected zoomed pane title in body:\n%s", body)
	}
	if strings.Contains(body, "right") {
		t.Fatalf("expected non-zoomed pane to be hidden:\n%s", body)
	}
	if strings.Count(body, "┌") != 1 {
		t.Fatalf("expected exactly one pane frame when zoomed:\n%s", body)
	}
}

func TestRenderBodyScrollbackOffsetShowsOlderRows(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			ScrollOffset: 1,
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 40, 8), 40, 10)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID: "term-1",
		Snapshot: &protocol.Snapshot{
			Scrollback: [][]protocol.Cell{{{Content: "A", Width: 1}}},
			Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "B", Width: 1}}, {{Content: "C", Width: 1}}}},
		},
	}}}

	body := renderBody(state, 40, 8)
	if !strings.Contains(body, "A") {
		t.Fatalf("expected scrollback row to be visible when offset > 0:\n%s", body)
	}
}

func TestRenderBodyDrawsFloatingPanesOnTop(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "base", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "float", TerminalID: "term-2"},
			},
			Root:     workbench.NewLeaf("pane-1"),
			Floating: []*workbench.FloatingState{{PaneID: "pane-2", Rect: workbench.Rect{X: 10, Y: 4, W: 24, H: 6}, Z: 1}},
		}},
	})
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 100, 28), 100, 30)

	body := renderBody(state, 100, 28)
	for _, want := range []string{"base", "flo"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q:\n%s", want, body)
		}
	}
}

func TestRenderBodyFloatingPaneClearsUnderlyingContent(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-2",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "base", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "float", TerminalID: "term-2"},
			},
			Root:     workbench.NewLeaf("pane-1"),
			Floating: []*workbench.FloatingState{{PaneID: "pane-2", Rect: workbench.Rect{X: 8, Y: 3, W: 20, H: 6}, Z: 1}},
		}},
	})
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 40, 14), 40, 16)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{
		{
			TerminalID: "term-1",
			Snapshot: &protocol.Snapshot{
				Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
					repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
					repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
					repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
					repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
					repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
					repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
				}},
			},
		},
		{
			TerminalID: "term-2",
			Name:       "float",
			State:      "running",
		},
	}}

	body := xansi.Strip(renderBody(state, 40, 14))
	lines := strings.Split(body, "\n")
	if len(lines) <= 5 {
		t.Fatalf("expected rendered body height, got %d lines:\n%s", len(lines), body)
	}
	line := []rune(lines[5])
	if len(line) <= 12 {
		t.Fatalf("expected rendered body width, got line %q", lines[5])
	}
	if got := string(line[12]); got != " " {
		t.Fatalf("expected floating interior to clear underlying content, got %q in line %q", got, lines[5])
	}
}

func TestRenderBodyCachedOverlapDoesNotPaintActivePaneOverFloating(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:              "tab-1",
			Name:            "tab 1",
			ActivePaneID:    "pane-1",
			FloatingVisible: true,
			Panes: map[string]*workbench.PaneState{
				"pane-1":  {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
				"float-1": {ID: "float-1", Title: "float", TerminalID: "term-2"},
			},
			Root: workbench.NewLeaf("pane-1"),
			Floating: []*workbench.FloatingState{{
				PaneID: "float-1",
				Rect:   workbench.Rect{X: 10, Y: 4, W: 14, H: 6},
				Z:      0,
			}},
		}},
	})

	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
			repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
			repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
			repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
			repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
			repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
		}},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 40, 14), 40, 16)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{
		{TerminalID: "term-1", Snapshot: snapshot},
		{TerminalID: "term-2", Name: "float", State: "running"},
	}}

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	body := xansi.Strip(renderBodyFrameWithCoordinator(coordinator, state, 40, 14).content)
	lines := strings.Split(body, "\n")
	if got := string([]rune(lines[6])[12]); got != " " {
		t.Fatalf("expected floating interior blank on first render, got %q in %q", got, lines[6])
	}

	snapshot.Screen.Cells[0][0].Content = "Z"

	body = xansi.Strip(renderBodyFrameWithCoordinator(coordinator, state, 40, 14).content)
	lines = strings.Split(body, "\n")
	if got := string([]rune(lines[6])[12]); got != " " {
		t.Fatalf("expected cached overlap render to preserve floating interior, got %q in %q", got, lines[6])
	}
}

func repeatCells(text string) []protocol.Cell {
	cells := make([]protocol.Cell, 0, len(text))
	for _, ch := range text {
		cells = append(cells, protocol.Cell{Content: string(ch), Width: 1})
	}
	return cells
}

func TestRenderBodyShowsActionableEmptyStateForUnboundPane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	body := xansi.Strip(renderBody(WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 72, 12), 72, 14), 72, 12))
	for _, want := range []string{
		"unconnected",
		"No terminal attached",
		"Attach existing terminal",
		"[ Create new terminal ]",
		"[ Open terminal manager ]",
		"[ Close pane ]",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected actionable empty-state hint %q:\n%s", want, body)
		}
	}
}

func TestRenderBodyShowsRecoveryStateForEmptyWorkspace(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: -1,
	})

	body := xansi.Strip(renderBody(WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 72, 12), 72, 14), 72, 12))
	for _, want := range []string{
		"No tabs in this workspace",
		"Ctrl-F open terminal picker",
		"Ctrl-T then c create a new tab",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected empty-workspace recovery hint %q:\n%s", want, body)
		}
	}
}

func TestRenderBodyShowsRecoveryStateForEmptyTab(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:    "tab-1",
			Name:  "tab 1",
			Panes: map[string]*workbench.PaneState{},
		}},
	})

	body := xansi.Strip(renderBody(WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 72, 12), 72, 14), 72, 12))
	for _, want := range []string{
		"No panes in this tab",
		"Ctrl-F create the first pane via terminal picker",
		"Ctrl-T then c create a fresh tab",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected empty-tab recovery hint %q:\n%s", want, body)
		}
	}
}

func TestRenderBodyShowsExitedPaneMetaAndPreservesSnapshot(t *testing.T) {
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

	exitCode := 42
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 72, 12), 72, 14)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID: "term-1",
		Name:       "shell",
		State:      "exited",
		ExitCode:   &exitCode,
		Snapshot: &protocol.Snapshot{
			Screen: protocol.ScreenData{
				Cells: [][]protocol.Cell{
					{{Content: "l", Width: 1}, {Content: "a", Width: 1}, {Content: "s", Width: 1}, {Content: "t", Width: 1}, {Content: " ", Width: 1}, {Content: "o", Width: 1}, {Content: "u", Width: 1}, {Content: "t", Width: 1}, {Content: "p", Width: 1}, {Content: "u", Width: 1}, {Content: "t", Width: 1}},
				},
			},
		},
	}}}

	body := xansi.Strip(renderBody(state, 72, 12))
	for _, want := range []string{paneExitedIcon() + "42", "last output", "R restart current terminal", "Ctrl-F choose another terminal"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected exited pane rendering to contain %q:\n%s", want, body)
		}
	}
}

func TestRenderBodyShowsPaneMetaForSharedOwner(t *testing.T) {
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

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 72, 12), 72, 14)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID:   "term-1",
		Name:         "shell",
		State:        "running",
		OwnerPaneID:  "pane-1",
		BoundPaneIDs: []string{"pane-1", "pane-2"},
	}}, Bindings: []runtime.VisiblePaneBinding{{
		PaneID:    "pane-1",
		Role:      "owner",
		Connected: true,
	}}}

	body := xansi.Strip(renderBody(state, 72, 12))
	if !strings.Contains(body, "◆ owner") || !strings.Contains(body, "⇄2") {
		t.Fatalf("expected shared owner pane meta in frame:\n%s", body)
	}
}

func TestRenderBodyShowsPaneMetaForSharedFollower(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-2",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "owner", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "follower", TerminalID: "term-1"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 72, 12), 72, 14)
	state.Runtime = &VisibleRuntimeStateProxy{
		Terminals: []runtime.VisibleTerminal{{
			TerminalID:   "term-1",
			Name:         "shell",
			State:        "running",
			OwnerPaneID:  "pane-1",
			BoundPaneIDs: []string{"pane-1", "pane-2"},
		}},
		Bindings: []runtime.VisiblePaneBinding{
			{PaneID: "pane-1", Role: "owner", Connected: true},
			{PaneID: "pane-2", Role: "follower", Connected: true},
		},
	}

	body := xansi.Strip(renderBody(state, 72, 12))
	if !strings.Contains(body, "follow") {
		t.Fatalf("expected shared follower pane meta in frame:\n%s", body)
	}
}

func TestRenderBodyPrefersTitleOverMetaInNarrowPane(t *testing.T) {
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

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 16, 8), 16, 10)
	state.Runtime = &VisibleRuntimeStateProxy{
		Terminals: []runtime.VisibleTerminal{{
			TerminalID:   "term-1",
			Name:         "shell",
			State:        "running",
			OwnerPaneID:  "pane-1",
			BoundPaneIDs: []string{"pane-1", "pane-2"},
		}},
		Bindings: []runtime.VisiblePaneBinding{
			{PaneID: "pane-1", Role: "owner", Connected: true},
		},
	}

	body := xansi.Strip(renderBody(state, 16, 8))
	if !strings.Contains(body, "shell") {
		t.Fatalf("expected pane title to survive in narrow pane:\n%s", body)
	}
	if strings.Contains(body, "owner") {
		t.Fatalf("expected compact meta to be dropped before title in narrow pane:\n%s", body)
	}
}

func TestRenderFrameUsesDedicatedTerminalPoolPageLayout(t *testing.T) {
	state := makeTestState()
	state = AttachTerminalPool(state, &modal.TerminalManagerState{
		Title:    "Terminal Pool",
		Footer:   "[Enter] here  [Ctrl-T] tab  [Ctrl-O] float  [Ctrl-E] edit  [Ctrl-K] kill  [Esc] close",
		Selected: 0,
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell", State: "visible", Description: "running · 1 pane bound"},
			{TerminalID: "term-2", Name: "logs", State: "parked", Description: "running · 0 panes bound"},
		},
	})
	state = WithStatus(state, "", "", string(input.ModeTerminalManager))
	frame := xansi.Strip(NewCoordinator(func() VisibleRenderState { return state }).RenderFrame())

	for _, want := range []string{"Terminal Pool", "term-1", "term-2", "[Enter] here", "[Ctrl-E] edit", "TERMINAL-MANAGER", "[Enter] HERE", "[Ctrl-T] TAB"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("expected terminal pool page to contain %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "demo") {
		t.Fatalf("expected workbench pane body to be replaced by terminal pool page:\n%s", frame)
	}
}

func TestRenderFrameTerminalPoolPageUsesUnifiedStatusBarWhenDetailsOverflow(t *testing.T) {
	state := makeTestState()
	state = AttachTerminalPool(state, &modal.TerminalManagerState{
		Title:    "Terminal Pool",
		Footer:   "[Enter] dedicated footer",
		Selected: 0,
		Items: []modal.PickerItem{
			{
				TerminalID:  "term-1",
				Name:        "shell",
				State:       "visible",
				Command:     "bash -lc 'run-long-command'",
				Location:    "main/tab 1/pane-1",
				Description: "running · 1 pane bound",
				Observed:    true,
			},
			{
				TerminalID:  "term-2",
				Name:        "logs",
				State:       "parked",
				Description: "running · 0 panes bound",
			},
		},
	})
	state = WithTermSize(state, 180, 10)
	state = WithStatus(state, "", "", "terminal-manager")

	frame := xansi.Strip(NewCoordinator(func() VisibleRenderState { return state }).RenderFrame())
	if strings.Contains(frame, "[Enter] dedicated footer") {
		t.Fatalf("expected terminal pool page footer to be removed from body:\n%s", frame)
	}
	for _, want := range []string{"TERMINAL-MANAGER", "[Enter] HERE", "[Ctrl-T] TAB", "[Ctrl-O] FLOAT", "[Esc] BACK"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("expected terminal pool unified status hint %q:\n%s", want, frame)
		}
	}
}
