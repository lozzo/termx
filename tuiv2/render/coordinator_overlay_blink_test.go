package render

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/modal"
)

func TestCoordinatorProjectsHostCursorForPickerOverlay(t *testing.T) {
	vm := makeTestVM()
	picker := &modal.PickerState{
		Items:     []modal.PickerItem{{TerminalID: "term-1", Name: "demo"}},
		Query:     "demo",
		Cursor:    2,
		CursorSet: true,
	}
	vm = AttachRenderPicker(vm, picker)

	coordinator := NewCoordinatorWithVM(func() RenderVM { return vm })
	_ = coordinator.RenderFrame()

	x, y, ok := pickerOverlayCursorTarget(picker, TermSize{Width: vm.TermSize.Width, Height: FrameBodyHeight(vm.TermSize.Height)})
	if !ok {
		t.Fatal("expected picker overlay cursor target")
	}
	if got, want := coordinator.CursorSequence(), hostCursorANSI(x, y+TopChromeRows, "bar", false); got != want {
		t.Fatalf("expected picker overlay to project host cursor, got %q want %q", got, want)
	}
}

func TestCoordinatorBlinksPickerOverlayHostCursor(t *testing.T) {
	vm := makeTestVM()
	picker := &modal.PickerState{
		Items:     []modal.PickerItem{{TerminalID: "term-1", Name: "demo"}},
		Query:     "demo",
		Cursor:    2,
		CursorSet: true,
	}
	vm = AttachRenderPicker(vm, picker)

	coordinator := NewCoordinatorWithVM(func() RenderVM { return vm })
	frameOn := xansi.Strip(coordinator.RenderFrame())
	if !strings.Contains(frameOn, "search: demo") {
		t.Fatalf("expected visible picker query text without synthetic cursor marker, got %q", frameOn)
	}
	x, y, ok := pickerOverlayCursorTarget(picker, TermSize{Width: vm.TermSize.Width, Height: FrameBodyHeight(vm.TermSize.Height)})
	if !ok {
		t.Fatal("expected picker overlay cursor target")
	}
	if got, want := coordinator.CursorSequence(), hostCursorANSI(x, y+TopChromeRows, "bar", false); got != want {
		t.Fatalf("expected visible picker host cursor, got %q want %q", got, want)
	}

	coordinator.AdvanceCursorBlink()
	frameOff := xansi.Strip(coordinator.RenderFrame())
	if !strings.Contains(frameOff, "search: demo") {
		t.Fatalf("expected picker cursor off phase to keep layout stable, got %q", frameOff)
	}
	if got := coordinator.CursorSequence(); got != hideCursorANSI() {
		t.Fatalf("expected picker cursor off phase to hide host cursor, got %q", got)
	}
}

func TestCoordinatorNeedsCursorTicksForPromptOverlay(t *testing.T) {
	vm := makeTestVM()
	vm = AttachRenderPrompt(vm, &modal.PromptState{
		Kind:   "create-terminal-name",
		Value:  "alpha",
		Cursor: 3,
	})

	coordinator := NewCoordinatorWithVM(func() RenderVM { return vm })
	if !coordinator.NeedsCursorTicks() {
		t.Fatal("expected prompt overlay to request cursor blink ticks")
	}
}

func TestCoordinatorBlinksTerminalPoolHostCursor(t *testing.T) {
	vm := makeTestVM()
	manager := &modal.TerminalManagerState{
		Items:     []modal.PickerItem{{TerminalID: "term-1", Name: "demo", State: "running"}},
		Query:     "demo",
		Cursor:    1,
		CursorSet: true,
	}
	vm = AttachRenderTerminalPool(vm, manager)

	coordinator := NewCoordinatorWithVM(func() RenderVM { return vm })
	frameOn := xansi.Strip(coordinator.RenderFrame())
	if !strings.Contains(frameOn, "search: demo") {
		t.Fatalf("expected visible terminal pool query text without synthetic cursor marker, got %q", frameOn)
	}
	layout := buildTerminalPoolPageLayout(manager, vm.TermSize.Width, FrameBodyHeight(vm.TermSize.Height))
	want := hostCursorANSI(layout.queryRect.X+valueCursorCellOffset(manager.Query, queryCursorIndex(manager.Query, manager.Cursor, manager.CursorSet), layout.queryRect.W), layout.queryRect.Y+TopChromeRows, "bar", false)
	if got := coordinator.CursorSequence(); got != want {
		t.Fatalf("expected terminal pool to project host cursor, got %q want %q", got, want)
	}

	coordinator.AdvanceCursorBlink()
	frameOff := xansi.Strip(coordinator.RenderFrame())
	if !strings.Contains(frameOff, "search: demo") {
		t.Fatalf("expected terminal pool cursor off phase to keep layout stable, got %q", frameOff)
	}
	if got := coordinator.CursorSequence(); got != hideCursorANSI() {
		t.Fatalf("expected terminal pool cursor off phase to hide host cursor, got %q", got)
	}
}

func TestCoordinatorRevealCursorBlinkShowsOverlayCursorImmediately(t *testing.T) {
	vm := makeTestVM()
	vm = AttachRenderPicker(vm, &modal.PickerState{
		Items:     []modal.PickerItem{{TerminalID: "term-1", Name: "demo"}},
		Query:     "demo",
		Cursor:    2,
		CursorSet: true,
	})

	coordinator := NewCoordinatorWithVM(func() RenderVM { return vm })
	_ = coordinator.RenderFrame()
	coordinator.AdvanceCursorBlink()
	frameOff := xansi.Strip(coordinator.RenderFrame())
	if !strings.Contains(frameOff, "search: demo") {
		t.Fatalf("expected off phase before reveal, got %q", frameOff)
	}
	if got := coordinator.CursorSequence(); got != hideCursorANSI() {
		t.Fatalf("expected reveal precondition to hide host cursor, got %q", got)
	}

	coordinator.RevealCursorBlink()
	_ = coordinator.RenderFrame()
	x, y, ok := pickerOverlayCursorTarget(vm.Overlay.Picker, TermSize{Width: vm.TermSize.Width, Height: FrameBodyHeight(vm.TermSize.Height)})
	if !ok {
		t.Fatal("expected picker overlay cursor target")
	}
	if got, want := coordinator.CursorSequence(), hostCursorANSI(x, y+TopChromeRows, "bar", false); got != want {
		t.Fatalf("expected reveal to show overlay host cursor immediately, got %q want %q", got, want)
	}
}
