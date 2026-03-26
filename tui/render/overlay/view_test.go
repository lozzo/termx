package overlay

import (
	"strings"
	"testing"

	featureoverlay "github.com/lozzow/termx/tui/features/overlay"
)

func TestOverlayViewShowsConnectPickerLabel(t *testing.T) {
	view := Render(featureoverlay.KindConnectPicker)
	if !strings.Contains(view, "connect picker") {
		t.Fatalf("expected connect picker label, got %q", view)
	}
}
