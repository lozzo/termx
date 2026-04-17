package render

import (
	"testing"

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
