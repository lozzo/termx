package terminalpool

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/tui/core/types"
	"github.com/lozzow/termx/tui/render/projection"
)

func TestTerminalPoolViewShowsVisibleParkedExitedGroups(t *testing.T) {
	view := Render(samplePoolProjection(), 120, 40)
	for _, want := range []string{"visible (1)", "parked (1)", "exited (1)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q", want)
		}
	}
	if !strings.Contains(view, "visible-shell") {
		t.Fatalf("expected selected terminal name, got %q", view)
	}
}

func TestTerminalPoolViewShowsThreeColumnWireframe(t *testing.T) {
	view := Render(samplePoolProjection(), 120, 40)
	for _, want := range []string{"three-column wireframe", "terminals", "preview", "details", "snapshot preview pending", "terminal id: term-visible"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected pool detail %q, got %q", want, view)
		}
	}
	if !strings.Contains(view, "> visible-shell [running]") {
		t.Fatalf("expected selected marker, got %q", view)
	}
}

func TestTerminalPoolViewShowsActionHints(t *testing.T) {
	view := Render(samplePoolProjection(), 120, 40)
	for _, want := range []string{"j/k move", "x kill", "X remove", "? help"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected action hint %q, got %q", want, view)
		}
	}
}

func samplePoolProjection() projection.Screen {
	return projection.Screen{
		Pool: projection.TerminalPool{
			SelectedTerminalID: types.TerminalID("term-visible"),
			Visible:            []projection.PoolItem{{ID: types.TerminalID("term-visible"), Name: "visible-shell", State: "running"}},
			Parked:             []projection.PoolItem{{ID: types.TerminalID("term-parked"), Name: "parked-shell", State: "running"}},
			Exited:             []projection.PoolItem{{ID: types.TerminalID("term-exited"), Name: "exited-shell", State: "exited"}},
		},
	}
}
