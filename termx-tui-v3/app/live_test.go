package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestLiveAppAttachRenderInputAndResize(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{
			TerminalID: "term-1",
			Channel:    9,
			Cols:       80,
			Rows:       24,
			CanResize:  true,
		},
	}
	host := NewFakeTerminalHost(8)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Cols:       80,
		Rows:       24,
		Lines:      []string{"$ echo hi", "hi"},
	}}); err != nil {
		t.Fatalf("post surface: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x"}); err != nil {
		t.Fatalf("send input: %v", err)
	}
	if err := runtime.Post(LiveResizeMsg{Cols: 100, Rows: 40}); err != nil {
		t.Fatalf("post resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if len(terminal.Attaches) != 1 || terminal.Attaches[0].TerminalID != "term-1" {
		t.Fatalf("unexpected attach requests %#v", terminal.Attaches)
	}
	if len(terminal.Inputs) != 1 || string(terminal.Inputs[0].Bytes) != "x" || terminal.Inputs[0].Channel != 9 {
		t.Fatalf("unexpected input requests %#v", terminal.Inputs)
	}
	if len(terminal.Resizes) != 1 || terminal.Resizes[0].Cols != 100 || terminal.Resizes[0].Rows != 40 || terminal.Resizes[0].Channel != 9 {
		t.Fatalf("unexpected resize requests %#v", terminal.Resizes)
	}
	if runtime.State().Session.Cols != 100 || runtime.State().Surface.Cols != 100 {
		t.Fatalf("resize was not reflected in state %#v", runtime.State())
	}
	last := lastFrame(t, host.Frames())
	if len(last.Lines) == 0 || last.Lines[0] != "$ echo hi" {
		t.Fatalf("expected live surface frame, got %#v", last.Lines)
	}
}

func TestLiveAppShowsTerminalServiceError(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachErr: errors.New("attach failed"),
	}
	host := NewFakeTerminalHost(4)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if runtime.State().Session.LastError != "attach failed" {
		t.Fatalf("expected attach error in state, got %#v", runtime.State())
	}
	frames := host.Frames()
	last := lastFrame(t, frames)
	if len(last.Lines) < 2 || !strings.Contains(last.Lines[len(last.Lines)-1], "attach failed") {
		t.Fatalf("expected rendered error status, got %#v", last.Lines)
	}
}

func TestLiveRuntimeIncludesShellReducer(t *testing.T) {
	host := NewFakeTerminalHost(4)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: &services.FakeTerminalService{}},
	)

	if err := runtime.Post(ShellSetHeaderVisibleMsg{Visible: false}); err != nil {
		t.Fatalf("post shell action: %v", err)
	}
	if err := runtime.Post(ShellSetPanelPresentationMsg{Presentation: state.PanelPresentationSplitLine}); err != nil {
		t.Fatalf("post panel action: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if runtime.State().Shell.HeaderVisible {
		t.Fatalf("expected hidden header, got %#v", runtime.State().Shell)
	}
	if runtime.State().Shell.PanelPresentation != state.PanelPresentationSplitLine {
		t.Fatalf("expected split line presentation, got %#v", runtime.State().Shell)
	}
}

func TestInteractiveRuntimeIncludesShellReducer(t *testing.T) {
	host := NewFakeTerminalHost(4)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: &services.FakeTerminalService{}},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	if err := runtime.Post(ShellOpenTerminalPickerMsg{}); err != nil {
		t.Fatalf("post terminal picker action: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if !runtime.State().Shell.Overlay.Open || runtime.State().Shell.Overlay.Kind != state.OverlayTerminalPicker {
		t.Fatalf("expected terminal picker overlay, got %#v", runtime.State().Shell.Overlay)
	}
}

func lastFrame(t *testing.T, frames []render.Frame) render.Frame {
	t.Helper()
	if len(frames) == 0 {
		t.Fatal("expected rendered frames")
	}
	return frames[len(frames)-1]
}
