package workbench

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/tui/render/projection"
)

func TestWorkbenchViewShowsLiveExitedAndUnconnectedPaneState(t *testing.T) {
	view := Render(sampleProjectionWithThreePaneStates(), 120, 40)
	for _, want := range []string{"live / connected", "exited / scrollback", "unconnected / waiting"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q", want)
		}
	}
}

func TestWorkbenchViewShowsPaneBodyLines(t *testing.T) {
	view := Render(sampleProjectionWithThreePaneStates(), 120, 40)
	for _, want := range []string{"hello from shell", "process done"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected body line %q, got %q", want, view)
		}
	}
}

func TestWorkbenchViewShowsWireframeHeaderAndFooter(t *testing.T) {
	view := Render(sampleProjectionWithThreePaneStates(), 120, 40)
	for _, want := range []string{"termx [main] workbench", "layout: 3 pane(s)", "ACTIVE shell-live", "c connect  p pool"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view detail %q, got %q", want, view)
		}
	}
	for _, border := range []string{"┌", "┐", "└", "┘", "│", "─"} {
		if !strings.Contains(view, border) {
			t.Fatalf("expected wireframe border %q, got %q", border, view)
		}
	}
}

func TestUnconnectedPaneShowsActions(t *testing.T) {
	view := Render(sampleUnconnectedProjection(), 80, 24)
	for _, want := range []string{"connect existing", "create terminal", "open pool", "actions: connect / create / pool"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q", want)
		}
	}
}

func sampleProjectionWithThreePaneStates() projection.Screen {
	screen := projection.Screen{
		WorkspaceName: "main",
		Panes: []projection.Pane{
			{Title: "shell-live", Status: "live", Body: "hello from shell"},
			{Title: "shell-exited", Status: "exited", Body: "process done"},
			{Title: "unconnected", Status: "unconnected", Body: "connect existing\ncreate terminal\nopen pool"},
		},
	}
	return screen
}

func sampleUnconnectedProjection() projection.Screen {
	return projection.Screen{
		WorkspaceName: "main",
		Panes: []projection.Pane{
			{Title: "unconnected", Status: "unconnected", Body: "connect existing\ncreate terminal\nopen pool"},
		},
	}
}
