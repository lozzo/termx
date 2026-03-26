package overlay

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/tui/core/types"
	"github.com/lozzow/termx/tui/render/projection"
)

func TestOverlayViewShowsLabels(t *testing.T) {
	connectView := Render(projection.Screen{Overlay: projection.Overlay{Kind: "connect-picker", Selected: types.TerminalID("term-1"), Items: []projection.PoolItem{{ID: types.TerminalID("term-1"), Name: "shell-1", State: "running"}, {ID: types.TerminalID("term-2"), Name: "shell-2", State: "running"}}}})
	for _, want := range []string{"connect picker", "j/k move", "enter connect", "> shell-1 [running]"} {
		if !strings.Contains(connectView, want) {
			t.Fatalf("expected %q label, got %q", want, connectView)
		}
	}

	for _, tc := range []struct {
		screen projection.Screen
		want   string
	}{
		{screen: projection.Screen{Overlay: projection.Overlay{Kind: "help"}}, want: "help"},
		{screen: projection.Screen{Overlay: projection.Overlay{Kind: "prompt"}}, want: "prompt"},
	} {
		view := Render(tc.screen)
		if !strings.Contains(view, tc.want) {
			t.Fatalf("expected %q label, got %q", tc.want, view)
		}
	}
}
