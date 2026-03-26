package workbench

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/tui/render/projection"
)

func TestWorkbenchViewShowsLiveExitedAndUnconnectedPaneState(t *testing.T) {
	view := Render(sampleProjectionWithThreePaneStates(), 120, 40)
	for _, want := range []string{"live", "exited", "unconnected"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q", want)
		}
	}
}

func TestUnconnectedPaneShowsActions(t *testing.T) {
	view := Render(sampleUnconnectedProjection(), 80, 24)
	for _, want := range []string{"connect existing", "create terminal", "open pool"} {
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
