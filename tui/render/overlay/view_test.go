package overlay

import (
	"strings"
	"testing"

	featureoverlay "github.com/lozzow/termx/tui/features/overlay"
)

func TestOverlayViewShowsLabels(t *testing.T) {
	cases := map[featureoverlay.Kind]string{
		featureoverlay.KindConnectPicker: "connect picker",
		featureoverlay.KindHelp:          "help",
		featureoverlay.KindPrompt:        "prompt",
	}
	for kind, want := range cases {
		view := Render(kind)
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q label, got %q", want, view)
		}
	}
}
