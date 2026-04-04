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
	if !strings.Contains(lastLine, "ws:main") && !strings.Contains(lastLine, "W WORKSPACE") {
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
	for _, want := range []string{"base", "float"} {
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
		"[ Attach existing terminal ]",
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
	for _, want := range []string{"○42", "last output"} {
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

	for _, want := range []string{"Terminal Pool", "term-1", "term-2", "[Enter] here", "[Ctrl-E] edit", "TERMINAL-MANAGER", "Enter HERE", "Ctrl-T TAB"} {
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
	for _, want := range []string{"TERMINAL-MANAGER", "Enter HERE", "Ctrl-T TAB", "Ctrl-O FLOAT", "Esc BACK"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("expected terminal pool unified status hint %q:\n%s", want, frame)
		}
	}
}
