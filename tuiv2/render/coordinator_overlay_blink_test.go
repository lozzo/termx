package render

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/modal"
)

func TestCoordinatorKeepsHostCursorHiddenForPickerOverlay(t *testing.T) {
	state := makeTestState()
	state = WithOverlayPicker(state, &modal.PickerState{
		Items:     []modal.PickerItem{{TerminalID: "term-1", Name: "demo"}},
		Query:     "demo",
		Cursor:    2,
		CursorSet: true,
	})

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	_ = coordinator.RenderFrame()

	if got := coordinator.CursorSequence(); got != hideCursorANSI() {
		t.Fatalf("expected picker overlay to keep host cursor hidden, got %q", got)
	}
}

func TestCoordinatorBlinksPickerOverlayUnderscoreCursor(t *testing.T) {
	state := makeTestState()
	state = WithOverlayPicker(state, &modal.PickerState{
		Items:     []modal.PickerItem{{TerminalID: "term-1", Name: "demo"}},
		Query:     "demo",
		Cursor:    2,
		CursorSet: true,
	})

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	frameOn := xansi.Strip(coordinator.RenderFrame())
	if !strings.Contains(frameOn, "search: de_mo") {
		t.Fatalf("expected visible picker underscore cursor, got %q", frameOn)
	}

	coordinator.AdvanceCursorBlink()
	frameOff := xansi.Strip(coordinator.RenderFrame())
	if strings.Contains(frameOff, "search: de_mo") {
		t.Fatalf("expected picker underscore cursor to disappear during off phase, got %q", frameOff)
	}
	if !strings.Contains(frameOff, "search: de mo") {
		t.Fatalf("expected picker cursor off phase to keep layout stable, got %q", frameOff)
	}
}

func TestCoordinatorNeedsCursorTicksForPromptOverlay(t *testing.T) {
	state := makeTestState()
	state = AttachPrompt(state, &modal.PromptState{
		Kind:   "create-terminal-name",
		Value:  "alpha",
		Cursor: 3,
	})

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	if !coordinator.NeedsCursorTicks() {
		t.Fatal("expected prompt overlay to request cursor blink ticks")
	}
}

func TestCoordinatorBlinksTerminalPoolUnderscoreCursor(t *testing.T) {
	state := makeTestState()
	state = AttachTerminalPool(state, &modal.TerminalManagerState{
		Items:     []modal.PickerItem{{TerminalID: "term-1", Name: "demo", State: "running"}},
		Query:     "demo",
		Cursor:    1,
		CursorSet: true,
	})

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	frameOn := xansi.Strip(coordinator.RenderFrame())
	if !strings.Contains(frameOn, "search: d_emo") {
		t.Fatalf("expected visible terminal pool underscore cursor, got %q", frameOn)
	}
	if got := coordinator.CursorSequence(); got != hideCursorANSI() {
		t.Fatalf("expected terminal pool to keep host cursor hidden, got %q", got)
	}

	coordinator.AdvanceCursorBlink()
	frameOff := xansi.Strip(coordinator.RenderFrame())
	if strings.Contains(frameOff, "search: d_emo") {
		t.Fatalf("expected terminal pool underscore cursor to disappear during off phase, got %q", frameOff)
	}
	if !strings.Contains(frameOff, "search: d emo") {
		t.Fatalf("expected terminal pool cursor off phase to keep layout stable, got %q", frameOff)
	}
}

func TestCoordinatorRevealCursorBlinkShowsOverlayCursorImmediately(t *testing.T) {
	state := makeTestState()
	state = WithOverlayPicker(state, &modal.PickerState{
		Items:     []modal.PickerItem{{TerminalID: "term-1", Name: "demo"}},
		Query:     "demo",
		Cursor:    2,
		CursorSet: true,
	})

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	_ = coordinator.RenderFrame()
	coordinator.AdvanceCursorBlink()
	frameOff := xansi.Strip(coordinator.RenderFrame())
	if !strings.Contains(frameOff, "search: de mo") {
		t.Fatalf("expected off phase before reveal, got %q", frameOff)
	}

	coordinator.RevealCursorBlink()
	frameOn := xansi.Strip(coordinator.RenderFrame())
	if !strings.Contains(frameOn, "search: de_mo") {
		t.Fatalf("expected reveal to show overlay cursor immediately, got %q", frameOn)
	}
}
