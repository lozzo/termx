package render

import (
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestPaneEntriesForTabPreferRuntimePaneViewport(t *testing.T) {
	tab := workbench.VisibleTab{
		ID:           "tab-1",
		ActivePaneID: "pane-1",
		ScrollOffset: 5,
		Panes: []workbench.VisiblePane{{
			ID:         "pane-1",
			TerminalID: "term-1",
			Rect:       workbench.Rect{X: 0, Y: 0, W: 20, H: 8},
		}},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Bindings: []runtime.VisiblePaneBinding{{
			PaneID:         "pane-1",
			ViewportOffset: 2,
		}},
		Terminals: []runtime.VisibleTerminal{{
			TerminalID: "term-1",
			State:      "running",
		}},
	}

	entries := paneEntriesForTab(tab, nil, 20, 8, newRuntimeLookup(runtimeState), bodyProjectionOptions{}, defaultUITheme())
	if len(entries) != 1 {
		t.Fatalf("expected one pane render entry, got %#v", entries)
	}
	if got := entries[0].ScrollOffset; got != 2 {
		t.Fatalf("expected render entry to prefer runtime pane viewport 2, got %d", got)
	}
	if got := entries[0].ContentKey.ScrollOffset; got != 2 {
		t.Fatalf("expected content key to follow runtime pane viewport 2, got %d", got)
	}
}

func TestPaneEntriesForTabMarksAlternateScreenEntriesConservativeOnlyInMultiPaneLayouts(t *testing.T) {
	tab := workbench.VisibleTab{
		ID:           "tab-1",
		ActivePaneID: "pane-1",
		Panes: []workbench.VisiblePane{
			{
				ID:         "pane-1",
				TerminalID: "term-1",
				Rect:       workbench.Rect{X: 0, Y: 0, W: 10, H: 8},
			},
			{
				ID:         "pane-2",
				TerminalID: "term-2",
				Rect:       workbench.Rect{X: 10, Y: 0, W: 10, H: 8},
			},
		},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtime.VisibleTerminal{
			{
				TerminalID: "term-1",
				State:      "running",
				Snapshot: &protocol.Snapshot{
					TerminalID: "term-1",
					Size:       protocol.Size{Cols: 8, Rows: 4},
					Screen: protocol.ScreenData{
						IsAlternateScreen: true,
						Cells:             [][]protocol.Cell{repeatCells("left")},
					},
					Modes: protocol.TerminalModes{AlternateScreen: true},
				},
			},
			{
				TerminalID: "term-2",
				State:      "running",
				Snapshot: &protocol.Snapshot{
					TerminalID: "term-2",
					Size:       protocol.Size{Cols: 8, Rows: 4},
					Screen: protocol.ScreenData{
						Cells: [][]protocol.Cell{repeatCells("right")},
					},
					Modes: protocol.TerminalModes{AutoWrap: true},
				},
			},
		},
	}

	entries := paneEntriesForTab(tab, nil, 20, 8, newRuntimeLookup(runtimeState), bodyProjectionOptions{}, defaultUITheme())
	if len(entries) != 2 {
		t.Fatalf("expected two pane render entries, got %#v", entries)
	}
	if !entries[0].ConservativeRedraw {
		t.Fatalf("expected alternate-screen pane to force conservative redraw in multi-pane layout, got %#v", entries[0])
	}
	if entries[1].ConservativeRedraw {
		t.Fatalf("expected non-alt-screen pane to keep normal redraw policy, got %#v", entries[1])
	}

	singlePaneTab := tab
	singlePaneTab.Panes = append([]workbench.VisiblePane(nil), tab.Panes[:1]...)
	entries = paneEntriesForTab(singlePaneTab, nil, 20, 8, newRuntimeLookup(runtimeState), bodyProjectionOptions{}, defaultUITheme())
	if len(entries) != 1 {
		t.Fatalf("expected single pane render entry, got %#v", entries)
	}
	if entries[0].ConservativeRedraw {
		t.Fatalf("expected single alternate-screen pane to keep incremental redraw paths, got %#v", entries[0])
	}
}
